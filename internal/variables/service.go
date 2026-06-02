package variables

import (
	"encoding/json"
	"fmt"
	"net/http"

	datacorevariables "github.com/mydecisive/mdai-data-core/variables"
)

const (
	TypeManual        = "manual"
	StorageTypeValkey = "mdai-valkey"
)

type (
	// Schema is the raw JSON bytes of a single variable's schema entry, as
	// written by the operator into the schema ConfigMap. It is forwarded
	// verbatim in list-variables responses.
	Schema         = json.RawMessage
	HubVariables   map[string]Schema
	ByHub          map[string]HubVariables
	HubDefinitions map[string]Definition
)

type Definition struct {
	Name         string
	Schema       Schema
	Type         string
	DataType     datacorevariables.DataType
	StorageType  string
	VariableRefs []string
	Default      json.RawMessage // nil ⇔ no default declared
}

var (
	ErrFetchFailed       = HTTPError{"failed to fetch variables", http.StatusInternalServerError}
	ErrDecodeFailed      = HTTPError{"failed to decode variables", http.StatusInternalServerError}
	ErrNoVariablesFound  = HTTPError{"no variables found for hub", http.StatusNotFound}
	ErrVariableNotFound  = HTTPError{"variable not found", http.StatusNotFound}
	ErrVariableNotManual = HTTPError{"only manual variables can be updated or deleted", http.StatusConflict}
)

func DecodeByHub(rawByHub map[string]map[string]string) (ByHub, error) {
	out := make(ByHub, len(rawByHub))
	for hubName, rawHubVariables := range rawByHub {
		schemas, err := DecodeHub(rawHubVariables)
		if err != nil {
			return nil, err
		}
		out[hubName] = schemas
	}

	return out, nil
}

func DecodeHub(rawHubVariables map[string]string) (HubVariables, error) {
	definitions, err := DecodeHubDefinitions(rawHubVariables)
	if err != nil {
		return nil, err
	}
	return hubDefinitionsToSchemas(definitions), nil
}

// DecodeHubDefinitions fails fast on the first invalid schema to keep error
// handling simple. If partial decoding is needed in the future, we can
// introduce a Partial variant that collects errors per variable.
func DecodeHubDefinitions(rawHubVariables map[string]string) (HubDefinitions, error) {
	out := make(HubDefinitions, len(rawHubVariables))
	for variableName, rawSchema := range rawHubVariables {
		definition, err := parseDefinition(variableName, rawSchema)
		if err != nil {
			return nil, err
		}
		out[variableName] = definition
	}

	return out, nil
}

func GetVariable(varName string, rawHubVariables map[string]string) (Definition, error) {
	if len(rawHubVariables) == 0 {
		return Definition{}, ErrNoVariablesFound
	}

	rawSchema, ok := rawHubVariables[varName]
	if !ok {
		return Definition{}, ErrVariableNotFound
	}

	return parseDefinition(varName, rawSchema)
}

func (d Definition) IsManual() bool {
	return d.Type == TypeManual
}

func hubDefinitionsToSchemas(definitions HubDefinitions) HubVariables {
	out := make(HubVariables, len(definitions))
	for variableName, definition := range definitions {
		out[variableName] = definition.Schema
	}

	return out
}

// rawDefinition is the on-the-wire shape of a variable's schema ConfigMap
// entry. Optional fields use json.RawMessage so they can be either absent or
// validated with a domain-specific error message after the single decode.
type rawDefinition struct {
	Type         string          `json:"type"`
	DataType     string          `json:"dataType"`
	StorageType  string          `json:"storageType"`
	VariableRefs json.RawMessage `json:"variableRefs,omitempty"`
	Default      json.RawMessage `json:"default,omitempty"`
}

func parseDefinition(varName, rawSchema string) (Definition, error) {
	var decoded rawDefinition
	if err := json.Unmarshal([]byte(rawSchema), &decoded); err != nil {
		return Definition{}, fmt.Errorf("invalid schema for variable %s: %w", varName, err)
	}

	if decoded.Type == "" {
		return Definition{}, fmt.Errorf("invalid schema for variable %s: missing type", varName)
	}
	if decoded.DataType == "" {
		return Definition{}, fmt.Errorf("invalid schema for variable %s: missing dataType", varName)
	}
	if decoded.StorageType == "" {
		return Definition{}, fmt.Errorf("invalid schema for variable %s: missing storageType", varName)
	}

	var variableRefs []string
	if decoded.VariableRefs != nil {
		if err := json.Unmarshal(decoded.VariableRefs, &variableRefs); err != nil {
			return Definition{}, fmt.Errorf("invalid schema for variable %s: variableRefs must be array of strings", varName)
		}
	}

	defaultRaw := decoded.Default
	if decoded.Type != TypeManual {
		defaultRaw = nil
	}

	return Definition{
		Name:         varName,
		Schema:       json.RawMessage(rawSchema),
		Type:         decoded.Type,
		DataType:     datacorevariables.DataType(decoded.DataType),
		StorageType:  decoded.StorageType,
		VariableRefs: variableRefs,
		Default:      defaultRaw,
	}, nil
}
