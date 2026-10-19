package monitor

import (
	"strings"
	"testing"
)

func TestStrictInput(t *testing.T) {
	for _, s := range []string{"", "null", "[]", "{}", `{"name":"x"}`, `{"name":"x","url":null}`, `{"name":"x","url":1}`, `{"name":"x","url":"https://example.com/","na\u006de":"y"}`, `{"name":"x","url":"https://example.com/","interval_seconds":1e2}`, `{"name":"x","url":"https://example.com/","timeout_seconds":1.0}`, `{"name":"x","url":"https://example.com/","timeout_seconds":-0}`, `{"name":"x","url":"https://example.com/","timeout_seconds":"10"}`, `{"name":"x","url":"https://example.com/","timeout_seconds":9223372036854775808}`, `{"name":"\ud800","url":"https://example.com/"}`, `{"name":"x","url":"https://example.com/","version":"1"}`, `{"name":"x","url":"https://example.com/"} {}`} {
		if _, ok := parseInput([]byte(s), true); ok {
			t.Error("structural input accepted", s)
		}
	}
	v, ok := parseInput([]byte(`{"name":" \t中文🔐 \r\n","url":"HTTP://EXAMPLE.COM.:80"}`), true)
	v = defaults(v)
	if !ok || len(validate(v)) != 0 || *v.Name != "中文🔐" || *v.URL != "http://example.com/" || *v.Method != "GET" || *v.IntervalSeconds != 60 || *v.TimeoutSeconds != 10 || *v.ExpectedStatus != 200 {
		t.Fatal("defaults/normalization", v)
	}
	v, ok = parseInput([]byte(`{"name":"x"}`), false)
	if !ok || v.URL != nil || v.IntervalSeconds != nil || len(validate(v)) != 0 {
		t.Fatal("patch missing fields changed")
	}
	for _, s := range []string{`{}`, `{"url":null}`, `{"method":null}`, `{"url":"x","url":"y"}`} {
		if _, ok := parseInput([]byte(s), false); ok {
			t.Error(s)
		}
	}
	for field, body := range map[string]string{"name": `{"name":"\u0085x"}`, "url": `{"url":"https://example.com/?token=private"}`, "method": `{"method":"get"}`, "interval_seconds": `{"interval_seconds":61}`, "timeout_seconds": `{"timeout_seconds":31}`, "expected_status": `{"expected_status":199}`} {
		v, ok := parseInput([]byte(body), false)
		if !ok || validate(v)[field] == "" {
			t.Fatal("validation", field)
		}
	}
	for _, name := range []string{"", strings.Repeat("a", 101), "\u2000x", "x\n", "x\x7f"} {
		if validate(Input{Name: &name})["name"] == "" {
			t.Error("invalid name")
		}
	}
	for _, status := range []int64{200, 302, 599} {
		if len(validate(Input{ExpectedStatus: &status})) != 0 {
			t.Error(status)
		}
	}
}
func TestSelectorsAndTag(t *testing.T) {
	for _, q := range []string{"limit=0", "limit=01", "limit=101", "limit=1&limit=2", "after=", "x=y", "%zz=x"} {
		if _, _, ok := selectors(q, true); ok {
			t.Error(q)
		}
	}
	for _, tag := range []string{"", `*`, `W/"monitor:019a5abc-1234-7123-8123-123456789abc:1"`, `"monitor:019a5abc-1234-7123-8123-123456789abc:01"`, `"monitor:019a5abc-1234-7123-8123-123456789abc:9223372036854775808"`} {
		if _, _, ok := condition(tag); ok {
			t.Error(tag)
		}
	}
}
