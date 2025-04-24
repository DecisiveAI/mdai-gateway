package eventing

import (
	"github.com/decisiveai/event-handler-webservice/types"
	datacore "github.com/decisiveai/mdai-data-core/variables"
	"github.com/go-logr/logr"
	"github.com/valkey-io/valkey-go"
	"log"
	"strings"
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
// TODO: get client, logger on init -- assuming this fn will live in its own module
func ReceiveMdaiEvent(client valkey.Client, logger logr.Logger, event types.MdaiEvent) {
	// Is this where the Queue goes?
	// TODO: Wire up eventing library

	dataAdapter := datacore.NewValkeyAdapter(client, logger)

	// Match on whole name, e.g. "NoisyServiceAlert.firing"
	if workflow, exists := FakeConfiguredOODAs[event.Name]; exists {
		for _, handlerName := range workflow {
			safeInvokeHandler(handlerName, dataAdapter, event)
		}
		// Match on alert name regardless of status, e.g. NoisyServiceAlert
	} else if workflow, exists := FakeConfiguredOODAs[strings.Split(event.Name, ".")[0]]; exists {
		for _, handlerName := range workflow {
			safeInvokeHandler(handlerName, dataAdapter, event)
		}
	} else {
		// TODO: wire up logger
		log.Printf("No configured automation for event: %s", event.Name)
	}
}

func safeInvokeHandler(handler HandlerName, adapter *datacore.ValkeyAdapter, event types.MdaiEvent) {
	if handler, exists := SupportedHandlers[handler]; exists {
		handler(adapter, event)
	} else {
		// TODO: wire up logger
		log.Printf("No handler found for event type: %s", handler)
	}
}
