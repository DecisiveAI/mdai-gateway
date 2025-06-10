package types

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/decisiveai/mdai-event-hub/eventing"
	"github.com/prometheus/alertmanager/template"
	"github.com/stretchr/testify/require"
)

type testCase struct {
	name     string
	filename string
}

func TestAdaptPrometheusAlertToMdaiEvents(t *testing.T) {
	tests := []testCase{
		{"case_1", "alert_1.json"},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dataPath := filepath.Join("testdata", tc.filename)
			input, err := os.ReadFile(dataPath)
			require.NoError(t, err)

			var rawAlert template.Data
			err = json.Unmarshal(input, &rawAlert)
			require.NoError(t, err)

			idGenerated := CreateEventUuid()
			layout := time.RFC3339Nano
			tsStr1 := "2018-08-03T09:52:26.739266876+02:00"
			//tsStr2 := "2018-08-03T09:52:26.739266878+02:00"
			//tsStr3 := "2018-08-03T09:52:26.739266880+02:00"
			//tsStr4 := "2018-08-03T09:52:26.739266882+02:00"
			//tsStr5 := "2018-08-03T09:52:26.739266884+02:00"
			//tsStr6 := "2018-08-03T09:52:26.739266886+02:00"

			ts1, err := time.Parse(layout, tsStr1)
			require.NoError(t, err)
			//ts2, err := time.Parse(layout, tsStr2)
			//require.NoError(t, err)
			//ts3, err := time.Parse(layout, tsStr3)
			//require.NoError(t, err)
			//ts4, err := time.Parse(layout, tsStr4)
			//require.NoError(t, err)
			//ts5, err := time.Parse(layout, tsStr5)
			//require.NoError(t, err)
			//ts6, err := time.Parse(layout, tsStr6)
			//require.NoError(t, err)

			expectedEvents := []eventing.MdaiEvent{
				{
					Name:      "logBytesOutTooHighBySvc.resolved",
					Source:    Prometheus,
					Id:        idGenerated,
					Timestamp: ts1,
					HubName:   "mdaihub-second",
					Payload:   `{"alertname":"logBytesOutTooHighBySvc","dc":"eu-west-1","instance":"localhost:9090","job":"prometheus24","status":"resolved","value":""}`,
				},
			}

			gotEvents := AdaptPrometheusAlertToMdaiEvents(rawAlert)
			gotEvents[i].Id = idGenerated
			require.Equal(t, expectedEvents, gotEvents)

		})
	}
}
