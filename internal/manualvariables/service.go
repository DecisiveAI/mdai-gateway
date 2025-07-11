package manualvariables

import (
	"net/http"

	datacorekube "github.com/decisiveai/mdai-data-core/kube"
	"github.com/decisiveai/mdai-gateway/internal/valkey"
	corev1 "k8s.io/api/core/v1"
)

type ByHub map[string]map[string]string

func Get(cmController *datacorekube.ConfigMapController) (ByHub, error) {
	cmController.Lock.RLock()
	defer cmController.Lock.RUnlock()

	hubMap := make(ByHub)

	indexer := cmController.CmInformer.Informer().GetIndexer()
	hubNames := indexer.ListIndexFuncValues(datacorekube.ByHub)
	for _, hubName := range hubNames {
		objs, err := indexer.ByIndex(datacorekube.ByHub, hubName)
		if err != nil {
			continue
		}
		for _, obj := range objs {
			cm := obj.(*corev1.ConfigMap) //nolint:forcetypeassert
			hubMap[hubName] = cm.Data
		}
	}
	return hubMap, nil
}

var (
	ErrMissingQueryParams     = HTTPError{"missing hub or variable name", http.StatusBadRequest}
	ErrNoManualVariablesFound = HTTPError{"no hubs with manual variables found", http.StatusNotFound}
	ErrHubNotFound            = HTTPError{"hub not found", http.StatusNotFound}
	ErrVariableNotFound       = HTTPError{"variable not found", http.StatusNotFound}
)

func GetVarType(hubName string, varName string, hubsVariables ByHub) (valkey.VariableType, error) {
	if len(hubsVariables) == 0 {
		return "", ErrNoManualVariablesFound
	}

	if hubName == "" || varName == "" {
		return "", ErrMissingQueryParams
	}

	hubFound := hubsVariables[hubName]
	if hubFound == nil {
		return "", ErrHubNotFound
	}

	varType, ok := hubFound[varName]
	if !ok {
		return "", ErrVariableNotFound
	}

	return valkey.VariableType(varType), nil
}
