package manualvariables

import (
	"net/http"

	"github.com/mydecisive/mdai-gateway/internal/valkey"
)

type ByHub map[string]map[string]string

var (
	ErrMissingQueryParams     = HTTPError{"missing hub or variable name", http.StatusBadRequest}
	ErrNoManualVariablesFound = HTTPError{"no hubs with manual variables found", http.StatusNotFound}
	ErrHubNotFound            = HTTPError{"hub not found", http.StatusNotFound}
	ErrVariableNotFound       = HTTPError{"variable not found", http.StatusNotFound}
)

func GetVarType(hubName string, varName string, hubsVariables ByHub) (valkey.VariableType, error) {
	if len(hubsVariables) == 0 {
		return "", ErrNoManualVariablesFound
	}

	if hubName == "" || varName == "" {
		return "", ErrMissingQueryParams
	}

	hubFound := hubsVariables[hubName]
	if hubFound == nil {
		return "", ErrHubNotFound
	}

	varType, ok := hubFound[varName]
	if !ok {
		return "", ErrVariableNotFound
	}

	return valkey.VariableType(varType), nil
}
