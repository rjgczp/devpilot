package registration

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestOpenAPIContract(t *testing.T) {
	// JSON syntax is valid YAML; use the standard parser for this OpenAPI file.
	b, err := os.ReadFile("../../docs/api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		OpenAPI string `json:"openapi"`
		Paths   map[string]struct {
			Post struct {
				Responses map[string]json.RawMessage `json:"responses"`
			} `json:"post"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if json.Unmarshal(b, &doc) != nil || doc.OpenAPI != "3.1.0" {
		t.Fatal("invalid API document")
	}
	responses := doc.Paths["/api/v1/auth/register"].Post.Responses
	for _, status := range []string{"201", "400", "405", "409", "413", "415", "422", "500", "503"} {
		if responses[status] == nil {
			t.Fatalf("undocumented status %s", status)
		}
	}
	properties := doc.Components.Schemas["User"].Properties
	if len(properties) != 4 || properties["id"] == nil || properties["email"] == nil || properties["status"] == nil || properties["created_at"] == nil {
		t.Fatal("public response schema drift")
	}
	for code := range messages {
		if !bytes.Contains(b, []byte(`"`+code+`"`)) {
			t.Fatalf("undocumented code %s", code)
		}
	}
	h := newTestHandler()
	w := perform(h, payload("a@example.com", validPassword))
	assertResponse(t, w, 201, "")
	var actual struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &actual) != nil || len(actual.Data) != len(properties) {
		t.Fatal("response projection differs from contract")
	}
	for name := range actual.Data {
		if properties[name] == nil {
			t.Fatalf("undocumented response field %s", name)
		}
	}
}
