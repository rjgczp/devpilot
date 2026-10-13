package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestProcessSignals(t *testing.T) {
	binary := buildBinary(t)
	for _, signal := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(fmt.Sprint(signal), func(t *testing.T) {
			address := freeAddress(t)
			cmd, output := startBinary(t, binary, address, "1s")
			waitForHealth(t, address)
			if err := cmd.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			if err := waitForProcess(t, cmd); err != nil {
				t.Fatalf("expected clean exit: %v; logs: %s", err, output.String())
			}
			if !strings.Contains(output.String(), "server started") || !strings.Contains(output.String(), "server stopped") {
				t.Fatalf("missing lifecycle logs: %s", output.String())
			}
			for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
				var event map[string]any
				if err := json.Unmarshal([]byte(line), &event); err != nil {
					t.Fatalf("non-JSON process log: %v: %s", err, line)
				}
			}
			if strings.Contains(output.String(), "top-secret") || strings.Contains(output.String(), "DATABASE_URL") {
				t.Fatalf("process log disclosed secret: %s", output.String())
			}
		})
	}
}

func TestProcessSignalsWithInFlightRequest(t *testing.T) {
	binary := buildBinary(t)
	for _, signal := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(fmt.Sprint(signal), func(t *testing.T) {
			database, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			accepted := make(chan net.Conn, 1)
			go func() {
				conn, err := database.Accept()
				if err == nil {
					accepted <- conn
				}
			}()

			address := freeAddress(t)
			cmd, output := startBinary(t, binary, address, "2s",
				"DATABASE_URL=postgres://devpilot:top-secret@"+database.Addr().String()+"/devpilot?sslmode=disable",
				"READINESS_TIMEOUT=250ms",
			)
			waitForHealth(t, address)
			readyResult := make(chan error, 1)
			go func() {
				client := &http.Client{Timeout: time.Second}
				response, err := client.Get("http://" + address + "/readyz")
				if err == nil {
					_ = response.Body.Close()
					if response.StatusCode != http.StatusServiceUnavailable {
						err = fmt.Errorf("readyz status = %d, want 503", response.StatusCode)
					}
				}
				readyResult <- err
			}()
			select {
			case conn := <-accepted:
				defer conn.Close()
			case <-time.After(time.Second):
				t.Fatal("readiness request did not reach database checker")
			}
			if err := cmd.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			if err := <-readyResult; err != nil {
				t.Fatalf("in-flight request failed: %v", err)
			}
			if err := waitForProcess(t, cmd); err != nil {
				t.Fatalf("expected clean exit: %v; logs: %s", err, output.String())
			}
		})
	}
}

func TestProcessStartsWhenDatabaseIsUnavailable(t *testing.T) {
	binary := buildBinary(t)
	address := freeAddress(t)
	cmd, output := startBinary(t, binary, address, "1s")
	waitForHealth(t, address)

	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + address + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want 503", response.StatusCode)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := waitForProcess(t, cmd); err != nil {
		t.Fatalf("expected clean exit: %v; logs: %s", err, output.String())
	}
}

func TestProcessRejectsInvalidConfigurationWithoutSecret(t *testing.T) {
	binary := buildBinary(t)
	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(), "DATABASE_URL=postgres://user:top-secret@", "HTTP_ADDR=127.0.0.1:8099")
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "DATABASE_URL") {
		t.Fatalf("expected configuration failure, got %v and %s", err, output)
	}
	if strings.Contains(string(output), "top-secret") {
		t.Fatalf("configuration failure leaked secret: %s", output)
	}
}

func TestProcessShutdownTimeout(t *testing.T) {
	binary := buildBinary(t)
	address := freeAddress(t)
	cmd, output := startBinary(t, binary, address, "30ms")
	waitForHealth(t, address)
	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("GET /healthz HTTP/1.1\r\nHost: incomplete")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := waitForProcess(t, cmd); err == nil {
		t.Fatalf("expected non-zero timeout exit; logs: %s", output.String())
	}
	if !strings.Contains(output.String(), "deadline_exceeded") {
		t.Fatalf("missing timeout reason: %s", output.String())
	}
}

func buildBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "devpilot-api")
	cmd := exec.Command("go", "build", "-o", path, ".")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build API binary: %v: %s", err, output)
	}
	return path
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	return address
}

func startBinary(t *testing.T, binary, address, timeout string, extraEnv ...string) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()
	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(),
		"DATABASE_URL=postgres://devpilot:top-secret@127.0.0.1:1/devpilot?sslmode=disable",
		"HTTP_ADDR="+address,
		"SHUTDOWN_TIMEOUT="+timeout,
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	output := &bytes.Buffer{}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	return cmd, output
}

func waitForHealth(t *testing.T, address string) {
	t.Helper()
	client := &http.Client{Timeout: 150 * time.Millisecond}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get("http://" + address + "/healthz")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("API process did not become healthy")
}

func waitForProcess(t *testing.T, cmd *exec.Cmd) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("API process did not exit")
		return nil
	}
}
