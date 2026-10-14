package registration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/user"
)

type storeFunc func(context.Context, string, string) (user.Public, error)

func (f storeFunc) Create(c context.Context, e, p string) (user.Public, error) { return f(c, e, p) }

type hashFunc func(string) (string, error)

func (f hashFunc) Hash(p string) (string, error) { return f(p) }

func newTestHandler() *Handler {
	return New(storeFunc(func(_ context.Context, e, p string) (user.Public, error) {
		return user.Public{ID: "0199a000-0000-7000-8000-000000000001", Email: e, Status: "active", CreatedAt: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)}, nil
	}), hashFunc(func(string) (string, error) { return "safe-test-PHC", nil }), slog.New(slog.NewJSONHandler(io.Discard, nil)))
}

func payload(email, password string) string {
	b, _ := json.Marshal(map[string]string{"email": email, "password": password})
	return string(b)
}
func request(body string) *http.Request {
	r := httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}
func perform(h http.Handler, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, request(body))
	return w
}

const validPassword = "exact password phrase"

func assertResponse(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status=%d want %d body=%s", w.Code, status, w.Body.String())
	}
	if w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing safe headers")
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(w.Body.Bytes(), &body) != nil {
		t.Fatal("not JSON")
	}
	if code != "" {
		var e struct{ Code string }
		_ = json.Unmarshal(body["error"], &e)
		if e.Code != code || body["data"] != nil {
			t.Fatalf("bad error envelope: %s", w.Body.String())
		}
	} else if body["data"] == nil || body["error"] != nil {
		t.Fatal("bad success envelope")
	}
}

func TestStrictRequestsAndCheapRejection(t *testing.T) {
	good := payload("alice@example.com", validPassword)
	cases := []struct {
		name, body string
		status     int
		code       string
	}{
		{"empty", "", 400, "invalid_request"}, {"null", "null", 400, "invalid_request"}, {"array", "[]", 400, "invalid_request"},
		{"extra-value", good + " {}", 400, "invalid_request"}, {"missing", `{"email":"a@example.com"}`, 400, "invalid_request"},
		{"unknown", strings.TrimSuffix(good, "}") + `,"status":"active"}`, 400, "invalid_request"},
		{"duplicate", strings.TrimSuffix(good, "}") + `,"email":"other@example.com"}`, 400, "invalid_request"},
		{"escaped-duplicate", strings.TrimSuffix(good, "}") + `,"\u0065mail":"other@example.com"}`, 400, "invalid_request"},
		{"case-key", strings.Replace(good, "email", "Email", 1), 400, "invalid_request"},
		{"null-password", `{"email":"a@example.com","password":null}`, 400, "invalid_request"},
		{"number", `{"email":1,"password":"long password phrase"}`, 400, "invalid_request"},
		{"invalid-utf8", `{"email":"a@example.com","password":"` + string([]byte{255}) + `"}`, 400, "invalid_request"},
		{"huge", good + strings.Repeat(" ", 4097), 413, "request_too_large"},
		{"email-policy", payload("国际@example.com", validPassword), 422, "validation_failed"},
		{"password-policy", payload("alice@example.com", "too short"), 422, "validation_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandler()
			h.hasher = hashFunc(func(string) (string, error) { t.Error("invalid request hashed"); return "", nil })
			h.store = storeFunc(func(context.Context, string, string) (user.Public, error) {
				t.Error("invalid request stored")
				return user.Public{}, nil
			})
			for _, busy := range []bool{false, true} {
				if busy {
					h.slots <- struct{}{}
					h.slots <- struct{}{}
				}
				w := httptest.NewRecorder()
				r := request(tc.body)
				r.ContentLength = -1
				h.ServeHTTP(w, r)
				assertResponse(t, w, tc.status, tc.code)
				if busy {
					<-h.slots
					<-h.slots
				}
			}
		})
	}
	h := newTestHandler()
	assertResponse(t, perform(h, good+strings.Repeat(" ", MaxBodyBytes-len(good))), 201, "")
	assertResponse(t, perform(h, good+strings.Repeat(" ", MaxBodyBytes-len(good)+1)), 413, "request_too_large")
}

func TestRepresentationAndMethods(t *testing.T) {
	for _, method := range []string{"GET", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD"} {
		w := httptest.NewRecorder()
		r := request("ignored")
		r.Method = method
		newTestHandler().ServeHTTP(w, r)
		if w.Code != 405 || w.Header().Get("Allow") != "POST" {
			t.Fatal("method contract")
		}
		if method == "HEAD" {
			if w.Body.Len() != 0 {
				t.Fatal("HEAD body")
			}
		} else {
			assertResponse(t, w, 405, "method_not_allowed")
		}
	}
	for _, media := range []string{"", "text/plain", "application/json; charset=latin1", "application/json; foo=bar"} {
		w := httptest.NewRecorder()
		r := request("{}")
		r.Header.Set("Content-Type", media)
		newTestHandler().ServeHTTP(w, r)
		assertResponse(t, w, 415, "unsupported_media_type")
	}
	r := request("{}")
	r.Header.Set("Content-Encoding", "identity")
	w := httptest.NewRecorder()
	newTestHandler().ServeHTTP(w, r)
	assertResponse(t, w, 415, "unsupported_media_type")
	r = request(payload("alice@example.com", validPassword))
	r.Header.Set("Content-Type", "application/json; charset=UTF-8")
	w = httptest.NewRecorder()
	newTestHandler().ServeHTTP(w, r)
	assertResponse(t, w, 201, "")
}

func TestUnicodeRawEscapes(t *testing.T) {
	for _, raw := range []string{`\ud800`, `\udfff`, `\udc00\ud800`, `\ud800\u0061`, `\ud800x`, `\ud800\\udc00`, `\u12`, `\uxxxx`, `\ud800\u12xz`} {
		t.Run(raw, func(t *testing.T) {
			h := newTestHandler()
			h.hasher = hashFunc(func(string) (string, error) { t.Fatal("malformed escape hashed"); return "", nil })
			assertResponse(t, perform(h, `{"email":"a@example.com","password":"longpasswordphrase`+raw+`"}`), 400, "invalid_request")
		})
	}
	for _, tc := range []struct{ raw, want string }{
		{strings.Repeat(`\ud83d\ude00`, 15), strings.Repeat("😀", 15)},
		{strings.Repeat(`\ud83d\ude00`, 128), strings.Repeat("😀", 128)},
		{strings.Repeat(`\ufffd`, 15), strings.Repeat("�", 15)},
		{strings.Repeat("�", 15), strings.Repeat("�", 15)},
		{` exact\u0065\u0301 password `, " exacte\u0301 password "},
		{`literal \\ud800 phrase`, `literal \ud800 phrase`},
	} {
		h := newTestHandler()
		h.hasher = hashFunc(func(p string) (string, error) {
			if p != tc.want {
				t.Fatal("decoded password changed")
			}
			return "hash", nil
		})
		assertResponse(t, perform(h, `{"email":"a@example.com","password":"`+tc.raw+`"}`), 201, "")
	}
}

func TestValidationBoundaries(t *testing.T) {
	for _, email := range []string{strings.Repeat("a", 65) + "@example.com", "a@" + strings.Repeat("b", 64) + ".com"} {
		in := input{Email: email, Password: validPassword}
		if validate(&in)["email"] != "invalid_length" {
			t.Fatal("email length reason must precede syntax")
		}
	}
	for _, tc := range []struct {
		password string
		ok       bool
	}{
		{strings.Repeat("a", 14), false}, {strings.Repeat("a", 15), true}, {strings.Repeat("a", 128), true}, {strings.Repeat("a", 129), false},
		{strings.Repeat("😀", 128), true}, {strings.Repeat("😀", 129), false}, {strings.Repeat(" ", 15), true},
		{strings.Repeat("e\u0301", 7), false}, {strings.Repeat("e\u0301", 8), true},
	} {
		in := input{Email: "a@example.com", Password: tc.password}
		if (len(validate(&in)) == 0) != tc.ok {
			t.Fatal("password boundary")
		}
		if in.Password != tc.password {
			t.Fatal("password modified")
		}
	}
	maxEmail := strings.Repeat("a", 64) + "@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 61)
	for _, email := range []string{"a..b@example.com", ".a@example.com", "a.@example.com", "\"a\"@example.com", "a@localhost", "a@-host.com", "a@host-.com", "a@host.com.", "a@[127.0.0.1]", "a@127.0.0.1", strings.Repeat("a", 65) + "@example.com", "a@" + strings.Repeat("x", 64) + ".com", maxEmail + "x"} {
		if validEmail(email) && len(email) <= 254 {
			t.Fatalf("accepted unsupported email: %q", email)
		}
	}
	in := input{Email: " \tAlice.Example+tag@Example.COM\r\n", Password: validPassword}
	if len(validate(&in)) != 0 || in.Email != "alice.example+tag@example.com" {
		t.Fatal("normalization")
	}
	in = input{Email: maxEmail, Password: validPassword}
	if len(maxEmail) != 254 || len(validate(&in)) != 0 {
		t.Fatal("254 byte email boundary")
	}
}

type observedWriter struct {
	http.ResponseWriter
	writes atomic.Int32
}

func (w *observedWriter) WriteHeader(s int) { w.writes.Add(1); w.ResponseWriter.WriteHeader(s) }
func (w *observedWriter) Write(b []byte) (int, error) {
	w.writes.Add(1)
	return w.ResponseWriter.Write(b)
}

func TestCancellationKeepsSlotsUntilHashReturns(t *testing.T) {
	h := newTestHandler()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	done := make(chan struct{}, 2)
	h.hasher = hashFunc(func(string) (string, error) { entered <- struct{}{}; <-release; return "hash", nil })
	h.store = storeFunc(func(context.Context, string, string) (user.Public, error) {
		t.Error("canceled request inserted")
		return user.Public{}, nil
	})
	cancelers := []context.CancelFunc{}
	writers := []*observedWriter{}
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancelers = append(cancelers, cancel)
		r := request(payload("a@example.com", validPassword)).WithContext(ctx)
		w := &observedWriter{ResponseWriter: httptest.NewRecorder()}
		writers = append(writers, w)
		go func() { h.ServeHTTP(w, r); done <- struct{}{} }()
	}
	for i := 0; i < 2; i++ {
		<-entered
	}
	for _, cancel := range cancelers {
		cancel()
	}
	if len(h.slots) != 2 {
		t.Fatal("slots released while hashing")
	}
	w := perform(h, payload("b@example.com", validPassword))
	assertResponse(t, w, 503, "registration_busy")
	if w.Header().Get("Retry-After") != "1" {
		t.Fatal("retry header")
	}
	assertResponse(t, perform(h, "{}"), 400, "invalid_request")
	close(release)
	for i := 0; i < 2; i++ {
		<-done
		if writers[i].writes.Load() != 0 {
			t.Fatal("canceled response attempted")
		}
	}
	if len(h.slots) != 0 {
		t.Fatal("slots leaked")
	}
	h.hasher = newTestHandler().hasher
	h.store = newTestHandler().store
	assertResponse(t, perform(h, payload("c@example.com", validPassword)), 201, "")
}

func TestErrorResponsesAndSlotRelease(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{user.ErrConflict, 409, "email_conflict"}, {user.ErrUnavailable, 503, "service_unavailable"}, {user.ErrTimeout, 503, "database_timeout"}, {user.ErrSchema, 500, "internal_error"}, {errors.New("sentinel PgError DSN password"), 500, "internal_error"},
	} {
		h := newTestHandler()
		var logs bytes.Buffer
		h.logger = slog.New(slog.NewJSONHandler(&logs, nil))
		h.store = storeFunc(func(context.Context, string, string) (user.Public, error) { return user.Public{}, tc.err })
		w := perform(h, payload("sentinel@example.com", validPassword))
		assertResponse(t, w, tc.status, tc.code)
		if strings.Contains(logs.String()+w.Body.String(), "sentinel") {
			t.Fatal("secret leaked")
		}
	}
	for _, panicHash := range []bool{false, true} {
		h := newTestHandler()
		h.hasher = hashFunc(func(string) (string, error) {
			if panicHash {
				panic("sentinel panic")
			}
			return "", errors.New("sentinel random failure")
		})
		h.store = storeFunc(func(context.Context, string, string) (user.Public, error) {
			t.Error("failed hash inserted")
			return user.Public{}, nil
		})
		w := perform(h, payload("a@example.com", validPassword))
		assertResponse(t, w, 500, "internal_error")
		if len(h.slots) != 0 || strings.Contains(w.Body.String(), "sentinel") {
			t.Fatal("hash failure unsafe")
		}
	}
}

func TestServerDeadlineAndUnknownCommit(t *testing.T) {
	h := newTestHandler()
	h.processingTimeout = time.Millisecond
	h.hasher = hashFunc(func(string) (string, error) { time.Sleep(5 * time.Millisecond); return "hash", nil })
	h.store = storeFunc(func(context.Context, string, string) (user.Public, error) {
		t.Error("expired request inserted")
		return user.Public{}, nil
	})
	assertResponse(t, perform(h, payload("a@example.com", validPassword)), 503, "service_unavailable")
	h = newTestHandler()
	h.databaseTimeout = time.Millisecond
	h.store = storeFunc(func(ctx context.Context, _, _ string) (user.Public, error) {
		<-ctx.Done()
		return user.Public{}, user.ErrTimeout
	})
	assertResponse(t, perform(h, payload("a@example.com", validPassword)), 503, "database_timeout")
	h = newTestHandler()
	calls := 0
	storedHash := ""
	h.store = storeFunc(func(_ context.Context, _, hash string) (user.Public, error) {
		calls++
		if storedHash != "" {
			return user.Public{}, user.ErrConflict
		}
		storedHash = hash
		return user.Public{}, user.ErrUnavailable
	})
	w := perform(h, payload("a@example.com", validPassword))
	assertResponse(t, w, 503, "service_unavailable")
	if !strings.Contains(w.Body.String(), "could not be confirmed") || calls != 1 || storedHash == "" {
		t.Fatal("ambiguous outcome incorrectly handled")
	}
	assertResponse(t, perform(h, payload("a@example.com", "another password phrase")), 409, "email_conflict")
	if calls != 2 || storedHash != "safe-test-PHC" {
		t.Fatal("credentials replaced")
	}
}

func TestClientCancellationInStoreAndRealHTTP(t *testing.T) {
	h := newTestHandler()
	entered := make(chan struct{})
	finished := make(chan struct{})
	h.store = storeFunc(func(ctx context.Context, _, _ string) (user.Public, error) {
		close(entered)
		<-ctx.Done()
		return user.Public{}, user.ErrCanceled
	})
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed := &observedWriter{ResponseWriter: w}
		defer func() { writes.Store(observed.writes.Load()); close(finished) }()
		h.ServeHTTP(observed, r)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "POST", server.URL, strings.NewReader(payload("a@example.com", validPassword)))
	r.Header.Set("Content-Type", "application/json")
	clientDone := make(chan error, 1)
	go func() {
		resp, err := server.Client().Do(r)
		if resp != nil {
			resp.Body.Close()
		}
		clientDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("store not reached")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("store cancellation not propagated")
	}
	if writes.Load() != 0 || <-clientDone == nil {
		t.Fatal("canceled client got response attempt")
	}
	h = newTestHandler()
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	w := &observedWriter{ResponseWriter: httptest.NewRecorder()}
	h.ServeHTTP(w, request("{}").WithContext(ctx))
	if w.writes.Load() != 0 {
		t.Fatal("pre-canceled request wrote response")
	}
}

func FuzzParseInput(f *testing.F) {
	for _, s := range []string{payload("a@example.com", validPassword), `{"password":"\ud800"}`, `{"password":"\ud83d\ude00"}`, `"\\"`, "\xff"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > MaxBodyBytes {
			return
		}
		_, _ = parseInput(b)
	})
}
