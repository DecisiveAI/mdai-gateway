package eventing

import (
	"github.com/decisiveai/event-handler-webservice/types"
	datacore "github.com/decisiveai/mdai-data-core/variables"
)

type WorkflowMap map[string][]HandlerName

type HandlerName string
type ActionHandler func(adapter *datacore.ValkeyAdapter, event types.MdaiEvent)
type HandlerMap map[HandlerName]ActionHandler
