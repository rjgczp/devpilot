package auth

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestOpenAPIAuthenticationContract(t *testing.T) {
	b, err := os.ReadFile("../../docs/api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			Description string
			Responses   map[string]json.RawMessage
			Parameters  []struct {
				Name     string
				Required bool
			}
			RequestBody struct {
				Content map[string]struct {
					Schema struct {
						Properties map[string]struct{ MinLength int }
					}
				}
			}
		}
	}
	if json.Unmarshal(b, &doc) != nil {
		t.Fatal("invalid OpenAPI")
	}
	for _, tc := range []struct {
		path, method string
		statuses     []string
	}{
		{"login", "post", []string{"200", "400", "401", "403", "405", "413", "415", "422", "500", "503"}},
		{"logout", "post", []string{"204", "403", "405", "500", "503"}},
		{"me", "get", []string{"200", "401", "405", "500", "503"}},
	} {
		op := doc.Paths["/api/v1/auth/"+tc.path][tc.method]
		if len(op.Responses) != len(tc.statuses) {
			t.Fatal("status set drift")
		}
		for _, status := range tc.statuses {
			if op.Responses[status] == nil {
				t.Fatal("missing response status")
			}
		}
		if tc.method == "post" {
			if len(op.Parameters) != 2 || op.Parameters[0].Name != "Origin" || !op.Parameters[0].Required || op.Parameters[1].Name != "X-DevPilot-CSRF" || !op.Parameters[1].Required {
				t.Fatal("CSRF proof undocumented")
			}
		}
		if tc.path != "logout" {
			for _, phrase := range []string{"24h", "Refresh Token", "no "} {
				if !strings.Contains(op.Description, phrase) {
					t.Fatal("lifetime documentation drift")
				}
			}
		}
		if tc.path == "login" && op.RequestBody.Content["application/json"].Schema.Properties["password"].MinLength != 1 {
			t.Fatal("login used registration password minimum")
		}
	}
	for code, message := range messages {
		quotedCode, _ := json.Marshal(code)
		quotedMessage, _ := json.Marshal(message)
		if !strings.Contains(string(b), string(quotedCode)) || !strings.Contains(string(b), string(quotedMessage)) {
			t.Fatal("fixed error contract drift")
		}
	}
	if doc.Paths["/api/v1/auth/register"]["post"].RequestBody.Content["application/json"].Schema.Properties["password"].MinLength != 15 {
		t.Fatal("registration contract changed")
	}
	if len(doc.Paths) != 4 {
		t.Fatal("unexpected auth or refresh route")
	}
}
