package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type brokenWriter struct{ *httptest.ResponseRecorder }

func (w brokenWriter) Write([]byte) (int, error) { panic("sentinel response write failure") }

func TestPanicAfterResponseStartsAbortsConnection(t *testing.T) {
	h := newHandler()
	defer func() {
		if recovered := recover(); recovered != http.ErrAbortHandler {
			t.Fatal("post-write panic was exposed or appended")
		}
	}()
	h.ServeHTTP(brokenWriter{httptest.NewRecorder()}, req("POST", "login", body("a@example.com", "a")))
}
