package connection

import (
	"context"
	"encoding/json"
	"fmt"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/samber/lo"
	"go.uber.org/zap"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"time"
)

type OctantConnectionData struct {
	SourceType     string      `json:"sourceType"`
	TelemetryTypes []Telemetry `json:"telemetryTypes"`
	Deployment     *Deployment `json:"deployment,omitempty"`
}

type Deployment struct {
	Type   string         `json:"type"`
	Fields map[string]any `json:"fields"`
}

type ArgoDeployment struct {
	Branch string `json:"branch"`
}

var _ Connection[OctantConnectionData] = (*OctantConnection)(nil)

type OctantConnection struct {
	K8sClient  kubernetes.Interface
	PromClient promv1.API
	Logger     *zap.Logger
}

type ingressEgress int

const (
	ingress ingressEgress = iota
	egress
)

const (
	fidelityCheckFail = "fail"
	fidelityCheckPass = "pass"

	fidelityMetricResult = "result"
	fidelityMetricSignal = "signal"
)

func (oc *OctantConnection) GetConnectionStatus(ctx context.Context, namespace, connectionName string) (*Status, error) {
	var (
		receivingData bool
		sendingData   bool
		dataIntegrity bool
	)
	connection, err := oc.GetConnectionByName(ctx, namespace, connectionName)
	if err != nil {
		return nil, fmt.Errorf("getting connection: %w", err)
	}

	// for each telemetry type on the connection, check for increasing metrics on the receiver (receiving data)
	receivingData, err = oc.queryTelemetryStatus(ctx, ingress, connection.TelemetryTypes)
	if err != nil {
		return nil, fmt.Errorf("querying telemetry ingress status: %w", err)
	}

	// for each telemetry type on the connection, check for increasing metrics on the exporter (sending data)
	sendingData, err = oc.queryTelemetryStatus(ctx, egress, connection.TelemetryTypes)
	if err != nil {
		return nil, fmt.Errorf("querying telemetry egress status: %w", err)
	}

	dataIntegrity, err = oc.verifyDataIntegrity(ctx, connection.TelemetryTypes)
	if err != nil {
		return nil, fmt.Errorf("verifying data integrity: %w", err)
	}

	return &Status{
		ReceivingData: receivingData,
		SendingData:   sendingData,
		DataIntegrity: dataIntegrity,
	}, nil
}

func (oc *OctantConnection) verifyDataIntegrity(ctx context.Context, telemetryTypes []Telemetry) (bool, error) {
	// compare now to 10 minutes ago
	results, _, err := oc.PromClient.QueryRange(ctx, "mdai_fidelity_required_signal_checks_total", promv1.Range{
		Start: time.Now().Add(-10 * time.Minute),
		End:   time.Now(),
		Step:  10 * time.Minute,
	})
	if err != nil {
		return false, fmt.Errorf("failed to query prometheus: %w", err)
	}
	if results == nil {
		return false, nil
	}

	resultMatrix, ok := results.(model.Matrix)
	if !ok {
		return false, fmt.Errorf("failed to convert result to model.Matrix")
	}

	for _, telemetryType := range telemetryTypes {
		if !dataFidelityCheck(oc.Logger, resultMatrix, telemetryType) {
			return false, nil
		}
	}
	return true, nil
}

func dataFidelityCheck(logger *zap.Logger, resultMatrix model.Matrix, telemetryType Telemetry) bool {
	failed := lo.Filter(resultMatrix, func(item *model.SampleStream, _ int) bool {
		return item.Metric[fidelityMetricResult] == fidelityCheckFail &&
			string(item.Metric[fidelityMetricSignal]) == string(telemetryType)
	})
	passed := lo.Filter(resultMatrix, func(item *model.SampleStream, _ int) bool {
		return item.Metric[fidelityMetricResult] == fidelityCheckPass &&
			string(item.Metric[fidelityMetricSignal]) == string(telemetryType)
	})

	// sanity check... this shouldn't happen.
	if len(failed) != 1 || len(passed) != 1 {
		logger.Warn("unable to perform data fidelity check, expected 1 set of failed and passed fidelity metric values")
		return false
	}

	// if the fidelity check failures are increasing OR the passed fidelity checks are NOT increasing, fail fast
	if areSeriesValuesIncreasing(failed[0]) || !areSeriesValuesIncreasing(passed[0]) {
		return false
	}
	return true
}

func (oc *OctantConnection) queryTelemetryStatus(ctx context.Context, ie ingressEgress, telemetryTypes []Telemetry) (bool, error) {
	for _, connectionType := range telemetryTypes {
		var promQuery string
		switch connectionType {
		case Logs:
			promQuery = lo.Ternary(
				ie == ingress,
				"otelcol_receiver_accepted_log_records_total{receiver=\"datadog\", job=\"otel-collector\"}",
				"otelcol_exporter_sent_log_records{receiver=\"datadog\", job=\"otel-collector\"}",
			)
		case Traces:
			promQuery = lo.Ternary(
				ie == ingress,
				"otelcol_receiver_accepted_spans_total{receiver=\"datadog\", job=\"otel-collector\"}",
				"otelcol_exporter_sent_spans{receiver=\"datadog\", job=\"otel-collector\"}",
			)
		case Metrics:
			promQuery = lo.Ternary(
				ie == ingress,
				"otelcol_receiver_accepted_metric_points_total{receiver=\"datadog\", job=\"otel-collector\"}",
				"otelcol_exporter_sent_metric_points{receiver=\"datadog\", job=\"otel-collector\"}",
			)
		default:
			return false, fmt.Errorf("unknown telemetry type: %s", connectionType)
		}

		// TODO: figure out how to query a label to get EXACTLY the collector we want to look at, don't want to sum across multiple collectors
		// compare the last minute of results
		results, _, err := oc.PromClient.QueryRange(ctx, promQuery, promv1.Range{
			Start: time.Now().Add(-1 * time.Minute),
			End:   time.Now(),
			Step:  time.Minute,
		})
		if err != nil {
			return false, fmt.Errorf("failed to query prometheus: %w", err)
		}

		var metricsIncreasing bool
		metricsIncreasing, err = areMatrixValuesIncreasing(results)
		if err != nil {
			return false, fmt.Errorf("analyzing query range results: %w", err)
		}

		// return immediately if one of the telemetry types isn't increasing, we don't need to keep checking
		if !metricsIncreasing {
			return false, nil
		}
	}
	return true, nil
}

func areMatrixValuesIncreasing(results model.Value) (bool, error) {
	if results == nil {
		return false, nil
	}
	resultMatrix, ok := results.(model.Matrix)
	if !ok {
		return false, fmt.Errorf("failed to convert result to model.Matrix")
	}

	for _, series := range resultMatrix {
		// we can return immediately if the values went up in our time range, no need to keep going.
		if areSeriesValuesIncreasing(series) {
			return true, nil
		}
	}
	return false, nil
}

func areSeriesValuesIncreasing(series *model.SampleStream) bool {
	for i := 1; i < len(series.Values); i++ {
		prev := series.Values[i-1]
		curr := series.Values[i]

		diff := float64(curr.Value) - float64(prev.Value)

		if diff > 0 {
			// we can return immediately if the values went up in our time range, no need to keep going.
			return true
		}
	}
	return false
}

func (oc *OctantConnection) GetConnectionByName(ctx context.Context, namespace, name string) (*OctantConnectionData, error) {
	configmap, err := oc.K8sClient.CoreV1().ConfigMaps(namespace).Get(ctx, connectionsConfigmapName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil // nolint: nilnil
		}
		return nil, fmt.Errorf("failed to get configmap %s: %w", connectionsConfigmapName, err)
	}

	if _, ok := configmap.Data[name]; !ok {
		return nil, nil // nolint: nilnil
	}

	var connection OctantConnectionData
	if err = json.Unmarshal([]byte(configmap.Data[name]), &connection); err != nil {
		return nil, fmt.Errorf("failed to unmarshal connection data: %w", err)
	}
	return &connection, nil
}

func (oc *OctantConnection) SaveConnection(ctx context.Context, connection OctantConnectionData, namespace, connectionName string) error {
	jsonData, err := json.Marshal(connection)
	if err != nil {
		return fmt.Errorf("failed to marshal connection data: %w", err)
	}

	cm, err := oc.K8sClient.CoreV1().ConfigMaps(namespace).Get(ctx, connectionsConfigmapName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Create the confmap if it does not exist
			return createConnectionConfigMap(ctx, oc.K8sClient, namespace, connectionsConfigmapName, connectionName, string(jsonData))
		}
		return fmt.Errorf("failed to fetch configmap %s: %w", connectionsConfigmapName, err)
	}
	// Update the confmap if it already exists
	return updateConfigMapWithConnection(ctx, oc.K8sClient, namespace, cm, connectionName, string(jsonData))
}

func (oc *OctantConnection) DeleteConnection(ctx context.Context, namespace, connectionName string) error {
	cm, err := oc.K8sClient.CoreV1().ConfigMaps(namespace).Get(ctx, connectionsConfigmapName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to fetch configmap %s: %w", connectionsConfigmapName, err)
	}

	if cm.Data == nil {
		return nil
	}
	if _, exists := cm.Data[connectionName]; !exists {
		return nil
	}

	delete(cm.Data, connectionName)

	if _, err = oc.K8sClient.CoreV1().ConfigMaps(namespace).Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to update configmap %s after deletion: %w", connectionsConfigmapName, err)
	}

	return nil
}
