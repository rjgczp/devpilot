package app

import "net/http"

// NewRoutes composes business routes without changing health endpoint contracts.
func NewRoutes(health, registration http.Handler, authentication ...http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/v1/auth/register", registration)
	if len(authentication) > 0 {
		for _, path := range []string{"/api/v1/auth/login", "/api/v1/auth/logout", "/api/v1/auth/me"} {
			mux.Handle(path, authentication[0])
		}
	}
	mux.Handle("/", health)
	return mux
}
