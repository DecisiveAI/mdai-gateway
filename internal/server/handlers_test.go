package server

import (
	"bytes"
	"encoding/json"
	"errors"
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
	"github.com/mydecisive/mdai-gateway/internal/variables"
	"github.com/mydecisive/mdai-gateway/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	valkeymock "github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	alert3 = "../../testdata/alert_post_body_3.json"
	alert2 = "../../testdata/alert_post_body_2.json"
	alert1 = "../../testdata/alert_post_body_1.json"
)

func TestGetConfiguredVariablesSchema(t *testing.T) {
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

	hubMap, err := cmController.GetAllHubsVariablesSchemaConfigMapData()

	require.NoError(t, err)
	assert.Len(t, hubMap, 1)

	// go through variables
	require.Contains(t, hubMap, "mdaihub-sample")
	require.IsType(t, map[string]string{}, hubMap["mdaihub-sample"])

	assert.Equal(t, sampleHubVariablesSchemaRaw(), hubMap["mdaihub-sample"])
}

func TestHandleListVariables(t *testing.T) {
	expectedSchemas := expectedHubVariablesSchema(t)

	listTests := []struct {
		expected any
		name     string
		target   string
		status   int
	}{
		{
			name:   "List",
			target: "/variables/list",
			status: http.StatusOK,
			expected: variables.ByHub{
				"mdaihub-sample": expectedSchemas,
			},
		},
		{
			name:     "ListHub",
			target:   "/variables/list/hub/mdaihub-sample",
			status:   http.StatusOK,
			expected: expectedSchemas,
		},
		{
			name:     "ListHub_NonExistent",
			target:   "/variables/list/hub/nonexistent_hub",
			status:   http.StatusNotFound,
			expected: "no variables found for hub",
		},
	}

	clientset := newFakeClientset(t)
	deps := setupReadOnlyMocks(t, clientset)
	mux := NewRouter(t.Context(), deps)

	for _, tt := range listTests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.target, http.NoBody)
			rr := httptest.NewRecorder()

			mux.ServeHTTP(rr, req)

			assert.Equal(t, tt.status, rr.Code)

			expectedBody, err := json.Marshal(tt.expected)
			require.NoError(t, err)
			assert.JSONEq(t, string(expectedBody), rr.Body.String())
		})
	}
}

func TestHandleGetVariables(t *testing.T) {
	getTests := []struct {
		expected  any
		valkey    func(t *testing.T, m *valkeymock.Client)
		cmprepare func(t *testing.T, cs kubernetes.Interface, cmController *datacorekube.HubConfigMapController)
		cmcleanup func(t *testing.T, cs kubernetes.Interface, cmController *datacorekube.HubConfigMapController)
		name      string
		target    string
		status    int
	}{
		{
			name:     "Int",
			target:   "/variables/values/hub/mdaihub-sample/var/data_int",
			status:   http.StatusOK,
			expected: map[string]int{"data_int": 3},
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
			expected: map[string]bool{"data_boolean": true},
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
			expected: map[string]string{"data_string": "foo"},
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
			name:     "ComputedString",
			target:   "/variables/values/hub/mdaihub-sample/var/computed_string",
			status:   http.StatusOK,
			expected: map[string]string{"computed_string": "derived"},
			valkey: func(t *testing.T, m *valkeymock.Client) {
				t.Helper()
				key := "variable/mdaihub-sample/computed_string"
				m.EXPECT().
					Do(gomock.Any(), valkeymock.Match("GET", key)).
					Return(valkeymock.Result(
						valkeymock.ValkeyBlobString("derived"),
					))
			},
		},
		{
			name:   "Set",
			target: "/variables/values/hub/mdaihub-sample/var/data_set",
			status: http.StatusOK,
			expected: map[string][]string{
				"data_set": {"manual_service_1", "manual_service_2", "manual_service_3"},
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
			expected: map[string]map[string]string{
				"data_map": {"attrib.1": "value1", "attrib.2": "value2", "attrib.3": "value3"},
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
			status:   http.StatusNotFound,
			expected: "variable has no value",
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
			name:     "Set_NoValue",
			target:   "/variables/values/hub/mdaihub-sample/var/data_set",
			status:   http.StatusNotFound,
			expected: "variable has no value",
			valkey: func(t *testing.T, m *valkeymock.Client) {
				t.Helper()
				key := "variable/mdaihub-sample/data_set"
				m.EXPECT().
					Do(gomock.Any(), valkeymock.Match("SMEMBERS", key)).
					Return(valkeymock.Result(valkeymock.ValkeyArray()))
			},
		},
		{
			name:     "Map_NoValue",
			target:   "/variables/values/hub/mdaihub-sample/var/data_map",
			status:   http.StatusNotFound,
			expected: "variable has no value",
			valkey: func(t *testing.T, m *valkeymock.Client) {
				t.Helper()
				key := "variable/mdaihub-sample/data_map"
				m.EXPECT().
					Do(gomock.Any(), valkeymock.Match("HGETALL", key)).
					Return(valkeymock.Result(valkeymock.ValkeyMap(map[string]valkey.ValkeyMessage{})))
			},
		},
		{
			name:     "MetaHashSet",
			target:   "/variables/values/hub/mdaihub-sample/var/meta_hash_set",
			status:   http.StatusOK,
			expected: map[string]string{"meta_hash_set": "service|critical"},
			valkey: func(t *testing.T, m *valkeymock.Client) {
				t.Helper()
				key := "variable/mdaihub-sample/meta_hash_set"
				m.EXPECT().
					Do(gomock.Any(), valkeymock.Match("HASHSET.LOOKUP", key)).
					Return(valkeymock.Result(
						valkeymock.ValkeyBlobString("service|critical"),
					))
			},
		},
		{
			name:     "MetaPriorityList",
			target:   "/variables/values/hub/mdaihub-sample/var/meta_priority_list",
			status:   http.StatusOK,
			expected: map[string][]string{"meta_priority_list": {"alpha", "beta"}},
			valkey: func(t *testing.T, m *valkeymock.Client) {
				t.Helper()
				key := "variable/mdaihub-sample/meta_priority_list"
				m.EXPECT().
					Do(gomock.Any(), valkeymock.Match("PRIORITYLIST.GET", key)).
					Return(valkeymock.Result(
						valkeymock.ValkeyArray(
							valkeymock.ValkeyBlobString("alpha"),
							valkeymock.ValkeyBlobString("beta"),
						),
					))
			},
		},
		{
			name:     "NonExistentHub",
			target:   "/variables/values/hub/nonexistent_hub/var/data_string",
			status:   http.StatusNotFound,
			expected: "no variables found for hub",
		},
		{
			name:     "NonExistentVariable",
			target:   "/variables/values/hub/mdaihub-sample/var/nonexistent_variable",
			status:   http.StatusNotFound,
			expected: "variable not found",
		},
		{
			name:     "UnsupportedVariableType",
			target:   "/variables/values/hub/mdaihub-sample/var/data_unsupported_type",
			status:   http.StatusInternalServerError,
			expected: "failed to read variable value",
			cmprepare: func(t *testing.T, clientset kubernetes.Interface, cmController *datacorekube.HubConfigMapController) {
				t.Helper()

				updateSchemaConfigMap(t, clientset, cmController, func(cm *corev1.ConfigMap) {
					cm.Data["data_unsupported_type"] = `{"type":"manual","dataType":"booleaninttstring","storageType":"mdai-valkey"}`
				})
			},
			cmcleanup: func(t *testing.T, cs kubernetes.Interface, cmController *datacorekube.HubConfigMapController) {
				t.Helper()

				updateSchemaConfigMap(t, cs, cmController, func(cm *corev1.ConfigMap) {
					delete(cm.Data, "data_unsupported_type")
				})
			},
		},
		{
			// A value that exists but cannot be parsed into its declared type is data
			// corruption (writable only out-of-band; the gateway's write path canonicalizes).
			// It is the client's data, not a server fault, so the contract is 422 — not 500.
			name:     "Int_CorruptStoredValue",
			target:   "/variables/values/hub/mdaihub-sample/var/data_int",
			status:   http.StatusUnprocessableEntity,
			expected: "stored value is not valid for its data type",
			valkey: func(t *testing.T, m *valkeymock.Client) {
				t.Helper()
				key := "variable/mdaihub-sample/data_int"
				m.EXPECT().
					Do(gomock.Any(), valkeymock.Match("GET", key)).
					Return(valkeymock.Result(valkeymock.ValkeyBlobString("not-an-int")))
			},
		},
	}

	clientset := newFakeClientset(t)
	deps := setupReadOnlyMocks(t, clientset)
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
				defer tt.cmcleanup(t, clientset, deps.ConfigMapController)
			}

			req := httptest.NewRequest(http.MethodGet, tt.target, http.NoBody)
			rr := httptest.NewRecorder()

			mux.ServeHTTP(rr, req)

			assert.Equal(t, tt.status, rr.Code)

			expectedBody, err := json.Marshal(tt.expected)
			require.NoError(t, err)
			assert.JSONEq(t, string(expectedBody), rr.Body.String())
		})
	}
}

func TestHandleGetVariables_AppliesDefaultOnNotFound(t *testing.T) {
	clientset := newFakeClientset(t)
	deps := setupReadOnlyMocks(t, clientset)

	updateSchemaConfigMap(t, clientset, deps.ConfigMapController, func(cm *corev1.ConfigMap) {
		cm.Data["sampling_rate"] = `{"type":"manual","dataType":"int","storageType":"mdai-valkey","default":100,"serializeAs":[{"name":"SAMPLING_RATE"}]}`
	})
	t.Cleanup(func() {
		updateSchemaConfigMap(t, clientset, deps.ConfigMapController, func(cm *corev1.ConfigMap) {
			delete(cm.Data, "sampling_rate")
		})
	})

	deps.ValkeyClient.(*valkeymock.Client).EXPECT(). //nolint:forcetypeassert
								Do(gomock.Any(), valkeymock.Match("GET", "variable/mdaihub-sample/sampling_rate")).
								Return(valkeymock.Result(valkeymock.ValkeyNil()))

	mux := NewRouter(t.Context(), deps)
	req := httptest.NewRequest(http.MethodGet, "/variables/values/hub/mdaihub-sample/var/sampling_rate", http.NoBody)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.JSONEq(t, `{"sampling_rate":100}`, rr.Body.String())
}

func TestHandleGetVariables_StoredValueWinsOverDefault(t *testing.T) {
	clientset := newFakeClientset(t)
	deps := setupReadOnlyMocks(t, clientset)

	updateSchemaConfigMap(t, clientset, deps.ConfigMapController, func(cm *corev1.ConfigMap) {
		cm.Data["sampling_rate"] = `{"type":"manual","dataType":"int","storageType":"mdai-valkey","default":100,"serializeAs":[{"name":"SAMPLING_RATE"}]}`
	})
	t.Cleanup(func() {
		updateSchemaConfigMap(t, clientset, deps.ConfigMapController, func(cm *corev1.ConfigMap) {
			delete(cm.Data, "sampling_rate")
		})
	})

	deps.ValkeyClient.(*valkeymock.Client).EXPECT(). //nolint:forcetypeassert
								Do(gomock.Any(), valkeymock.Match("GET", "variable/mdaihub-sample/sampling_rate")).
								Return(valkeymock.Result(valkeymock.ValkeyBlobString("50")))

	mux := NewRouter(t.Context(), deps)
	req := httptest.NewRequest(http.MethodGet, "/variables/values/hub/mdaihub-sample/var/sampling_rate", http.NoBody)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.JSONEq(t, `{"sampling_rate":50}`, rr.Body.String())
}

func TestHandleGetHubVariableValues(t *testing.T) {
	tests := []struct {
		expected any
		valkey   func(t *testing.T, m *valkeymock.Client)
		name     string
		target   string
		status   int
	}{
		{
			name:   "AllValues",
			target: "/variables/values/hub/mdaihub-sample",
			status: http.StatusOK,
			expected: map[string]any{
				"data_boolean":       true,
				"data_int":           3,
				"data_string":        "foo",
				"computed_string":    "derived",
				"data_set":           []string{"manual_service_1", "manual_service_2", "manual_service_3"},
				"data_map":           map[string]string{"attrib.1": "value1", "attrib.2": "value2", "attrib.3": "value3"},
				"meta_hash_set":      "service|critical",
				"meta_priority_list": []string{"alpha", "beta"},
			},
			valkey: func(t *testing.T, m *valkeymock.Client) {
				t.Helper()

				m.EXPECT().
					Do(gomock.Any(), valkeymock.Match("GET", "variable/mdaihub-sample/data_boolean")).
					Return(valkeymock.Result(valkeymock.ValkeyBlobString("true")))
				m.EXPECT().
					Do(gomock.Any(), valkeymock.Match("GET", "variable/mdaihub-sample/data_int")).
					Return(valkeymock.Result(valkeymock.ValkeyBlobString("3")))
				m.EXPECT().
					Do(gomock.Any(), valkeymock.Match("GET", "variable/mdaihub-sample/data_string")).
					Return(valkeymock.Result(valkeymock.ValkeyBlobString("foo")))
				m.EXPECT().
					Do(gomock.Any(), valkeymock.Match("GET", "variable/mdaihub-sample/computed_string")).
					Return(valkeymock.Result(valkeymock.ValkeyBlobString("derived")))
				m.EXPECT().
					Do(gomock.Any(), valkeymock.Match("SMEMBERS", "variable/mdaihub-sample/data_set")).
					Return(valkeymock.Result(valkeymock.ValkeyArray(
						valkeymock.ValkeyBlobString("manual_service_1"),
						valkeymock.ValkeyBlobString("manual_service_2"),
						valkeymock.ValkeyBlobString("manual_service_3"),
					)))
				m.EXPECT().
					Do(gomock.Any(), valkeymock.Match("HGETALL", "variable/mdaihub-sample/data_map")).
					Return(valkeymock.Result(valkeymock.ValkeyMap(map[string]valkey.ValkeyMessage{
						"attrib.1": valkeymock.ValkeyBlobString("value1"),
						"attrib.2": valkeymock.ValkeyBlobString("value2"),
						"attrib.3": valkeymock.ValkeyBlobString("value3"),
					})))
				m.EXPECT().
					Do(gomock.Any(), valkeymock.Match("HASHSET.LOOKUP", "variable/mdaihub-sample/meta_hash_set")).
					Return(valkeymock.Result(valkeymock.ValkeyBlobString("service|critical")))
				m.EXPECT().
					Do(gomock.Any(), valkeymock.Match("PRIORITYLIST.GET", "variable/mdaihub-sample/meta_priority_list")).
					Return(valkeymock.Result(valkeymock.ValkeyArray(
						valkeymock.ValkeyBlobString("alpha"),
						valkeymock.ValkeyBlobString("beta"),
					)))
			},
		},
		{
			name:     "NonExistentHub",
			target:   "/variables/values/hub/nonexistent_hub",
			status:   http.StatusNotFound,
			expected: "no variables found for hub",
		},
	}

	clientset := newFakeClientset(t)
	deps := setupReadOnlyMocks(t, clientset)
	mux := NewRouter(t.Context(), deps)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.valkey != nil {
				tt.valkey(t, deps.ValkeyClient.(*valkeymock.Client)) //nolint:forcetypeassert
			}

			req := httptest.NewRequest(http.MethodGet, tt.target, http.NoBody)
			rr := httptest.NewRecorder()

			mux.ServeHTTP(rr, req)

			assert.Equal(t, tt.status, rr.Code)

			expectedBody, err := json.Marshal(tt.expected)
			require.NoError(t, err)
			assert.JSONEq(t, string(expectedBody), rr.Body.String())
		})
	}
}

// A single variable whose stored value is corrupt must not take down the whole hub
// listing: the offending variable is encoded as null, every other variable still returns,
// and the request stays 200.
func TestHandleGetHubVariableValues_CorruptValueDoesNotFailWholeRequest(t *testing.T) {
	clientset := newFakeClientset(t)
	deps := setupReadOnlyMocks(t, clientset)

	// Reduce the sample schema to two manual scalars so the bulk read touches only these.
	updateSchemaConfigMap(t, clientset, deps.ConfigMapController, func(cm *corev1.ConfigMap) {
		for k := range cm.Data {
			delete(cm.Data, k)
		}
		cm.Data["ok_string"] = `{"type":"manual","dataType":"string","storageType":"mdai-valkey"}`
		cm.Data["bad_int"] = `{"type":"manual","dataType":"int","storageType":"mdai-valkey"}`
	})

	m := deps.ValkeyClient.(*valkeymock.Client) //nolint:forcetypeassert
	m.EXPECT().
		Do(gomock.Any(), valkeymock.Match("GET", "variable/mdaihub-sample/ok_string")).
		Return(valkeymock.Result(valkeymock.ValkeyBlobString("hello"))).AnyTimes()
	m.EXPECT().
		Do(gomock.Any(), valkeymock.Match("GET", "variable/mdaihub-sample/bad_int")).
		Return(valkeymock.Result(valkeymock.ValkeyBlobString("not-an-int"))).AnyTimes()

	mux := NewRouter(t.Context(), deps)
	req := httptest.NewRequest(http.MethodGet, "/variables/values/hub/mdaihub-sample", http.NoBody)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.JSONEq(t, `{"ok_string":"hello","bad_int":null}`, rr.Body.String())
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
		name         string
		body         string
		expectedData string // expected JSON for the event payload's "data" field
	}{
		// Scalars: DELETE ignores the body and publishes data:null.
		{name: "string", body: `{"data":"data_string"}`, expectedData: "null"},
		{name: "string-no-body", body: ``, expectedData: "null"},
		{name: "string-wrong-type-body", body: `{"data":123}`, expectedData: "null"},
		{name: "boolean", body: `{"data":true}`, expectedData: "null"},
		{name: "int", body: `{"data":123}`, expectedData: "null"},
		// Collections: DELETE still parses the body (element-level removal).
		{name: "set", body: `{"data":["data_set"]}`, expectedData: `["data_set"]`},
		{name: "map", body: `{"data": ["attrib.111"]}`, expectedData: `["attrib.111"]`},
	}

	clientset := newFakeClientset(t)
	deps := setupMocks(t, clientset)
	ctx := t.Context()
	mux := NewRouter(ctx, deps)

	for _, tt := range deleteTests {
		t.Run(tt.name, func(t *testing.T) {
			urlVar := tt.name
			// Subtests share the data_<dataType> URL convention used by setupMocks.
			for _, dt := range []string{"string", "boolean", "int", "set", "map"} {
				if strings.HasPrefix(tt.name, dt) {
					urlVar = dt
					break
				}
			}

			req := httptest.NewRequest(http.MethodDelete, "/variables/hub/mdaihub-sample/var/data_"+urlVar, bytes.NewBufferString(tt.body))
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
			assert.JSONEq(t, fmt.Sprintf(`{"variableRef":%q,"dataType":%q,"operation":"remove","data":%s}`, "data_"+urlVar, urlVar, tt.expectedData), result.Payload)
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

func TestHandleSetVariables_PublishFailureHidesInternalError(t *testing.T) {
	clientset := newFakeClientset(t)
	deps := setupMocks(t, clientset)
	ctx := t.Context()

	mockPublisher := &mocks.MockPublisher{}
	mockPublisher.On("Publish", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("nats auth failed"))
	deps.EventPublisher = mockPublisher
	mux := NewRouter(ctx, deps)

	mockClient, ok := deps.ValkeyClient.(*valkeymock.Client)
	if !ok {
		t.Fatal("ValkeyClient is not a *valkeymock.Client")
	}
	mockClient.EXPECT().Do(ctx, XaddMatcher{}).Return(valkeymock.Result(valkeymock.ValkeyString(""))).Times(1)

	req := httptest.NewRequest(http.MethodPost, "/variables/hub/mdaihub-sample/var/data_string", bytes.NewBufferString(`{"data":"value"}`))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Equal(t, "Failed to publish event\n", rr.Body.String())
	mockPublisher.AssertExpectations(t)
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
	deps := setupReadOnlyMocks(t, clientset)
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
	// Scalar DELETEs intentionally accept any body (covered by TestHandleDeleteVariables).
	// Only collection DELETEs continue to validate the body shape.
	setTests := []struct {
		name     string
		body     string
		expected string
	}{
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
	deps := setupReadOnlyMocks(t, clientset)
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

func TestHandleSetDeleteVariables_NonExistentHub(t *testing.T) {
	clientset := newFakeClientset(t)
	deps := setupReadOnlyMocks(t, clientset)
	mux := NewRouter(t.Context(), deps)

	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/variables/hub/nonexistent_hub/var/data_string", bytes.NewBufferString(`{"data":"value"}`))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()

			mux.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusNotFound, rr.Code)

			var result string
			err := json.Unmarshal(rr.Body.Bytes(), &result)
			require.NoError(t, err)
			assert.Equal(t, "no variables found for hub", result)
		})
	}
}

func TestHandleSetDeleteVariables_RejectsNonManual(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		target   string
		body     string
		expected string
	}{
		{
			name:     "post computed variable",
			method:   http.MethodPost,
			target:   "/variables/hub/mdaihub-sample/var/computed_string",
			body:     `{"data":"value"}`,
			expected: variables.ErrVariableNotManual.Error(),
		},
		{
			name:     "delete computed variable",
			method:   http.MethodDelete,
			target:   "/variables/hub/mdaihub-sample/var/computed_string",
			body:     `{"data":"value"}`,
			expected: variables.ErrVariableNotManual.Error(),
		},
		{
			name:     "post meta variable",
			method:   http.MethodPost,
			target:   "/variables/hub/mdaihub-sample/var/meta_hash_set",
			body:     `{"data":"value"}`,
			expected: variables.ErrVariableNotManual.Error(),
		},
		{
			name:     "delete meta variable",
			method:   http.MethodDelete,
			target:   "/variables/hub/mdaihub-sample/var/meta_priority_list",
			body:     `{"data":["value"]}`,
			expected: variables.ErrVariableNotManual.Error(),
		},
	}

	clientset := newFakeClientset(t)
	deps := setupReadOnlyMocks(t, clientset)
	mux := NewRouter(t.Context(), deps)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.target, bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusConflict, rr.Code)

			expectedBody, err := json.Marshal(tt.expected)
			require.NoError(t, err)
			assert.JSONEq(t, string(expectedBody), rr.Body.String())
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
	deps := setupReadOnlyMocks(t, clientset)
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
	deps := setupReadOnlyMocks(t, clientset)

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
	deps := setupReadOnlyMocks(t, clientset)
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
	deps := setupReadOnlyMocks(t, clientset)
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
	deps := setupReadOnlyMocks(t, clientset)
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
	deps := setupReadOnlyMocks(t, clientset)
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
	deps := setupReadOnlyMocks(t, clientset)
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
