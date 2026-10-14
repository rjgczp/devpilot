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
