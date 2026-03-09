package integration

import (
	"context"
	"fmt"
)

type Type string

type Integration interface {
	GetIntegrationsFromSecret(ctx context.Context, namespace string) (map[string]any, error)
	SetIntegration(ctx context.Context, namespace, integrationName string, integrationData any) error
	DeleteIntegration(ctx context.Context, namespace, integrationName string) error
}

func GetIntegration(integrationType string) (any, error) {
	switch integrationType {
	case DataDogIntegrationType:
		return &DataDogIntegration{deps.}, nil
	default:
		return nil, fmt.Errorf("integration type %s is not supported", integrationType)
	}
}
