package monitor

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/rjgczp/devpilot/internal/credentials"
)

type ExecutionAuthorizationInput struct {
	ConfigurationVersion string
	ConsentProfile       string
	AuthorizationSeconds int
}

type ExecutionAuthorization struct {
	WorkspaceID            string    `json:"workspace_id"`
	ProjectID              string    `json:"project_id"`
	MonitorID              string    `json:"monitor_id"`
	ConfigurationVersion   string    `json:"configuration_version"`
	ActivationGeneration   string    `json:"activation_generation"`
	SchedulerEpoch         string    `json:"scheduler_epoch"`
	IssuedAt               time.Time `json:"issued_at"`
	ExpiresAt              time.Time `json:"expires_at"`
	EffectiveAuthorization bool      `json:"effective_authorization"`
	BlockedReason          *string   `json:"blocked_reason"`
}

func parseExecutionAuthorization(b []byte) (ExecutionAuthorizationInput, bool) {
	var a ExecutionAuthorizationInput
	if !credentials.ValidJSON(b) {
		return a, false
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		return a, false
	}
	seen := map[string]bool{}
	for d.More() {
		tok, err = d.Token()
		if err != nil {
			return a, false
		}
		key, ok := tok.(string)
		if !ok || seen[key] {
			return a, false
		}
		seen[key] = true
		var raw json.RawMessage
		if d.Decode(&raw) != nil || len(raw) == 0 {
			return a, false
		}
		switch key {
		case "configuration_version", "consent_profile":
			if raw[0] != '"' {
				return a, false
			}
			var value string
			if json.Unmarshal(raw, &value) != nil {
				return a, false
			}
			if key == "configuration_version" {
				a.ConfigurationVersion = value
			} else {
				a.ConsentProfile = value
			}
		case "authorization_seconds":
			if raw[0] == 'n' || json.Unmarshal(raw, &a.AuthorizationSeconds) != nil {
				return a, false
			}
		default:
			return a, false
		}
	}
	return a, len(seen) == 3
}

func validateExecutionAuthorization(a ExecutionAuthorizationInput) map[string]string {
	fields := map[string]string{}
	if _, ok := positiveVersion(a.ConfigurationVersion); !ok {
		fields["configuration_version"] = "invalid_value"
	}
	if a.ConsentProfile != "periodic-http-get-v1" {
		fields["consent_profile"] = "invalid_value"
	}
	if a.AuthorizationSeconds < 60 || a.AuthorizationSeconds > 86400 {
		fields["authorization_seconds"] = "invalid_value"
	}
	return fields
}
