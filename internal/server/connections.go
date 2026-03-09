package server

import (
	"context"
	"net/http"
)

func handleGetConnections(ctx context.Context, deps HandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		http.Error(w, "NOT YET IMPLEMENTED LMAO", http.StatusTeapot)
	}
}

func handlePutConnection(ctx context.Context, deps HandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		http.Error(w, "NOT YET IMPLEMENTED LMAO", http.StatusTeapot)
	}
}

func handleDeleteConnection(ctx context.Context, deps HandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		http.Error(w, "NOT YET IMPLEMENTED LMAO", http.StatusTeapot)
	}
}
