package integration

const DataDogIntegrationType IntegrationType = "datadog"

type DataDogIntegration struct {
	ApiKey string `json:"api_key"`
	DDUrl  string `json:"dd_url"`
}
