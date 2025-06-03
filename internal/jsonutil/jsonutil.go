package jsonutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
)

var bufferPool = sync.Pool{ //nolint:gochecknoglobals
	New: func() any {
		return new(bytes.Buffer)
	},
}

func MarshalWithBuffer(v any) ([]byte, error) {
	b := bufferPool.Get()
	buf, ok := b.(*bytes.Buffer)
	if !ok {
		return nil, fmt.Errorf("bufferPool returned unexpected type %T", b)
	}
	buf.Reset()
	defer bufferPool.Put(buf)

	enc := json.NewEncoder(buf)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
