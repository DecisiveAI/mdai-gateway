package types

import (
	"github.com/google/uuid"
)

func CreateEventUuid() string {
	id := uuid.New()
	return id.String()
}
