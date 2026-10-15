package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

// Validate actual handler bodies against the new OpenAPI schemas, including
// closed projections and the two distinct dependency-error alternatives.
func validateContract(t *testing.T, doc map[string]any, schema map[string]any, value any) {
	t.Helper()
	if ref, ok := schema["$ref"].(string); ok {
		n := strings.TrimPrefix(ref, "#/components/schemas/")
		validateContract(t, doc, doc["components"].(map[string]any)["schemas"].(map[string]any)[n].(map[string]any), value)
		return
	}
	if one, ok := schema["oneOf"].([]any); ok {
		// The dependency alternatives are selected by their fixed error code.
		code := value.(map[string]any)["error"].(map[string]any)["code"]
		for _, item := range one {
			s := item.(map[string]any)
			c := s["properties"].(map[string]any)["error"].(map[string]any)["properties"].(map[string]any)["code"].(map[string]any)["const"]
			if c == code {
				validateContract(t, doc, s, value)
				return
			}
		}
		t.Fatal("no OpenAPI error alternative")
	}
	if types, ok := schema["type"].([]any); ok {
		if value == nil {
			for _, typ := range types {
				if typ == "null" {
					return
				}
			}
			t.Fatal("null not allowed")
		}
	}
	if c, ok := schema["const"]; ok && !reflect.DeepEqual(c, value) {
		t.Fatalf("fixed contract value differs: want %v", c)
	}
	if enum, ok := schema["enum"].([]any); ok {
		found := false
		for _, v := range enum {
			if reflect.DeepEqual(v, value) {
				found = true
			}
		}
		if !found {
			t.Fatal("value outside documented enum")
		}
	}
	switch v := value.(type) {
	case map[string]any:
		properties := schema["properties"].(map[string]any)
		for _, key := range schema["required"].([]any) {
			if _, ok := v[key.(string)]; !ok {
				t.Fatal("missing required response field")
			}
		}
		for key, val := range v {
			s, ok := properties[key]
			if !ok {
				t.Fatal("undocumented response field")
			}
			validateContract(t, doc, s.(map[string]any), val)
		}
	case []any:
		if max, ok := schema["maxItems"].(float64); ok && len(v) > int(max) {
			t.Fatal("unbounded documented page")
		}
		for _, val := range v {
			validateContract(t, doc, schema["items"].(map[string]any), val)
		}
	case string:
		if max, ok := schema["maxLength"].(float64); ok && utf8.RuneCountInString(v) > int(max) {
			t.Fatal("response string bound")
		}
		if schema["format"] == "uuid" && !validUUID(v) {
			t.Fatal("UUID contract")
		}
		if schema["format"] == "date-time" {
			if _, err := time.Parse(time.RFC3339Nano, v); err != nil || !strings.HasSuffix(v, "Z") {
				t.Fatal("UTC timestamp contract")
			}
		}
	default:
		t.Fatalf("unsupported response value %T", v)
	}
}

func TestWorkspaceOpenAPIContract(t *testing.T) {
	b, err := os.ReadFile("../../docs/api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if json.Unmarshal(b, &doc) != nil {
		t.Fatal("invalid OpenAPI")
	}
	paths := doc["paths"].(map[string]any)
	for _, tc := range []struct {
		path, method string
		statuses     []int
	}{
		{collection, "post", []int{201, 400, 401, 403, 405, 413, 415, 422, 500, 503}},
		{collection, "get", []int{200, 400, 401, 405, 500, 503}},
		{collection + "/{workspace_id}", "get", []int{200, 400, 401, 404, 405, 500, 503}},
	} {
		op := paths[tc.path].(map[string]any)[tc.method].(map[string]any)
		responses := op["responses"].(map[string]any)
		if len(responses) != len(tc.statuses) {
			t.Fatal("documented status set differs")
		}
		if len(op["security"].([]any)) != 2 {
			t.Fatal("environment Cookie alternatives")
		}
		if tc.method == "post" && len(op["parameters"].([]any)) != 2 {
			t.Fatal("missing CSRF proof")
		}
		for _, status := range tc.statuses {
			for _, timeout := range []bool{false, true} {
				if timeout && status != 503 {
					continue
				}
				t.Run(fmt.Sprintf("%s-%s-%d-%v", tc.path, tc.method, status, timeout), func(t *testing.T) {
					s := &repositoryStub{}
					var fault error
					if status == 500 {
						fault = user.ErrInternal
					}
					if status == 503 {
						fault = user.ErrUnavailable
						if timeout {
							fault = user.ErrTimeout
						}
					}
					if status == 404 {
						fault = ErrNotFound
					}
					if fault != nil {
						s.create = func(context.Context, session.Digest, string) (Public, error) { return Public{}, fault }
						s.list = func(context.Context, session.Digest, int, string) (Page, error) { return Page{}, fault }
						s.detail = func(context.Context, session.Digest, string) (Public, error) { return Public{}, fault }
					}
					path := strings.ReplaceAll(tc.path, "{workspace_id}", exampleID)
					r := request(strings.ToUpper(tc.method), path, `{"name":"团队"}`)
					switch status {
					case 400:
						r.URL.RawQuery = "unknown=1"
					case 401:
						r.Header.Del("Cookie")
					case 403:
						r.Header.Del("Origin")
					case 405:
						r.Method = "PUT"
					case 413:
						r.Body = io.NopCloser(strings.NewReader(strings.Repeat(" ", 4097)))
					case 415:
						r.Header.Set("Content-Type", "text/plain")
					case 422:
						r.Body = io.NopCloser(strings.NewReader(`{"name":""}`))
					}
					w := response(t, testHandler(s, io.Discard), r, status, "")
					var actual any
					if json.Unmarshal(w.Body.Bytes(), &actual) != nil {
						t.Fatal("actual JSON")
					}
					spec := responses[strconv.Itoa(status)].(map[string]any)
					validateContract(t, doc, spec["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any), actual)
					for key, h := range spec["headers"].(map[string]any) {
						hs := h.(map[string]any)["schema"].(map[string]any)
						if c, ok := hs["const"].(string); ok && w.Header().Get(key) != c {
							t.Fatal("documented response header differs")
						}
					}
				})
			}
		}
	}
	for code, message := range messages {
		qc, _ := json.Marshal(code)
		qm, _ := json.Marshal(message)
		if !strings.Contains(string(b), string(qc)) || !strings.Contains(string(b), string(qm)) {
			t.Fatal("fixed code/message undocumented")
		}
	}
}
