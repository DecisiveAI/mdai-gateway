# Event Handler Webservice
### NOTE: this requires the 0.6.x helm installation

# INSTALL
```sh
docker build -t event-handler-webservice:0.0.1 . 
kind load docker-image event-handler-webservice:0.0.1 --name mdai
kubectl apply -f testdata/mdai-event-handler-config-configmap.yaml
kubectl patch secret valkey-secret -n mdai --type=json -p='[{"op": "add", "path": "/data/VALKEY_ENDPOINT", "value": "bWRhaS12YWxrZXktcHJpbWFyeS5tZGFpLnN2Yy5jbHVzdGVyLmxvY2FsOjYzNzkK"}]'
kubectl apply -f deployment/deployment.yaml -f deployment/service.yaml
```

`testdata` contains
* three JSON POST bodies (to simulate data from Alert Manager)
* a Kubernetes manifest to create an instance of the custom resource

# to simulate an alert via curl
```sh
curl -X POST -H "Content-Type: application/json" -d@testdata/alert_post_body_1.json http://localhost:8081/alerts
```
