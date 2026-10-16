package project

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProjectSlowBodiesAndEarlyDrainAreBounded(t *testing.T) {
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
			_, err = io.WriteString(conn, method+" "+route+" HTTP/1.1\r\nHost: example.com\r\nOrigin: https://app.example.com\r\nX-DevPilot-CSRF: 1\r\nCookie: __Host-devpilot_session="+strings.Repeat("A", 43)+"\r\nIf-Match: \"project:"+exampleID+":1\"\r\nContent-Type: application/json\r\nTransfer-Encoding: chunked\r\n\r\n")
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
func TestProjectClientDisconnectCancelsMutation(t *testing.T) {
	entered, finished := make(chan struct{}), make(chan struct{})
	s := &repositoryStub{run: func(ctx context.Context) { close(entered); <-ctx.Done(); close(finished) }}
	h := testHandler(s, io.Discard)
	server := httptest.NewServer(h)
	defer server.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"name":"x"}`
	_, err = io.WriteString(conn, "POST "+listPath+" HTTP/1.1\r\nHost: example.com\r\nOrigin: https://app.example.com\r\nX-DevPilot-CSRF: 1\r\nCookie: __Host-devpilot_session="+strings.Repeat("A", 43)+"\r\nContent-Type: application/json\r\nContent-Length: 12\r\n\r\n"+payload)
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

func TestProjectReadIgnoresBodyWithBoundedDrain(t *testing.T) {
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
