package monitor

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// Import and call allowlists keep new outbound capabilities review-visible.
// Runtime syscall auditing is separately required; AST review is not that proof.
func TestMonitorHasNoTargetNetworkCapability(t *testing.T) {
	allowed := map[string]bool{}
	for _, s := range []string{"context", "encoding/json", "errors", "io", "log/slog", "mime", "net/http", "net/netip", "net/url", "path", "strconv", "strings", "time", "unicode", "unicode/utf8", "github.com/jackc/pgx/v5", "github.com/rjgczp/devpilot/internal/auth", "github.com/rjgczp/devpilot/internal/config", "github.com/rjgczp/devpilot/internal/credentials", "github.com/rjgczp/devpilot/internal/session", "github.com/rjgczp/devpilot/internal/user", "golang.org/x/net/idna"} {
		allowed[s] = true
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, e := parser.ParseFile(token.NewFileSet(), name, nil, parser.ParseComments)
		if e != nil {
			t.Fatal(e)
		}
		for _, im := range f.Imports {
			if !allowed[strings.Trim(im.Path.Value, `"`)] {
				t.Error("unreviewed capability import", name, im.Path.Value)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "Dial", "DialContext", "DialTimeout", "LookupHost", "LookupIP", "LookupIPAddr", "LookupNetIP", "LookupCNAME", "Get", "Head", "Post", "Do", "Client", "Transport", "RoundTripper", "DefaultClient", "DefaultTransport", "ProxyFromEnvironment", "RoundTrip":
				// Header.Get is data lookup, never HTTP Get.
				if sel.Sel.Name == "Get" {
					if receiver, ok := sel.X.(*ast.Ident); !ok || receiver.Name != "http" {
						return true
					}
				}
				t.Error("target network symbol", name, sel.Sel.Name)
			}
			return true
		})
	}
}
