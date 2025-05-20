package main

const (
	valkeyEndpointEnvVarKey            = "VALKEY_ENDPOINT"
	valkeyPasswordEnvVarKey            = "VALKEY_PASSWORD"
	valkeyAuditStreamExpiryMSEnvVarKey = "VALKEY_AUDIT_STREAM_EXPIRY_MS"
	httpPortEnvVarKey                  = "HTTP_PORT"
	otelSdkDisabledEnvVar              = "OTEL_SDK_DISABLED"
	otelExporterOtlpEndpointEnvVar     = "OTEL_EXPORTER_OTLP_ENDPOINT"
	otelZapCoreName                    = "github.com/decisiveai/event-handler-webservice"

	defaultHttpPort = "8081"

	firingStatus   = "firing"
	resolvedStatus = "resolved"

	actionContextAnnotationsKey  = "action_context"
	relevantLabelsAnnotationsKey = "relevant_labels"
	HubName                      = "hub_name"

	mdaiHubEventHistoryStreamName = "mdai_hub_event_history"
)
