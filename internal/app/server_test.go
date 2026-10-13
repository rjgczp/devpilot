package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewServerTimeouts(t *testing.T) {
	server := NewServer(":8080", http.NewServeMux())
	if server.ReadHeaderTimeout != 5*time.Second || server.ReadTimeout != 10*time.Second || server.WriteTimeout != 10*time.Second || server.IdleTimeout != 60*time.Second {
		t.Fatalf("unexpected server timeouts: %+v", server)
	}
}

func TestIncompleteHeadersTimeOut(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(listener.Addr().String(), http.NewServeMux())
	server.ReadHeaderTimeout = 50 * time.Millisecond
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost: incomplete")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err == nil && !strings.Contains(line, "408") {
		t.Fatalf("expected timeout response or closed connection, got %q", line)
	}
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("server left incomplete header connection open")
	}
}

func TestJSONLogger(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(&output, slog.LevelInfo)
	logger.Info("server started", "address", "127.0.0.1:8080")
	logger.Error("database readiness check failed", "reason", "unavailable")

	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		if entry["msg"] == "" || entry["level"] == "" {
			t.Fatalf("missing structured log fields: %s", line)
		}
	}
	if strings.Contains(output.String(), "DATABASE_URL") || strings.Contains(output.String(), "top-secret") {
		t.Fatalf("logger output contains a secret: %s", output.String())
	}
}
