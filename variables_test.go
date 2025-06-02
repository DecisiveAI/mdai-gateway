package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/decisiveai/mdai-data-core/variables"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/valkey-io/valkey-go"
	vmock "github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"
)

var (
	mdaiHubGVR = schema.GroupVersionResource{
		Group:    "hub.mydecisive.ai",
		Version:  "v1",
		Resource: "mdaihubs",
	}
	configMapGVR = schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "configmaps",
	}
)

func newAdapterWithMock(t *testing.T) (*ValkeyAdapter.ValkeyAdapter, *vmock.Client, context.Context, *gomock.Controller) {
	t.Helper()
	ctrl := gomock.NewController(t)
	client := vmock.NewClient(ctrl)
	adapter := ValkeyAdapter.NewValkeyAdapter(client, logr.Discard())
	return adapter, client, context.Background(), ctrl
}

func newFakeDynamicClient(mdaiHub *unstructured.Unstructured, configMap *unstructured.Unstructured) dynamic.Interface {
	scheme := runtime.NewScheme()
	listKinds := map[schema.GroupVersionResource]string{
		mdaiHubGVR:   "MdaiHubList",
		configMapGVR: "ConfigMapList",
	}
	dynClient := fake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		listKinds,
		mdaiHub,
		configMap,
	)

	return dynClient
}

func TestGetConfiguredManualVariables(t *testing.T) {

	ctx := context.TODO()

	mdaiHub := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "hub.mydecisive.ai/v1",
			"kind":       "MdaiHub",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample",
				"namespace": "mdai",
			},
			"spec": map[string]interface{}{},
		},
	}

	configMap := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample-manual-variables",
				"namespace": "mdai",
			},
			"data": map[string]interface{}{
				"any_service_alerted_man": "boolean",
				"attribute_map_manual":    "map",
				"service_list_manual":     "set",
				"service_manual":          "string",
				"severity_number_man":     "int",
			},
		},
	}

	dynClient := newFakeDynamicClient(
		mdaiHub,
		configMap,
	)

	// List ConfigMaps
	cmList, err := dynClient.Resource(configMapGVR).Namespace("mdai").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list ConfigMaps: %v", err)
	}
	if len(cmList.Items) != 1 {
		t.Errorf("Expected 1 ConfigMap, got %d", len(cmList.Items))
	}
	// List MdaiHubs
	mdaiList, err := dynClient.Resource(mdaiHubGVR).Namespace("mdai").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list MdaiHubs: %v", err)
	}
	if len(mdaiList.Items) != 1 {
		t.Errorf("Expected 1 MdaiHub, got %d", len(mdaiList.Items))
	}

	hubMap, err := getConfiguredManualVariables(ctx, dynClient)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(hubMap))
	// go through variables
	hubVars := hubMap["mdaihub-sample"].(map[string]string)
	assert.Equal(t, "boolean", hubVars["any_service_alerted_man"])
	assert.Equal(t, "map", hubVars["attribute_map_manual"])
	assert.Equal(t, "set", hubVars["service_list_manual"])
	assert.Equal(t, "string", hubVars["service_manual"])
	assert.Equal(t, "int", hubVars["severity_number_man"])
}

func TestHandleListVariables_List(t *testing.T) {
	ctx := context.TODO()
	mdaiHub := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "hub.mydecisive.ai/v1",
			"kind":       "MdaiHub",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample",
				"namespace": "mdai",
			},
			"spec": map[string]interface{}{},
		},
	}

	configMap := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample-manual-variables",
				"namespace": "mdai",
			},
			"data": map[string]interface{}{
				"any_service_alerted_man": "boolean",
				"attribute_map_manual":    "map",
				"service_list_manual":     "set",
				"service_manual":          "string",
				"severity_number_man":     "int",
			},
		},
	}
	dynClient := newFakeDynamicClient(
		mdaiHub,
		configMap,
	)

	handler := HandleListVariables(ctx, dynClient)

	//  GET /variables/list/
	req := httptest.NewRequest(http.MethodGet, "/variables/list/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, status)
	}

	expected := map[string]map[string]string{
		"mdaihub-sample": {
			"any_service_alerted_man": "boolean",
			"attribute_map_manual":    "map",
			"service_list_manual":     "set",
			"service_manual":          "string",
			"severity_number_man":     "int",
		}}

	var result map[string]map[string]string

	err := json.Unmarshal(rr.Body.Bytes(), &result)
	assert.Nil(t, err)
	assert.Equal(t, expected, result)
}

func TestHandleListVariables_ListHub(t *testing.T) {
	ctx := context.TODO()
	mdaiHub := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "hub.mydecisive.ai/v1",
			"kind":       "MdaiHub",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample",
				"namespace": "mdai",
			},
			"spec": map[string]interface{}{},
		},
	}

	configMap := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample-manual-variables",
				"namespace": "mdai",
			},
			"data": map[string]interface{}{
				"any_service_alerted_man": "boolean",
				"attribute_map_manual":    "map",
				"service_list_manual":     "set",
				"service_manual":          "string",
				"severity_number_man":     "int",
			},
		},
	}
	dynClient := newFakeDynamicClient(
		mdaiHub,
		configMap,
	)

	handler := HandleListVariables(ctx, dynClient)

	//  GET /variables/list/hub/mdaihub-sample/
	req := httptest.NewRequest(http.MethodGet, "/variables/list/hub/mdaihub-sample/", nil)
	rr := httptest.NewRecorder()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /variables/list/hub/{hubName}/", handler)

	mux.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, status)
	}
	t.Logf("Response: %s", rr.Body.String())

	expected := map[string]string{
		"any_service_alerted_man": "boolean",
		"attribute_map_manual":    "map",
		"service_list_manual":     "set",
		"service_manual":          "string",
		"severity_number_man":     "int",
	}

	var result map[string]string

	err := json.Unmarshal(rr.Body.Bytes(), &result)
	assert.Nil(t, err)
	assert.Equal(t, expected, result)

}

func TestHandleListVariables_ListHub_NonExistent(t *testing.T) {
	ctx := context.TODO()
	mdaiHub := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "hub.mydecisive.ai/v1",
			"kind":       "MdaiHub",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample",
				"namespace": "mdai",
			},
			"spec": map[string]interface{}{},
		},
	}

	configMap := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample-manual-variables",
				"namespace": "mdai",
			},
			"data": map[string]interface{}{
				"any_service_alerted_man": "boolean",
				"attribute_map_manual":    "map",
				"service_list_manual":     "set",
				"service_manual":          "string",
				"severity_number_man":     "int",
			},
		},
	}
	dynClient := newFakeDynamicClient(
		mdaiHub,
		configMap,
	)

	handler := HandleListVariables(ctx, dynClient)

	req := httptest.NewRequest(http.MethodGet, "/variables/list/hub/nonexistent_hub/", nil)
	rr := httptest.NewRecorder()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /variables/list/hub/{hubName}/", handler)

	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Equal(t, "\"Hub not found\"\n", rr.Body.String())

}

func TestHandleGetVariables_Int(t *testing.T) {
	ctx := context.TODO()
	mdaiHub := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "hub.mydecisive.ai/v1",
			"kind":       "MdaiHub",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample",
				"namespace": "mdai",
			},
			"spec": map[string]interface{}{},
		},
	}

	configMap := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample-manual-variables",
				"namespace": "mdai",
			},
			"data": map[string]interface{}{
				"any_service_alerted_man": "boolean",
				"attribute_map_manual":    "map",
				"service_list_manual":     "set",
				"service_manual":          "string",
				"severity_number_man":     "int",
			},
		},
	}
	dynClient := newFakeDynamicClient(
		mdaiHub,
		configMap,
	)

	// Test valkey values
	_, client, ctx, ctrl := newAdapterWithMock(t)
	defer ctrl.Finish()
	key := "variable/mdaihub-sample/severity_number_man"
	client.EXPECT().
		Do(ctx, vmock.Match("GET", key)).
		Return(vmock.Result(
			vmock.ValkeyBlobString("3"),
		))

	handler := HandleGetVariables(ctx, client, dynClient)

	// Geting Int
	req := httptest.NewRequest(http.MethodGet, "/variables/values/hub/mdaihub-sample/var/severity_number_man/", nil)
	rr := httptest.NewRecorder()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /variables/values/hub/{hubName}/var/{varName}/", handler)

	mux.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, status)
	}

	expected := map[string]string{
		"severity_number_man": "3",
	}

	var result map[string]string

	err := json.Unmarshal(rr.Body.Bytes(), &result)
	assert.Nil(t, err)
	assert.Equal(t, expected, result)
}

func TestHandleGetVariables_Bool(t *testing.T) {
	ctx := context.TODO()
	mdaiHub := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "hub.mydecisive.ai/v1",
			"kind":       "MdaiHub",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample",
				"namespace": "mdai",
			},
			"spec": map[string]interface{}{},
		},
	}

	configMap := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample-manual-variables",
				"namespace": "mdai",
			},
			"data": map[string]interface{}{
				"any_service_alerted_man": "boolean",
				"attribute_map_manual":    "map",
				"service_list_manual":     "set",
				"service_manual":          "string",
				"severity_number_man":     "int",
			},
		},
	}
	dynClient := newFakeDynamicClient(
		mdaiHub,
		configMap,
	)

	// Test valkey values
	_, client, ctx, ctrl := newAdapterWithMock(t)
	defer ctrl.Finish()
	key := "variable/mdaihub-sample/any_service_alerted_man"
	client.EXPECT().
		Do(ctx, vmock.Match("GET", key)).
		Return(vmock.Result(
			vmock.ValkeyBlobString("true"),
		))

	handler := HandleGetVariables(ctx, client, dynClient)

	// Geting Boolean
	req := httptest.NewRequest(http.MethodGet, "/variables/values/hub/mdaihub-sample/var/any_service_alerted_man/", nil)
	rr := httptest.NewRecorder()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /variables/values/hub/{hubName}/var/{varName}/", handler)

	mux.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, status)
	}

	expected := map[string]string{
		"any_service_alerted_man": "true",
	}

	var result map[string]string

	err := json.Unmarshal(rr.Body.Bytes(), &result)
	assert.Nil(t, err)
	assert.Equal(t, expected, result)
}

func TestHandleGetVariables_String(t *testing.T) {
	ctx := context.TODO()
	mdaiHub := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "hub.mydecisive.ai/v1",
			"kind":       "MdaiHub",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample",
				"namespace": "mdai",
			},
			"spec": map[string]interface{}{},
		},
	}

	configMap := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample-manual-variables",
				"namespace": "mdai",
			},
			"data": map[string]interface{}{
				"any_service_alerted_man": "boolean",
				"attribute_map_manual":    "map",
				"service_list_manual":     "set",
				"service_manual":          "string",
				"severity_number_man":     "int",
			},
		},
	}
	dynClient := newFakeDynamicClient(
		mdaiHub,
		configMap,
	)

	// Test valkey values
	_, client, ctx, ctrl := newAdapterWithMock(t)
	defer ctrl.Finish()
	key := "variable/mdaihub-sample/service_manual"
	client.EXPECT().
		Do(ctx, vmock.Match("GET", key)).
		Return(vmock.Result(
			vmock.ValkeyBlobString("service1"),
		))

	handler := HandleGetVariables(ctx, client, dynClient)

	req := httptest.NewRequest(http.MethodGet, "/variables/values/hub/mdaihub-sample/var/service_manual/", nil)
	rr := httptest.NewRecorder()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /variables/values/hub/{hubName}/var/{varName}/", handler)

	mux.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, status)
	}

	expected := map[string]string{
		"service_manual": "service1",
	}

	var result map[string]string

	err := json.Unmarshal(rr.Body.Bytes(), &result)
	assert.Nil(t, err)
	assert.Equal(t, expected, result)
}

func TestHandleGetVariables_Set(t *testing.T) {
	ctx := context.TODO()
	mdaiHub := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "hub.mydecisive.ai/v1",
			"kind":       "MdaiHub",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample",
				"namespace": "mdai",
			},
			"spec": map[string]interface{}{},
		},
	}

	configMap := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample-manual-variables",
				"namespace": "mdai",
			},
			"data": map[string]interface{}{
				"any_service_alerted_man": "boolean",
				"attribute_map_manual":    "map",
				"service_list_manual":     "set",
				"service_manual":          "string",
				"severity_number_man":     "int",
			},
		},
	}
	dynClient := newFakeDynamicClient(
		mdaiHub,
		configMap,
	)

	// Test valkey values
	_, client, ctx, ctrl := newAdapterWithMock(t)
	defer ctrl.Finish()
	key := "variable/mdaihub-sample/service_list_manual"
	client.EXPECT().
		Do(ctx, vmock.Match("SMEMBERS", key)).
		Return(vmock.Result(
			vmock.ValkeyArray(
				vmock.ValkeyBlobString("manual_service_1"),
				vmock.ValkeyBlobString("manual_service_2"),
				vmock.ValkeyBlobString("manual_service_3")),
		))

	handler := HandleGetVariables(ctx, client, dynClient)

	req := httptest.NewRequest(http.MethodGet, "/variables/values/hub/mdaihub-sample/var/service_list_manual/", nil)
	rr := httptest.NewRecorder()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /variables/values/hub/{hubName}/var/{varName}/", handler)

	mux.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, status)
	}

	expected := map[string][]string{
		"service_list_manual": {
			"manual_service_1",
			"manual_service_2",
			"manual_service_3",
		},
	}

	var result map[string][]string

	err := json.Unmarshal(rr.Body.Bytes(), &result)
	assert.Nil(t, err)
	assert.Equal(t, expected, result)
}

func TestHandleGetVariables_Map(t *testing.T) {
	ctx := context.TODO()
	mdaiHub := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "hub.mydecisive.ai/v1",
			"kind":       "MdaiHub",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample",
				"namespace": "mdai",
			},
			"spec": map[string]interface{}{},
		},
	}

	configMap := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample-manual-variables",
				"namespace": "mdai",
			},
			"data": map[string]interface{}{
				"any_service_alerted_man": "boolean",
				"attribute_map_manual":    "map",
				"service_list_manual":     "set",
				"service_manual":          "string",
				"severity_number_man":     "int",
			},
		},
	}
	dynClient := newFakeDynamicClient(
		mdaiHub,
		configMap,
	)

	// Test valkey values
	_, client, ctx, ctrl := newAdapterWithMock(t)
	defer ctrl.Finish()
	key := "variable/mdaihub-sample/attribute_map_manual"
	client.EXPECT().
		Do(ctx, vmock.Match("HGETALL", key)).
		Return(vmock.Result(vmock.ValkeyMap(map[string]valkey.ValkeyMessage{
			"attrib.1": vmock.ValkeyBlobString("value1"),
			"attrib.2": vmock.ValkeyBlobString("value2"),
			"attrib.3": vmock.ValkeyBlobString("value3"),
		})))

	handler := HandleGetVariables(ctx, client, dynClient)

	req := httptest.NewRequest(http.MethodGet, "/variables/values/hub/mdaihub-sample/var/attribute_map_manual/", nil)
	rr := httptest.NewRecorder()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /variables/values/hub/{hubName}/var/{varName}/", handler)

	mux.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, status)
	}

	expected := map[string]map[string]string{
		"attribute_map_manual": {
			"attrib.1": "value1",
			"attrib.2": "value2",
			"attrib.3": "value3",
		},
	}

	var result map[string]map[string]string

	err := json.Unmarshal(rr.Body.Bytes(), &result)
	assert.Nil(t, err)
	assert.Equal(t, expected, result)
}

func TestHandleGetVariables_NonExistentHub(t *testing.T) {
	ctx := context.TODO()
	mdaiHub := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "hub.mydecisive.ai/v1",
			"kind":       "MdaiHub",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample",
				"namespace": "mdai",
			},
			"spec": map[string]interface{}{},
		},
	}

	configMap := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample-manual-variables",
				"namespace": "mdai",
			},
			"data": map[string]interface{}{
				"severity_number_man": "int",
			},
		},
	}
	dynClient := newFakeDynamicClient(
		mdaiHub,
		configMap,
	)

	// Test valkey values
	_, client, ctx, ctrl := newAdapterWithMock(t)
	defer ctrl.Finish()

	handler := HandleGetVariables(ctx, client, dynClient)

	req := httptest.NewRequest(http.MethodGet, "/variables/values/hub/nonexistent_hub/var/severity_number_man/", nil)
	rr := httptest.NewRecorder()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /variables/values/hub/{hubName}/var/{varName}/", handler)

	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "hub not found")

}

func TestHandleGetVariables_NonExistentVariable(t *testing.T) {
	ctx := context.TODO()
	mdaiHub := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "hub.mydecisive.ai/v1",
			"kind":       "MdaiHub",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample",
				"namespace": "mdai",
			},
			"spec": map[string]interface{}{},
		},
	}

	configMap := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample-manual-variables",
				"namespace": "mdai",
			},
			"data": map[string]interface{}{
				"severity_number_man": "int",
			},
		},
	}
	dynClient := newFakeDynamicClient(
		mdaiHub,
		configMap,
	)

	// Test valkey values
	_, client, ctx, ctrl := newAdapterWithMock(t)
	defer ctrl.Finish()

	handler := HandleGetVariables(ctx, client, dynClient)

	req := httptest.NewRequest(http.MethodGet, "/variables/values/hub/mdaihub-sample/var/nonexistent_variable/", nil)
	rr := httptest.NewRecorder()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /variables/values/hub/{hubName}/var/{varName}/", handler)

	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "variable not found")

}
func TestHandleGetVariables_String_NoValue(t *testing.T) {
	ctx := context.TODO()
	mdaiHub := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "hub.mydecisive.ai/v1",
			"kind":       "MdaiHub",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample",
				"namespace": "mdai",
			},
			"spec": map[string]interface{}{},
		},
	}

	configMap := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "mdaihub-sample-manual-variables",
				"namespace": "mdai",
			},
			"data": map[string]interface{}{
				"service_manual": "string",
			},
		},
	}
	dynClient := newFakeDynamicClient(
		mdaiHub,
		configMap,
	)

	// Test valkey values
	_, client, ctx, ctrl := newAdapterWithMock(t)
	defer ctrl.Finish()
	key := "variable/mdaihub-sample/service_manual"
	client.EXPECT().
		Do(ctx, vmock.Match("GET", key)).
		Return(vmock.Result(
			vmock.ValkeyNil(),
		))

	handler := HandleGetVariables(ctx, client, dynClient)

	req := httptest.NewRequest(http.MethodGet, "/variables/values/hub/mdaihub-sample/var/service_manual/", nil)
	rr := httptest.NewRecorder()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /variables/values/hub/{hubName}/var/{varName}/", handler)

	mux.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, status)
	}

	expected := map[string]string{"service_manual": ""}

	var result map[string]string

	err := json.Unmarshal(rr.Body.Bytes(), &result)
	assert.Nil(t, err)
	assert.Equal(t, expected, result)
}
