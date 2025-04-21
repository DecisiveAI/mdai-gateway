package eventing

import (
	"encoding/json"
	"fmt"
	"github.com/decisiveai/event-handler-webservice/types"
	"log"
)

const (
	HandleAddNoisyServiceToSet      HandlerName = "addNoisyServiceToSet"
	HandleRemoveNoisyServiceFromSet HandlerName = "removeNoisyServiceFromSet"
)

// Go doesn't support dynamic accessing of exports. So this is a workaround.
// The handler library will have to export a map that can by dynamically accessed.
// To enforce this, handlers are declared with a lower case first character so they
// are not exported directly but can only be accessed through the map
var SupportedHandlers = HandlerMap{
	HandleAddNoisyServiceToSet:      handleAddNoisyServiceToSet,
	HandleRemoveNoisyServiceFromSet: handleRemoveNoisyServiceFromSet,
}

func processEventPayload(event types.MdaiEvent) (map[string]interface{}, error) {
	var payloadData map[string]interface{}

	err := json.Unmarshal([]byte(event.Payload), &payloadData)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	return payloadData, nil
}

func handleAddNoisyServiceToSet(event types.MdaiEvent) {
	// TODO: incorporate variable library
	payloadData, err := processEventPayload(event)
	if err != nil {
		// TODO: Wire up logger
		log.Fatal("failed to process payload: %w", err)
	}
	serviceName := payloadData["service_name"].(string)
	hubName := payloadData["hubName"].(string)
	noisyServices := variables.Get(hubName, "service_list")

	noisyServices.add(serviceName)

	variables.Set(hubName, "service_list", noisyServices)
}

func handleRemoveNoisyServiceFromSet(event types.MdaiEvent) {
	// TODO: incorporate variable library
	payloadData, err := processEventPayload(event)
	if err != nil {
		// TODO: Wire up logger
		log.Fatal("failed to process payload: %w", err)
	}
	serviceName := payloadData["service_name"].(string)
	hubName := payloadData["hubName"].(string)
	noisyServices := variables.Get(hubName, "service_list")

	noisyServices.remove(serviceName)

	variables.Set(hubName, "service_list", noisyServices)
}
