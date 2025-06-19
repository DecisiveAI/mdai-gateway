package adapter

import "github.com/decisiveai/mdai-event-hub/eventing"

type EventAdapter interface {
	ToMdaiEvents() ([]eventing.MdaiEvent, error)
}
