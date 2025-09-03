package mocks

import (
	"context"

	"github.com/decisiveai/mdai-data-core/eventing"
	"github.com/stretchr/testify/mock"
)

type MockPublisher struct {
	mock.Mock
}

func (m *MockPublisher) Publish(ctx context.Context, event eventing.MdaiEvent, subject string) error {
	args := m.Called(ctx, event, subject)
	return args.Error(0)
}

func (m *MockPublisher) Close() error {
	args := m.Called()
	return args.Error(0)
}
