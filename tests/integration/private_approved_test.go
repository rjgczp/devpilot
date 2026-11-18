//go:build integration && schedulerfixture && privateworker && linux && budgetfixture && networkfixture && releasefixture && jointfixture && periodicfixture && privateloopfixture && capacityfixture && privateservicefixture && approvedfixture

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateApprovedPG(t *testing.T) { privateServicePG(t, approvedOptions()) }
func TestPrivateApprovedRefusalPG(t *testing.T) {
	o := approvedOptions()
	o.Refusal = true
	privateServicePG(t, o)
}
func approvedOptions() *privateServiceOptions {
	return &privateServiceOptions{URL: "https://monitor.owned-test.example.com/fixture/owned-path", Mode: "private-approved", Apply: func(t *testing.T, db *pgxpool.Pool) {
		var approval struct {
			Hash       string `json:"candidate_sha256"`
			Production bool   `json:"production"`
			Scope      string `json:"scope"`
		}
		b, e := os.ReadFile(filepath.Join(root(), "deploy/targetadmission/isolated-test-approval.json"))
		if e != nil || json.Unmarshal(b, &approval) != nil || approval.Production || approval.Scope != "disposable-pg18-linux-no-upstream" {
			t.Fatal("M16 requires independent isolated approval", e)
		}
		b, e = os.ReadFile(filepath.Join(root(), "deploy/targetadmission/approved-mode.sql.candidate"))
		h := sha256.Sum256(b)
		if e != nil || hex.EncodeToString(h[:]) != approval.Hash {
			t.Fatal("M16 unapproved source", e)
		}
		if _, e = db.Exec(context.Background(), string(b)); e != nil {
			t.Fatal("approved isolated M16", e)
		}
		// Mode cannot be borrowed from fixture or private-isolated; existing Worker
		// readiness stays closed with wrong deployment/own mode. No extra identity.
		var count int
		if e = db.QueryRow(context.Background(), `SELECT count(*) FROM information_schema.role_routine_grants WHERE routine_name='approved_worker_ready' AND grantee='devpilot_scheduler_worker' AND privilege_type='EXECUTE'`).Scan(&count); e != nil || count != 1 {
			t.Fatal("exact new boolean grant", count, e)
		}
	}}
}
