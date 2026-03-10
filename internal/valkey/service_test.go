package valkey

import (
	"encoding/json"
	"testing"

	"github.com/mydecisive/mdai-gateway/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestGetParser(t *testing.T) {
	testCases := []struct {
		expectedValue  any
		name           string
		varType        VariableType
		command        CommandType
		expectedErrMsg string
		inputJSON      json.RawMessage
		expectErr      bool
	}{
		{
			name:          "SetAdd ValidList",
			varType:       VariableTypeSet,
			command:       CommandAdd,
			inputJSON:     json.RawMessage(`["a", "b"]`),
			expectErr:     false,
			expectedValue: []string{"a", "b"},
		},
		{
			name:          "MapAdd ValidMap",
			varType:       VariableTypeMap,
			command:       CommandAdd,
			inputJSON:     json.RawMessage(`{"key1":"val1"}`),
			expectErr:     false,
			expectedValue: map[string]string{"key1": "val1"},
		},
		{
			name:          "IntAdd ValidInt",
			varType:       VariableTypeInt,
			command:       CommandAdd,
			inputJSON:     json.RawMessage(`123`),
			expectErr:     false,
			expectedValue: "123",
		},
		{
			name:          "BoolAdd ValidBool",
			varType:       VariableTypeBool,
			command:       CommandAdd,
			inputJSON:     json.RawMessage(`true`),
			expectErr:     false,
			expectedValue: "true",
		},
		{
			name:           "SetAdd InvalidJSON",
			varType:        VariableTypeSet,
			command:        CommandAdd,
			inputJSON:      json.RawMessage(`"not-a-list"`),
			expectErr:      true,
			expectedErrMsg: "list expected",
		},
		{
			name:           "MapAdd InvalidJSON",
			varType:        VariableTypeMap,
			command:        CommandAdd,
			inputJSON:      json.RawMessage(`["not-a-map"]`),
			expectErr:      true,
			expectedErrMsg: "map expected",
		},
		{
			name:           "IntAdd Invalid JSON",
			varType:        VariableTypeInt,
			command:        CommandAdd,
			inputJSON:      json.RawMessage(`"not-an-int"`),
			expectErr:      true,
			expectedErrMsg: "int expected",
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
		_, err := GetParser(VariableTypeSet, "invalid-command")
		require.Error(t, err)
	})
}

func TestGetValue(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		varType   VariableType
		hubName   string
		mockSetup func(m *mocks.MockKVAdapter)
		expected  any
		expectErr bool
	}{
		{
			name:    "set value",
			key:     "foo",
			varType: "set",
			hubName: "hub",
			mockSetup: func(m *mocks.MockKVAdapter) {
				m.On("GetSetAsStringSlice", mock.Anything, "foo", "hub").
					Return([]string{"foo", "bar"}, nil).Once()
			},
			expected:  []string{"foo", "bar"},
			expectErr: false,
		},
		{
			name:    "map value",
			key:     "foo",
			varType: "map",
			hubName: "hub",
			mockSetup: func(m *mocks.MockKVAdapter) {
				m.On("GetMap", mock.Anything, "foo", "hub").
					Return(map[string]string{"foo": "bar"}, nil).Once()
			},
			expected:  map[string]string{"foo": "bar"},
			expectErr: false,
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
			expected:  "bar",
			expectErr: false,
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
			expected:  "true",
			expectErr: false,
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
			expected:  "999",
			expectErr: false,
		},
		{
			name:      "invalid value",
			key:       "foo_invalid",
			varType:   "invalid",
			hubName:   "hub",
			mockSetup: func(m *mocks.MockKVAdapter) {},
			expected:  nil,
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockKV := &mocks.MockKVAdapter{}
			tc.mockSetup(mockKV)
			t.Cleanup(func() { mockKV.AssertExpectations(t) })

			val, err := GetValue(t.Context(), mockKV, tc.key, tc.varType, tc.hubName)

			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expected, val)
			}
		})
	}
}
