package monitor

import (
	"net/url"
	"strings"
	"testing"
)

func TestCanonicalURL(t *testing.T) {
	for raw, want := range map[string]string{
		"HTTP://EXAMPLE.COM.:80":                                  "http://example.com/",
		"https://example.com:443":                                 "https://example.com/",
		"https://xn--bcher-kva.de/":                               "https://xn--bcher-kva.de/",
		"https://example.com/a%2fb":                               "https://example.com/a%2Fb",
		"https://example.com/%41/%25/%252f/%3f/%23/%20/%e4%b8%ad": "https://example.com/%41/%25/%252f/%3F/%23/%20/%E4%B8%AD",
		"https://example.com/a/../b//./C":                         "https://example.com/a/../b//./C",
		"https://127.0.0.1.example.com/":                          "https://127.0.0.1.example.com/",
		"https://example.com/:@!$&'()*+,;=~_-":                    "https://example.com/:@!$&'()*+,;=~_-",
	} {
		got, ok := canonicalURL(raw)
		if !ok || got != want {
			t.Errorf("canonical %q: %q %v", raw, got, ok)
			continue
		}
		twice, ok := canonicalURL(got)
		if !ok || twice != got {
			t.Error("not idempotent")
		}
		u, e := url.Parse(got)
		if e != nil || u.String() != got || u.EscapedPath() != strings.TrimPrefix(got, u.Scheme+"://"+u.Host) {
			t.Error("serialization changed escaped path", got, u)
		}
	}
}
func TestRejectedURLs(t *testing.T) {
	bad := []string{"", " https://example.com/", "https://example.com/ ", "//example.com/", "https:example.com/", "file:///tmp/x", "ftp://example.com/", "gopher://example.com/",
		"https://user:private@example.com/", "https://@example.com/", "https://example.com/?", "https://example.com/?token=secret", "https://example.com/#", "https://example.com/#x",
		"https://example.com:80/", "http://example.com:443/", "http://example.com:8080/", "https://example.com:0443/", "https://example.com:/", "https://example.com:+443/",
		"https://bücher.de/", "https://%65xample.com/", "https://example..com/", "https://example.com../", "https://-example.com/", "https://example-.com/", "https://ex_ample.com/", "https://ab--cd.com/",
		"https://xn--.com/", "https://xn--a.com/", "https://xn--abc-.com/", "https://localhost/", "https://foo.localhost/", "https://foo.local/", "https://foo.internal/", "https://home.arpa/", "https://foo.invalid/", "https://foo.test/", "https://foo.example/", "https://foo.onion/", "https://metadata.google.internal/",
		"http://127.0.0.1/", "http://10.0.0.1/", "http://169.254.169.254/", "http://100.100.100.200/", "https://8.8.8.8/", "http://0.0.0.0/", "http://255.255.255.255/", "http://224.0.0.1/", "http://2130706433/", "http://127.1/", "http://0177.0.0.1/", "http://0x7f000001/", "http://0x7f.0.0.1/",
		"http://[::1]/", "http://[::ffff:127.0.0.1]/", "http://[fe80::1%25eth0]/", "http://[64:ff9b::a9fe:a9fe]/", "http://[2002:7f00:1::]/", "http://::1/",
		"https://example.com/\\x", "https://example.com/%5c", "https://example.com/%00", "https://example.com/%1F", "https://example.com/%7f", "https://example.com/%0d%0a", "https://example.com/%", "https://example.com/%GG", "https://example.com/[x]", "https://example.com/中", "https://example.com/a\n",
		"https://" + strings.Repeat("a", 64) + ".com/", "https://a.c/", "https://a.123/", "https://a.0x1/", "https://example.com/" + strings.Repeat("x", 2048),
	}
	for _, raw := range bad {
		if got, ok := canonicalURL(raw); ok {
			t.Errorf("accepted %q as %q", raw, got)
		}
	}
	raw := "https://example.com/" + strings.Repeat("a", 2048-len("https://example.com/"))
	if _, ok := canonicalURL(raw); !ok {
		t.Error("2048 boundary")
	}
}
func TestURLPathRawPathSerialization(t *testing.T) {
	for _, path := range []string{"/a%2Fb", "/%41", "/%25", "/%252F", "/%3F", "/%23", "/a/../b//./", "/%E4%B8%AD"} {
		raw := "https://example.com" + path
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if u.EscapedPath() != path || u.String() != raw {
			t.Fatalf("lost escaped semantics: %#v", u)
		}
		canonical, ok := canonicalURL(raw)
		if !ok || canonical != raw {
			t.Fatal("canonical path changed", raw, canonical)
		}
		if path == "/a%2Fb" {
			if u.Path != "/a/b" || u.RawPath != "/a%2Fb" {
				t.Fatal("unexpected Go decode")
			}
			u.RawPath = ""
			if u.String() == raw {
				t.Fatal("control did not demonstrate decoded serialization change")
			}
		}
	}
}
func FuzzCanonicalURL(f *testing.F) {
	for _, s := range []string{"https://example.com/", "HTTP://EXAMPLE.COM.:80/a%2fb", "https://xn--bcher-kva.de/", "http://[::1]/", "https://example.com/%252F", "https://example.com/?"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out, ok := canonicalURL(s)
		if !ok {
			return
		}
		again, ok := canonicalURL(out)
		if !ok || again != out {
			t.Fatal("not idempotent")
		}
		u, err := url.Parse(out)
		if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" || u.String() != out || !validHost(u.Hostname()) {
			t.Fatal("invalid canonical origin")
		}
	})
}
