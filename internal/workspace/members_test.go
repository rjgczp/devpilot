package workspace

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/session"
)

func (s *repositoryStub) ListMembers(ctx context.Context, d session.Digest, id string, n int, after string) (MemberPage, error) {
	s.calls.Add(1)
	if s.members != nil {
		return s.members(ctx, d, id, n, after)
	}
	return MemberPage{Data: []Member{{UserID: exampleID, Role: "member", JoinedAt: time.Unix(0, 0).UTC()}}}, nil
}
func (s *repositoryStub) DeleteMember(ctx context.Context, d session.Digest, wid, target string) error {
	s.calls.Add(1)
	if s.remove != nil {
		return s.remove(ctx, d, wid, target)
	}
	return nil
}
func TestMemberRosterProtocol(t *testing.T) {
	path := collection + "/" + exampleID + "/members"
	s := &repositoryStub{}
	w := response(t, testHandler(s, io.Discard), request("GET", path, "ignored"), 200, "")
	if w.Body.String() != "{\"data\":[{\"user_id\":\""+exampleID+"\",\"role\":\"member\",\"joined_at\":\"1970-01-01T00:00:00Z\"}],\"pagination\":{\"next_after\":null}}\n" || s.calls.Load() != 1 {
		t.Fatal("roster projection")
	}
	for _, q := range []string{"limit=0", "limit=101", "limit=01", "limit=+1", "limit=1&limit=2", "after=", "after=bad", "user_id=other", "%xx=x", "x=%zz"} {
		s = &repositoryStub{}
		response(t, testHandler(s, io.Discard), request("GET", path+"?"+q, ""), 400, "invalid_request")
		if s.calls.Load() != 0 {
			t.Fatal("invalid selectors reached database")
		}
	}
	for _, m := range []string{"POST", "HEAD", "OPTIONS", "PUT", "PATCH", "DELETE"} {
		s = &repositoryStub{}
		w = response(t, testHandler(s, io.Discard), request(m, path, `{"user_id":"other","role":"owner"}`), 405, "method_not_allowed")
		if w.Header().Get("Allow") != "GET" || s.calls.Load() != 0 {
			t.Fatal("unsupported member method")
		}
	}
}

func TestMemberRosterOpenAPIContract(t *testing.T) {
	b, err := os.ReadFile("../../docs/api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if json.Unmarshal(b, &doc) != nil {
		t.Fatal("OpenAPI")
	}
	op := doc["paths"].(map[string]any)[collection+"/{workspace_id}/members"].(map[string]any)
	if len(op) != 1 || op["post"] != nil {
		t.Fatal("unapproved member route")
	}
	responses := op["get"].(map[string]any)["responses"].(map[string]any)
	for _, tc := range []struct {
		status string
		code   string
		fault  error
	}{{"200", "", nil}, {"401", "unauthenticated", session.ErrInvalid}, {"404", "workspace_not_found", ErrNotFound}} {
		s := &repositoryStub{}
		if tc.fault != nil {
			s.members = func(context.Context, session.Digest, string, int, string) (MemberPage, error) {
				return MemberPage{}, tc.fault
			}
		}
		w := response(t, testHandler(s, io.Discard), request("GET", collection+"/"+exampleID+"/members", ""), map[string]int{"200": 200, "401": 401, "404": 404}[tc.status], tc.code)
		var actual any
		json.Unmarshal(w.Body.Bytes(), &actual)
		schema := responses[tc.status].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		validateContract(t, doc, schema, actual)
	}
}

func TestMemberNamedInvalidWorkspaceKeepsDetailContract(t *testing.T) {
	for _, id := range []string{"members", "members-extra"} {
		s := &repositoryStub{detail: func(context.Context, session.Digest, string) (Public, error) { return Public{}, ErrNotFound }}
		h := testHandler(s, io.Discard)
		response(t, h, request("GET", collection+"/"+id, ""), 404, "workspace_not_found")
		if s.calls.Load() != 1 {
			t.Fatal("detail bypassed live identity")
		}
		r := request("GET", collection+"/"+id, "")
		r.Header.Del("Cookie")
		response(t, h, r, 401, "unauthenticated")
	}
}
