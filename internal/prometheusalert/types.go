package prometheusalert

import (
	alertmanager "github.com/prometheus/alertmanager/template"
	"go.uber.org/zap/zapcore"
)

type prometheusAlertResponse struct {
	Message    string `json:"message"`
	Total      int    `json:"total"`
	Successful int    `json:"successful"`
	Failed     int    `json:"failed,omitempty"`
}

type loggedPrometheusAlert struct {
	alertmanager.Data
}

func (lpa loggedPrometheusAlert) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("receiver", lpa.Receiver)
	enc.AddString("status", lpa.Status)
	enc.AddInt("alert_count", len(lpa.Alerts))
	return nil
}

type AlertPayload struct {
	Labels alertmanager.KV `json:"labels"`
	Value  string          `json:"value"`
	Status string          `json:"status"`
}

func (ap AlertPayload) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("value", ap.Value)
	enc.AddString("status", ap.Status)
	enc.AddString("labels", ap.Labels.String())
	return nil
}

type PrometheusAlertData struct {
	alertmanager.Data
}

func NewPrometheusAlertData(data alertmanager.Data) PrometheusAlertData {
	return PrometheusAlertData{data}
}
