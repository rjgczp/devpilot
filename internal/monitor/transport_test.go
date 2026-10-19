package monitor

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/app"
)

func TestMonitorSlowBodiesAndEarlyDrainAreBounded(t *testing.T) {
	for _, method := range []string{"POST", "PATCH", "GET", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			s := &repositoryStub{}
			h := testHandler(s, io.Discard)
			h.processingTimeout = 60 * time.Millisecond
			server := httptest.NewServer(h)
			defer server.Close()
			conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(time.Second))
			route := listPath
			if method == "PATCH" {
				route = itemPath
			}
			tag := ""
			if method == "PATCH" || method == "GET" {
				tag = "If-Match: \"monitor:" + exampleID + ":1\"\r\n"
			}
			_, err = io.WriteString(conn, method+" "+route+" HTTP/1.1\r\nHost: example.com\r\nOrigin: https://app.example.com\r\nX-DevPilot-CSRF: 1\r\nCookie: __Host-devpilot_session="+strings.Repeat("A", 43)+"\r\n"+tag+"Content-Type: application/json\r\nTransfer-Encoding: chunked\r\n\r\n")
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			res, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err == nil {
				defer res.Body.Close()
				if res.StatusCode == 200 || res.StatusCode == 201 {
					t.Fatal("slow request false success")
				}
			} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
				t.Fatal("unbounded body")
			}
			if time.Since(start) > 500*time.Millisecond || s.calls.Load() != 0 {
				t.Fatal("slow body database work/drain")
			}
		})
	}
}
func TestMonitorClientDisconnectCancelsMutation(t *testing.T) {
	entered, finished := make(chan struct{}), make(chan struct{})
	s := &repositoryStub{run: func(ctx context.Context) { close(entered); <-ctx.Done(); close(finished) }}
	h := testHandler(s, io.Discard)
	server := httptest.NewServer(h)
	defer server.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"name":"x","url":"https://example.com/"}`
	_, err = io.WriteString(conn, "POST "+listPath+" HTTP/1.1\r\nHost: example.com\r\nOrigin: https://app.example.com\r\nX-DevPilot-CSRF: 1\r\nCookie: __Host-devpilot_session="+strings.Repeat("A", 43)+"\r\nContent-Type: application/json\r\nContent-Length: "+strconv.Itoa(len(payload))+"\r\n\r\n"+payload)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		conn.Close()
		t.Fatal("Store not reached")
	}
	conn.Close()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("disconnect did not cancel Store")
	}
}

func TestMonitorReadIgnoresBodyWithBoundedDrain(t *testing.T) {
	s := &repositoryStub{}
	h := testHandler(s, io.Discard)
	h.processingTimeout = 50 * time.Millisecond
	server := httptest.NewServer(h)
	defer server.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Second))
	io.WriteString(conn, "GET "+listPath+" HTTP/1.1\r\nHost: example.com\r\nCookie: __Host-devpilot_session="+strings.Repeat("A", 43)+"\r\nTransfer-Encoding: chunked\r\n\r\n")
	start := time.Now()
	res, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || time.Since(start) > 500*time.Millisecond || s.calls.Load() != 1 {
		t.Fatal("GET body changed read or drain unbounded")
	}
}

func TestMonitorServerRejectsPathAliasesWithoutRedirect(t *testing.T) {
	s := &repositoryStub{}
	h := testHandler(s, io.Discard)
	miss := http.NotFoundHandler()
	server := httptest.NewServer(app.NewRoutes(miss, miss, miss, miss, miss, h))
	defer server.Close()
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for _, path := range []string{listPath + "/", listPath + "/../monitors", strings.Replace(listPath, "/projects/", "//projects/", 1), strings.Replace(listPath, "/monitors", "/%6donitors", 1), itemPath + "/run"} {
		r, _ := http.NewRequest("PATCH", server.URL+path, strings.NewReader(`{"name":"x"}`))
		resp, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 || resp.Header.Get("Location") != "" {
			t.Fatal("alias was accepted/redirected", path, resp.StatusCode)
		}
	}
	if s.calls.Load() != 0 {
		t.Fatal("path rejection reached DB")
	}
}

type panicAfterHeader struct{ header http.Header }

func (w *panicAfterHeader) Header() http.Header       { return w.header }
func (w *panicAfterHeader) WriteHeader(int)           { panic("PRIVATE write panic") }
func (w *panicAfterHeader) Write([]byte) (int, error) { panic("PRIVATE write panic") }
func TestMonitorPostWritePanicAborts(t *testing.T) {
	defer func() {
		if recover() != http.ErrAbortHandler {
			t.Error("post-write panic did not abort")
		}
	}()
	testHandler(&repositoryStub{}, io.Discard).ServeHTTP(&panicAfterHeader{header: make(http.Header)}, request("GET", itemPath, ""))
}
