//go:build schedulerfixture && linux && processfixture

package probeinput

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/monitoring/probesandbox"
)

func envelope() handoff.Envelope {
	n := time.Now().UTC()
	return handoff.Envelope{Lineage: handoff.Lineage{WorkspaceID: "01900000-0000-7000-8000-000000000001", ProjectID: "01900000-0000-7000-8000-000000000002", MonitorID: "01900000-0000-7000-8000-000000000003", RunID: "01900000-0000-7000-8000-000000000004", ConfigurationVersion: "1", ActivationGeneration: "2", SchedulerEpoch: "3", FencingToken: "4", NamespaceIncarnation: "01900000-0000-7000-8000-000000000005", SupervisorIncarnation: "01900000-0000-7000-8000-000000000006"}, StartedAt: n, PermitDeadline: n.Add(35 * time.Second), Target: []byte("https://fixture.example.com/URL_PATH_SENTINEL"), TimeoutSeconds: 10, ExpectedStatus: 200, IntervalSeconds: 60}
}
func pipe(t *testing.T, data []byte) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 8192 {
		t.Fatal("fixture pipe payload too large")
	}
	if _, err = w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}
func TestCredentialFreeInputProcess(t *testing.T) {
	if os.Getuid() != 0 {
		t.Fatal("requires explicit isolated Linux parent launch identity")
	}
	frame, receipt, err := handoff.Encode(envelope())
	if err != nil {
		t.Fatal(err)
	}
	rf, err := handoff.ReceiptFrame(receipt)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/setpriv", "--bounding-set=-all", "--inh-caps=-all", "--ambient-caps=-all", "--reuid=65534", "--regid=65534", "--clear-groups", "--no-new-privs", "/testbin/cp4-probe")
	cmd.Env = []string{"LANG=C", "TZ=UTC"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	cmd.ExtraFiles = []*os.File{pipe(t, frame), pipe(t, rf)}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var output struct {
		Proof        probesandbox.Proof `json:"proof"`
		ConsumedOnce bool               `json:"consumed_once"`
		Valid        bool               `json:"kernel_snapshot_valid"`
	}
	raw, err := bufio.NewReader(io.LimitReader(stdout, 4097)).ReadBytes('\n')
	if err != nil || len(raw) > 4096 || json.Unmarshal(raw, &output) != nil {
		_ = cmd.Wait()
		t.Fatal("missing bounded process proof", err, stderr.String())
	}
	p := output.Proof
	if p.UID != 65534 || p.GID != 65534 || !p.CapabilitiesZero || !p.NoNewPrivileges || !p.SeccompThreads || p.MemoryMax != 134217728 || p.CPUQuota != p.CPUPeriod || p.PIDsMax != 128 || p.FDLimit != 256 || !p.FDExhaustion || !p.InheritedFDsOnly || !p.RootReadOnly || !p.EnvironmentClean || !p.SecretsUnavailable || !p.SocketsDenied || !p.AdminDenied || !output.ConsumedOnce || !output.Valid {
		t.Fatal("process boundary proof mismatch", string(raw))
	}
	// External observation while the real process is still alive. Dumpable=0
	// prevents even another same-UID process from recovering its environment/FDs.
	for _, name := range []string{"status", "limits", "cgroup", "cmdline"} {
		b, err := os.ReadFile("/proc/" + strconv.Itoa(cmd.Process.Pid) + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []string{"URL_PATH_SENTINEL", "DB_PASSWORD_SENTINEL", "COOKIE_SENTINEL", "CLOUD_SECRET_SENTINEL"} {
			if bytes.Contains(b, []byte(s)) {
				t.Fatal("credential/URL escaped child process boundary")
			}
		}
		if name != "cmdline" {
			t.Logf("external pid=%d %s=%s", cmd.Process.Pid, name, b)
		}
	}
	if len(cmd.Args) != 9 || len(cmd.Env) != 2 {
		t.Fatal("unexpected argv/environment")
	}
	t.Logf("child proof=%s", raw)
	if _, err = stdin.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	stdin.Close()
	if err = cmd.Wait(); err != nil {
		t.Fatal("input-only process failed", err, stderr.String())
	}

	// Re-exec with exactly the same already-consumed anonymous readers cannot
	// restore the capability: both pipes remain at EOF in the new process.
	restart := exec.CommandContext(ctx, "/usr/bin/setpriv", "--bounding-set=-all", "--inh-caps=-all", "--ambient-caps=-all", "--reuid=65534", "--regid=65534", "--clear-groups", "--no-new-privs", "/testbin/cp4-probe")
	restart.Env = cmd.Env
	restart.ExtraFiles = cmd.ExtraFiles
	var rejected bytes.Buffer
	restart.Stdout = &rejected
	restart.Stderr = &rejected
	if err = restart.Run(); err == nil || rejected.String() != "probe input rejected\n" {
		t.Fatal("restart restored consumed pipe", err, rejected.String())
	}
	t.Log("real restart with old readers rejected at EOF; no reconnect state or alternate input channel")
	for _, s := range []string{"URL_PATH_SENTINEL", "DB_PASSWORD_SENTINEL", "COOKIE_SENTINEL", "CLOUD_SECRET_SENTINEL"} {
		if bytes.Contains(raw, []byte(s)) || strings.Contains(stderr.String(), s) {
			t.Fatal("diagnostic leak")
		}
	}
}
func TestInputProcessRejectsMalformedAndCrossRunReplay(t *testing.T) {
	for _, mode := range []string{"truncated", "oversized", "forged", "cross-run", "cross-version", "cross-scope", "cross-epoch", "cross-fence", "cross-namespace", "duplicate-frame", "bad-receipt", "noncanonical-json"} {
		t.Run(mode, func(t *testing.T) {
			frame, r, err := handoff.Encode(envelope())
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "truncated":
				frame = frame[:100]
			case "oversized":
				frame = append(frame, 0)
			case "forged":
				frame[100] ^= 1
			case "cross-run":
				r.RunID = "01900000-0000-7000-8000-000000000007"
			case "cross-version":
				r.ConfigurationVersion = "2"
			case "cross-scope":
				r.ProjectID = r.WorkspaceID
			case "cross-epoch":
				r.SchedulerEpoch = "99"
			case "cross-fence":
				r.FencingToken = "99"
			case "cross-namespace":
				r.NamespaceIncarnation = r.SupervisorIncarnation
			case "duplicate-frame":
				frame = append(frame, frame...)
			case "noncanonical-json":
				binary.BigEndian.PutUint32(frame[8:12], 4095)
			}
			rf, err := handoff.ReceiptFrame(r)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "bad-receipt" {
				rf = rf[:100]
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "/usr/bin/setpriv", "--bounding-set=-all", "--inh-caps=-all", "--ambient-caps=-all", "--reuid=65534", "--regid=65534", "--clear-groups", "--no-new-privs", "/testbin/cp4-probe")
			cmd.Env = []string{"LANG=C", "TZ=UTC"}
			cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
			cmd.ExtraFiles = []*os.File{pipe(t, frame), pipe(t, rf)}
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output
			err = cmd.Run()
			if ctx.Err() != nil {
				t.Fatal("bounded negative test timed out", mode)
			}
			var code *exec.ExitError
			if !errors.As(err, &code) || code.ExitCode() != 2 || output.String() != "probe input rejected\n" {
				t.Fatal("malformed input accepted or leaked", mode, err, output.String())
			}
			t.Logf("real child case=%s exit=%d fixed diagnostic only", mode, code.ExitCode())
		})
	}
}
