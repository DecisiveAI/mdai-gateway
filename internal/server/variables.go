package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/mydecisive/mdai-data-core/eventing"
	datacorevariables "github.com/mydecisive/mdai-data-core/variables"
	"github.com/mydecisive/mdai-gateway/internal/adapter"
	"github.com/mydecisive/mdai-gateway/internal/httputil"
	"github.com/mydecisive/mdai-gateway/internal/nats"
	"github.com/mydecisive/mdai-gateway/internal/stringutil"
	"github.com/mydecisive/mdai-gateway/internal/valkey"
	"github.com/mydecisive/mdai-gateway/internal/variables"
	"go.uber.org/zap"
)

const (
	getHubValuesEndpoint      = "GET /variables/values/hub/{hubName}"
	getSingleVariableEndpoint = "GET /variables/values/hub/{hubName}/var/{varName}"
)

func handleListAllVariables(_ context.Context, deps HandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hubsVariables, err := deps.ConfigMapController.GetAllHubsVariablesSchemaConfigMapData()
		if err != nil {
			httputil.WriteJSONResponse(w, deps.Logger, http.StatusInternalServerError, "failed to fetch variables")
			return
		}
		if len(hubsVariables) == 0 {
			httputil.WriteJSONResponse(w, deps.Logger, http.StatusNotFound, "no hubs with variables found")
			return
		}

		schemas, err := variables.DecodeByHub(hubsVariables)
		if err != nil {
			writeVariablesError(w, deps.Logger, variables.ErrDecodeFailed)
			return
		}

		httputil.WriteJSONResponse(w, deps.Logger, http.StatusOK, schemas)
	}
}

func handleListHubVariables(_ context.Context, deps HandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hubName := r.PathValue("hubName")
		if hubName == "" {
			http.Error(w, "hub name required", http.StatusBadRequest)
			return
		}
		variablesMap, err := fetchHubVariablesSchema(hubName, deps)
		if err != nil {
			writeVariablesError(w, deps.Logger, err)
			return
		}

		schemas, err := variables.DecodeHub(variablesMap)
		if err != nil {
			writeVariablesError(w, deps.Logger, variables.ErrDecodeFailed)
			return
		}

		httputil.WriteJSONResponse(w, deps.Logger, http.StatusOK, schemas)
	}
}

func handleGetVariables(ctx context.Context, deps HandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hubName := r.PathValue("hubName")
		varName := r.PathValue("varName")
		if hubName == "" || varName == "" {
			http.Error(w, "hub and var name required", http.StatusBadRequest)
			return
		}
		logger := deps.Logger.With(
			zap.String("endpoint", getSingleVariableEndpoint),
			zap.String("hubName", hubName),
			zap.String("variableName", varName),
		)

		variablesMap, err := fetchHubVariablesSchema(hubName, deps)
		if err != nil {
			writeVariablesError(w, logger, err)
			return
		}

		variable, err := variables.GetVariable(varName, variablesMap)
		if err != nil {
			logRequestedVariableSchemaError(logger, err)
			writeVariablesError(w, logger, err)
			return
		}

		value, duration, err := readVariableValueObserved(
			ctx,
			logger,
			deps.VariableReader,
			hubName,
			variable,
		)
		if err != nil {
			httputil.WriteJSONResponse(w, logger, http.StatusInternalServerError, "failed to read variable value")
			return
		}
		logSlowValueRead(logger, deps.SlowValueReadThreshold, duration)

		response := map[string]any{varName: value}
		httputil.WriteJSONResponse(w, logger, http.StatusOK, response)
	}
}

func handleGetHubVariableValues(ctx context.Context, deps HandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := deps.Logger.With(zap.String("endpoint", getHubValuesEndpoint))

		hubName := r.PathValue("hubName")
		if hubName == "" {
			http.Error(w, "hub name required", http.StatusBadRequest)
			return
		}
		logger = logger.With(zap.String("hubName", hubName))

		variablesMap, err := fetchHubVariablesSchema(hubName, deps)
		if err != nil {
			writeVariablesError(w, logger, err)
			return
		}

		definitions, err := variables.DecodeHubDefinitions(variablesMap)
		if err != nil {
			writeVariablesError(w, logger, variables.ErrDecodeFailed)
			return
		}

		values, duration, err := readHubVariableValues(ctx, logger, deps.VariableReader, hubName, definitions)
		if err != nil {
			httputil.WriteJSONResponse(w, logger, http.StatusInternalServerError, "failed to read variable values")
			return
		}
		logSlowValueRead(logger.With(zap.Int("variableCount", len(definitions))), deps.SlowValueReadThreshold, duration)

		httputil.WriteJSONResponse(w, logger, http.StatusOK, values)
	}
}

func handleSetDeleteVariables(ctx context.Context, deps HandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close() //nolint:errcheck

		hubName := r.PathValue("hubName")
		varName := r.PathValue("varName")
		if hubName == "" || varName == "" {
			http.Error(w, "hub and var name required", http.StatusBadRequest)
			return
		}

		variable, err := resolveManualValkeyVariable(hubName, varName, deps)
		if err != nil {
			writeVariablesError(w, deps.Logger, err)
			return
		}

		var raw map[string]json.RawMessage
		if err = json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, "Invalid JSON format in request payload", http.StatusBadRequest)
			return
		}

		if raw["data"] == nil {
			http.Error(w, `Invalid request payload. expect {"data": any}`, http.StatusBadRequest)
			return
		}

		command := valkey.CommandAdd
		if r.Method == http.MethodDelete {
			command = valkey.CommandDel
		}

		parser, err := valkey.GetParser(variable.DataType, command)
		if err != nil {
			http.Error(w, "Invalid request payload: "+err.Error(), http.StatusBadRequest)
			return
		}

		payload, err := parser(raw["data"])
		if err != nil {
			http.Error(w, "Invalid request payload: "+stringutil.UpperFirst(err.Error()), http.StatusBadRequest)
			return
		}

		event, err := eventing.NewMdaiEvent(hubName, varName, string(variable.DataType), string(command), payload)
		if err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		subject := subjectFromVarsEvent(*event, varName)

		deps.Logger.Info("Publishing MdaiEvent",
			zap.String("id", event.ID),
			zap.String("name", event.Name),
			zap.String("source", event.Source),
			zap.String("subject", subject.String()),
		)

		if _, err := nats.PublishEvents(ctx, deps.Logger, deps.EventPublisher, []adapter.EventPerSubject{{Event: *event, Subject: subject}}, deps.AuditAdapter); err != nil {
			deps.Logger.Error("Failed to publish MdaiEvent", zap.Error(err))
			http.Error(w, "Failed to publish event", http.StatusInternalServerError)
			return
		}

		status := http.StatusOK
		if r.Method == http.MethodPost {
			status = http.StatusCreated
		}

		httputil.WriteJSONResponse(w, deps.Logger, status, event)
	}
}

func fetchHubVariablesSchema(hubName string, deps HandlerDeps) (map[string]string, error) {
	variablesMap, found, err := deps.ConfigMapController.GetVariablesSchemaConfigMapDataByHubName(hubName)
	if err != nil {
		return nil, variables.ErrFetchFailed
	}
	if !found {
		return nil, variables.ErrNoVariablesFound
	}

	return variablesMap, nil
}

func resolveManualValkeyVariable(hubName string, varName string, deps HandlerDeps) (variables.Definition, error) {
	hubsVariables, err := fetchHubVariablesSchema(hubName, deps)
	if err != nil {
		return variables.Definition{}, err
	}

	variable, err := variables.GetVariable(varName, hubsVariables)
	if err != nil {
		return variables.Definition{}, err
	}
	if variable.StorageType != variables.StorageTypeValkey {
		return variables.Definition{}, variables.HTTPError{
			Msg:    fmt.Sprintf("unsupported storage type %q", variable.StorageType),
			Status: http.StatusInternalServerError,
		}
	}
	if !variable.IsManual() {
		return variables.Definition{}, variables.ErrVariableNotManual
	}

	return variable, nil
}

func writeVariablesError(w http.ResponseWriter, logger *zap.Logger, err error) {
	status := http.StatusInternalServerError
	var httpErr variables.HTTPError
	if errors.As(err, &httpErr) {
		status = httpErr.HTTPStatus()
	}
	httputil.WriteJSONResponse(w, logger, status, err.Error())
}

func logRequestedVariableSchemaError(logger *zap.Logger, err error) {
	var httpErr variables.HTTPError
	if errors.As(err, &httpErr) {
		return
	}

	logger.Warn("Failed to parse variable schema",
		zap.Error(err),
	)
}

// TODO: parallelize reads with errgroup to reduce latency for hubs with many variables.
func readHubVariableValues(
	ctx context.Context,
	logger *zap.Logger,
	reader *datacorevariables.ValkeyAdapter,
	hubName string,
	definitions variables.HubDefinitions,
) (map[string]any, time.Duration, error) {
	values := make(map[string]any, len(definitions))
	start := time.Now()
	for _, variable := range definitions {
		value, _, err := readVariableValueObserved(ctx, logger.With(zap.String("variableName", variable.Name)), reader, hubName, variable)
		if err != nil {
			return nil, time.Since(start), err
		}
		values[variable.Name] = value
	}

	return values, time.Since(start), nil
}

func readVariableValueObserved(
	ctx context.Context,
	logger *zap.Logger,
	reader *datacorevariables.ValkeyAdapter,
	hubName string,
	variable variables.Definition,
) (any, time.Duration, error) {
	logger = logger.With(zap.String("dataType", string(variable.DataType)))
	start := time.Now()
	value, _, err := readVariableValue(ctx, reader, hubName, variable)
	duration := time.Since(start)
	if err != nil {
		logger.Error("Failed to read variable value",
			zap.Int64("duration_ms", duration.Milliseconds()),
			zap.Error(err),
		)
		return nil, duration, err
	}
	return value, duration, nil
}

func logSlowValueRead(logger *zap.Logger, threshold time.Duration, duration time.Duration) {
	if duration < threshold {
		return
	}

	logger.Warn("Slow variable value read", zap.Int64("duration_ms", duration.Milliseconds()))
}

func readVariableValue(ctx context.Context, reader *datacorevariables.ValkeyAdapter, hubName string, variable variables.Definition) (any, bool, error) {
	if variable.StorageType != variables.StorageTypeValkey {
		return nil, false, fmt.Errorf("unsupported storage type %q", variable.StorageType)
	}

	res, err := datacorevariables.Resolve(ctx, reader, hubName, variable.Name, variable.DataType, variable.Default)
	if err != nil {
		return nil, false, err
	}
	if !res.Found {
		return nil, false, nil
	}
	typed, err := res.Typed()
	if err != nil {
		return nil, false, fmt.Errorf("typed conversion for %s: %w", variable.Name, err)
	}
	return typed, true, nil
}
