package valkey

import (
	"encoding/json"
	"testing"

	"github.com/mydecisive/mdai-data-core/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetParser(t *testing.T) {
	testCases := []struct {
		expectedValue  any
		name           string
		varType        variables.DataType
		command        CommandType
		expectedErrMsg string
		inputJSON      json.RawMessage
		expectErr      bool
	}{
		{
			name:          "SetAdd ValidList",
			varType:       variables.DataTypeSet,
			command:       CommandAdd,
			inputJSON:     json.RawMessage(`["a", "b"]`),
			expectErr:     false,
			expectedValue: []string{"a", "b"},
		},
		{
			name:          "MapAdd ValidMap",
			varType:       variables.DataTypeMap,
			command:       CommandAdd,
			inputJSON:     json.RawMessage(`{"key1":"val1"}`),
			expectErr:     false,
			expectedValue: map[string]string{"key1": "val1"},
		},
		{
			name:          "IntAdd ValidInt",
			varType:       variables.DataTypeInt,
			command:       CommandAdd,
			inputJSON:     json.RawMessage(`123`),
			expectErr:     false,
			expectedValue: "123",
		},
		{
			name:          "BoolAdd ValidBool",
			varType:       variables.DataTypeBoolean,
			command:       CommandAdd,
			inputJSON:     json.RawMessage(`true`),
			expectErr:     false,
			expectedValue: "true",
		},
		{
			name:          "FloatAdd ValidFloat",
			varType:       variables.DataTypeFloat,
			command:       CommandAdd,
			inputJSON:     json.RawMessage(`1.5`),
			expectErr:     false,
			expectedValue: "1.5",
		},
		{
			name:          "FloatAdd IntegerShape",
			varType:       variables.DataTypeFloat,
			command:       CommandAdd,
			inputJSON:     json.RawMessage(`1`),
			expectErr:     false,
			expectedValue: "1",
		},
		{
			name:          "FloatAdd TrailingZeroCollapses",
			varType:       variables.DataTypeFloat,
			command:       CommandAdd,
			inputJSON:     json.RawMessage(`1.50`),
			expectErr:     false,
			expectedValue: "1.5",
		},
		{
			name:          "FloatAdd NegativeZeroNormalizes",
			varType:       variables.DataTypeFloat,
			command:       CommandAdd,
			inputJSON:     json.RawMessage(`-0`),
			expectErr:     false,
			expectedValue: "0",
		},
		{
			name:           "SetAdd InvalidJSON",
			varType:        variables.DataTypeSet,
			command:        CommandAdd,
			inputJSON:      json.RawMessage(`"not-a-list"`),
			expectErr:      true,
			expectedErrMsg: "list expected",
		},
		{
			name:           "MapAdd InvalidJSON",
			varType:        variables.DataTypeMap,
			command:        CommandAdd,
			inputJSON:      json.RawMessage(`["not-a-map"]`),
			expectErr:      true,
			expectedErrMsg: "map expected",
		},
		{
			name:           "IntAdd Invalid JSON",
			varType:        variables.DataTypeInt,
			command:        CommandAdd,
			inputJSON:      json.RawMessage(`"not-an-int"`),
			expectErr:      true,
			expectedErrMsg: "int expected",
		},
		{
			name:           "FloatAdd InvalidJSON",
			varType:        variables.DataTypeFloat,
			command:        CommandAdd,
			inputJSON:      json.RawMessage(`"not-a-float"`),
			expectErr:      true,
			expectedErrMsg: "float expected",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			parser, err := GetParser(tc.varType, tc.command)
			require.NoError(t, err)
			assert.NotNil(t, parser)
			actualValue, err := parser(tc.inputJSON)
			if tc.expectErr {
				assert.Equal(t, tc.expectedErrMsg, err.Error())
			}
			assert.Equal(t, tc.expectedValue, actualValue)
		})
	}

	t.Run("UnsupportedVariableType", func(t *testing.T) {
		_, err := GetParser("invalid-type", CommandAdd)
		require.Error(t, err)
	})

	t.Run("UnsupportedCommand", func(t *testing.T) {
		_, err := GetParser(variables.DataTypeSet, "invalid-command")
		require.Error(t, err)
	})
}
