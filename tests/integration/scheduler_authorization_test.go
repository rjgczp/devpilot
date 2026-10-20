//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/session"
)

func TestExecutionAuthorizationAtomicIntentAndRevocation(t *testing.T) {
	db := openDB(t)
	migrate(t)
	f, m := monitorFixture(t, db)
	ctx := context.Background()
	s := monitor.Store{Pool: db}
	a := activationRead(t, db, f.od, m)
	input := monitor.ExecutionAuthorizationInput{ConfigurationVersion: a.ConfigurationVersion, ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 86400}
	for _, tc := range []struct {
		digest             session.Digest
		wid, pid, mid, tag string
		want               error
	}{
		{session.Digest{}, m.WorkspaceID, m.ProjectID, m.ID, "", session.ErrInvalid},
		{f.od, f.z.ID, m.ProjectID, m.ID, "", monitor.ErrNotFound},
		{f.md, m.WorkspaceID, m.ProjectID, m.ID, "", monitor.ErrForbidden},
		{f.od, m.WorkspaceID, m.ProjectID, m.ID, "", monitor.ErrPreconditionRequired},
		{f.od, m.WorkspaceID, m.ProjectID, m.ID, `"monitor-activation:` + m.ID + `:1:99"`, monitor.ErrPreconditionFailed},
	} {
		_, err := s.ExecutionAuthorize(ctx, tc.digest, tc.wid, tc.pid, tc.mid, input, tc.tag)
		if !errors.Is(err, tc.want) {
			t.Fatalf("priority got %v want %v", err, tc.want)
		}
	}
	auth, err := s.ExecutionAuthorize(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, input, monitor.ActivationETag(a))
	if err != nil {
		t.Fatal(err)
	}
	if auth.ActivationGeneration != "2" || auth.EffectiveAuthorization || auth.BlockedReason == nil || *auth.BlockedReason != "scheduler_unavailable" || auth.ExpiresAt.Sub(auth.IssuedAt) != 24*time.Hour {
		t.Fatal("authorization shape", auth)
	}
	if queryInt(t, db, `SELECT schedule_count FROM scheduler_budget_state WHERE kind='global' AND budget_key='global'`) < 1 {
		t.Fatal("registration not reserved")
	}
	a = activationRead(t, db, f.od, m)
	if a.DesiredState != "enabled" || a.ActivationGeneration != auth.ActivationGeneration {
		t.Fatal("intent not atomic")
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_schedules WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3 AND next_due_at IS NULL AND authorized_activation_generation=2`, m.WorkspaceID, m.ProjectID, m.ID) != 1 {
		t.Fatal("gate did not remain closed")
	}
	if _, err = s.ExecutionAuthorize(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, input, monitor.ActivationETag(a)); !errors.As(err, new(monitor.ActivationRateLimit)) {
		t.Fatal("cooling bypass", err)
	}
	// The legacy command can only revoke a new grant, even enabled -> enabled.
	activationSeed(t, db, m, "next_enable_allowed_at=statement_timestamp()-interval '1s'")
	a, err = activationPut(s, f.od, m, a, "enabled")
	if err != nil {
		t.Fatal(err)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_schedules WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3 AND authorized_activation_generation IS NULL AND next_due_at IS NULL AND scheduler_epoch=2`, m.WorkspaceID, m.ProjectID, m.ID) != 1 {
		t.Fatal("old writer granted authorization")
	}
	activationSeed(t, db, m, "next_enable_allowed_at=statement_timestamp()-interval '1s'")
	var count int
	if err = db.QueryRow(ctx, `SELECT schedule_count FROM scheduler_budget_state WHERE kind='global' AND budget_key='global'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	auth, err = s.ExecutionAuthorize(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, input, monitor.ActivationETag(a))
	if err != nil {
		t.Fatal(err)
	}
	if queryInt(t, db, `SELECT schedule_count FROM scheduler_budget_state WHERE kind='global' AND budget_key='global'`) != count {
		t.Fatal("upsert leaked schedule capacity")
	}
	// Every successful configuration PATCH invalidates the new authorization.
	inputPatch := monitorInput("updated")
	if _, err = s.Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, inputPatch, monitor.ETag(m)); err != nil {
		t.Fatal(err)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_schedules WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3 AND authorized_activation_generation IS NULL AND expires_at IS NULL AND next_due_at IS NULL`, m.WorkspaceID, m.ProjectID, m.ID) != 1 {
		t.Fatal("PATCH left a grant")
	}
}
