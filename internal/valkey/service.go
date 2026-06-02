package valkey

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mydecisive/mdai-data-core/variables"
)

type CommandType string

const (
	CommandAdd CommandType = "add"
	CommandDel CommandType = "remove"
)

var errUnsupportedVariableType = errors.New("unsupported variable type")

type ParseFn func(json.RawMessage) (any, error)

func parseSet(data json.RawMessage) (any, error) {
	slice, err := variables.CanonicalizeSet(data)
	if err != nil {
		return nil, errors.New("list expected")
	}
	return slice, nil
}

func parseMap(data json.RawMessage) (any, error) {
	hash, err := variables.CanonicalizeMap(data)
	if err != nil {
		return nil, errors.New("map expected")
	}
	return hash, nil
}

func parseScalar(dt variables.DataType, expectedMsg string) ParseFn {
	return func(data json.RawMessage) (any, error) {
		canonical, err := variables.CanonicalizeScalar(data, dt)
		if err != nil {
			return nil, errors.New(expectedMsg)
		}
		return canonical, nil
	}
}

// Stateless constant dispatch table; one allocation at package init, none per request.
//
//nolint:gochecknoglobals
var (
	parseString  = parseScalar(variables.DataTypeString, "string expected")
	parseInt     = parseScalar(variables.DataTypeInt, "int expected")
	parseBoolean = parseScalar(variables.DataTypeBoolean, "boolean expected")
	parseFloat   = parseScalar(variables.DataTypeFloat, "float expected")
)

//nolint:gochecknoglobals
var parsers = map[variables.DataType]map[CommandType]ParseFn{
	variables.DataTypeSet: {
		CommandAdd: parseSet,
		CommandDel: parseSet,
	},
	variables.DataTypeMap: {
		CommandAdd: parseMap,
		CommandDel: parseSet,
	},
	variables.DataTypeString: {
		CommandAdd: parseString,
		CommandDel: parseString,
	},
	variables.DataTypeInt: {
		CommandAdd: parseInt,
		CommandDel: parseInt,
	},
	variables.DataTypeBoolean: {
		CommandAdd: parseBoolean,
		CommandDel: parseBoolean,
	},
	variables.DataTypeFloat: {
		CommandAdd: parseFloat,
		CommandDel: parseFloat,
	},
}

func GetParser(varType variables.DataType, command CommandType) (ParseFn, error) {
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
