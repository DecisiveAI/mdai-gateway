[![Tests](https://github.com/DecisiveAI/event-handler-webservice/actions/workflows/test.yml/badge.svg)](https://github.com/DecisiveAI/event-handler-webservice/actions/workflows/test.yml)
![Coverage](https://img.shields.io/badge/Coverage-0-red)
# Event Handler Webservice
### NOTE: this requires the 0.6.x helm installation

# INSTALL
```sh
helm upgrade --install --create-namespace --namespace mdai event-handler-webservice ./deployment
```

`testdata` contains
* JSON POST bodies (to simulate data from Alert Manager)

# to simulate an alert via curl
```sh
curl -X POST -H "Content-Type: application/json" -d@testdata/alert_post_body_1.json http://localhost:8081/alerts
```

```sh
curl -X POST -H "Content-Type: application/json" -d@testdata/event.json http://localhost:8081/events
```