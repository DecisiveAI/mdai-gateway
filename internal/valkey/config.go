package valkey

import "time"

type Config struct {
	Password    string
	InitAddress []string

	InitialBackoffInterval time.Duration
	MaxBackoffElapsedTime  time.Duration
}
