package integration

import (
	"context"
)

type Type string

type Integration interface {
	GetIntegrations(ctx context.Context, namespace string) (map[string]any, error)
	SetIntegration(ctx context.Context, namespace, integrationName string, integrationData any) error
	DeleteIntegration(ctx context.Context, namespace, integrationName string) error
}
