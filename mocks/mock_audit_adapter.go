package mocks

import (
	"context"

	"github.com/stretchr/testify/mock"
)

type MockAuditAdapter struct {
	mock.Mock
}

func (m *MockAuditAdapter) InsertAuditLogEventFromMap(ctx context.Context, eventMap map[string]string) error {
	args := m.Called(ctx, eventMap)
	return args.Error(0)
}
