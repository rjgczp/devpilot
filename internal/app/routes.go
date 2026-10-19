package app

import (
	"net/http"
	"path"
	"strings"
)

// NewRoutes composes business routes without changing health endpoint contracts.
// Optional handlers are authentication first, then workspace and project; old callers retain
// their existing routes when either is absent.
func NewRoutes(health, registration http.Handler, authentication ...http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/v1/auth/register", registration)
	if len(authentication) > 0 {
		for _, path := range []string{"/api/v1/auth/login", "/api/v1/auth/logout", "/api/v1/auth/me"} {
			mux.Handle(path, authentication[0])
		}
	}
	if len(authentication) > 1 {
		mux.Handle("/api/v1/invitations", authentication[1])
		mux.Handle("/api/v1/invitations/", authentication[1])
		mux.Handle("/api/v1/workspaces", authentication[1])
		mux.Handle("/api/v1/workspaces/{workspace_id}", authentication[1])
	}
	mux.Handle("/", health)
	// Bypass ServeMux path cleaning for the member namespace: reject ambiguous
	// member paths in the handler instead of redirecting a mutation to an alias.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Monitor namespace must bypass cleaning and precede the parent handler.
		if len(authentication) > 3 && (strings.HasPrefix(r.URL.Path, "/api/v1/workspaces/") || strings.HasPrefix(path.Clean(r.URL.Path), "/api/v1/workspaces/")) && strings.Contains(r.URL.Path, "/projects") && strings.Contains(r.URL.Path, "/monitors") {
			authentication[3].ServeHTTP(w, r)
			return
		}
		// Project namespace is intercepted before ServeMux path normalization.
		if len(authentication) > 2 && (strings.HasPrefix(r.URL.Path, "/api/v1/workspaces/") || strings.HasPrefix(path.Clean(r.URL.Path), "/api/v1/workspaces/")) && strings.Contains(r.URL.Path, "/projects") {
			authentication[2].ServeHTTP(w, r)
			return
		}
		if len(authentication) > 1 && strings.HasPrefix(path.Clean(r.URL.Path), "/api/v1/invitations") {
			authentication[1].ServeHTTP(w, r)
			return
		}
		if len(authentication) > 1 && strings.HasPrefix(path.Clean(r.URL.Path), "/api/v1/workspaces/") && (strings.Contains(strings.TrimPrefix(r.URL.Path, "/api/v1/workspaces/"), "/members") || strings.Contains(r.URL.Path, "/invitations")) {
			authentication[1].ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
}
