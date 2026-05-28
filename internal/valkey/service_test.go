package valkey

import (
	"encoding/json"
	"testing"

	variables "github.com/mydecisive/mdai-data-core/variables"
	"github.com/mydecisive/mdai-gateway/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

func TestGetValue(t *testing.T) {
	tests := []struct {
		name          string
		key           string
		varType       variables.DataType
		hubName       string
		mockSetup     func(m *mocks.MockKVAdapter)
		expected      any
		expectedFound bool
		expectErr     bool
	}{
		{
			name:    "set value",
			key:     "foo",
			varType: "set",
			hubName: "hub",
			mockSetup: func(m *mocks.MockKVAdapter) {
				m.On("GetSet", mock.Anything, "foo", "hub").
					Return([]string{"foo", "bar"}, true, nil).Once()
			},
			expected:      []string{"foo", "bar"},
			expectedFound: true,
			expectErr:     false,
		},
		{
			name:    "map value",
			key:     "foo",
			varType: "map",
			hubName: "hub",
			mockSetup: func(m *mocks.MockKVAdapter) {
				m.On("GetMap", mock.Anything, "foo", "hub").
					Return(map[string]string{"foo": "bar"}, true, nil).Once()
			},
			expected:      map[string]string{"foo": "bar"},
			expectedFound: true,
			expectErr:     false,
		},
		{
			name:    "string value",
			key:     "foo_string",
			varType: "string",
			hubName: "hub",
			mockSetup: func(m *mocks.MockKVAdapter) {
				m.On("GetString", mock.Anything, "foo_string", "hub").
					Return("bar", true, nil).Once()
			},
			expected:      "bar",
			expectedFound: true,
			expectErr:     false,
		},
		{
			name:    "boolean value",
			key:     "foo_bool",
			varType: "boolean",
			hubName: "hub",
			mockSetup: func(m *mocks.MockKVAdapter) {
				m.On("GetString", mock.Anything, "foo_bool", "hub").
					Return("true", true, nil).Once()
			},
			expected:      true,
			expectedFound: true,
			expectErr:     false,
		},
		{
			name:    "int value",
			key:     "foo_int",
			varType: "int",
			hubName: "hub",
			mockSetup: func(m *mocks.MockKVAdapter) {
				m.On("GetString", mock.Anything, "foo_int", "hub").
					Return("999", true, nil).Once()
			},
			expected:      999,
			expectedFound: true,
			expectErr:     false,
		},
		{
			name:    "meta priority list value",
			key:     "foo_meta_list",
			varType: variables.DataTypeMetaPriorityList,
			hubName: "hub",
			mockSetup: func(m *mocks.MockKVAdapter) {
				m.On("GetMetaPriorityList", mock.Anything, "foo_meta_list", "hub").
					Return([]string{"a", "b"}, true, nil).Once()
			},
			expected:      []string{"a", "b"},
			expectedFound: true,
			expectErr:     false,
		},
		{
			name:    "meta hash set value",
			key:     "foo_meta_hash",
			varType: variables.DataTypeMetaHashSet,
			hubName: "hub",
			mockSetup: func(m *mocks.MockKVAdapter) {
				m.On("GetMetaHashSet", mock.Anything, "foo_meta_hash", "hub").
					Return("joined-value", true, nil).Once()
			},
			expected:      "joined-value",
			expectedFound: true,
			expectErr:     false,
		},
		{
			name:    "missing value returns found false",
			key:     "missing",
			varType: variables.DataTypeString,
			hubName: "hub",
			mockSetup: func(m *mocks.MockKVAdapter) {
				m.On("GetString", mock.Anything, "missing", "hub").
					Return("", false, nil).Once()
			},
			expected:      "",
			expectedFound: false,
			expectErr:     false,
		},
		{
			name:          "invalid value",
			key:           "foo_invalid",
			varType:       "invalid",
			hubName:       "hub",
			mockSetup:     func(m *mocks.MockKVAdapter) {},
			expected:      nil,
			expectedFound: false,
			expectErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockKV := &mocks.MockKVAdapter{}
			tc.mockSetup(mockKV)
			t.Cleanup(func() { mockKV.AssertExpectations(t) })

			val, found, err := GetValue(t.Context(), mockKV, tc.key, tc.varType, tc.hubName)

			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedFound, found)
				assert.Equal(t, tc.expected, val)
			}
		})
	}
}
