package auth

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

	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/password"
	"github.com/rjgczp/devpilot/internal/registration"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

type usersFunc func(context.Context, string) (user.Credentials, error)

func (f usersFunc) FindCredentials(ctx context.Context, email string) (user.Credentials, error) {
	return f(ctx, email)
}

type verifyFunc func(string, string) (bool, error)

func (f verifyFunc) Verify(secret, phc string) (bool, error) { return f(secret, phc) }

type tokenFunc func() (string, session.Digest, error)

func (f tokenFunc) New() (string, session.Digest, error) { return f() }

type sessionsFake struct {
	create func(context.Context, user.Credentials, session.Digest, time.Duration) (session.Created, error)
	lookup func(context.Context, session.Digest) (user.Public, error)
	revoke func(context.Context, session.Digest) error
}

func (s sessionsFake) Create(ctx context.Context, u user.Credentials, d session.Digest, ttl time.Duration) (session.Created, error) {
	if s.create != nil {
		return s.create(ctx, u, d, ttl)
	}
	return session.Created{User: u.Public, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(ttl)}, nil
}
func (s sessionsFake) Lookup(ctx context.Context, d session.Digest) (user.Public, error) {
	if s.lookup != nil {
		return s.lookup(ctx, d)
	}
	return publicUser(), nil
}
func (s sessionsFake) Revoke(ctx context.Context, d session.Digest) error {
	if s.revoke != nil {
		return s.revoke(ctx, d)
	}
	return nil
}
func publicUser() user.Public {
	return user.Public{ID: "0199a000-0000-7000-8000-000000000001", Email: "alice@example.com", Status: "active", CreatedAt: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)}
}
func newHandler() *Handler {
	origin, _ := config.ParseOrigin("https://example.com")
	return New(usersFunc(func(_ context.Context, email string) (user.Credentials, error) {
		u := publicUser()
		u.Email = email
		return user.Credentials{Public: u, PasswordHash: password.DummyPHC}, nil
	}), sessionsFake{}, verifyFunc(func(string, string) (bool, error) { return true, nil }), session.NewTokens(), password.NewBudget(), config.AuthConfig{Mode: "production", Origin: origin, SessionTTL: 24 * time.Hour}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
}
func body(email, secret string) string {
	b, _ := json.Marshal(map[string]string{"email": email, "password": secret})
	return string(b)
}
func req(method, path, payload string) *http.Request {
	r := httptest.NewRequest(method, "https://example.com/api/v1/auth/"+path, strings.NewReader(payload))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://example.com")
	r.Header.Set("X-DevPilot-CSRF", "1")
	return r
}
func perform(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func assert(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d want %d, code %s", w.Code, status, code)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("safe headers")
	}
	if status == 204 {
		if w.Body.Len() != 0 || w.Header().Get("Content-Type") != "" {
			t.Fatal("204 body")
		}
		return
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Fatal("JSON media")
	}
	var decoded struct {
		Error struct{ Code, Message string }
		Data  json.RawMessage
	}
	if json.Unmarshal(w.Body.Bytes(), &decoded) != nil {
		t.Fatal("response JSON")
	}
	if code != "" {
		if decoded.Error.Code != code || decoded.Error.Message != messages[code] || decoded.Data != nil {
			t.Fatal("error contract")
		}
	} else if decoded.Data == nil {
		t.Fatal("success projection")
	}
}
func withCookie(r *http.Request) string {
	token, _, _ := session.NewTokens().New()
	r.Header.Set("Cookie", "__Host-devpilot_session="+token)
	return token
}

func TestLoginStrictInputAndCheapRejection(t *testing.T) {
	good := body("a@example.com", "short")
	cases := []struct {
		payload string
		status  int
		code    string
	}{
		{"", 400, "invalid_request"}, {"null", 400, "invalid_request"}, {"[]", 400, "invalid_request"}, {good + " {}", 400, "invalid_request"},
		{`{"email":"a@example.com"}`, 400, "invalid_request"}, {`{"email":"a@example.com","password":null}`, 400, "invalid_request"},
		{`{"email":1,"password":"a"}`, 400, "invalid_request"}, {`{"email":"a@example.com","password":"a","email":"b@example.com"}`, 400, "invalid_request"},
		{`{"email":"a@example.com","password":"a","\u0065mail":"b@example.com"}`, 400, "invalid_request"},
		{`{"email":"a@example.com","password":"a","status":"active"}`, 400, "invalid_request"},
		{body("国际@example.com", "a"), 422, "validation_failed"}, {body("a@example.com", ""), 422, "validation_failed"}, {body("a@example.com", strings.Repeat("😀", 129)), 422, "validation_failed"},
		{good + strings.Repeat(" ", 4097), 413, "request_too_large"},
	}
	for _, escape := range []string{`\ud800`, `\udfff`, `\udc00\ud800`, `\ud800\u0061`, `\ud800x`, `\u12`, `\uxxxx`, `\ud800\u12xz`, string([]byte{255})} {
		cases = append(cases, struct {
			payload string
			status  int
			code    string
		}{`{"email":"a@example.com","password":"` + escape + `"}`, 400, "invalid_request"})
	}
	for _, tc := range cases {
		for _, saturated := range []bool{false, true} {
			h := newHandler()
			if saturated {
				h.budget.Acquire()
				h.budget.Acquire()
				defer h.budget.Release()
				defer h.budget.Release()
			}
			h.users = usersFunc(func(context.Context, string) (user.Credentials, error) {
				t.Error("cheap rejection queried users")
				return user.Credentials{}, nil
			})
			h.verifier = verifyFunc(func(string, string) (bool, error) { t.Error("cheap rejection computed password"); return false, nil })
			assert(t, perform(h, req("POST", "login", tc.payload)), tc.status, tc.code)
		}
	}
	for _, media := range []string{"", "text/plain", "application/json; charset=latin1", "application/json; extra=x"} {
		r := req("POST", "login", good)
		r.Header.Set("Content-Type", media)
		assert(t, perform(newHandler(), r), 415, "unsupported_media_type")
	}
	r := req("POST", "login", good)
	r.Header.Set("Content-Encoding", "identity")
	assert(t, perform(newHandler(), r), 415, "unsupported_media_type")
	h := newHandler()
	seen := ""
	h.verifier = verifyFunc(func(p, _ string) (bool, error) { seen = p; return true, nil })
	assert(t, perform(h, req("POST", "login", `{"email":" A@Example.COM ","password":"\ud83d\ude00\ufffd "}`)), 200, "")
	if seen != "😀� " {
		t.Fatal("password changed")
	}
	assert(t, perform(h, req("POST", "login", good+strings.Repeat(" ", 4096-len(good)))), 200, "")
	assert(t, perform(h, req("POST", "login", good+strings.Repeat(" ", 4097-len(good)))), 413, "request_too_large")
}

func TestCSRFProofAndMethods(t *testing.T) {
	for _, path := range []string{"login", "logout"} {
		for _, tc := range []struct {
			header string
			values []string
		}{
			{"Origin", nil}, {"Origin", []string{"null"}}, {"Origin", []string{"https://example.com.attacker.com"}}, {"Origin", []string{"https://sub.example.com"}}, {"Origin", []string{"http://example.com"}}, {"Origin", []string{"https://example.com:444"}}, {"Origin", []string{"https://example.com/"}}, {"Origin", []string{"https://example.com", "https://example.com"}},
			{"X-DevPilot-CSRF", nil}, {"X-DevPilot-CSRF", []string{"0"}}, {"X-DevPilot-CSRF", []string{"1", "1"}},
			{"Sec-Fetch-Site", []string{"cross-site"}}, {"Sec-Fetch-Site", []string{"same-site"}}, {"Sec-Fetch-Site", []string{"none"}}, {"Sec-Fetch-Site", []string{"same-origin", "same-origin"}},
		} {
			h := newHandler()
			h.users = usersFunc(func(context.Context, string) (user.Credentials, error) {
				t.Fatal("forged lookup")
				return user.Credentials{}, nil
			})
			h.sessions = sessionsFake{revoke: func(context.Context, session.Digest) error { t.Fatal("forged revoke"); return nil }}
			r := req("POST", path, body("a@example.com", "a"))
			withCookie(r)
			r.Header.Del(tc.header)
			for _, v := range tc.values {
				r.Header.Add(tc.header, v)
			}
			r.Header.Set("Referer", "https://example.com/")
			w := perform(h, r)
			assert(t, w, 403, "csrf_rejected")
			if w.Header().Get("Set-Cookie") != "" {
				t.Fatal("CSRF changed Cookie")
			}
		}
	}
	for _, path := range []string{"login", "logout", "me"} {
		allowed := "POST"
		if path == "me" {
			allowed = "GET"
		}
		for _, method := range []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "HEAD"} {
			if method == allowed {
				continue
			}
			w := perform(newHandler(), req(method, path, "ignored"))
			if w.Code != 405 || w.Header().Get("Allow") != allowed || w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("method or CORS contract")
			}
			if method == "HEAD" {
				if w.Body.Len() != 0 {
					t.Fatal("HEAD body")
				}
			} else {
				assert(t, w, 405, "method_not_allowed")
			}
		}
	}
}

func TestUniformCredentialFailuresAndDummyIsolation(t *testing.T) {
	baseline := ""
	for _, kind := range []string{"missing", "wrong", "disabled", "bad-PHC", "dummy-matching"} {
		h := newHandler()
		var calls atomic.Int32
		phcSeen := ""
		h.users = usersFunc(func(context.Context, string) (user.Credentials, error) {
			u := user.Credentials{Public: publicUser(), PasswordHash: password.DummyPHC}
			switch kind {
			case "missing", "dummy-matching":
				return user.Credentials{}, user.ErrNotFound
			case "disabled":
				u.Status = "disabled"
			case "bad-PHC":
				u.PasswordHash = "$argon2id$v=19$m=4294967295,t=3,p=255$x$x"
			}
			return u, nil
		})
		h.verifier = verifyFunc(func(_, phc string) (bool, error) { calls.Add(1); phcSeen = phc; return kind != "wrong", nil })
		h.sessions = sessionsFake{create: func(context.Context, user.Credentials, session.Digest, time.Duration) (session.Created, error) {
			t.Error("credential rejection created session")
			return session.Created{}, nil
		}}
		w := perform(h, req("POST", "login", body("sentinel@example.com", "devpilot dummy credential")))
		assert(t, w, 401, "invalid_credentials")
		if baseline == "" {
			baseline = w.Body.String()
		}
		if w.Body.String() != baseline || calls.Load() != 1 || phcSeen != password.DummyPHC || w.Header().Get("Set-Cookie") != "" {
			t.Fatal("failure or dummy inconsistency")
		}
	}
}

func TestCookieIdentityAndLogoutContracts(t *testing.T) {
	for _, mode := range []string{"production", "development"} {
		h := newHandler()
		h.policy.Mode = mode
		w := perform(h, req("POST", "login", body("alice@example.com", "a")))
		assert(t, w, 200, "")
		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatal("missing session Cookie")
		}
		c := cookies[0]
		if c.Name != h.cookieName() || c.Path != "/" || c.Domain != "" || !c.HttpOnly || c.Secure != (mode == "production") || c.SameSite != http.SameSiteLaxMode || c.MaxAge < 1 || c.MaxAge > 86400 || len(c.Value) != 43 {
			t.Fatal("Cookie attributes")
		}
		if strings.Contains(w.Body.String(), c.Value) || strings.Contains(w.Body.String(), "password") {
			t.Fatal("secret projection")
		}
		r := req("GET", "me", "")
		r.Header.Del("Origin")
		r.Header.Del("X-DevPilot-CSRF")
		r.AddCookie(c)
		w = perform(h, r)
		assert(t, w, 200, "")
		if w.Header().Get("Set-Cookie") != "" {
			t.Fatal("me changes Cookie")
		}
		r = req("POST", "logout", "")
		r.AddCookie(c)
		w = perform(h, r)
		assert(t, w, 204, "")
		d := w.Result().Cookies()[0]
		if d.Name != c.Name || d.Path != c.Path || d.Domain != c.Domain || d.Secure != c.Secure || !d.HttpOnly || d.SameSite != c.SameSite || d.MaxAge != -1 || !d.Expires.Before(time.Now()) {
			t.Fatal("deletion attributes")
		}
	}
	h := newHandler()
	for _, value := range []string{"", "bad", "a=b; __Host-devpilot_session=bad", "devpilot_session=ignored"} {
		r := req("GET", "me", "")
		r.Header.Set("Cookie", value)
		assert(t, perform(h, r), 401, "unauthenticated")
		r = req("POST", "logout", "")
		r.Header.Set("Cookie", value)
		assert(t, perform(h, r), 204, "")
	}
	h.sessions = sessionsFake{lookup: func(context.Context, session.Digest) (user.Public, error) { return user.Public{}, session.ErrInvalid }}
	r := req("GET", "me", "")
	withCookie(r)
	w := perform(h, r)
	assert(t, w, 401, "unauthenticated")
	if w.Header().Get("Set-Cookie") != "" {
		t.Fatal("invalid me deletes Cookie")
	}
}

func TestDependencyFailurePrivacyAndUnknownCommit(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{user.ErrUnavailable, 503, "service_unavailable"}, {user.ErrTimeout, 503, "database_timeout"}, {user.ErrSchema, 500, "internal_error"}, {errors.New("sentinel driver token"), 500, "internal_error"}} {
		for _, path := range []string{"login", "me", "logout"} {
			h := newHandler()
			var logs bytes.Buffer
			h.logger = slog.New(slog.NewJSONHandler(&logs, nil))
			calls := 0
			h.sessions = sessionsFake{create: func(context.Context, user.Credentials, session.Digest, time.Duration) (session.Created, error) {
				calls++
				return session.Created{}, tc.err
			}, lookup: func(context.Context, session.Digest) (user.Public, error) { return user.Public{}, tc.err }, revoke: func(context.Context, session.Digest) error { calls++; return tc.err }}
			method := "POST"
			if path == "me" {
				method = "GET"
			}
			r := req(method, path, body("sentinel@example.com", "sentinel password"))
			withCookie(r)
			w := perform(h, r)
			assert(t, w, tc.status, tc.code)
			if strings.Contains(logs.String()+w.Body.String(), "sentinel") || w.Header().Get("Set-Cookie") != "" || calls > 1 {
				t.Fatal("unsafe error or auto retry")
			}
		}
	}
	h := newHandler()
	h.users = usersFunc(func(context.Context, string) (user.Credentials, error) {
		return user.Credentials{}, user.ErrUnavailable
	})
	h.verifier = verifyFunc(func(string, string) (bool, error) { t.Fatal("DB failure used dummy"); return false, nil })
	assert(t, perform(h, req("POST", "login", body("a@example.com", "a"))), 503, "service_unavailable")
	h = newHandler()
	h.tokens = tokenFunc(func() (string, session.Digest, error) { return "", session.Digest{}, errors.New("sentinel RNG") })
	assert(t, perform(h, req("POST", "login", body("a@example.com", "a"))), 500, "internal_error")
}

type hashFunc func(string) (string, error)

func (f hashFunc) Hash(s string) (string, error) { return f(s) }

type registerStore func(context.Context, string, string) (user.Public, error)

func (f registerStore) Create(c context.Context, e, p string) (user.Public, error) { return f(c, e, p) }
func registrationWithBudget(budget *password.Budget) http.Handler {
	return registration.New(registerStore(func(context.Context, string, string) (user.Public, error) { return publicUser(), nil }), hashFunc(func(string) (string, error) { return password.DummyPHC, nil }), slog.New(slog.NewJSONHandler(io.Discard, nil)), budget)
}

func TestMixedBudgetCancellationAndFaultRecovery(t *testing.T) {
	for _, dummy := range []bool{false, true} {
		h := newHandler()
		if dummy {
			h.users = usersFunc(func(context.Context, string) (user.Credentials, error) { return user.Credentials{}, user.ErrNotFound })
		}
		h.sessions = sessionsFake{create: func(context.Context, user.Credentials, session.Digest, time.Duration) (session.Created, error) {
			t.Error("canceled verification started session creation")
			return session.Created{}, nil
		}}
		entered := make(chan struct{}, 2)
		release := make(chan struct{})
		done := make(chan struct{}, 2)
		var computations atomic.Int32
		h.verifier = verifyFunc(func(string, string) (bool, error) {
			computations.Add(1)
			entered <- struct{}{}
			<-release
			return true, nil
		})
		registering := registration.New(registerStore(func(context.Context, string, string) (user.Public, error) {
			t.Error("canceled hash started insertion")
			return user.Public{}, nil
		}), hashFunc(func(string) (string, error) {
			computations.Add(1)
			entered <- struct{}{}
			<-release
			return password.DummyPHC, nil
		}), slog.New(slog.NewJSONHandler(io.Discard, nil)), h.budget)
		authContext, cancelAuth := context.WithCancel(context.Background())
		registerContext, cancelRegister := context.WithCancel(context.Background())
		authWriter := &observedWriter{ResponseWriter: httptest.NewRecorder()}
		registerWriter := &observedWriter{ResponseWriter: httptest.NewRecorder()}
		go func() {
			h.ServeHTTP(authWriter, req("POST", "login", body("a@example.com", "a")).WithContext(authContext))
			done <- struct{}{}
		}()
		go func() {
			registering.ServeHTTP(registerWriter, req("POST", "register", body("c@example.com", "long register password")).WithContext(registerContext))
			done <- struct{}{}
		}()
		<-entered
		<-entered
		cancelAuth()
		cancelRegister()
		assert(t, perform(h, req("POST", "login", body("b@example.com", "b"))), 503, "authentication_busy")
		w := perform(registrationWithBudget(h.budget), req("POST", "register", body("d@example.com", "long register password")))
		if w.Code != 503 || !strings.Contains(w.Body.String(), "registration_busy") || h.budget.Active() != 2 || computations.Load() != 2 {
			t.Fatal("mixed budget escaped")
		}
		close(release)
		<-done
		<-done
		if h.budget.Active() != 0 || len(h.workflows) != 0 || authWriter.writes.Load() != 0 || registerWriter.writes.Load() != 0 {
			t.Fatal("cancellation leaked work or attempted response")
		}
		w = perform(registrationWithBudget(h.budget), req("POST", "register", body("e@example.com", "long register password")))
		if w.Code != 201 || w.Header().Get("Set-Cookie") != "" {
			t.Fatal("registration not recovered")
		}
	}

	for _, fault := range []string{"panic", "verify-error", "rng", "create", "schema"} {
		h := newHandler()
		switch fault {
		case "panic":
			h.verifier = verifyFunc(func(string, string) (bool, error) { panic("sentinel PHC secret") })
		case "verify-error":
			h.verifier = verifyFunc(func(string, string) (bool, error) { return false, errors.New("sentinel") })
		case "rng":
			h.tokens = tokenFunc(func() (string, session.Digest, error) { return "", session.Digest{}, errors.New("sentinel") })
		case "create", "schema":
			h.sessions = sessionsFake{create: func(context.Context, user.Credentials, session.Digest, time.Duration) (session.Created, error) {
				return session.Created{}, user.ErrSchema
			}}
		}
		assert(t, perform(h, req("POST", "login", body("a@example.com", "a"))), 500, "internal_error")
		if h.budget.Active() != 0 || len(h.workflows) != 0 {
			t.Fatal("fault leaked shared capacity")
		}
		w := perform(registrationWithBudget(h.budget), req("POST", "register", body("a@example.com", "long register password")))
		if w.Code != 201 {
			t.Fatal("login fault poisoned registration")
		}
	}
}

func TestWorkflowBoundAndTimeouts(t *testing.T) {
	h := newHandler()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	done := make(chan struct{}, 2)
	h.sessions = sessionsFake{create: func(context.Context, user.Credentials, session.Digest, time.Duration) (session.Created, error) {
		entered <- struct{}{}
		<-release
		return session.Created{User: publicUser(), ExpiresAt: time.Now().Add(24 * time.Hour)}, nil
	}}
	for i := 0; i < 2; i++ {
		go func() {
			h.ServeHTTP(httptest.NewRecorder(), req("POST", "login", body("a@example.com", "a")))
			done <- struct{}{}
		}()
	}
	<-entered
	<-entered
	if h.budget.Active() != 0 {
		t.Fatal("DB wait retained Argon slots")
	}
	w := perform(h, req("POST", "login", body("b@example.com", "b")))
	assert(t, w, 503, "authentication_busy")
	w = perform(registrationWithBudget(h.budget), req("POST", "register", body("c@example.com", "long register password")))
	if w.Code != 201 {
		t.Fatal("workflow capacity leaked into registration")
	}
	close(release)
	<-done
	<-done
	if len(h.workflows) != 0 {
		t.Fatal("workflow capacity leaked")
	}
	h = newHandler()
	h.databaseTimeout = time.Millisecond
	h.users = usersFunc(func(ctx context.Context, _ string) (user.Credentials, error) {
		<-ctx.Done()
		return user.Credentials{}, user.ErrTimeout
	})
	assert(t, perform(h, req("POST", "login", body("a@example.com", "a"))), 503, "database_timeout")
	h = newHandler()
	h.processingTimeout = time.Millisecond
	h.verifier = verifyFunc(func(string, string) (bool, error) { time.Sleep(5 * time.Millisecond); return true, nil })
	assert(t, perform(h, req("POST", "login", body("a@example.com", "a"))), 503, "service_unavailable")
}

type observedWriter struct {
	http.ResponseWriter
	writes atomic.Int32
}

func (w *observedWriter) WriteHeader(status int) {
	w.writes.Add(1)
	w.ResponseWriter.WriteHeader(status)
}
func (w *observedWriter) Write(b []byte) (int, error) {
	w.writes.Add(1)
	return w.ResponseWriter.Write(b)
}
func TestClientCancellationPropagatesWithoutResponse(t *testing.T) {
	for _, path := range []string{"login", "me", "logout"} {
		h := newHandler()
		entered := make(chan struct{})
		done := make(chan struct{})
		wait := func(ctx context.Context) error { close(entered); <-ctx.Done(); return user.ErrCanceled }
		h.users = usersFunc(func(ctx context.Context, _ string) (user.Credentials, error) { return user.Credentials{}, wait(ctx) })
		h.sessions = sessionsFake{lookup: func(ctx context.Context, _ session.Digest) (user.Public, error) { return user.Public{}, wait(ctx) }, revoke: func(ctx context.Context, _ session.Digest) error { return wait(ctx) }}
		ctx, cancel := context.WithCancel(context.Background())
		method := "POST"
		if path == "me" {
			method = "GET"
		}
		r := req(method, path, body("a@example.com", "a")).WithContext(ctx)
		withCookie(r)
		w := &observedWriter{ResponseWriter: httptest.NewRecorder()}
		go func() { defer close(done); h.ServeHTTP(w, r) }()
		<-entered
		cancel()
		<-done
		if w.writes.Load() != 0 || w.Header().Get("Set-Cookie") != "" || h.budget.Active() != 0 || len(h.workflows) != 0 {
			t.Fatal("canceled response or capacity leak")
		}
	}
}

// Models the observed scheduling gap between a passed deadline and delivery
// of context timer cancellation, without relying on sleeps or OS scheduling.
type elapsedDeadlineContext struct{ context.Context }

func (elapsedDeadlineContext) Deadline() (time.Time, bool) { return time.Now().Add(-time.Second), true }
func TestElapsedDeadlineDoesNotWaitForTimerDelivery(t *testing.T) {
	h := newHandler()
	w := httptest.NewRecorder()
	r := req("POST", "login", body("a@example.com", "a"))
	if !h.expired(w, r, elapsedDeadlineContext{context.Background()}) {
		t.Fatal("elapsed deadline accepted before timer delivery")
	}
	assert(t, w, 503, "service_unavailable")
}
