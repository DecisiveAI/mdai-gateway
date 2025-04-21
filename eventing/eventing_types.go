package eventing

import "github.com/decisiveai/event-handler-webservice/types"

type WorkflowMap map[string][]HandlerName

type HandlerName string
type ActionHandler func(event types.MdaiEvent)
type HandlerMap map[HandlerName]ActionHandler
