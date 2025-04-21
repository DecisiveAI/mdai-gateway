package eventing

import (
	"github.com/decisiveai/event-handler-webservice/types"
	"log"
)

func EmitMdaiEvent(event types.MdaiEvent) {
	// TODO: Wire up eventing library
	// submits the provided event to the stream
}

const NoisyServiceFired string = "NoisyServiceFired"
const NoisyServiceResolved string = "NoisyServiceResolved"

// This would come from the config, presumably passed by the operator
var FakeConfiguredOODAs = WorkflowMap{
	NoisyServiceFired:    {HandleAddNoisyServiceToSet},
	NoisyServiceResolved: {HandleRemoveNoisyServiceFromSet},
}

func ReceiveMdaiEvent(event types.MdaiEvent) {
	// TODO: Wire up eventing library
	if workflow, exists := FakeConfiguredOODAs[event.Name]; exists {
		for _, handlerName := range workflow {
			safeInvokeHandler(handlerName, event)
		}
	} else {
		// TODO: wire up logger
		log.Printf("No configured automation for event: %s", event.Name)
	}
}

func safeInvokeHandler(handler HandlerName, event types.MdaiEvent) {
	if handler, exists := SupportedHandlers[handler]; exists {
		handler(event)
	} else {
		// TODO: wire up logger
		log.Printf("No handler found for event type: %s", handler)
	}
}
