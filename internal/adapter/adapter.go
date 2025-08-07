package adapter

import "github.com/decisiveai/mdai-event-hub/pkg/eventing"

type EventAdapter interface {
	ToMdaiEvents() ([]eventing.EventPerSubject, int, error)
}
