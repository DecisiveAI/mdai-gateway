[![Chores](https://github.com/mydecisive/mdai-gateway/actions/workflows/chores.yml/badge.svg)](https://github.com/mydecisive/mdai-gateway/actions/workflows/chores.yml)
[![codecov](https://codecov.io/gh/MyDecisive/mdai-gateway/graph/badge.svg?token=UPHRBSXOON)](https://codecov.io/gh/MyDecisive/mdai-gateway)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/mdai-gateway)](https://artifacthub.io/packages/search?repo=mdai-gateway)

# MDAI Gateway

# INSTALL
```sh
helm upgrade --install --create-namespace --namespace mdai mdai-gateway ./deployment
```

`testdata` contains
* JSON POST bodies (to simulate data from Alert Manager)

# To simulate an alert via curl
```sh
curl -X POST -H "Content-Type: application/json" -d@testdata/alert_test.json http://localhost:8081/alerts/alertmanager
```
```sh
curl -X POST -H "Content-Type: application/json" -d@testdata/alert_top_talkers.json http://localhost:8081/alerts/alertmanager
```
```sh
curl -X POST -H "Content-Type: application/json" -d@testdata/alert_anomalous_error_rate.json http://localhost:8081/alerts/alertmanager
```

# To simulate a manual var update event via curl
Only manual variables can be updated or deleted. The sample hub below exposes manual variables such as `data_string`, `data_boolean`, `data_int`, `data_set`, and `data_map`.

Add to string:
```sh
curl -X POST -H "Content-Type: application/json" -d@testdata/var-test.json \
  http://localhost:8081/variables/hub/mdaihub-sample/var/data_string
```
Remove from string:
```sh
curl -X DELETE -H "Content-Type: application/json" -d@testdata/var-test-empty.json \
  http://localhost:8081/variables/hub/mdaihub-sample/var/data_string
```
Add to set:
```sh
curl -X POST -H "Content-Type: application/json" -d@testdata/var-test-set.json \
  http://localhost:8081/variables/hub/mdaihub-sample/var/data_set
```
Remove from set:
```sh
curl -X DELETE -H "Content-Type: application/json" -d@testdata/var-test-set.json \
  http://localhost:8081/variables/hub/mdaihub-sample/var/data_set
```
Add to map:
```sh
curl -X POST -H "Content-Type: application/json" -d@testdata/var-test-map.json \
  http://localhost:8081/variables/hub/mdaihub-sample/var/data_map
```
Remove from map:
```sh
curl -X DELETE -H "Content-Type: application/json" -d@testdata/var-test-map-delete.json \
  http://localhost:8081/variables/hub/mdaihub-sample/var/data_map
```
# API
## Variables Schema API

### List variables
#### All hubs
request:
```
GET /variables/list
```
response:
```
{hubName:{variableName: schema}}
```
example:
```
{
  "mdaihub-sample": {
    "data_string": {
      "type": "manual",
      "dataType": "string",
      "storageType": "mdai-valkey",
      "serializeAs": [{"name": "DATA_STRING"}]
    },
    "computed_string": {
      "type": "computed",
      "dataType": "string",
      "storageType": "mdai-valkey",
      "serializeAs": [{"name": "COMPUTED_STRING"}]
    },
    "meta_priority_list": {
      "type": "meta",
      "dataType": "metaPriorityList",
      "storageType": "mdai-valkey",
      "variableRefs": ["data_string", "data_set"],
      "serializeAs": [{"name": "META_PRIORITY_LIST"}]
    }
  }
}
```


#### Given hub
request:
```
GET /variables/list/hub/{hubName}
```
response:
```
{variableName: schema}
```
example:
```
{
  "data_boolean": {
    "type": "manual",
    "dataType": "boolean",
    "storageType": "mdai-valkey",
    "serializeAs": [{"name": "DATA_BOOLEAN"}]
  },
  "meta_hash_set": {
    "type": "meta",
    "dataType": "metaHashSet",
    "storageType": "mdai-valkey",
    "variableRefs": ["data_string", "data_set"],
    "serializeAs": [{"name": "META_HASH_SET"}]
  }
}
```

### Get variable value(s)
#### All values for a hub
request:
```
GET /variables/values/hub/{hubName}
```
response:
```
{
  "data_string": "string value",
  "data_boolean": true,
  "data_int": 123,
  "data_set": ["service1", "service2"],
  "data_map": {"attrib.111": "value.111"},
  "computed_string": "derived value",
  "meta_hash_set": "service|critical",
  "meta_priority_list": ["default", "service_list"]
}
```

#### Single variable
request:
```
GET /variables/values/hub/{hubName}/var/{varName}
```
response:
```
{"data_string":"string value"}
{"data_boolean":true}
{"data_int":123}
{"data_set":["service1","service2"]}
{"data_map":{"attrib.111":"value.111","attrib.222":"value.222"}}
{"meta_hash_set":"service|critical"}
{"meta_priority_list":["default","service_list"]}
{"computed_string":"derived value"}
{"data_string":null}
```
If the variable exists in schema but has no backing value in storage yet, the API returns `null`.

### Set variable value(s)
request:
```
POST /variables/hub/{hubName}/var/{varName}
```
Only variables with type manual can be updated. Computed and meta variables return `409 Conflict` with:
```
"only manual variables can be updated or deleted"
```
#### payloads:
string:
```
{"data": variableValue}
```
examples: ```{"data": "string_value"}```


boolean:
```
{"data": variableValue}
```
examples: ```{"data": true}```


integer:
```
{"data": variableValue}
```
examples: ```{"data": 123}```


set:
```
{"data":[elementValue]}
```
example: ```{"data":["service1", "service2"]}```


map:
```
{"data":{elementKey: elementValue}}
```
example: ```{"data":{"attrib.111": "value.111", "attrib.222": "value.222"}}```



### Delete variable value(s)
request:
```
DELETE /variables/hub/{hubName}/var/{varName}
```
Only variables with schema `type: "manual"` can be deleted. Computed and meta variables return `409 Conflict` with:
```
"only manual variables can be updated or deleted"
```
#### payloads:
string:
```
{"data": variableValue}
```
examples: ```{"data": "string_value"}```


boolean:
```
{"data": variableValue}
```
examples: ```{"data": true}```


integer:
```
{"data": variableValue}
```
examples: ```{"data": 123}```


set:
```
{"data":[elementValue]}
```
example: ```{"data":["service1", "service2"]}```


map:
```
{"data":[elementKey]}
```
example: ```{"data":["attrib.111", "attrib.222"]}```
