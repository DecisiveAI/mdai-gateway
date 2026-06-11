package adapter

import (
	"time"

	"github.com/mydecisive/mdai-data-core/eventing"
)

type EventAdapter interface {
	ToMdaiEvents() ([]EventPerSubject, int, error)
}

type EventPerSubject struct {
	Event      eventing.MdaiEvent
	Subject    eventing.MdaiEventSubject
	DedupeKey  string
	ChangeTime time.Time
}
