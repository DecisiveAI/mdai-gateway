package eventing

import (
	"github.com/decisiveai/event-handler-webservice/types"
	"log"
)

func EmitMdaiEvent(event types.MdaiEvent) {
	// TODO: Wire up eventing library
	// submits the provided event to the stream
}

// This would come from the config, presumably passed by the operator
const NoisyServiceFired string = "NoisyServiceFired"
const NoisyServiceResolved string = "NoisyServiceResolved"

var FakeConfiguredOODAs = WorkflowMap{
	NoisyServiceFired:    {HandleAddNoisyServiceToSet},
	NoisyServiceResolved: {HandleRemoveNoisyServiceFromSet},
}

// end stuff from config

// where does this live?
func ReceiveMdaiEvent(event types.MdaiEvent) {
	// Is this where the Queue goes?
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
