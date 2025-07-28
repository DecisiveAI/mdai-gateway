package valkey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

type (
	VariableType string
	CommandType  string
)

const (
	VariableTypeSet  VariableType = "set"
	VariableTypeMap  VariableType = "map"
	VariableTypeBool VariableType = "boolean"
	VariableTypeInt  VariableType = "int"
	VariableTypeStr  VariableType = "string"

	CommandAdd CommandType = "add"
	CommandDel CommandType = "remove"
)

var errUnsupportedVariableType = errors.New("unsupported variable type")

type ParseFn func(json.RawMessage) (any, error)

func unmarshalTo[T any](errMsg string) ParseFn {
	return func(data json.RawMessage) (any, error) {
		var v T
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, errors.New(errMsg)
		}

		return v, nil
	}
}

func unmarshalToAndTransform[T any](errMsg string, transform func(T) any) ParseFn {
	return func(data json.RawMessage) (any, error) {
		var v T
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, errors.New(errMsg)
		}

		return transform(v), nil
	}
}

func GetParser(varType VariableType, command CommandType) (ParseFn, error) {
	parsers := map[VariableType]map[CommandType]ParseFn{
		VariableTypeSet: {
			CommandAdd: unmarshalTo[[]string]("list expected"),
			CommandDel: unmarshalTo[[]string]("list expected"),
		},
		VariableTypeMap: {
			CommandAdd: unmarshalTo[map[string]string]("map expected"),
			CommandDel: unmarshalTo[[]string]("list expected"),
		},
		VariableTypeStr: {
			CommandAdd: unmarshalTo[string]("string expected"),
			CommandDel: unmarshalTo[string]("string expected"),
		},
		VariableTypeInt: {
			CommandAdd: unmarshalToAndTransform[int]("int expected", func(v int) any {
				return strconv.Itoa(v)
			}),
			CommandDel: unmarshalToAndTransform[int]("int expected", func(v int) any {
				return strconv.Itoa(v)
			}),
		},
		VariableTypeBool: {
			CommandAdd: unmarshalToAndTransform[bool]("boolean expected", func(v bool) any {
				return strconv.FormatBool(v)
			}),
			CommandDel: unmarshalToAndTransform[bool]("boolean expected", func(v bool) any {
				return strconv.FormatBool(v)
			}),
		},
	}

	commands, ok := parsers[varType]
	if !ok {
		return nil, fmt.Errorf("%w %q", errUnsupportedVariableType, varType)
	}
	parser, ok := commands[command]
	if !ok {
		return nil, fmt.Errorf("unsupported command %q for variable type %q", command, varType)
	}
	return parser, nil
}

type kvAdapter interface {
	GetSetAsStringSlice(ctx context.Context, variableKey string, hubName string) ([]string, error)
	GetMap(ctx context.Context, variableKey string, hubName string) (map[string]string, error)
	GetString(ctx context.Context, variableKey string, hubName string) (string, bool, error)
}

func GetValue(ctx context.Context, a kvAdapter, varRef string, varType VariableType, hubName string) (any, error) {
	switch varType {
	case VariableTypeSet:
		return a.GetSetAsStringSlice(ctx, varRef, hubName)
	case VariableTypeMap:
		return a.GetMap(ctx, varRef, hubName)
	case VariableTypeStr, VariableTypeInt, VariableTypeBool:
		v, _, err := a.GetString(ctx, varRef, hubName)
		return v, err
	default:
		return nil, fmt.Errorf("%w %s", errUnsupportedVariableType, varType)
	}
}
