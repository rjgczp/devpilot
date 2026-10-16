package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRoutesKeepHealthAndRegistrationSeparate(t *testing.T) {
	health := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("alive\n"))
	})
	register := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201) })
	routes := NewRoutes(health, register)
	for _, path := range []string{"/healthz", "/readyz"} {
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Body.String() != "alive\n" || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
			t.Fatal("health routing changed")
		}
	}
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/auth/register", nil))
	if w.Code != 201 {
		t.Fatal("registration route missing")
	}
}

func TestAuthenticationRoutesUseDedicatedHandler(t *testing.T) {
	miss := http.HandlerFunc(http.NotFound)
	routes := NewRoutes(miss, miss, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, path := range []string{"/api/v1/auth/login", "/api/v1/auth/logout", "/api/v1/auth/me"} {
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, httptest.NewRequest("POST", path, nil))
		if w.Code != 204 {
			t.Fatal("auth route missing")
		}
	}
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest("GET", "/not-a-route", nil))
	if w.Code != 404 {
		t.Fatal("unrelated route changed")
	}
}

func TestProjectNamespaceDoesNotNormalizeMutations(t *testing.T) {
	called := 0
	project := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++; w.WriteHeader(418) })
	routes := NewRoutes(http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), project)
	for _, path := range []string{"/api/v1/workspaces/w/projects", "/api/v1/workspaces/w/projects/p", "/api/v1/workspaces/w//projects/p", "/api/v1/workspaces/w/projects/../../..", "/api/v1/workspaces/w/%70rojects/p"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("PATCH", path, nil)
		routes.ServeHTTP(w, r)
		if w.Code != 418 || w.Header().Get("Location") != "" {
			t.Fatal("Project path cleaned/redirected", path, w.Code)
		}
	}
	if called != 5 {
		t.Fatal("project handler unreachable")
	}
}
