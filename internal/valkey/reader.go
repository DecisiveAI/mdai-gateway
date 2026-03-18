package valkey

import (
	"context"

	datacorevariables "github.com/mydecisive/mdai-data-core/variables"
	valkeygo "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// Reader wraps the data-core adapter to provide additional existence checks for the API convenience.
type Reader struct {
	client  valkeygo.Client
	adapter *datacorevariables.ValkeyAdapter
}

func NewReader(client valkeygo.Client, logger *zap.Logger) *Reader {
	return &Reader{
		client:  client,
		adapter: datacorevariables.NewValkeyAdapter(client, logger),
	}
}

func (r *Reader) GetSet(ctx context.Context, variableKey string, hubName string) ([]string, bool, error) {
	found, err := r.adapter.Exists(ctx, variableKey, hubName)
	if err != nil || !found {
		return nil, found, err
	}

	values, err := r.adapter.GetSetAsStringSlice(ctx, variableKey, hubName)
	return values, true, err
}

func (r *Reader) GetMap(ctx context.Context, variableKey string, hubName string) (map[string]string, bool, error) {
	found, err := r.adapter.Exists(ctx, variableKey, hubName)
	if err != nil || !found {
		return nil, found, err
	}

	values, err := r.adapter.GetMap(ctx, variableKey, hubName)
	return values, true, err
}

func (r *Reader) GetString(ctx context.Context, variableKey string, hubName string) (string, bool, error) {
	return r.adapter.GetString(ctx, variableKey, hubName)
}

func (r *Reader) GetMetaPriorityList(ctx context.Context, variableKey string, hubName string) ([]string, bool, error) {
	return r.adapter.GetMetaPriorityList(ctx, variableKey, hubName)
}

func (r *Reader) GetMetaHashSet(ctx context.Context, variableKey string, hubName string) (string, bool, error) {
	return r.adapter.GetMetaHashSet(ctx, variableKey, hubName)
}
