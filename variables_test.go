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
	ctrl := gomock.NewController(t)
	client := vmock.NewClient(ctrl)
	adapter := ValkeyAdapter.NewValkeyAdapter(client, logr.Discard(), "hub")
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

	// Test valkey values
	adapter, client, ctx, ctrl := newAdapterWithMock(t)
	defer ctrl.Finish()

	// Map
	key := "variable/hub/attribute_map_manual"

	client.EXPECT().
		Do(ctx, vmock.Match("HGETALL", key)).
		Return(vmock.Result(vmock.ValkeyMap(map[string]valkey.ValkeyMessage{
			"attrib.1": vmock.ValkeyBlobString("value1"),
			"attrib.2": vmock.ValkeyBlobString("value2"),
			"attrib.3": vmock.ValkeyBlobString("value3"),
		})))

	got, err := adapter.GetMap(ctx, "attribute_map_manual")
	assert.NoError(t, err)

	expected := map[string]string{
		"attrib.1": "value1",
		"attrib.2": "value2",
		"attrib.3": "value3",
	}
	assert.Equal(t, expected, got)

	// Set
	key = "variable/hub/service_list_manual"
	client.EXPECT().
		Do(ctx, vmock.Match("SMEMBERS", key)).
		Return(vmock.Result(
			vmock.ValkeyArray(
				vmock.ValkeyBlobString("man_service_1"),
				vmock.ValkeyBlobString("man_service_2"),
				vmock.ValkeyBlobString("man_service_3")),
		))

	gotSet, err := adapter.GetSetAsStringSlice(ctx, "service_list_manual")
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{"man_service_1", "man_service_2", "man_service_3"}, gotSet)

	// String
	key = "variable/hub/service_manual"
	client.EXPECT().
		Do(ctx, vmock.Match("GET", key)).
		Return(vmock.Result(
			vmock.ValkeyBlobString("man_single_service"),
		))

	gotString, found, err := adapter.GetString(ctx, "service_manual")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "man_single_service", gotString)

	// Int
	key = "variable/hub/severity_number_man"
	client.EXPECT().
		Do(ctx, vmock.Match("GET", key)).
		Return(vmock.Result(
			vmock.ValkeyBlobString("3"),
		))

	gotInt, found, err := adapter.GetString(ctx, "severity_number_man")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "3", gotInt)

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
	req := httptest.NewRequest(http.MethodGet, "/variables/list/mdaihub-sample/", nil)
	rr := httptest.NewRecorder()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /variables/list/{hub}/", handler)

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
