package manualvariables

import (
	"testing"

	"github.com/mydecisive/mdai-gateway/internal/valkey"
	"github.com/stretchr/testify/require"
)

func TestGetVarType(t *testing.T) {
	tests := []struct {
		name          string
		hubName       string
		varName       string
		hubsVariables ByHub
		wantType      valkey.VariableType
		wantErr       error
	}{
		{
			name:          "no hubs present",
			hubName:       "hub1",
			varName:       "var1",
			hubsVariables: ByHub{},
			wantType:      "",
			wantErr:       ErrNoManualVariablesFound,
		},
		{
			name:    "missing hubName and varName",
			hubName: "",
			varName: "",
			hubsVariables: ByHub{
				"hub1": {"var1": "string"},
			},
			wantType: "",
			wantErr:  ErrMissingQueryParams,
		},
		{
			name:    "hub not found",
			hubName: "missing-hub",
			varName: "var1",
			hubsVariables: ByHub{
				"hub1": {"var1": "string"},
			},
			wantType: "",
			wantErr:  ErrHubNotFound,
		},
		{
			name:    "variable not found in hub",
			hubName: "hub1",
			varName: "missing-var",
			hubsVariables: ByHub{
				"hub1": {"var1": "string"},
			},
			wantType: "",
			wantErr:  ErrVariableNotFound,
		},
		{
			name:    "successful lookup",
			hubName: "hub1",
			varName: "var1",
			hubsVariables: ByHub{
				"hub1": {"var1": "boolean"},
			},
			wantType: "boolean",
			wantErr:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, err := GetVarType(tt.hubName, tt.varName, tt.hubsVariables)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Empty(t, gotType)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantType, gotType)
			}
		})
	}
}
