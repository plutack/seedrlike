package media

import (
	"net/url"
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	for _, name := range []string{"a.mp4", "a.M4V", "a.webm", "a.ogv", "a.mp3", "a.m4a", "a.aac", "a.ogg", "a.oga", "a.wav", "a.flac", "a.opus"} {
		typ, ok := Detect(name)
		if !ok || (typ.Kind != "video" && typ.Kind != "audio") || typ.MIME == "" {
			t.Errorf("not detected: %q", name)
		}
	}
	for _, name := range []string{"a.mkv", "a.avi", "a.mp4.exe", "a.html", "a", ""} {
		if _, ok := Detect(name); ok {
			t.Errorf("unsupported accepted: %q", name)
		}
	}
}
func TestDownloadURL(t *testing.T) {
	raw, err := DownloadURL("Store1", "id-123_ABC", "movie #1?你好.mp4")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host != "store1.gofile.io" || u.Scheme != "https" || u.RawQuery != "" || u.Fragment != "" || !strings.HasSuffix(u.Path, "/movie #1?你好.mp4") {
		t.Fatalf("unsafe or corrupt URL %q: %v", raw, err)
	}
	for _, tc := range []struct{ server, id, name string }{
		{"evil.com", "id", "a.mp4"}, {"server:443", "id", "a.mp4"}, {"server@evil", "id", "a.mp4"}, {"-bad", "id", "a.mp4"}, {"a/b", "id", "a.mp4"},
		{"s", "../id", "a.mp4"}, {"s", "id?x", "a.mp4"}, {"s", "id#x", "a.mp4"}, {"s", "", "a.mp4"},
		{"s", "id", "../a.mp4"}, {"s", "id", `a\b.mp4`}, {"s", "id", "a\n.mp4"}, {"s", "id", ".."},
	} {
		if raw, err := DownloadURL(tc.server, tc.id, tc.name); err == nil {
			t.Errorf("unsafe URL accepted: %+v -> %s", tc, raw)
		}
	}
}
func TestSafeDownloadURL(t *testing.T) {
	for _, raw := range []string{"https://store1.gofile.io/download/a", "https://store-eu-par-1.gofile.io/a", "https://store-eu-par-1.gofileusercontent.com/a", "https://gofile.io/a"} {
		u, _ := url.Parse(raw)
		if !SafeDownloadURL(u) {
			t.Errorf("valid redirect rejected: %s", raw)
		}
	}
	for _, raw := range []string{"http://store1.gofile.io/a", "https://gofile.io.evil.com/a", "https://evilgofile.io/a", "https://gofile.io@127.0.0.1/a", "https://u:p@gofile.io/a", "https://gofile.io:444/a", "https://127.0.0.1/a", "https://.gofile.io/a", "https://bad_.gofile.io/a"} {
		u, _ := url.Parse(raw)
		if SafeDownloadURL(u) {
			t.Errorf("unsafe redirect accepted: %s", raw)
		}
	}
	if SafeDownloadURL(nil) {
		t.Fatal("nil URL accepted")
	}
}
