package main

import "time"

const (
	httpPortEnvVarKey = "HTTP_PORT"

	eventsEndpoint = "/events"

	otelZapCoreName = "github.com/decisiveai/event-handler-webservice"
	defaultHTTPPort = "8081"

	contextCancelTimeout     = 5 * time.Second
	defaultReadTimeout       = 5 * time.Second
	defaultWriteTimeout      = 10 * time.Second
	defaultIdleTimeout       = 60 * time.Second
	defaultReadHeaderTimeout = 2 * time.Second
)
