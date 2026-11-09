//go:build integration && historyfixture

package integration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/monitor"
)

// Administrator-only projection data in a guarded disposable DB. No Probe,
// GET, physical stop, Worker execution or production capacity proof is claimed.
// Schema-required release timestamps are synthetic projection values only; no
// reservation, termination receipt or trusted physical proof is constructed.
func historySeed(t *testing.T, db *pgxpool.Pool, m monitor.Public, v, g, ep int64, n int, at time.Time) string {
	t.Helper()
	state := "completed"
	var code, ms, success, commit, failure any = 200, 15 + n, true, nil, nil
	if n%7 == 0 {
		code, success, failure = 503, false, "unexpected_status"
	}
	if n >= 53 {
		state = "unknown"
		if n == 54 {
			state = "started"
		}
		code, ms, success, failure = nil, nil, nil, nil
	}
	var id string
	if err := db.QueryRow(context.Background(), `INSERT INTO monitor_runs(workspace_id,project_id,monitor_id,run_id,configuration_version,activation_generation,scheduler_epoch,fencing_token,scheduled_at,claimed_at,started_at,finished_at,lease_owner,worker_incarnation,lease_until,attempt_state,result_commit_id,http_status,duration_ms,success,failure_code,physical_release_confirmed_at)
 VALUES($1,$2,$3,uuidv7(),$4,$5,$6,$7,$8,$8,$8::timestamptz+interval '1 millisecond',CASE WHEN $9='started' THEN NULL ELSE $8::timestamptz+interval '2 milliseconds' END,session_user,uuidv7(),$8::timestamptz+interval '45 seconds',$9,CASE WHEN $9='completed' THEN uuidv7() ELSE $10::uuid END,$11,$12,$13,$14,CASE WHEN $9='started' THEN NULL ELSE $8::timestamptz+interval '3 milliseconds' END) RETURNING run_id::text`, m.WorkspaceID, m.ProjectID, m.ID, v, g, ep, n, at, state, commit, code, ms, success, failure).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestMonitorHistoryReadOnlyPG(t *testing.T) {
	db := openDB(t)
	goose(t, root()+"/migrations", "up-to 12", true)
	signature := "scheduler_private.commit_result(uuid,uuid,uuid,uuid,uuid,bigint,bigint,bigint,bigint,uuid,uuid,uuid,integer,integer,boolean,text)"
	original := func() string {
		t.Helper()
		var value string
		if err := db.QueryRow(context.Background(), `SELECT jsonb_build_object('definition',pg_get_functiondef(oid),'owner',proowner,'acl',proacl,'config',proconfig)::text FROM pg_proc WHERE oid=$1::regprocedure`, signature).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	accepted := original()
	goose(t, root()+"/migrations", "up", true)
	goose(t, root()+"/migrations", "up", true)
	if original() != accepted {
		t.Fatal("history migration altered accepted completion function/ACL")
	}
	// Projection rows are not physical evidence. Only undo this read function;
	// retain all existing physical/retention guards and destroy this test cluster.
	t.Cleanup(func() {
		goose(t, root()+"/migrations", "down-to 12", true)
		if original() != accepted {
			t.Error("history rollback altered accepted completion function/ACL")
		}
	})
	ctx := context.Background()
	api := status74API(t, db)
	f, m, v, g, ep := status74Authorized(t, db)
	if _, e := db.Exec(ctx, `SELECT scheduler_private.lock_budgets($1,$2,$3)`, m.WorkspaceID, m.ProjectID, m.ID); e != nil {
		t.Fatal(e)
	}
	anchor := time.Now().UTC().Add(-time.Hour)
	ids := []string{}
	for i := 1; i <= 53; i++ {
		ids = append(ids, historySeed(t, db, m, v, g, ep, i, anchor.Add(time.Duration(i/2)*time.Second)))
	}
	origin, _ := config.ParseOrigin("https://app.example.com")
	h := monitor.New(monitor.Store{Pool: api}, config.AuthConfig{Mode: "production", Origin: origin}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	server, client := checkpointHTTPHandlerServer(t, h)
	path := status75Base(m) + "/history"
	read := func(path, token string, want int) monitor.HistoryPage {
		t.Helper()
		code, headers, raw := checkpointHTTP(t, client, server.URL, "GET", path, "", token, "", nil)
		if code != want {
			t.Fatalf("history HTTP%d wanted%d: %s", code, want, raw)
		}
		if headers.Get("Cache-Control") != "no-store" || headers.Get("X-Content-Type-Options") != "nosniff" || headers.Get("ETag") != "" {
			t.Fatal("read headers")
		}
		var page monitor.HistoryPage
		if want == 200 {
			if json.Unmarshal(raw, &page) != nil {
				t.Fatal("history response")
			}
			for _, bad := range []string{"private-path", "SENTINEL", "https://", "token_hash", "password_hash", "lease_owner", "worker_incarnation", "namespace_incarnation", "termination_receipt", "origin_key", "ip_key", "stop_reason", "fencing_token", "scheduler_epoch", "activation_generation"} {
				if strings.Contains(string(raw), bad) {
					t.Fatal("private history field", bad)
				}
			}
			var body struct{ Data []map[string]json.RawMessage }
			if json.Unmarshal(raw, &body) != nil {
				t.Fatal("shape")
			}
			for _, item := range body.Data {
				if len(item) != 8 {
					t.Fatal("unbounded item fields", item)
				}
			}
		} else if strings.Contains(string(raw), `"data"`) || strings.Contains(string(raw), m.ID) {
			t.Fatal("error disclosed history")
		}
		return page
	}
	first := read(path, f.memberToken, 200)
	if len(first.Data) != 20 || first.Pagination.NextAfter == nil || first.Data[0].ID != ids[52] || first.Data[0].Status != "unknown" || first.Data[0].HTTPStatus != nil || first.Data[0].DurationMS != nil || first.Data[0].FailureCode != nil {
		t.Fatal("default page/new unknown fabricated", first)
	}
	// New insertion after page1 must not enter the old cursor's bounded window.
	newID := historySeed(t, db, m, v, g, ep, 54, anchor.Add(2*time.Minute))
	seen := map[string]bool{}
	page := first
	for {
		for _, r := range page.Data {
			if seen[r.ID] || r.ID == newID {
				t.Fatal("unstable pagination", r.ID)
			}
			seen[r.ID] = true
		}
		if page.Pagination.NextAfter == nil {
			break
		}
		page = read(path+"?after="+*page.Pagination.NextAfter, f.ownerToken, 200)
	}
	if len(seen) != 53 {
		t.Fatal("lost history rows", len(seen))
	}
	if read(path, f.ownerToken, 200).Data[0].ID != newID {
		t.Fatal("refresh did not include new run")
	}
	running := read(path+"?state=running", f.memberToken, 200)
	if len(running.Data) != 1 || running.Data[0].ID != newID || running.Data[0].DurationMS != nil || running.Data[0].HTTPStatus != nil || running.Data[0].FinishedAt != nil {
		t.Fatal("running minimal projection")
	}
	maximum := read(path+"?limit=50", f.ownerToken, 200)
	if len(maximum.Data) != 50 || maximum.Pagination.NextAfter == nil {
		t.Fatal("max page")
	}
	failures := read(path+"?state=failure", f.memberToken, 200)
	if len(failures.Data) != 7 {
		t.Fatal("failure filter", len(failures.Data))
	}
	for _, r := range failures.Data {
		if r.Status != "failure" || r.HTTPStatus == nil || *r.HTTPStatus != 503 || r.FailureCode == nil || *r.FailureCode != "unexpected_status" {
			t.Fatal("minimal failure")
		}
	}
	unknown := read(path+"?state=unknown", f.memberToken, 200)
	if len(unknown.Data) != 1 || unknown.Data[0].ID != ids[52] {
		t.Fatal("unknown filter")
	}
	read(path+"?limit=51", f.ownerToken, 400)
	read(path+"?state=success&after="+*first.Pagination.NextAfter, f.ownerToken, 400)
	// A cursor position is self-contained: no lookup of its boundary row is
	// required (cleanup can remove that row). It carries no read authority.
	bytes, _ := base64.RawURLEncoding.DecodeString(*first.Pagination.NextAfter)
	var cursor map[string]any
	_ = json.Unmarshal(bytes, &cursor)
	cursor["i"] = "ffffffff-ffff-4fff-8fff-ffffffffffff"
	bytes, _ = json.Marshal(cursor)
	// Re-encode in the endpoint's canonical field order, without inventing an
	// administrator deletion or weakening the real retention guards.
	var canonical struct {
		V int       `json:"v"`
		W string    `json:"w"`
		P string    `json:"p"`
		M string    `json:"m"`
		S string    `json:"s"`
		B time.Time `json:"b"`
		I string    `json:"i"`
		U time.Time `json:"u"`
		J string    `json:"j"`
	}
	_ = json.Unmarshal(bytes, &canonical)
	bytes, _ = json.Marshal(canonical)
	read(path+"?after="+base64.RawURLEncoding.EncodeToString(bytes), f.memberToken, 200)
	var other monitor.Public
	t.Run("foreign-scope", func(t *testing.T) { _, other = monitorFixture(t, db) })
	read(status75Base(other)+"/history?after="+*first.Pagination.NextAfter, f.ownerToken, 400)
	read(path, f.otherToken, 404)
	read(path, "", 401)
	for _, mixed := range []string{
		fmt.Sprintf("/api/v1/workspaces/%s/projects/%s/monitors/%s/history", other.WorkspaceID, m.ProjectID, m.ID),
		fmt.Sprintf("/api/v1/workspaces/%s/projects/%s/monitors/%s/history", m.WorkspaceID, other.ProjectID, m.ID),
		fmt.Sprintf("/api/v1/workspaces/%s/projects/%s/monitors/%s/history", m.WorkspaceID, m.ProjectID, other.ID),
	} {
		read(mixed, f.ownerToken, 404)
	}
	read(status75Base(other)+"/history", f.ownerToken, 404)
	if _, e := db.Exec(ctx, `DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, m.WorkspaceID, f.member.ID); e != nil {
		t.Fatal(e)
	}
	read(path, f.memberToken, 404)
	if _, e := db.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_hash=$1`, f.od[:]); e != nil {
		t.Fatal(e)
	}
	read(path, f.ownerToken, 401)
	var pe *pgconn.PgError
	_, e := api.Exec(ctx, `SELECT * FROM monitor_runs`)
	if !errors.As(e, &pe) || pe.Code != "42501" {
		t.Fatal("API gained table access", e)
	}
	historySignature := "scheduler_private.monitor_history(bytea,uuid,uuid,uuid,integer,text,timestamptz,uuid,timestamptz,uuid)"
	for _, role := range []string{"devpilot_scheduler_api", "devpilot_scheduler_worker", "devpilot_scheduler_controller", "devpilot_scheduler_supervisor", "public"} {
		var yes bool
		if e := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, historySignature).Scan(&yes); e != nil || yes != (role == "devpilot_scheduler_api") {
			t.Fatal("history ACL", role, yes, e)
		}
	}
	var indexes int
	if e := db.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE tablename='monitor_runs'`).Scan(&indexes); e != nil || indexes == 0 {
		t.Fatal("existing scope indexes missing")
	}
	t.Log("real PG18 restricted LOGIN:20/50,stable keyset/tied timestamps,new insert,unknown/nulls,Owner/Member,scope/IDOR/revocation,private projection and no table grant")
}
