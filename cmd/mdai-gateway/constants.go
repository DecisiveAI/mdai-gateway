package main

import "time"

const (
	httpPortEnvVarKey = "HTTP_PORT"
	defaultHTTPPort   = "8081"

	defaultReadHeaderTimeout = 5 * time.Second
	defaultReadTimeout       = 10 * time.Second
	defaultWriteTimeout      = 10 * time.Second
	defaultIdleTimeout       = 120 * time.Second
)
