package project

import (
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

func contractOK(doc, schema map[string]any, v any) bool {
	if ref, ok := schema["$ref"].(string); ok {
		return contractOK(doc, doc["components"].(map[string]any)["schemas"].(map[string]any)[strings.TrimPrefix(ref, "#/components/schemas/")].(map[string]any), v)
	}
	if one, ok := schema["oneOf"].([]any); ok {
		n := 0
		for _, s := range one {
			if contractOK(doc, s.(map[string]any), v) {
				n++
			}
		}
		return n == 1
	}
	if c, ok := schema["const"]; ok && c != v {
		return false
	}
	if v == nil {
		types, _ := schema["type"].([]any)
		for _, s := range types {
			if s == "null" {
				return true
			}
		}
		return false
	}
	switch x := v.(type) {
	case map[string]any:
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			return false
		}
		required, _ := schema["required"].([]any)
		for _, key := range required {
			if _, ok := x[key.(string)]; !ok {
				return false
			}
		}
		for key, val := range x {
			s, ok := props[key].(map[string]any)
			if !ok || !contractOK(doc, s, val) {
				return false
			}
		}
	case []any:
		for _, val := range x {
			if !contractOK(doc, schema["items"].(map[string]any), val) {
				return false
			}
		}
	case string:
		if pattern, ok := schema["pattern"].(string); ok && !regexp.MustCompile(pattern).MatchString(x) {
			return false
		}
	default:
		return false
	}
	return true
}
func TestProjectOpenAPIResponsesAndProjection(t *testing.T) {
	b, err := os.ReadFile("../../docs/api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if json.Unmarshal(b, &doc) != nil {
		t.Fatal("OpenAPI JSON")
	}
	for code, msg := range messages {
		if !strings.Contains(string(b), `"`+code+`"`) || !strings.Contains(string(b), msg) {
			t.Fatal("undocumented fixed message", code)
		}
	}
	paths := doc["paths"].(map[string]any)
	for _, route := range []string{"/api/v1/workspaces/{workspace_id}/projects", "/api/v1/workspaces/{workspace_id}/projects/{project_id}"} {
		for method, op := range paths[route].(map[string]any) {
			for _, tc := range []struct {
				status int
				err    error
			}{{200, nil}, {401, session.ErrInvalid}, {404, ErrNotFound}, {500, user.ErrSchema}, {503, user.ErrUnavailable}, {503, user.ErrTimeout}, {403, ErrForbidden}, {428, ErrPreconditionRequired}, {412, ErrPreconditionFailed}} {
				if method == "get" && (tc.status == 403 || tc.status == 428 || tc.status == 412) || method == "post" && (tc.status == 428 || tc.status == 412) {
					continue
				}
				status := tc.status
				if status == 200 && method == "post" {
					status = 201
				}
				s := &repositoryStub{err: tc.err}
				r := request(strings.ToUpper(method), strings.ReplaceAll(strings.ReplaceAll(route, "{workspace_id}", exampleID), "{project_id}", exampleID), `{"name":"x"}`)
				w := response(t, testHandler(s, io.Discard), r, status, "")
				var actual any
				if json.Unmarshal(w.Body.Bytes(), &actual) != nil {
					t.Fatal("JSON")
				}
				rs := op.(map[string]any)["responses"].(map[string]any)[strconv.Itoa(status)].(map[string]any)
				schema := rs["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
				if !contractOK(doc, schema, actual) {
					t.Fatal("OpenAPI response differs", route, method, status)
				}
				if status < 300 && !(method == "get" && strings.HasSuffix(route, "/projects")) {
					var out struct{ Data Public }
					json.Unmarshal(w.Body.Bytes(), &out)
					if w.Header().Get("ETag") != ETag(out.Data) {
						t.Fatal("ETag")
					}
				}
			}
		}
	}
	// Verify every reference, including legacy definitions, remains resolvable.
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, val := range x {
				if k == "$ref" {
					parts := strings.Split(strings.TrimPrefix(val.(string), "#/"), "/")
					var node any = doc
					for _, part := range parts {
						m, ok := node.(map[string]any)
						if !ok || m[part] == nil {
							t.Fatal("broken OpenAPI reference", val)
						}
						node = m[part]
					}
				} else {
					walk(val)
				}
			}
		case []any:
			for _, val := range x {
				walk(val)
			}
		}
	}
	walk(doc)
}
