package nats

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/mydecisive/mdai-data-core/audit"
	"github.com/mydecisive/mdai-data-core/eventing"
	"github.com/mydecisive/mdai-gateway/internal/adapter"
	"github.com/mydecisive/mdai-gateway/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	valkeymock "github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
)

func TestNewMdaiEvent(t *testing.T) {
	tests := []struct {
		name        string
		hubName     string
		varName     string
		varType     string
		action      string
		payload     any
		expectError bool
	}{
		{
			name:    "basic payload",
			hubName: "testhub",
			varName: "threshold",
			varType: "int",
			action:  "set",
			payload: float64(42), // json...
		},
		{
			name:        "unmarshalable payload should error",
			hubName:     "testhub",
			varName:     "bad",
			varType:     "str",
			action:      "set",
			payload:     make(chan int),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := eventing.NewMdaiEvent(tt.hubName, tt.varName, tt.varType, tt.action, tt.payload)

			if tt.expectError {
				require.Error(t, err)
				assert.Nil(t, event)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, event)

			expectedName := "var" + "." + tt.action
			assert.Equal(t, expectedName, event.Name)
			assert.Equal(t, tt.hubName, event.HubName)
			assert.Equal(t, eventing.ManualVariablesEventSource, event.Source)

			var decoded eventing.VariablesActionPayload
			require.NoError(t, json.Unmarshal([]byte(event.Payload), &decoded), "unmarshal payload")

			assert.Equal(t, tt.varName, decoded.VariableRef)
			assert.Equal(t, tt.varType, decoded.DataType)
			assert.Equal(t, tt.action, decoded.Operation)
			assert.Equal(t, tt.payload, decoded.Data)
		})
	}
}

type XaddMatcher struct{}

func (XaddMatcher) Matches(x any) bool {
	if cmd, ok := x.(valkey.Completed); ok {
		commands := cmd.Commands()
		return slices.Contains(commands, "XADD") && slices.Contains(commands, "mdai_hub_event_history")
	}
	return false
}

func (XaddMatcher) String() string {
	return "Wanted XADD to mdai_hub_event_history command"
}

func TestPublishEvents(t *testing.T) {
	ctx := t.Context()
	logger := zap.NewNop()

	ctrl := gomock.NewController(t)
	valkeyClient := valkeymock.NewClient(ctrl)
	valkeyClient.EXPECT().Do(mock.MatchedBy(func(arg any) bool {
		_, ok := arg.(context.Context)
		return ok
	}), XaddMatcher{}).Return(valkeymock.Result(valkeymock.ValkeyString(""))).AnyTimes()
	auditAdapter := audit.NewAuditAdapter(zap.NewNop(), valkeyClient)

	event := eventing.MdaiEvent{
		Name:    "var.set",
		HubName: "hub",
	}

	subject := eventing.MdaiEventSubject{
		Type: "test",
		Path: "subject",
	}

	t.Run("all success", func(t *testing.T) {
		mockPub := &mocks.MockPublisher{}
		mockPub.On("Publish", mock.Anything, event, subject).Return(nil).Once()

		success, err := PublishEvents(ctx, logger, mockPub, []adapter.EventPerSubject{{Event: event, Subject: subject}}, auditAdapter)
		require.NoError(t, err)
		assert.Equal(t, 1, success)

		mockPub.AssertExpectations(t)
	})

	t.Run("partial failure", func(t *testing.T) {
		mockPub := &mocks.MockPublisher{}
		mockPub.On("Publish", mock.Anything, event, subject).Return(errors.New("fail")).Once()

		success, err := PublishEvents(ctx, logger, mockPub, []adapter.EventPerSubject{{Event: event, Subject: subject}}, auditAdapter)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "fail")
		assert.Equal(t, 0, success)

		mockPub.AssertExpectations(t)
	})

	t.Run("context canceled", func(t *testing.T) {
		mockPub := &mocks.MockPublisher{}
		ctx, cancel := context.WithCancel(t.Context())
		cancel() // cancel immediately

		mockPub.On("Publish", ctx, event, subject).Return(ctx.Err()).Once()

		success, err := PublishEvents(ctx, logger, mockPub, []adapter.EventPerSubject{{Event: event, Subject: subject}}, auditAdapter)
		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, 0, success)

		mockPub.AssertExpectations(t)
	})
}
