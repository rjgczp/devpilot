package monitor

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/rjgczp/devpilot/internal/credentials"
)

const ActivationMaxBodyBytes = 1024

type ActivationInput struct{ DesiredState, ConfigurationVersion string }
type Activation struct {
	WorkspaceID                    string     `json:"workspace_id"`
	ProjectID                      string     `json:"project_id"`
	MonitorID                      string     `json:"monitor_id"`
	ConfigurationVersion           string     `json:"configuration_version"`
	DesiredState                   string     `json:"desired_state"`
	ActivationConfigurationVersion *string    `json:"activation_configuration_version"`
	ActivationGeneration           string     `json:"activation_generation"`
	NextDueAt                      *time.Time `json:"next_due_at"`
	NextEnableAllowedAt            *time.Time `json:"next_enable_allowed_at"`
	LastTransitionKind             string     `json:"last_transition_kind"`
	CreatedAt                      time.Time  `json:"created_at"`
	UpdatedAt                      time.Time  `json:"updated_at"`
	ExecutionState                 string     `json:"execution_state"`
	BlockedReason                  string     `json:"blocked_reason"`
}

var ErrActivationLimit = errors.New("activation intent limit reached")

type ActivationRateLimit struct{ RetryAfter int }

func (ActivationRateLimit) Error() string { return "activation intent cooling" }

func positiveVersion(s string) (int64, bool) {
	n, e := strconv.ParseInt(s, 10, 64)
	return n, e == nil && n > 0 && strconv.FormatInt(n, 10) == s
}
func ActivationETag(a Activation) string {
	return `"monitor-activation:` + a.MonitorID + `:` + a.ConfigurationVersion + `:` + a.ActivationGeneration + `"`
}
func activationCondition(tag string) (string, int64, int64, bool) {
	if len(tag) > 128 || !strings.HasPrefix(tag, `"monitor-activation:`) || !strings.HasSuffix(tag, `"`) {
		return "", 0, 0, false
	}
	p := strings.Split(strings.TrimSuffix(strings.TrimPrefix(tag, `"monitor-activation:`), `"`), ":")
	if len(p) != 3 || !validUUID(p[0]) {
		return "", 0, 0, false
	}
	v, ok := positiveVersion(p[1])
	g, okg := positiveVersion(p[2])
	return p[0], v, g, ok && okg
}
func parseActivation(b []byte) (ActivationInput, bool) {
	var a ActivationInput
	if !credentials.ValidJSON(b) {
		return a, false
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	tok, e := d.Token()
	if e != nil || tok != json.Delim('{') {
		return a, false
	}
	seen := map[string]bool{}
	for d.More() {
		tok, e = d.Token()
		if e != nil {
			return a, false
		}
		k, ok := tok.(string)
		if !ok || seen[k] {
			return a, false
		}
		seen[k] = true
		var raw json.RawMessage
		if d.Decode(&raw) != nil || len(raw) == 0 || raw[0] != '"' {
			return a, false
		}
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return a, false
		}
		switch k {
		case "desired_state":
			a.DesiredState = s
		case "configuration_version":
			a.ConfigurationVersion = s
		default:
			return a, false
		}
	}
	return a, len(seen) == 2
}
func validateActivation(a ActivationInput) map[string]string {
	fields := map[string]string{}
	if a.DesiredState != "enabled" && a.DesiredState != "disabled" {
		fields["desired_state"] = "invalid_value"
	}
	if _, ok := positiveVersion(a.ConfigurationVersion); !ok {
		fields["configuration_version"] = "invalid_value"
	}
	return fields
}
