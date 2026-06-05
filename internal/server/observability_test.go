package server

import (
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	datacorekube "github.com/mydecisive/mdai-data-core/kube"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	valkeymock "github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func TestHandleListVariables_MalformedSchema(t *testing.T) {
	clientset := newFakeClientset(t)
	deps, _ := setupObservedReadOnlyMocks(t, clientset)
	updateSchemaConfigMap(t, clientset, deps.ConfigMapController, func(cm *corev1.ConfigMap) {
		cm.Data["broken_var"] = `{`
	})

	mux := NewRouter(t.Context(), deps)

	tests := []struct {
		target string
	}{
		{target: "/variables/list"},
		{target: "/variables/list/hub/mdaihub-sample"},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.target, http.NoBody)
			rr := httptest.NewRecorder()

			mux.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusInternalServerError, rr.Code)
			assert.JSONEq(t, `"failed to decode variables"`, rr.Body.String())
		})
	}
}

func TestHandleGetHubVariableValues_MalformedSchema(t *testing.T) {
	clientset := newFakeClientset(t)
	deps, _ := setupObservedReadOnlyMocks(t, clientset)
	updateSchemaConfigMap(t, clientset, deps.ConfigMapController, func(cm *corev1.ConfigMap) {
		cm.Data = map[string]string{
			"data_string": sampleHubVariablesSchemaRaw()["data_string"],
			"broken_var":  `{`,
		}
	})

	mux := NewRouter(t.Context(), deps)
	req := httptest.NewRequest(http.MethodGet, "/variables/values/hub/mdaihub-sample", http.NoBody)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.JSONEq(t, `"failed to decode variables"`, rr.Body.String())
}

func TestHandleGetVariables_LogsMalformedRequestedSchema(t *testing.T) {
	clientset := newFakeClientset(t)
	deps, observedLogs := setupObservedReadOnlyMocks(t, clientset)
	updateSchemaConfigMap(t, clientset, deps.ConfigMapController, func(cm *corev1.ConfigMap) {
		cm.Data["broken_var"] = `{`
	})

	mux := NewRouter(t.Context(), deps)
	req := httptest.NewRequest(http.MethodGet, "/variables/values/hub/mdaihub-sample/var/broken_var", http.NoBody)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.JSONEq(t, `"invalid schema for variable broken_var: unexpected end of JSON input"`, rr.Body.String())

	requireObservedLog(t, observedLogs, zapcore.WarnLevel, "Failed to parse variable schema", map[string]any{
		"endpoint":     getSingleVariableEndpoint,
		"hubName":      "mdaihub-sample",
		"variableName": "broken_var",
	})
}

func TestHandleGetVariables_LogsValkeyReadError(t *testing.T) {
	clientset := newFakeClientset(t)
	deps, observedLogs := setupObservedReadOnlyMocks(t, clientset)

	deps.ValkeyClient.(*valkeymock.Client).EXPECT().
		Do(gomock.Any(), valkeymock.Match("GET", "variable/mdaihub-sample/data_string")).
		Return(valkeymock.ErrorResult(errors.New("boom")))

	mux := NewRouter(t.Context(), deps)
	req := httptest.NewRequest(http.MethodGet, "/variables/values/hub/mdaihub-sample/var/data_string", http.NoBody)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.JSONEq(t, `"failed to read variable value"`, rr.Body.String())

	requireObservedLog(t, observedLogs, zapcore.ErrorLevel, "Failed to read variable value", map[string]any{
		"endpoint":     getSingleVariableEndpoint,
		"hubName":      "mdaihub-sample",
		"variableName": "data_string",
		"dataType":     "string",
	})
}

func TestHandleGetHubVariableValues_LogsValkeyReadError(t *testing.T) {
	clientset := newFakeClientset(t)
	deps, observedLogs := setupObservedReadOnlyMocks(t, clientset)
	updateSchemaConfigMap(t, clientset, deps.ConfigMapController, func(cm *corev1.ConfigMap) {
		cm.Data = map[string]string{
			"data_string": sampleHubVariablesSchemaRaw()["data_string"],
		}
	})

	deps.ValkeyClient.(*valkeymock.Client).EXPECT().
		Do(gomock.Any(), valkeymock.Match("GET", "variable/mdaihub-sample/data_string")).
		Return(valkeymock.ErrorResult(errors.New("boom")))

	mux := NewRouter(t.Context(), deps)
	req := httptest.NewRequest(http.MethodGet, "/variables/values/hub/mdaihub-sample", http.NoBody)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.JSONEq(t, `"failed to read variable values"`, rr.Body.String())

	requireObservedLog(t, observedLogs, zapcore.ErrorLevel, "Failed to read variable value", map[string]any{
		"endpoint":     getHubValuesEndpoint,
		"hubName":      "mdaihub-sample",
		"variableName": "data_string",
		"dataType":     "string",
	})
}

func TestHandleGetVariables_LogsSlowRead(t *testing.T) {
	clientset := newFakeClientset(t)
	deps, observedLogs := setupObservedReadOnlyMocks(t, clientset)
	deps.SlowValueReadThreshold = 0

	deps.ValkeyClient.(*valkeymock.Client).EXPECT().
		Do(gomock.Any(), valkeymock.Match("GET", "variable/mdaihub-sample/data_string")).
		Return(valkeymock.Result(valkeymock.ValkeyBlobString("foo")))

	mux := NewRouter(t.Context(), deps)
	req := httptest.NewRequest(http.MethodGet, "/variables/values/hub/mdaihub-sample/var/data_string", http.NoBody)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.JSONEq(t, `{"data_string":"foo"}`, rr.Body.String())

	entry := requireObservedLog(t, observedLogs, zapcore.WarnLevel, "Slow variable value read", map[string]any{
		"endpoint":     getSingleVariableEndpoint,
		"hubName":      "mdaihub-sample",
		"variableName": "data_string",
	})
	_, ok := entry.ContextMap()["duration_ms"]
	require.True(t, ok)
}

func TestHandleGetHubVariableValues_LogsSlowRead(t *testing.T) {
	clientset := newFakeClientset(t)
	deps, observedLogs := setupObservedReadOnlyMocks(t, clientset)
	deps.SlowValueReadThreshold = 0
	updateSchemaConfigMap(t, clientset, deps.ConfigMapController, func(cm *corev1.ConfigMap) {
		cm.Data = map[string]string{
			"data_string": sampleHubVariablesSchemaRaw()["data_string"],
		}
	})

	deps.ValkeyClient.(*valkeymock.Client).EXPECT().
		Do(gomock.Any(), valkeymock.Match("GET", "variable/mdaihub-sample/data_string")).
		Return(valkeymock.Result(valkeymock.ValkeyBlobString("foo")))

	mux := NewRouter(t.Context(), deps)
	req := httptest.NewRequest(http.MethodGet, "/variables/values/hub/mdaihub-sample", http.NoBody)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.JSONEq(t, `{"data_string":"foo"}`, rr.Body.String())

	entry := requireObservedLog(t, observedLogs, zapcore.WarnLevel, "Slow variable value read", map[string]any{
		"endpoint":      getHubValuesEndpoint,
		"hubName":       "mdaihub-sample",
		"variableCount": 1,
	})
	_, ok := entry.ContextMap()["duration_ms"]
	require.True(t, ok)
}

func setupObservedReadOnlyMocks(t *testing.T, clientset kubernetes.Interface) (HandlerDeps, *observer.ObservedLogs) {
	t.Helper()

	deps := setupReadOnlyMocks(t, clientset)
	core, observedLogs := observer.New(zapcore.WarnLevel)
	deps.Logger = zap.New(core)

	return deps, observedLogs
}

func updateSchemaConfigMap(
	t *testing.T,
	clientset kubernetes.Interface,
	cmController *datacorekube.HubConfigMapController,
	mutate func(cm *corev1.ConfigMap),
) {
	t.Helper()

	ctx := t.Context()
	cmClient := clientset.CoreV1().ConfigMaps("mdai")

	cm, err := cmClient.Get(ctx, "mdaihub-sample-variables-schema", metav1.GetOptions{})
	require.NoError(t, err)

	mutate(cm)
	expectedData := maps.Clone(cm.Data)

	_, err = cmClient.Update(ctx, cm, metav1.UpdateOptions{})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		obj, exists, err := cmController.CmInformer.Informer().GetIndexer().GetByKey("mdai/mdaihub-sample-variables-schema")
		if err != nil || obj == nil || !exists {
			return false
		}
		current := obj.(*corev1.ConfigMap) //nolint:forcetypeassert
		return reflect.DeepEqual(current.Data, expectedData)
	}, 2*time.Second, 50*time.Millisecond)
}

func requireObservedLog(
	t *testing.T,
	observedLogs *observer.ObservedLogs,
	level zapcore.Level,
	message string,
	fields map[string]any,
) observer.LoggedEntry {
	t.Helper()

	for _, entry := range observedLogs.AllUntimed() {
		if entry.Level != level || entry.Message != message {
			continue
		}

		context := entry.ContextMap()
		if matchesObservedFields(context, fields) {
			return entry
		}
	}

	require.FailNowf(t, "missing log entry", "level=%s message=%q fields=%v logs=%v", level.String(), message, fields, observedLogs.AllUntimed())

	return observer.LoggedEntry{}
}

func matchesObservedFields(context map[string]any, fields map[string]any) bool {
	for key, expected := range fields {
		actual, ok := context[key]
		if !ok || !equalObservedValue(actual, expected) {
			return false
		}
	}

	return true
}

func equalObservedValue(actual any, expected any) bool {
	switch e := expected.(type) {
	case int:
		switch a := actual.(type) {
		case int:
			return a == e
		case int32:
			return int64(a) == int64(e)
		case int64:
			return a == int64(e)
		default:
			return false
		}
	default:
		return reflect.DeepEqual(actual, expected)
	}
}
