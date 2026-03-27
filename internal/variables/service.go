package variables

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/mydecisive/mdai-gateway/internal/valkey"
)

const (
	TypeManual        = "manual"
	StorageTypeValkey = "mdai-valkey"
)

type (
	// Schema TODO replace with shared type.
	Schema         map[string]any
	HubVariables   map[string]Schema
	ByHub          map[string]HubVariables
	HubDefinitions map[string]Definition
)

type Definition struct {
	Name         string
	Schema       Schema
	Type         string
	DataType     valkey.VariableType
	StorageType  string
	VariableRefs []string
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

// DecodeHubDefinitions fails fast on the first invalid schema to keep error handling simple.
// If partial decoding is needed in the future, we can introduce a Partial variant that collects errors per variable.
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

func parseDefinition(varName string, rawSchema string) (Definition, error) {
	var schema Schema
	if err := json.Unmarshal([]byte(rawSchema), &schema); err != nil {
		return Definition{}, fmt.Errorf("invalid schema for variable %s: %w", varName, err)
	}

	schemaType, err := requiredStringField(schema, varName, "type")
	if err != nil {
		return Definition{}, err
	}

	dataType, err := requiredStringField(schema, varName, "dataType")
	if err != nil {
		return Definition{}, err
	}

	storageType, err := requiredStringField(schema, varName, "storageType")
	if err != nil {
		return Definition{}, err
	}

	variableRefs, err := optionalStringSliceField(schema, varName, "variableRefs")
	if err != nil {
		return Definition{}, err
	}

	return Definition{
		Name:         varName,
		Schema:       schema,
		Type:         schemaType,
		DataType:     valkey.VariableType(dataType),
		StorageType:  storageType,
		VariableRefs: variableRefs,
	}, nil
}

func requiredStringField(schema Schema, varName string, fieldName string) (string, error) {
	value, ok := schema[fieldName]
	if !ok {
		return "", fmt.Errorf("invalid schema for variable %s: missing %s", varName, fieldName)
	}

	stringValue, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("invalid schema for variable %s: %s must be string", varName, fieldName)
	}
	if stringValue == "" {
		return "", fmt.Errorf("invalid schema for variable %s: missing %s", varName, fieldName)
	}

	return stringValue, nil
}

func optionalStringSliceField(schema Schema, varName string, fieldName string) ([]string, error) {
	value, ok := schema[fieldName]
	if !ok {
		return nil, nil
	}

	rawValues, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid schema for variable %s: %s must be array of strings", varName, fieldName)
	}

	values := make([]string, len(rawValues))
	for i, rawValue := range rawValues {
		stringValue, ok := rawValue.(string)
		if !ok {
			return nil, fmt.Errorf("invalid schema for variable %s: %s must be array of strings", varName, fieldName)
		}
		values[i] = stringValue
	}

	return values, nil
}
