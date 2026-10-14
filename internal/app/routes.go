package app

import "net/http"

// NewRoutes composes business routes without changing health endpoint contracts.
func NewRoutes(health, registration http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/v1/auth/register", registration)
	mux.Handle("/", health)
	return mux
}
