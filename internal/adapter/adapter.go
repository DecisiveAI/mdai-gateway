package adapter

import "github.com/mydecisive/mdai-data-core/eventing"

type EventAdapter interface {
	ToMdaiEvents() ([]EventPerSubject, int, error)
}

type EventPerSubject struct {
	Event   eventing.MdaiEvent
	Subject eventing.MdaiEventSubject
}
