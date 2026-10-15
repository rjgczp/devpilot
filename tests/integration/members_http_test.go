//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/rjgczp/devpilot/internal/workspace"
)

func TestMemberHTTPPermissionsAndRevocation(t *testing.T) {
	db := workspaceDB(t)
	for _, tls := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "tls"}[tls], func(t *testing.T) {
			f := membersFixture(t, db)
			var logs bytes.Buffer
			server, _ := authServer(t, db, tls, &logs)
			o := workspaceCookieClient(t, server, f.ownerToken, tls)
			m := workspaceCookieClient(t, server, f.memberToken, tls)
			x := workspaceCookieClient(t, server, f.otherToken, tls)
			path := "/api/v1/workspaces/" + f.w.ID + "/members"
			for _, client := range []*http.Client{o, m} {
				status, _, body := workspaceRequest(t, client, server.URL, "GET", path, "", false)
				var p workspace.MemberPage
				if status != 200 || json.Unmarshal(body, &p) != nil || len(p.Data) != 2 {
					t.Fatal("member list HTTP")
				}
				var generic map[string]any
				json.Unmarshal(body, &generic)
				for _, row := range generic["data"].([]any) {
					if len(row.(map[string]any)) != 4 {
						t.Fatal("member privacy projection")
					}
				}
			}
			for _, id := range []string{"members", "members-extra"} {
				status, _, body := workspaceRequest(t, o, server.URL, "GET", "/api/v1/workspaces/"+id, "", false)
				if status != 404 {
					t.Fatal("member-named malformed detail")
				}
				requireCode(t, body, "workspace_not_found")
			}
			var missing []byte
			for _, id := range []string{f.w.ID, "bad", "00000000-0000-0000-0000-000000000000"} {
				status, _, body := workspaceRequest(t, x, server.URL, "GET", "/api/v1/workspaces/"+id+"/members", "", false)
				if status != 404 {
					t.Fatal("roster existence")
				}
				if missing == nil {
					missing = body
				} else if !bytes.Equal(missing, body) {
					t.Fatal("roster disclosure")
				}
			}
			for _, target := range []string{f.owner.ID, f.other.ID, "00000000-0000-0000-0000-000000000000"} {
				status, _, body := workspaceRequest(t, m, server.URL, "DELETE", path+"/"+target, "", true, f.condition(f.w.ID, target))
				if status != 403 {
					t.Fatal("member removed other")
				}
				requireCode(t, body, "membership_forbidden")
			}
			status, _, body := workspaceRequest(t, o, server.URL, "DELETE", path+"/"+f.owner.ID, "", true, f.condition(f.w.ID, f.owner.ID))
			if status != 409 {
				t.Fatal("last owner HTTP")
			}
			requireCode(t, body, "last_owner_required")
			status, _, body = workspaceRequest(t, x, server.URL, "DELETE", path+"/bad", "", true, f.condition(f.w.ID, f.member.ID))
			if status != 404 || !bytes.Equal(body, missing) {
				t.Fatal("target format leaked to outsider")
			}
			for _, method := range []string{"POST", "PUT", "PATCH", "HEAD", "OPTIONS"} {
				status, h, b := workspaceRequest(t, o, server.URL, method, path, `{"user_id":"`+f.member.ID+`","role":"owner"}`, true)
				if status != 405 || h.Get("Allow") != "GET" || (method == "HEAD" && len(b) != 0) {
					t.Fatal("unapproved join method")
				}
			}
			status, _, _ = workspaceRequest(t, o, server.URL, "DELETE", path+"/"+f.member.ID, "", false, f.condition(f.w.ID, f.member.ID))
			if status != 403 {
				t.Fatal("delete CSRF")
			}
			status, _, _ = workspaceRequest(t, o, server.URL, "DELETE", path+"/"+f.member.ID, `{"actor_id":"`+f.owner.ID+`"}`, true, f.condition(f.w.ID, f.owner.ID))
			if status != 400 {
				t.Fatal("identity injection body")
			}
			for index, target := range []string{f.member.ID, f.member.ID, f.other.ID} {
				status, h, b := workspaceRequest(t, o, server.URL, "DELETE", path+"/"+target, "", true, f.condition(f.w.ID, target))
				if index > 0 {
					if status != 412 {
						t.Fatal("stale deletion", status)
					}
					continue
				}
				if status != 204 || len(b) != 0 || h.Get("Content-Type") != "" || h.Get("Location") != "" {
					t.Fatal("confirmed no-content/idempotence")
				}
			}
			for _, tail := range []string{"", "/members"} {
				status, _, _ := workspaceRequest(t, m, server.URL, "GET", "/api/v1/workspaces/"+f.w.ID+tail, "", false)
				if status != 404 {
					t.Fatal("old cookie kept permission")
				}
			}
			status, _, _ = workspaceRequest(t, m, server.URL, "DELETE", path+"/"+f.member.ID, "", true, f.condition(f.w.ID, f.member.ID))
			if status != 404 {
				t.Fatal("exit/removal retry authority")
			}
			status, _, body = workspaceRequest(t, m, server.URL, "GET", "/api/v1/workspaces", "", false)
			var page workspace.Page
			if status != 200 || json.Unmarshal(body, &page) != nil || len(page.Data) != 1 || page.Data[0].ID != f.z.ID {
				t.Fatal("removed membership still listed")
			}
			status, _, _ = authRequest(t, m, server.URL, "GET", "me", "", "")
			if status != 200 {
				t.Fatal("removal changed global session")
			}
			status, _, _ = workspaceRequest(t, m, server.URL, "DELETE", "/api/v1/workspaces/"+f.z.ID+"/members/"+f.member.ID, "", true, f.condition(f.z.ID, f.member.ID))
			if status != 204 {
				t.Fatal("member self exit")
			}
			status, _, _ = workspaceRequest(t, m, server.URL, "DELETE", "/api/v1/workspaces/"+f.z.ID+"/members/"+f.member.ID, "", true, f.condition(f.z.ID, f.member.ID))
			if status != 404 {
				t.Fatal("self exit retry")
			}
			o.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			for _, bad := range []string{path + "/", path + "//" + f.owner.ID, path + "/" + f.owner.ID + "/extra", strings.Replace(path, "/workspaces/", "//workspaces/", 1) + "/" + f.owner.ID, path + "%2f" + f.owner.ID} {
				req, _ := http.NewRequest("DELETE", server.URL+bad, nil)
				req.Header.Set("Origin", server.URL)
				req.Header.Set("X-DevPilot-CSRF", "1")
				resp, err := o.Do(req)
				if err != nil {
					t.Fatal("bad route request")
				}
				resp.Body.Close()
				if resp.StatusCode != 404 || resp.Header.Get("Location") != "" {
					t.Fatal("ambiguous mutation path redirected", bad, resp.StatusCode)
				}
			}
			if strings.Contains(logs.String(), f.owner.ID) || strings.Contains(logs.String(), f.memberToken) {
				t.Fatal("member log privacy")
			}
		})
	}
	ctx := context.Background()
	for _, kind := range []string{"revoke", "disable", "expire"} {
		t.Run(kind, func(t *testing.T) {
			f := membersFixture(t, db)
			var logs bytes.Buffer
			server, _ := authServer(t, db, false, &logs)
			c := workspaceCookieClient(t, server, f.ownerToken, false)
			sql := `UPDATE sessions SET revoked_at=statement_timestamp() WHERE token_hash=$1`
			arg := any(f.od[:])
			if kind == "disable" {
				sql = `UPDATE users SET status='disabled' WHERE id=$1`
				arg = f.owner.ID
			}
			if kind == "expire" {
				sql = `UPDATE sessions SET created_at=statement_timestamp()-interval '2h',expires_at=statement_timestamp()-interval '1h' WHERE token_hash=$1`
			}
			if _, err := db.Exec(ctx, sql, arg); err != nil {
				t.Fatal("identity fixture")
			}
			for _, method := range []string{"GET", "DELETE"} {
				path := "/api/v1/workspaces/" + f.w.ID + "/members"
				if method == "DELETE" {
					path += "/" + f.member.ID
				}
				status, _, body := workspaceRequest(t, c, server.URL, method, path, "", true)
				if status != 401 {
					t.Fatal("live identity")
				}
				requireCode(t, body, "unauthenticated")
			}
		})
	}
}
