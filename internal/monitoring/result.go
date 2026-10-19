package monitoring

import "time"

type Failure string

const (
	DNSFailed        Failure = "dns_failed"
	BlockedTarget    Failure = "blocked_target"
	ConnectFailed    Failure = "connect_failed"
	TLSFailed        Failure = "tls_failed"
	Timeout          Failure = "timeout"
	Canceled         Failure = "canceled"
	ResponseLimit    Failure = "response_limit"
	ProtocolError    Failure = "protocol_error"
	UnexpectedStatus Failure = "unexpected_status"
)

// Result is an observation of exactly one admitted snapshot. Preflight rejects
// return an error and a nil Result. There is no persistence/current-health DTO.
type Result struct {
	WorkspaceID          string        `json:"workspace_id"`
	ProjectID            string        `json:"project_id"`
	MonitorID            string        `json:"monitor_id"`
	ConfigurationVersion string        `json:"configuration_version"`
	StartedAt            time.Time     `json:"started_at"`
	FinishedAt           time.Time     `json:"finished_at"`
	Duration             time.Duration `json:"duration"`
	HTTPStatus           *int          `json:"http_status"`
	Success              bool          `json:"success"`
	FailureCode          *Failure      `json:"failure_code"`
}

type failureError struct{ code Failure }

func (e failureError) Error() string { return string(e.code) }
func fail(code Failure) error        { return failureError{code} }

func result(s Snapshot, start, finish time.Time, status *int, code Failure) *Result {
	r := &Result{WorkspaceID: s.scope.WorkspaceID, ProjectID: s.scope.ProjectID, MonitorID: s.scope.MonitorID,
		ConfigurationVersion: s.Version(), StartedAt: start.UTC(), FinishedAt: finish.UTC(), Duration: finish.Sub(start), HTTPStatus: status, Success: code == ""}
	if code != "" {
		r.FailureCode = &code
	}
	return r
}
