# Variables API Manual Testing Guide

Hub: `mdaihub-var-api-test` (namespace `mdai`)

Assumptions:
- mdai cluster is deployed into a local kind cluster
- The gateway API is reachable at `http://localhost:8081`
- Commands in the Valkey seed section can be pasted directly into an interactive `valkey-cli` session

| Variable | Type | DataType |
|---|---|---|
| `any_service_alerted` | computed | boolean |
| `attribute_map` | computed | map |
| `filter` | computed | string |
| `highRiskStates` | computed | set |
| `my_hash_set` | meta | metaHashSet |
| `my_priority_list` | meta | metaPriorityList |
| `riskLevel` | computed | string |
| `service_list` | computed | set |
| `severity_filters_by_level` | computed | map |
| `severity_number` | computed | int |
| `test_string` | **manual** | string |
| `test_int` | **manual** | int |
| `test_boolean` | **manual** | boolean |
| `test_set` | **manual** | set |
| `test_map` | **manual** | map |

---

## 0. Apply the test Hub CR

The test CR at `docs/configmap/mdaihub-var-api-test.yaml` extends the existing hub with 5 manual
variables needed to test write endpoints. The mdai-operator will reconcile it and update the
`mdaihub-var-api-test-variables-schema` ConfigMap automatically.

```bash
kubectl apply -f docs/k8s/mdaihub-var-api-test.yaml
```

Verify the ConfigMap was updated by the operator:

```bash
kubectl get configmap mdaihub-var-api-test-variables-schema -n mdai -o yaml
# Should contain test_string, test_int, test_boolean, test_set, test_map
```

---

## 1. Connect to Valkey with the password from Kubernetes Secret

The gateway deployment reads Valkey settings from the `valkey-secret` Secret in namespace `mdai`.
Use the same Secret when connecting `valkey-cli`.

```bash
export VALKEY_ENDPOINT="$(kubectl get secret valkey-secret -n mdai -o jsonpath='{.data.VALKEY_ENDPOINT}' | base64 --decode)"
export VALKEY_PASSWORD="$(kubectl get secret valkey-secret -n mdai -o jsonpath='{.data.VALKEY_PASSWORD}' | base64 --decode)"
echo "$VALKEY_ENDPOINT"
```

If `VALKEY_ENDPOINT` points to an in-cluster service, port-forward it locally:

```bash
kubectl -n mdai port-forward svc/valkey 6379:6379
```

Open `valkey-cli` using the password from the Secret:

```bash
valkey-cli -h 127.0.0.1 -p 6379 -a "$VALKEY_PASSWORD"
```

---

## 2. Seed Valkey for reproducible GET tests

The read endpoints only return non-`null` values when the corresponding Valkey keys already exist.
Seed the keys below before running the GET checks. `riskLevel` is intentionally left unset so the
guide can verify the `null` case. Run each command one by one and validate the result before moving
to the next command.

Reset each key individually:

```text
DEL variable/mdaihub-var-api-test/any_service_alerted
DEL variable/mdaihub-var-api-test/attribute_map
DEL variable/mdaihub-var-api-test/filter
DEL variable/mdaihub-var-api-test/highRiskStates
DEL variable/mdaihub-var-api-test/my_hash_set
DEL variable/mdaihub-var-api-test/my_priority_list
DEL variable/mdaihub-var-api-test/riskLevel
DEL variable/mdaihub-var-api-test/service_list
DEL variable/mdaihub-var-api-test/severity_filters_by_level
DEL variable/mdaihub-var-api-test/severity_number
DEL variable/mdaihub-var-api-test/test_string
DEL variable/mdaihub-var-api-test/test_int
DEL variable/mdaihub-var-api-test/test_boolean
DEL variable/mdaihub-var-api-test/test_set
DEL variable/mdaihub-var-api-test/test_map
DEL variable/mdaihub-var-api-test/default
```

Seed computed and meta source variables:

```text
SET variable/mdaihub-var-api-test/any_service_alerted true
GET variable/mdaihub-var-api-test/any_service_alerted

HSET variable/mdaihub-var-api-test/attribute_map region us-east-1
HGETALL variable/mdaihub-var-api-test/attribute_map
HSET variable/mdaihub-var-api-test/attribute_map team core
HGETALL variable/mdaihub-var-api-test/attribute_map

SET variable/mdaihub-var-api-test/filter payments
GET variable/mdaihub-var-api-test/filter

SADD variable/mdaihub-var-api-test/highRiskStates CA
SMEMBERS variable/mdaihub-var-api-test/highRiskStates
SADD variable/mdaihub-var-api-test/highRiskStates NY
SMEMBERS variable/mdaihub-var-api-test/highRiskStates
SADD variable/mdaihub-var-api-test/highRiskStates TX
SMEMBERS variable/mdaihub-var-api-test/highRiskStates

SADD variable/mdaihub-var-api-test/service_list checkout
SMEMBERS variable/mdaihub-var-api-test/service_list
SADD variable/mdaihub-var-api-test/service_list billing
SMEMBERS variable/mdaihub-var-api-test/service_list

HSET variable/mdaihub-var-api-test/severity_filters_by_level 1 INFO|WARNING
HGETALL variable/mdaihub-var-api-test/severity_filters_by_level
HSET variable/mdaihub-var-api-test/severity_filters_by_level 2 INFO
HGETALL variable/mdaihub-var-api-test/severity_filters_by_level

SET variable/mdaihub-var-api-test/severity_number 1
GET variable/mdaihub-var-api-test/severity_number

SET variable/mdaihub-var-api-test/default default
GET variable/mdaihub-var-api-test/default
```

Seed manual variables:

```text
SET variable/mdaihub-var-api-test/test_string seeded-string
GET variable/mdaihub-var-api-test/test_string

SET variable/mdaihub-var-api-test/test_int 42
GET variable/mdaihub-var-api-test/test_int

SET variable/mdaihub-var-api-test/test_boolean true
GET variable/mdaihub-var-api-test/test_boolean

SADD variable/mdaihub-var-api-test/test_set service1
SMEMBERS variable/mdaihub-var-api-test/test_set
SADD variable/mdaihub-var-api-test/test_set service2
SMEMBERS variable/mdaihub-var-api-test/test_set

HSET variable/mdaihub-var-api-test/test_map attrib.111 value.111
HGETALL variable/mdaihub-var-api-test/test_map
HSET variable/mdaihub-var-api-test/test_map attrib.222 value.222
HGETALL variable/mdaihub-var-api-test/test_map
```

Create the meta variables and validate them:

```text
PRIORITYLIST.GETORCREATE variable/mdaihub-var-api-test/my_priority_list variable/mdaihub-var-api-test/default variable/mdaihub-var-api-test/service_list
PRIORITYLIST.GET variable/mdaihub-var-api-test/my_priority_list

HASHSET.GETORCREATE variable/mdaihub-var-api-test/my_hash_set variable/mdaihub-var-api-test/severity_number variable/mdaihub-var-api-test/severity_filters_by_level
HASHSET.LOOKUP variable/mdaihub-var-api-test/my_hash_set
```

Final expected values:
- `filter` -> `"payments"`
- `severity_number` -> `1`
- `any_service_alerted` -> `true`
- `service_list` -> `["checkout","billing"]` in any order
- `attribute_map` -> `{"region":"us-east-1","team":"core"}`
- `my_hash_set` -> `"INFO|WARNING"`
- `my_priority_list` -> a non-empty array
- `riskLevel` -> `null`

---

## 3. List all variables

```bash
curl -s http://localhost:8081/variables/list | jq .
# Expect 200 — should include mdaihub-var-api-test with all 15 variables
```

---

## 4. List variables for a specific hub

```bash
# Happy path — expect all 15 variables
curl -s http://localhost:8081/variables/list/hub/mdaihub-var-api-test | jq .

# Corner case: non-existent hub → 404
curl -sv http://localhost:8081/variables/list/hub/no-such-hub
```

---

## 5. Get a single variable value

```bash
# String -> {"filter":"payments"}
curl -s http://localhost:8081/variables/values/hub/mdaihub-var-api-test/var/filter | jq .

# Int -> {"severity_number":1}
curl -s http://localhost:8081/variables/values/hub/mdaihub-var-api-test/var/severity_number | jq .

# Boolean -> {"any_service_alerted":true}
curl -s http://localhost:8081/variables/values/hub/mdaihub-var-api-test/var/any_service_alerted | jq .

# Set -> array order not guaranteed
curl -s http://localhost:8081/variables/values/hub/mdaihub-var-api-test/var/service_list | jq '.service_list | sort'

# Map -> {"region":"us-east-1","team":"core"}
curl -s http://localhost:8081/variables/values/hub/mdaihub-var-api-test/var/attribute_map | jq .

# Meta metaHashSet -> {"my_hash_set":"INFO|WARNING"}
curl -s http://localhost:8081/variables/values/hub/mdaihub-var-api-test/var/my_hash_set | jq .

# Meta metaPriorityList -> expect a non-empty array
curl -s http://localhost:8081/variables/values/hub/mdaihub-var-api-test/var/my_priority_list | jq .

# Corner case: variable with no value in Valkey → 200 with null
curl -s http://localhost:8081/variables/values/hub/mdaihub-var-api-test/var/riskLevel | jq .

# Corner case: non-existent hub → 404
curl -sv http://localhost:8081/variables/values/hub/fake-hub/var/filter

# Corner case: non-existent variable → 404
curl -sv http://localhost:8081/variables/values/hub/mdaihub-var-api-test/var/no_such_var
```

---

## 6. Get all variable values for a hub

```bash
# Happy path — returns all 15 variables; seeded values should be non-null except riskLevel
curl -s http://localhost:8081/variables/values/hub/mdaihub-var-api-test | jq .

# Corner case: non-existent hub → 404
curl -sv http://localhost:8081/variables/values/hub/nonexistent-hub
```

---

## 7. Set (POST) a variable value — happy path

These requests validate the write API contract only. The gateway publishes an `MdaiEvent` and
returns `201`; it does not update Valkey synchronously.

```bash
# String → 201
curl -sv -X POST http://localhost:8081/variables/hub/mdaihub-var-api-test/var/test_string \
  -H 'Content-Type: application/json' \
  -d '{"data":"hello world"}'

# Int → 201
curl -sv -X POST http://localhost:8081/variables/hub/mdaihub-var-api-test/var/test_int \
  -H 'Content-Type: application/json' \
  -d '{"data":42}'

# Boolean → 201
curl -sv -X POST http://localhost:8081/variables/hub/mdaihub-var-api-test/var/test_boolean \
  -H 'Content-Type: application/json' \
  -d '{"data":true}'

# Set → 201
curl -sv -X POST http://localhost:8081/variables/hub/mdaihub-var-api-test/var/test_set \
  -H 'Content-Type: application/json' \
  -d '{"data":["service1","service2"]}'

# Map → 201
curl -sv -X POST http://localhost:8081/variables/hub/mdaihub-var-api-test/var/test_map \
  -H 'Content-Type: application/json' \
  -d '{"data":{"attrib.111":"value.111","attrib.222":"value.222"}}'
```

---

## 8. Delete (DELETE) a variable value — happy path

Like `POST`, `DELETE` publishes an event and returns `200`. Do not expect Valkey to change
immediately unless a downstream event consumer is active and caught up.

```bash
# String → 200
curl -sv -X DELETE http://localhost:8081/variables/hub/mdaihub-var-api-test/var/test_string \
  -H 'Content-Type: application/json' \
  -d '{"data":"hello world"}'

# Set (remove specific items) → 200
curl -sv -X DELETE http://localhost:8081/variables/hub/mdaihub-var-api-test/var/test_set \
  -H 'Content-Type: application/json' \
  -d '{"data":["service1"]}'

# Map (remove by keys) → 200
curl -sv -X DELETE http://localhost:8081/variables/hub/mdaihub-var-api-test/var/test_map \
  -H 'Content-Type: application/json' \
  -d '{"data":["attrib.111","attrib.222"]}'
```

---

## 9. Write to computed/meta variables → 409

```bash
# Computed string → 409
curl -sv -X POST http://localhost:8081/variables/hub/mdaihub-var-api-test/var/filter \
  -H 'Content-Type: application/json' \
  -d '{"data":"test"}'
# Expect 409 "only manual variables can be updated or deleted"

# Computed boolean → 409
curl -sv -X POST http://localhost:8081/variables/hub/mdaihub-var-api-test/var/any_service_alerted \
  -H 'Content-Type: application/json' \
  -d '{"data":true}'

# Meta type → 409
curl -sv -X POST http://localhost:8081/variables/hub/mdaihub-var-api-test/var/my_hash_set \
  -H 'Content-Type: application/json' \
  -d '{"data":"test"}'

# DELETE on computed → 409
curl -sv -X DELETE http://localhost:8081/variables/hub/mdaihub-var-api-test/var/service_list \
  -H 'Content-Type: application/json' \
  -d '{"data":["service1"]}'
```

---

## 10. Type mismatch → 400

```bash
# String to int variable → 400
curl -sv -X POST http://localhost:8081/variables/hub/mdaihub-var-api-test/var/test_int \
  -H 'Content-Type: application/json' \
  -d '{"data":"not_an_int"}'

# Int to string variable → 400
curl -sv -X POST http://localhost:8081/variables/hub/mdaihub-var-api-test/var/test_string \
  -H 'Content-Type: application/json' \
  -d '{"data":123}'

# String to boolean variable → 400
curl -sv -X POST http://localhost:8081/variables/hub/mdaihub-var-api-test/var/test_boolean \
  -H 'Content-Type: application/json' \
  -d '{"data":"not_a_bool"}'

# String to set variable → 400
curl -sv -X POST http://localhost:8081/variables/hub/mdaihub-var-api-test/var/test_set \
  -H 'Content-Type: application/json' \
  -d '{"data":"not_a_list"}'

# Array to map variable → 400
curl -sv -X POST http://localhost:8081/variables/hub/mdaihub-var-api-test/var/test_map \
  -H 'Content-Type: application/json' \
  -d '{"data":["not","a","map"]}'
```

---

## 11. Invalid/missing payload → 400

The handler checks that the target variable is manual before it parses the request body. These
examples therefore target manual variables on purpose. If you send malformed JSON to a computed or
meta variable, the API returns `409` before payload validation.

```bash
# Malformed JSON → 400
curl -sv -X POST http://localhost:8081/variables/hub/mdaihub-var-api-test/var/test_string \
  -H 'Content-Type: application/json' \
  -d '{invalid json}'

# Missing "data" field → 400
curl -sv -X POST http://localhost:8081/variables/hub/mdaihub-var-api-test/var/test_string \
  -H 'Content-Type: application/json' \
  -d '{"value":"oops"}'

# Empty body → 400
curl -sv -X POST http://localhost:8081/variables/hub/mdaihub-var-api-test/var/test_string \
  -H 'Content-Type: application/json' \
  -d ''

# Same malformed payload against computed variable -> 409, not 400
curl -sv -X POST http://localhost:8081/variables/hub/mdaihub-var-api-test/var/filter \
  -H 'Content-Type: application/json' \
  -d '{invalid json}'
```

---

## 12. Non-existent hub/variable for writes → 404

```bash
# Non-existent hub → 404
curl -sv -X POST http://localhost:8081/variables/hub/fake-hub/var/test_string \
  -H 'Content-Type: application/json' \
  -d '{"data":"test"}'

# Non-existent variable → 404
curl -sv -X POST http://localhost:8081/variables/hub/mdaihub-var-api-test/var/does_not_exist \
  -H 'Content-Type: application/json' \
  -d '{"data":"test"}'
```

---

## 13. Wrong HTTP method → 405

```bash
curl -sv -X PUT http://localhost:8081/variables/hub/mdaihub-var-api-test/var/test_string \
  -H 'Content-Type: application/json' \
  -d '{"data":"test"}'
```

---

## 14. Optional end-to-end verification with a downstream consumer

This section only applies if the environment has a running consumer that processes the published
variable events and writes the resulting values back to Valkey. Without that consumer, the write API
responses are still correct, but the subsequent GETs will continue to show the last stored Valkey
values.

```bash
# Publish a string update
curl -sv -X POST http://localhost:8081/variables/hub/mdaihub-var-api-test/var/test_string \
  -H 'Content-Type: application/json' \
  -d '{"data":"roundtrip_test"}'
# Expect 201 and an MdaiEvent response body

# Poll until the consumer writes the value back to Valkey
until curl -s http://localhost:8081/variables/values/hub/mdaihub-var-api-test/var/test_string | jq -e '.test_string == "roundtrip_test"' >/dev/null; do sleep 1; done

# Publish a delete
curl -sv -X DELETE http://localhost:8081/variables/hub/mdaihub-var-api-test/var/test_string \
  -H 'Content-Type: application/json' \
  -d '{"data":"roundtrip_test"}'
# Expect 200

# Poll until the consumer removes the value again
until curl -s http://localhost:8081/variables/values/hub/mdaihub-var-api-test/var/test_string | jq -e '.test_string == null' >/dev/null; do sleep 1; done
```
