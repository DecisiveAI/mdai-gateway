package mocks

import (
	"context"

	"github.com/stretchr/testify/mock"
)

type MockKVAdapter struct {
	mock.Mock
}

func (m *MockKVAdapter) GetSetAsStringSlice(ctx context.Context, variableKey string, hubName string) ([]string, error) {
	args := m.Called(ctx, variableKey, hubName)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockKVAdapter) GetMap(ctx context.Context, variableKey string, hubName string) (map[string]string, error) {
	args := m.Called(ctx, variableKey, hubName)
	return args.Get(0).(map[string]string), args.Error(1)
}

func (m *MockKVAdapter) GetString(ctx context.Context, variableKey string, hubName string) (string, bool, error) {
	args := m.Called(ctx, variableKey, hubName)
	return args.Get(0).(string), args.Bool(1), args.Error(2)
}
