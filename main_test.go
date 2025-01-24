package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/fake"
)

func TestMain(m *testing.M) {
	if os.Getenv("SHOW_LOGS") == "1" {
		log.SetOutput(os.Stderr)
	} else {
		log.SetOutput(io.Discard)
	}
	m.Run()
}

func TestUpdateValkeyHandler(t *testing.T) {
	const (
		configmapName   = "mdai-event-handler-config"
		successResponse = `{"success": "variable(s) updated"}`
	)

	clientset := fake.NewClientset(getConfigMapFromFile(t))
	ctx := context.TODO()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	valkeyClient := mock.NewClient(ctrl)

	alertPostBody1, err := os.ReadFile("testdata/alert_post_body_1.json")
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("/alerts", handleAlertsPost(ctx, clientset, valkeyClient))
	valkeyClient.EXPECT().Do(ctx, mock.Match("SADD", "service_list", "service-b")).Return(mock.Result(mock.ValkeyInt64(1))).Times(1)
	valkeyClient.EXPECT().Do(ctx, mock.Match("SREM", "service_list", "service-a")).Return(mock.Result(mock.ValkeyInt64(1))).Times(1)
	valkeyClient.EXPECT().Do(ctx, mock.Match("SET", "service_list", "service-c")).Return(mock.Result(mock.ValkeyString("OK"))).Times(0)

	req := httptest.NewRequest(http.MethodPost, "/alerts", bytes.NewBuffer(alertPostBody1))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, successResponse, rec.Body.String())
}

func TestGetConfig(t *testing.T) {
	{
		ctx := context.Background()
		config, err := getConfig(ctx, fake.NewClientset())
		require.EqualError(t, err, "failed to get ConfigMap")
		require.Nil(t, config)
	}
	{
		ctx := context.Background()
		configMap := getConfigMapFromFile(t)
		configMap.Data = map[string]string{}
		config, err := getConfig(ctx, fake.NewClientset(configMap))
		require.EqualError(t, err, "no config found")
		require.Nil(t, config)
	}
	{
		ctx := context.Background()
		configMap := getConfigMapFromFile(t)
		config, err := getConfig(ctx, fake.NewClientset(configMap))
		require.NoError(t, err)
		require.NotNil(t, config)
	}
}

func getConfigMapFromFile(t *testing.T) *corev1.ConfigMap {
	t.Helper()
	var configMap corev1.ConfigMap
	configMapBody, err := os.ReadFile(filepath.Join("testdata", configmapName+"-configmap.yaml"))
	require.NoError(t, err)
	err = yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(configMapBody), 100).Decode(&configMap)
	require.NoError(t, err)
	return &configMap
}
