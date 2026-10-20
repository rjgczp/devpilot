package monitor

import "time"

type Attempt struct {
	WorkspaceID          string     `json:"workspace_id"`
	ProjectID            string     `json:"project_id"`
	MonitorID            string     `json:"monitor_id"`
	RunID                string     `json:"run_id"`
	ConfigurationVersion string     `json:"configuration_version"`
	ActivationGeneration string     `json:"activation_generation"`
	SchedulerEpoch       string     `json:"scheduler_epoch"`
	FencingToken         string     `json:"fencing_token"`
	AttemptState         string     `json:"attempt_state"`
	ScheduledAt          time.Time  `json:"scheduled_at"`
	ClaimedAt            time.Time  `json:"claimed_at"`
	StartedAt            *time.Time `json:"started_at"`
	FinishedAt           *time.Time `json:"finished_at"`
	DurationMS           *int       `json:"duration_ms"`
	HTTPStatus           *int       `json:"http_status"`
	Success              *bool      `json:"success"`
	FailureCode          *string    `json:"failure_code"`
	StopReason           *string    `json:"stop_reason"`
}

type ExecutionStatus struct {
	DesiredState               string     `json:"desired_state"`
	WorkspaceID                string     `json:"workspace_id"`
	ProjectID                  string     `json:"project_id"`
	MonitorID                  string     `json:"monitor_id"`
	ConfigurationVersion       string     `json:"configuration_version"`
	ActivationGeneration       string     `json:"activation_generation"`
	SchedulerEpoch             *string    `json:"scheduler_epoch"`
	AuthorizationState         string     `json:"authorization_state"`
	SchedulingState            string     `json:"scheduling_state"`
	EffectiveAuthorization     bool       `json:"effective_authorization"`
	ExpiresAt                  *time.Time `json:"expires_at"`
	NextDue                    *time.Time `json:"next_due"`
	InFlight                   bool       `json:"in_flight"`
	PauseSemantics             string     `json:"pause_semantics"`
	BlockedReason              *string    `json:"blocked_reason"`
	LatestAttempt              *Attempt   `json:"latest_attempt"`
	LatestCompletedObservation *Attempt   `json:"latest_completed_observation"`
	CurrentObservation         *Attempt   `json:"current_observation"`
}

func utcPointer(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	value := t.UTC()
	return &value
}
func (a *ExecutionStatus) normalizeUTC() {
	a.ExpiresAt = utcPointer(a.ExpiresAt)
	a.NextDue = utcPointer(a.NextDue)
	for _, attempt := range []*Attempt{a.LatestAttempt, a.LatestCompletedObservation, a.CurrentObservation} {
		if attempt != nil {
			attempt.ScheduledAt = attempt.ScheduledAt.UTC()
			attempt.ClaimedAt = attempt.ClaimedAt.UTC()
			attempt.StartedAt = utcPointer(attempt.StartedAt)
			attempt.FinishedAt = utcPointer(attempt.FinishedAt)
		}
	}
}
