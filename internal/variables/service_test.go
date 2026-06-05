package variables

import (
	"encoding/json"
	"testing"

	datacorevariables "github.com/mydecisive/mdai-data-core/variables"
	"github.com/stretchr/testify/require"
)

func mustDecodeHub(t *testing.T, rawHubVariables map[string]string) HubVariables {
	t.Helper()

	schemas, err := DecodeHub(rawHubVariables)
	require.NoError(t, err)

	return schemas
}

func TestDecodeHub(t *testing.T) {
	t.Run("successful decode", func(t *testing.T) {
		raw := `{"type":"manual","dataType":"string","storageType":"mdai-valkey","serializeAs":[{"name":"VAR1"}]}`
		schemas := mustDecodeHub(t, map[string]string{"var1": raw})
		require.Equal(t, HubVariables{"var1": json.RawMessage(raw)}, schemas)
	})

	t.Run("invalid json", func(t *testing.T) {
		_, err := DecodeHub(map[string]string{"var1": `{`})
		require.ErrorContains(t, err, "invalid schema for variable var1")
	})

	t.Run("missing required field", func(t *testing.T) {
		_, err := DecodeHub(map[string]string{"var1": `{"type":"manual","storageType":"mdai-valkey"}`})
		require.ErrorContains(t, err, "missing dataType")
	})
}

func TestGetVariable(t *testing.T) {
	tests := []struct {
		name         string
		varName      string
		hubVariables map[string]string
		wantType     datacorevariables.DataType
		wantErr      error
		wantErrText  string
		wantRefs     []string
		isManual     bool
	}{
		{
			name:         "no variables present",
			varName:      "var1",
			hubVariables: map[string]string{},
			wantErr:      ErrNoVariablesFound,
		},
		{
			name:         "variable not found",
			varName:      "var1",
			hubVariables: map[string]string{"other": `{"type":"manual","dataType":"string","storageType":"mdai-valkey"}`},
			wantErr:      ErrVariableNotFound,
		},
		{
			name:    "manual variable",
			varName: "var1",
			hubVariables: map[string]string{
				"var1": `{"type":"manual","dataType":"boolean","storageType":"mdai-valkey","variableRefs":["foo"]}`,
			},
			wantType: datacorevariables.DataTypeBoolean,
			wantRefs: []string{"foo"},
			isManual: true,
		},
		{
			name:    "computed variable",
			varName: "var1",
			hubVariables: map[string]string{
				"var1": `{"type":"computed","dataType":"set","storageType":"mdai-valkey"}`,
			},
			wantType: datacorevariables.DataTypeSet,
			isManual: false,
		},
		{
			name:    "invalid variable refs type",
			varName: "var1",
			hubVariables: map[string]string{
				"var1": `{"type":"manual","dataType":"string","storageType":"mdai-valkey","variableRefs":"foo"}`,
			},
			wantErrText: "variableRefs must be array of strings",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			definition, err := GetVariable(tt.varName, tt.hubVariables)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			if tt.wantErrText != "" {
				require.ErrorContains(t, err, tt.wantErrText)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantType, definition.DataType)
			require.Equal(t, tt.wantRefs, definition.VariableRefs)
			require.Equal(t, tt.isManual, definition.IsManual())
		})
	}
}

func TestGetVariable_DefaultExtraction(t *testing.T) {
	t.Run("manual variable carries declared default", func(t *testing.T) {
		definition, err := GetVariable("var1", map[string]string{
			"var1": `{"type":"manual","dataType":"int","storageType":"mdai-valkey","default":100}`,
		})
		require.NoError(t, err)
		require.JSONEq(t, `100`, string(definition.Default))
	})

	t.Run("manual variable without default has nil Default", func(t *testing.T) {
		definition, err := GetVariable("var1", map[string]string{
			"var1": `{"type":"manual","dataType":"int","storageType":"mdai-valkey"}`,
		})
		require.NoError(t, err)
		require.Nil(t, definition.Default)
	})

	t.Run("computed variable ignores any declared default", func(t *testing.T) {
		definition, err := GetVariable("var1", map[string]string{
			"var1": `{"type":"computed","dataType":"set","storageType":"mdai-valkey","default":["x"]}`,
		})
		require.NoError(t, err)
		require.Nil(t, definition.Default)
	})
}
