package valkey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	variables "github.com/mydecisive/mdai-data-core/variables"
)

type CommandType string

const (
	CommandAdd CommandType = "add"
	CommandDel CommandType = "remove"
)

var (
	errUnsupportedVariableType = errors.New("unsupported variable type")
	errFloatNotFinite          = errors.New("NaN and ±Inf are not allowed for float variables")
)

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

func parseFloat(data json.RawMessage) (any, error) {
	var v float64
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, errors.New("float expected")
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil, errFloatNotFinite
	}
	if v == 0 {
		v = 0 // normalize -0 → 0
	}
	return strconv.FormatFloat(v, 'g', -1, 64), nil
}

func GetParser(varType variables.DataType, command CommandType) (ParseFn, error) {
	parsers := map[variables.DataType]map[CommandType]ParseFn{
		variables.DataTypeSet: {
			CommandAdd: unmarshalTo[[]string]("list expected"),
			CommandDel: unmarshalTo[[]string]("list expected"),
		},
		variables.DataTypeMap: {
			CommandAdd: unmarshalTo[map[string]string]("map expected"),
			CommandDel: unmarshalTo[[]string]("list expected"),
		},
		variables.DataTypeString: {
			CommandAdd: unmarshalTo[string]("string expected"),
			CommandDel: unmarshalTo[string]("string expected"),
		},
		variables.DataTypeInt: {
			CommandAdd: unmarshalToAndTransform[int]("int expected", func(v int) any {
				return strconv.Itoa(v)
			}),
			CommandDel: unmarshalToAndTransform[int]("int expected", func(v int) any {
				return strconv.Itoa(v)
			}),
		},
		variables.DataTypeBoolean: {
			CommandAdd: unmarshalToAndTransform[bool]("boolean expected", func(v bool) any {
				return strconv.FormatBool(v)
			}),
			CommandDel: unmarshalToAndTransform[bool]("boolean expected", func(v bool) any {
				return strconv.FormatBool(v)
			}),
		},
		variables.DataTypeFloat: {
			CommandAdd: parseFloat,
			CommandDel: parseFloat,
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
	GetSet(ctx context.Context, variableKey string, hubName string) ([]string, bool, error)
	GetMap(ctx context.Context, variableKey string, hubName string) (map[string]string, bool, error)
	GetString(ctx context.Context, variableKey string, hubName string) (string, bool, error)
	GetMetaPriorityList(ctx context.Context, variableKey string, hubName string) ([]string, bool, error)
	GetMetaHashSet(ctx context.Context, variableKey string, hubName string) (string, bool, error)
}

type getterFunc[T any] func(ctx context.Context, variableKey string, hubName string) (T, bool, error)

func getParsedStringValue(
	ctx context.Context,
	getter getterFunc[string],
	varRef string,
	hubName string,
	valueType string,
	parse func(string) (any, error),
) (any, bool, error) {
	value, found, err := getter(ctx, varRef, hubName)
	if err != nil || !found {
		return nil, found, err
	}

	parsed, err := parse(value)
	if err != nil {
		return nil, false, fmt.Errorf("parse %s value for %s: %w", valueType, varRef, err)
	}

	return parsed, true, nil
}

func GetValue(ctx context.Context, a kvAdapter, varRef string, varType variables.DataType, hubName string) (any, bool, error) {
	switch varType {
	case variables.DataTypeSet:
		return a.GetSet(ctx, varRef, hubName)
	case variables.DataTypeMap:
		return a.GetMap(ctx, varRef, hubName)
	case variables.DataTypeString:
		return a.GetString(ctx, varRef, hubName)
	case variables.DataTypeInt:
		return getParsedStringValue(ctx, a.GetString, varRef, hubName, "int", func(value string) (any, error) {
			return strconv.Atoi(value)
		})
	case variables.DataTypeBoolean:
		return getParsedStringValue(ctx, a.GetString, varRef, hubName, "boolean", func(value string) (any, error) {
			return strconv.ParseBool(value)
		})
	case variables.DataTypeFloat:
		return getParsedStringValue(ctx, a.GetString, varRef, hubName, "float", func(value string) (any, error) {
			return strconv.ParseFloat(value, 64)
		})
	case variables.DataTypeMetaPriorityList:
		return a.GetMetaPriorityList(ctx, varRef, hubName)
	case variables.DataTypeMetaHashSet:
		return a.GetMetaHashSet(ctx, varRef, hubName)
	default:
		return nil, false, fmt.Errorf("%w %s", errUnsupportedVariableType, varType)
	}
}
