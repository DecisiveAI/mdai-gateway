[![Chores](https://github.com/DecisiveAI/mdai-gateway/actions/workflows/chores.yml/badge.svg)](https://github.com/DecisiveAI/mdai-gateway/actions/workflows/chores.yml)
[![codecov](https://codecov.io/gh/DecisiveAI/mdai-gateway/graph/badge.svg?token=UPHRBSXOON)](https://codecov.io/gh/DecisiveAI/mdai-gateway)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/mdai-gateway)](https://artifacthub.io/packages/search?repo=mdai-gateway)

# MDAI Gateway

# INSTALL
```sh
helm upgrade --install --create-namespace --namespace mdai mdai-gateway ./deployment
```

`testdata` contains
* JSON POST bodies (to simulate data from Alert Manager)

# to simulate an alert via curl
```sh
curl -X POST -H "Content-Type: application/json" -d@testdata/alert_test.json http://localhost:8081/alerts/alertmanager
```
```sh
curl -X POST -H "Content-Type: application/json" -d@testdata/alert_top_talkers.json http://localhost:8081/alerts/alertmanager
```

# to simulate a var update event via curl
```sh
curl -X POST -H "Content-Type: application/json" -d@testdata/var-test.json \
  http://localhost:8081/variables/hub/mdaihub-sample/var/manual_filter
```

# API
## Manual Variables API

### List variables
#### All hubs
request:
```
GET /variables/list/
```
response:
```
{hubName:{variableName: variableType}}
```
example:
```
{"mdaihub-sample":{"manual_filter":"string","service_list_manual":"set"},"mdaihub-second":{"manual_severity":"int","attributes":"map"}}
```


#### Given hub
request:
```
GET /variables/list/hub/{hubName}/
```
response:
```
{variableName: variableType}
```
example:
```
{"manual_filter":"string","service_list_manual":"set"}
```

### Get variable value(s)
request:
```
GET /variables/values/hub/{hubName}/var/{varName}/
```
#### response:

integer, boolean, string:
```
{variableName: variableValue}
```
set:
```
{variableName: [elementValue]}
```
map:
```
{variableName:{elementKey: elementValue}}
```


### Set variable value(s)
request:
```
POST /variables/hub/{hubName}/var/{varName}/
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
/variables/hub/{hubName}/var/{varName}/
request:
```
DELETE /variables/hub/{hubName}/var/{varName}/
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
