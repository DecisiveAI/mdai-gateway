package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mydecisive/mdai-data-core/audit"
	"github.com/mydecisive/mdai-data-core/eventing"
	datacorekube "github.com/mydecisive/mdai-data-core/kube"
	"github.com/mydecisive/mdai-gateway/internal/manualvariables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	valkeymock "github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	publisherClientName = "publisher-mdai-gateway"
	alert3              = "../../testdata/alert_post_body_3.json"
	alert2              = "../../testdata/alert_post_body_2.json"
	alert1              = "../../testdata/alert_post_body_1.json"
)

func TestGetConfiguredManualVariables(t *testing.T) {
	ctx := t.Context()

	clientset := newFakeClientset(t)

	// List ConfigMaps
	cmList, err := clientset.CoreV1().ConfigMaps("mdai").List(ctx, metav1.ListOptions{})
	require.NoError(t, err, "failed to list configmaps")
	assert.Len(t, cmList.Items, 1, "expected one configmap, got %d", len(cmList.Items))

	cmController, err := newFakeConfigMapController(t, clientset, "mdai")
	defer cmController.Stop()
	require.NoError(t, err)
	require.NotNil(t, cmController)
	require.NoError(t, err, "failed to start configmap controller")

	hubMap, err := cmController.GetAllHubsToDataMap()

	require.NoError(t, err)
	assert.Len(t, hubMap, 1)

	// go through variables
	require.Contains(t, hubMap, "mdaihub-sample")
	require.IsType(t, map[string]string{}, hubMap["mdaihub-sample"])

	hubVars := hubMap["mdaihub-sample"]
	assert.Equal(t, "boolean", hubVars["data_boolean"])
	assert.Equal(t, "map", hubVars["data_map"])
	assert.Equal(t, "set", hubVars["data_set"])
	assert.Equal(t, "string", hubVars["data_string"])
	assert.Equal(t, "int", hubVars["data_int"])
}

func TestHandleListVariables(t *testing.T) {
	listTests := []struct {
		out      any
		expected any
		name     string
		target   string
		status   int
	}{
		{
			name:   "List",
			target: "/variables/list",
			status: http.StatusOK,
			out:    &manualvariables.ByHub{},
			expected: &manualvariables.ByHub{
				"mdaihub-sample": {
					"data_boolean": "boolean",
					"data_map":     "map",
					"data_set":     "set",
					"data_string":  "string",
					"data_int":     "int",
				},
			},
		},
		{
			name:   "ListHub",
			target: "/variables/list/hub/mdaihub-sample",
			status: http.StatusOK,
			out:    &map[string]string{},
			expected: &map[string]string{
				"data_boolean": "boolean",
				"data_map":     "map",
				"data_set":     "set",
				"data_string":  "string",
				"data_int":     "int",
			},
		},
		{
			name:     "ListHub_NonExistent",
			target:   "/variables/list/hub/nonexistent_hub",
			status:   http.StatusNotFound,
			out:      new(string),
			expected: ptr("Hub not found"),
		},
	}

	clientset := newFakeClientset(t)
	deps := setupMocks(t, clientset)
	mux := NewRouter(t.Context(), deps)

	for _, tt := range listTests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.target, http.NoBody)
			rr := httptest.NewRecorder()

			mux.ServeHTTP(rr, req)

			assert.Equal(t, tt.status, rr.Code)

			err := json.Unmarshal(rr.Body.Bytes(), tt.out)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, tt.out)
		})
	}
}

func TestHandleGetVariables(t *testing.T) {
	getTests := []struct {
		out       any
		expected  any
		valkey    func(t *testing.T, m *valkeymock.Client)
		cmprepare func(t *testing.T, cs kubernetes.Interface, cmController *datacorekube.ConfigMapController)
		cmcleanup func(t *testing.T, cs kubernetes.Interface)
		name      string
		target    string
		status    int
	}{
		{
			name:     "Int",
			target:   "/variables/values/hub/mdaihub-sample/var/data_int",
			status:   http.StatusOK,
			out:      &map[string]string{},
			expected: &map[string]string{"data_int": "3"},
			valkey: func(t *testing.T, m *valkeymock.Client) {
				t.Helper()
				key := "variable/mdaihub-sample/data_int"
				m.EXPECT().
					Do(gomock.Any(), valkeymock.Match("GET", key)).
					Return(valkeymock.Result(
						valkeymock.ValkeyBlobString("3"),
					))
			},
		},
		{
			name:     "Boolean",
			target:   "/variables/values/hub/mdaihub-sample/var/data_boolean",
			status:   http.StatusOK,
			out:      &map[string]string{},
			expected: &map[string]string{"data_boolean": "true"},
			valkey: func(t *testing.T, m *valkeymock.Client) {
				t.Helper()
				key := "variable/mdaihub-sample/data_boolean"
				m.EXPECT().
					Do(gomock.Any(), valkeymock.Match("GET", key)).
					Return(valkeymock.Result(
						valkeymock.ValkeyBlobString("true"),
					))
			},
		},
		{
			name:     "String",
			target:   "/variables/values/hub/mdaihub-sample/var/data_string",
			status:   http.StatusOK,
			out:      &map[string]string{},
			expected: &map[string]string{"data_string": "foo"},
			valkey: func(t *testing.T, m *valkeymock.Client) {
				t.Helper()
				key := "variable/mdaihub-sample/data_string"
				m.EXPECT().
					Do(gomock.Any(), valkeymock.Match("GET", key)).
					Return(valkeymock.Result(
						valkeymock.ValkeyBlobString("foo"),
					))
			},
		},
		{
			name:   "Set",
			target: "/variables/values/hub/mdaihub-sample/var/data_set",
			status: http.StatusOK,
			out:    &map[string][]string{},
			expected: &map[string][]string{
				"data_set": {
					"manual_service_1",
					"manual_service_2",
					"manual_service_3",
				},
			},
			valkey: func(t *testing.T, m *valkeymock.Client) {
				t.Helper()
				key := "variable/mdaihub-sample/data_set"
				m.EXPECT().
					Do(gomock.Any(), valkeymock.Match("SMEMBERS", key)).
					Return(valkeymock.Result(
						valkeymock.ValkeyArray(
							valkeymock.ValkeyBlobString("manual_service_1"),
							valkeymock.ValkeyBlobString("manual_service_2"),
							valkeymock.ValkeyBlobString("manual_service_3")),
					))
			},
		},
		{
			name:   "Map",
			target: "/variables/values/hub/mdaihub-sample/var/data_map",
			status: http.StatusOK,
			out:    &map[string]map[string]string{},
			expected: &map[string]map[string]string{
				"data_map": {
					"attrib.1": "value1",
					"attrib.2": "value2",
					"attrib.3": "value3",
				},
			},
			valkey: func(t *testing.T, m *valkeymock.Client) {
				t.Helper()
				key := "variable/mdaihub-sample/data_map"
				m.EXPECT().
					Do(gomock.Any(), valkeymock.Match("HGETALL", key)).
					Return(valkeymock.Result(valkeymock.ValkeyMap(map[string]valkey.ValkeyMessage{
						"attrib.1": valkeymock.ValkeyBlobString("value1"),
						"attrib.2": valkeymock.ValkeyBlobString("value2"),
						"attrib.3": valkeymock.ValkeyBlobString("value3"),
					})))
			},
		},
		{
			name:     "String_NoValue",
			target:   "/variables/values/hub/mdaihub-sample/var/data_string",
			status:   http.StatusOK,
			out:      &map[string]string{},
			expected: &map[string]string{"data_string": ""},
			valkey: func(t *testing.T, m *valkeymock.Client) {
				t.Helper()
				key := "variable/mdaihub-sample/data_string"
				m.EXPECT().
					Do(gomock.Any(), valkeymock.Match("GET", key)).
					Return(valkeymock.Result(
						valkeymock.ValkeyNil(),
					))
			},
		},
		{
			name:     "NonExistentHub",
			target:   "/variables/values/hub/nonexistent_hub/var/data_string",
			status:   http.StatusNotFound,
			out:      new(string),
			expected: ptr("hub not found"),
		},
		{
			name:     "NonExistentVariable",
			target:   "/variables/values/hub/mdaihub-sample/var/nonexistent_variable",
			status:   http.StatusNotFound,
			out:      new(string),
			expected: ptr("variable not found"),
		},
		{
			name:     "UnsupportedVariableType",
			target:   "/variables/values/hub/mdaihub-sample/var/data_unsupported_type",
			status:   http.StatusInternalServerError,
			out:      new(string),
			expected: ptr("unsupported variable type booleaninttstring"),
			cmprepare: func(t *testing.T, clientset kubernetes.Interface, cmController *datacorekube.ConfigMapController) {
				t.Helper()

				ctx := t.Context()

				cm, err := clientset.CoreV1().ConfigMaps("mdai").Get(ctx, "mdaihub-sample-manual-variables", metav1.GetOptions{})
				require.NoError(t, err)

				cm.Data["data_unsupported_type"] = "booleaninttstring"

				_, err = clientset.CoreV1().ConfigMaps("mdai").Update(ctx, cm, metav1.UpdateOptions{})
				require.NoError(t, err)

				require.Eventually(t, func() bool {
					obj, exists, err := cmController.CmInformer.Informer().GetIndexer().GetByKey("mdai/mdaihub-sample-manual-variables")
					if err != nil || obj == nil || !exists {
						return false
					}
					cm := obj.(*corev1.ConfigMap) //nolint:forcetypeassert
					return cm.Data["data_unsupported_type"] == "booleaninttstring"
				}, 2*time.Second, 50*time.Millisecond)
			},
			cmcleanup: func(t *testing.T, cs kubernetes.Interface) {
				t.Helper()

				ctx := t.Context()
				cmClient := cs.CoreV1().ConfigMaps("mdai")

				cm, err := cmClient.Get(ctx, "mdaihub-sample-manual-variables", metav1.GetOptions{})
				require.NoError(t, err)

				delete(cm.Data, "data_unsupported_type")

				_, err = cmClient.Update(ctx, cm, metav1.UpdateOptions{})
				require.NoError(t, err)
			},
		},
	}

	clientset := newFakeClientset(t)
	deps := setupMocks(t, clientset)
	mux := NewRouter(t.Context(), deps)

	for _, tt := range getTests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.valkey != nil {
				tt.valkey(t, deps.ValkeyClient.(*valkeymock.Client)) //nolint:forcetypeassert
			}
			if tt.cmprepare != nil {
				tt.cmprepare(t, clientset, deps.ConfigMapController)
			}
			if tt.cmcleanup != nil {
				defer tt.cmcleanup(t, clientset)
			}

			req := httptest.NewRequest(http.MethodGet, tt.target, http.NoBody)
			rr := httptest.NewRecorder()

			mux.ServeHTTP(rr, req)

			assert.Equal(t, tt.status, rr.Code)

			err := json.Unmarshal(rr.Body.Bytes(), tt.out)
			require.NoError(t, err)

			switch out := tt.out.(type) {
			case *string:
				assert.Equal(t, *tt.expected.(*string), *out) //nolint:forcetypeassert
			case *map[string][]string:
				assert.Equal(t, *tt.expected.(*map[string][]string), *out) //nolint:forcetypeassert
			case *map[string]string:
				assert.Equal(t, *tt.expected.(*map[string]string), *out) //nolint:forcetypeassert
			case *map[string]map[string]string:
				assert.Equal(t, *tt.expected.(*map[string]map[string]string), *out) //nolint:forcetypeassert
			default:
				t.Fatalf("unsupported type: %T", out)
			}
		})
	}
}

type XaddMatcher struct{}

func (XaddMatcher) Matches(x any) bool {
	if cmd, ok := x.(valkey.Completed); ok {
		commands := cmd.Commands()
		return slices.Contains(commands, "XADD") && slices.Contains(commands, "mdai_hub_event_history")
	}
	return false
}

func (XaddMatcher) String() string {
	return "Wanted XADD to mdai_hub_event_history command"
}

func TestHandleDeleteVariables(t *testing.T) {
	deleteTests := []struct {
		name string
		body string
	}{
		{
			name: "string",
			body: `{"data":"data_string"}`,
		},
		{
			name: "boolean",
			body: `{"data":true}`,
		},
		{
			name: "int",
			body: `{"data":123}`,
		},
		{
			name: "set",
			body: `{"data":["data_set"]}`,
		},
		{
			name: "map",
			body: `{"data": ["attrib.111"]}`,
		},
	}

	clientset := newFakeClientset(t)
	deps := setupMocks(t, clientset)
	ctx := t.Context()
	mux := NewRouter(ctx, deps)

	for _, tt := range deleteTests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/variables/hub/mdaihub-sample/var/data_"+tt.name, bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")

			mockClient, ok := deps.ValkeyClient.(*valkeymock.Client)
			if !ok {
				t.Fatal("ValkeyClient is not a *valkeymock.Client")
			}
			mockClient.EXPECT().Do(ctx, XaddMatcher{}).Return(valkeymock.Result(valkeymock.ValkeyString(""))).Times(1)

			rr := httptest.NewRecorder()

			mux.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code)

			var result eventing.MdaiEvent

			err := json.Unmarshal(rr.Body.Bytes(), &result)
			require.NoError(t, err)

			assert.Equal(t, "manual_variables_api", result.Source)
			assert.Equal(t, "var.remove", result.Name)
			assert.Equal(t, "mdaihub-sample", result.HubName)
			assert.JSONEq(t, fmt.Sprintf(`{"variableRef":%q,"dataType":%q,"operation":"remove","data":%v}`, "data_"+tt.name, tt.name, stringifyData(t, tt.body)), result.Payload)
			assert.NotEmpty(t, result.ID)
			assert.NotZero(t, result.Timestamp)
			assert.WithinDuration(t, time.Now(), result.Timestamp, time.Minute)
		})
	}
}

func TestHandleSetVariables(t *testing.T) {
	setTests := []struct {
		name string
		body string
	}{
		{
			name: "string",
			body: `{"data":"data_string"}`,
		},
		{
			name: "boolean",
			body: `{"data":true}`,
		},
		{
			name: "int",
			body: `{"data":123}`,
		},
		{
			name: "set",
			body: `{"data":["data_set"]}`,
		},
		{
			name: "map",
			body: `{"data":{"attrib.111":"value.111"}}`,
		},
	}

	clientset := newFakeClientset(t)
	deps := setupMocks(t, clientset)
	ctx := t.Context()
	mux := NewRouter(ctx, deps)

	for _, tt := range setTests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/variables/hub/mdaihub-sample/var/data_"+tt.name, bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")

			mockClient, ok := deps.ValkeyClient.(*valkeymock.Client)
			if !ok {
				t.Fatal("ValkeyClient is not a *valkeymock.Client")
			}
			mockClient.EXPECT().Do(ctx, XaddMatcher{}).Return(valkeymock.Result(valkeymock.ValkeyString(""))).Times(1)

			rr := httptest.NewRecorder()

			mux.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusCreated, rr.Code)

			var result eventing.MdaiEvent

			err := json.Unmarshal(rr.Body.Bytes(), &result)
			require.NoError(t, err)

			assert.Equal(t, "manual_variables_api", result.Source)
			assert.Equal(t, "var.add", result.Name)
			assert.Equal(t, "mdaihub-sample", result.HubName)
			assert.JSONEq(t, fmt.Sprintf(`{"variableRef":%q,"dataType":%q,"operation":"add","data":%v}`, "data_"+tt.name, tt.name, stringifyData(t, tt.body)), result.Payload)
			assert.NotEmpty(t, result.ID)
			assert.NotZero(t, result.Timestamp)
			assert.WithinDuration(t, time.Now(), result.Timestamp, time.Minute)
		})
	}
}

func TestHandleSetVariables_InvalidRequestPayload(t *testing.T) {
	setTests := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name:     "string",
			body:     `{"data":true}`,
			expected: "Invalid request payload: String expected\n",
		},
		{
			name:     "boolean",
			body:     `{"data":"true"}`,
			expected: "Invalid request payload: Boolean expected\n",
		},
		{
			name:     "int",
			body:     `{"data":"123"}`,
			expected: "Invalid request payload: Int expected\n",
		},
		{
			name:     "int",
			body:     `{"data":"12.3"}`,
			expected: "Invalid request payload: Int expected\n",
		},
		{
			name:     "set",
			body:     `{"data":"set"}`,
			expected: "Invalid request payload: List expected\n",
		},
		{
			name:     "set",
			body:     `{"data":[123]}`,
			expected: "Invalid request payload: List expected\n",
		},
		{
			name:     "map",
			body:     `{"data":"map"}`,
			expected: "Invalid request payload: Map expected\n",
		},
		{
			name:     "map",
			body:     `{"data": {"foo":123}}`,
			expected: "Invalid request payload: Map expected\n",
		},
	}

	clientset := newFakeClientset(t)
	deps := setupMocks(t, clientset)
	mux := NewRouter(t.Context(), deps)

	for _, tt := range setTests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/variables/hub/mdaihub-sample/var/data_"+tt.name, bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()

			mux.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusBadRequest, rr.Code)
			assert.Equal(t, tt.expected, rr.Body.String())
		})
	}
}

func TestHandleDeleteVariables_InvalidRequestPayload(t *testing.T) {
	setTests := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name:     "string",
			body:     `{"data":true}`,
			expected: "Invalid request payload: String expected\n",
		},
		{
			name:     "boolean",
			body:     `{"data":"true"}`,
			expected: "Invalid request payload: Boolean expected\n",
		},
		{
			name:     "int",
			body:     `{"data":"123"}`,
			expected: "Invalid request payload: Int expected\n",
		},
		{
			name:     "int",
			body:     `{"data":12.3}`,
			expected: "Invalid request payload: Int expected\n",
		},
		{
			name:     "set",
			body:     `{"data":"set"}`,
			expected: "Invalid request payload: List expected\n",
		},
		{
			name:     "set",
			body:     `{"data":[123]}`,
			expected: "Invalid request payload: List expected\n",
		},
		{
			name:     "map",
			body:     `{"data":"map"}`,
			expected: "Invalid request payload: List expected\n",
		},
		{
			name:     "map",
			body:     `{"data": {"foo":123}}`,
			expected: "Invalid request payload: List expected\n",
		},
	}

	clientset := newFakeClientset(t)
	deps := setupMocks(t, clientset)
	mux := NewRouter(t.Context(), deps)

	for _, tt := range setTests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/variables/hub/mdaihub-sample/var/data_"+tt.name, bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()

			mux.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusBadRequest, rr.Code)
			assert.Equal(t, tt.expected, rr.Body.String())
		})
	}
}

func TestStringifyData(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "string",
			body: `{"data":"hello"}`,
			want: `"hello"`,
		},
		{
			name: "bool",
			body: `{"data":true}`,
			want: `"true"`,
		},
		{
			name: "int",
			body: `{"data":123}`,
			want: `"123"`,
		},
		{
			name: "array",
			body: `{"data":["foo","bar"]}`,
			want: `["foo","bar"]`,
		},
		{
			name: "map",
			body: `{"data":{"foo":"bar"}}`,
			want: `{"foo":"bar"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringifyData(t, tt.body)
			assert.JSONEq(t, tt.want, got)
		})
	}
}

func readPayloadFromFile(t *testing.T, fileName string) []byte {
	t.Helper()
	body, err := os.ReadFile(fileName) //nolint:gosec
	require.NoError(t, err)
	return body
}

func TestUpdateEventsHandler(t *testing.T) {
	const (
		post1Response = `{"message":"Processed Prometheus alerts", "skipped":0, "successful":3, "total":3}` + "\n"
		post2Response = `{"message":"Processed Prometheus alerts", "skipped":0, "successful":3, "total":3}` + "\n"
		post3Response = `{"message":"Processed Prometheus alerts", "skipped":0, "successful":2, "total":2}` + "\n"
		post4Response = `{"message":"Processed Prometheus alerts", "skipped":2, "successful":0, "total":2}` + "\n"
	)

	alertPostBody1 := readPayloadFromFile(t, alert1)
	clientset := newFakeClientset(t)
	deps := setupMocks(t, clientset)
	mux := NewRouter(t.Context(), deps)
	req := httptest.NewRequest(http.MethodPost, "/alerts/alertmanager", bytes.NewBuffer(alertPostBody1))
	req.Header.Set("Content-Type", "application/json")

	mockClient, ok := deps.ValkeyClient.(*valkeymock.Client)
	if !ok {
		t.Fatal("ValkeyClient is not a *valkeymock.Client")
	}
	mockClient.EXPECT().Do(gomock.Any(), XaddMatcher{}).Return(valkeymock.Result(valkeymock.ValkeyString(""))).Times(8)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)
	assert.JSONEq(t, post1Response, rr.Body.String())

	// one more time with different payload
	alertPostBody2 := readPayloadFromFile(t, alert2)
	req = httptest.NewRequest(http.MethodPost, "/alerts/alertmanager", bytes.NewBuffer(alertPostBody2))
	req.Header.Set("Content-Type", "application/json")

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)
	assert.JSONEq(t, post2Response, rr.Body.String())

	// one more time to emulate a scenario when alert was re-created or renamed
	alertPostBody3 := readPayloadFromFile(t, alert3)
	req = httptest.NewRequest(http.MethodPost, "/alerts/alertmanager", bytes.NewBuffer(alertPostBody3))
	req.Header.Set("Content-Type", "application/json")

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)
	assert.JSONEq(t, post3Response, rr.Body.String())

	// one more with skipped alerts
	req = httptest.NewRequest(http.MethodPost, "/alerts/alertmanager", bytes.NewBuffer(alertPostBody3))
	req.Header.Set("Content-Type", "application/json")

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)
	assert.JSONEq(t, post4Response, rr.Body.String())
}

func TestAlerts_Failuers(t *testing.T) {
	clientset := newFakeClientset(t)
	deps := setupMocks(t, clientset)
	mux := NewRouter(t.Context(), deps)

	// Prometheus JSON fail
	req := httptest.NewRequest(http.MethodPost, "/alerts/alertmanager", bytes.NewBufferString(`{"receiver":"foo","alerts": true}`))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Equal(t, "invalid Alertmanager payload\n", rr.Body.String())

	// io.ReadAll failure
	mux = NewRouter(t.Context(), deps)
	req = httptest.NewRequest(http.MethodPost, "/alerts/alertmanager", &errReader{})
	req.Header.Set("Content-Type", "application/json")

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Equal(t, "invalid Alertmanager payload\n", rr.Body.String())

	// bad json
	mux = NewRouter(t.Context(), deps)
	req = httptest.NewRequest(http.MethodPost, "/alerts/alertmanager", bytes.NewBufferString("foo"))
	req.Header.Set("Content-Type", "application/json")

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Equal(t, "invalid Alertmanager payload\n", rr.Body.String())
}

func TestAlerts_NotAllowed(t *testing.T) {
	clientset := newFakeClientset(t)
	deps := setupMocks(t, clientset)

	for _, method := range []string{http.MethodConnect, http.MethodOptions, http.MethodTrace, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		mux := NewRouter(t.Context(), deps)
		req := httptest.NewRequest(method, "/alerts/alertmanager", http.NoBody)
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		assert.Equal(t, "POST", rr.Header().Get("Allow"))
		assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
		assert.Equal(t, "Method Not Allowed\n", rr.Body.String())
	}
}

func TestAudit_Success(t *testing.T) {
	clientset := newFakeClientset(t)
	deps := setupMocks(t, clientset)
	mux := NewRouter(t.Context(), deps)

	deps.ValkeyClient.(*valkeymock.Client).EXPECT(). //nolint:forcetypeassert
								Do(gomock.Any(), valkeymock.Match("XREVRANGE", audit.MdaiHubEventHistoryStreamName, "+", "-")).
								Return(
			valkeymock.Result(
				valkeymock.ValkeyArray([]valkey.ValkeyMessage{ // Wrap outer result array
					valkeymock.ValkeyArray([]valkey.ValkeyMessage{ // One entry
						valkeymock.ValkeyString("1718920000000-0"), // entry ID
						valkeymock.ValkeyArray([]valkey.ValkeyMessage{ // fields
							valkeymock.ValkeyString("type"),
							valkeymock.ValkeyString("example_type"),
							valkeymock.ValkeyString("value"),
							valkeymock.ValkeyString(`{"foo":"bar"}`),
						}...),
					}...),
				}...),
			),
		).Times(1)

	req := httptest.NewRequest(http.MethodGet, "/audit", http.NoBody)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.JSONEq(t, `[{"type":"example_type","value":"{\"foo\":\"bar\"}"}]`+"\n", rr.Body.String())
}

func TestAudit_Fail(t *testing.T) {
	clientset := newFakeClientset(t)
	deps := setupMocks(t, clientset)
	mux := NewRouter(t.Context(), deps)

	deps.ValkeyClient.(*valkeymock.Client).EXPECT(). //nolint:forcetypeassert
								Do(gomock.Any(), valkeymock.Match("XREVRANGE", audit.MdaiHubEventHistoryStreamName, "+", "-")).
								Return(valkeymock.Result(valkeymock.ValkeyBlobString("foo"))).Times(1)

	req := httptest.NewRequest(http.MethodGet, "/audit", http.NoBody)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Equal(t, "Unable to fetch history from Valkey\n", rr.Body.String())
}

func TestAlets_TrailingJSON(t *testing.T) {
	clientset := newFakeClientset(t)
	deps := setupMocks(t, clientset)
	mux := NewRouter(t.Context(), deps)

	// trailing JSON after a valid object -> must be rejected
	req := httptest.NewRequest(
		http.MethodPost,
		"/alerts/alertmanager",
		bytes.NewBufferString(string(readPayloadFromFile(t, alert1))+" {}"), // second top-level JSON value
	)
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Equal(t, "request must contain a single JSON object\n", rr.Body.String())
}

func TestAlerts_BodyTooLarge(t *testing.T) {
	clientset := newFakeClientset(t)
	deps := setupMocks(t, clientset)
	mux := NewRouter(t.Context(), deps)
	tooBig := strings.Repeat("x", (10<<20)+1) // 10 MiB + 1 byte

	oversizedAlert := map[string]any{
		"receiver": "webhook",
		"status":   "firing",
		"alerts": []map[string]any{
			{
				"status": "firing",
				"labels": map[string]any{
					"service_name": tooBig, // long string forces read beyond MaxBytesReader
				},
			},
		},
	}

	body, err := json.Marshal(oversizedAlert)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/alerts/alertmanager", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rr.Code)
	assert.Equal(t, "request body too large (max 10MiB)\n", rr.Body.String())
}

func TestAlerts_WrongContentType(t *testing.T) {
	clientset := newFakeClientset(t)
	deps := setupMocks(t, clientset)
	mux := NewRouter(t.Context(), deps)

	body := strings.NewReader(`{"status":"firing","alerts":[]}`)

	// Content-Type wrong -> 415 Unsupported Media Type (middleware)
	req := httptest.NewRequest(http.MethodPost, "/alerts/alertmanager", body)
	req.Header.Set("Content-Type", "text/plain")

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnsupportedMediaType, rr.Code)
	assert.Equal(t, "Content-Type header must be application/json\n", rr.Body.String())
}
