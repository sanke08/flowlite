package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestParseVersion(t *testing.T) {
	cases := map[string]struct {
		v  version
		ok bool
	}{
		"v0.3.1":             {version{0, 3, 1, false}, true},
		"0.3.1":              {version{0, 3, 1, false}, true},
		"v0.4.0-dev+abc1234": {version{0, 4, 0, true}, true},
		"v1.2.3-rc1":         {version{1, 2, 3, true}, true},
		"dev":                {version{0, 0, 0, true}, true},
		"":                   {version{}, false},
		"v1.2":               {version{}, false},
		"vX.Y.Z":             {version{}, false},
	}
	for in, want := range cases {
		got, ok := parseVersion(in)
		if ok != want.ok || got != want.v {
			t.Errorf("parseVersion(%q) = %+v,%v want %+v,%v", in, got, ok, want.v, want.ok)
		}
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v0.4.0", "v0.3.1", true},
		{"v0.3.1", "v0.3.1", false},
		{"v0.3.1", "v0.4.0", false},
		{"v0.4.0", "v0.4.0-dev+abc", true},  // dev build updates to its own release
		{"v0.3.1", "v0.4.0-dev+abc", false}, // but not backwards
		{"v0.4.0", "dev", true},             // unversioned source build
		{"v1.0.0", "v0.9.9", true},
		{"v0.10.0", "v0.9.0", true}, // numeric, not lexical
		{"garbage", "v0.3.1", false},
		{"v0.4.0", "garbage", false},
	}
	for _, c := range cases {
		if got := isNewer(c.latest, c.current); got != c.want {
			t.Errorf("isNewer(%q, %q) = %v want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestAssetFor(t *testing.T) {
	r := &release{TagName: "v0.3.1", Assets: []asset{
		{Name: "flowlite-v0.3.1-macos-arm64", Size: 1},
		{Name: "flowlite-v0.3.1-windows-x64.zip", Size: 2},
	}}
	a, err := r.assetFor("darwin", "arm64")
	if err != nil || a.Size != 1 {
		t.Fatalf("darwin/arm64: %+v %v", a, err)
	}
	a, err = r.assetFor("windows", "amd64")
	if err != nil || a.Size != 2 {
		t.Fatalf("windows/amd64: %+v %v", a, err)
	}
	if _, err := r.assetFor("linux", "amd64"); err == nil {
		t.Fatal("linux should have no asset")
	}
	if _, err := (&release{TagName: "v9"}).assetFor("darwin", "arm64"); err == nil {
		t.Fatal("missing asset should error")
	}
}

func TestCheckCacheRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "update-check.json")
	now := time.Now().Truncate(time.Second)
	if err := writeCheckCacheFile(p, checkCache{CheckedAt: now, Latest: "v0.3.1"}); err != nil {
		t.Fatal(err)
	}
	c, err := readCheckCacheFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !c.CheckedAt.Equal(now) || c.Latest != "v0.3.1" {
		t.Fatalf("got %+v", c)
	}
	if _, err := readCheckCacheFile(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing cache should error")
	}
}

// TestApplyUpdate exercises the download → verify → rename path against a
// local server, using a shell script as the "binary" so the sanity check
// (which runs `<file> --version`) has something real to execute.
func TestApplyUpdate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the fake binary")
	}
	payload := []byte("#!/bin/sh\necho flowlite v9.9.9\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "flowlite")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := asset{Name: "flowlite-v9.9.9-macos-arm64", Size: int64(len(payload)), URL: srv.URL}
	if err := applyUpdate(context.Background(), target, a); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != string(payload) {
		t.Fatalf("target not replaced: %q", got)
	}
	st, _ := os.Stat(target)
	if st.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", st.Mode())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}

	// A size mismatch must leave the old binary untouched and nothing behind.
	bad := asset{Name: a.Name, Size: a.Size + 5, URL: srv.URL}
	if err := applyUpdate(context.Background(), target, bad); err == nil {
		t.Fatal("size mismatch should fail")
	}
	got, _ = os.ReadFile(target)
	if string(got) != string(payload) {
		t.Fatal("target changed after failed update")
	}
	entries, _ = os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp files left behind after failure: %v", entries)
	}
}

func TestValidators(t *testing.T) {
	for _, good := range []string{"150", "400", " 900 ", "400ms"} {
		if err := validHold(good); err != nil {
			t.Errorf("validHold(%q): %v", good, err)
		}
	}
	for _, bad := range []string{"149", "901", "abc", ""} {
		if err := validHold(bad); err == nil {
			t.Errorf("validHold(%q) should fail", bad)
		}
	}
	for _, good := range []string{"", "auto", "en", "hin", "NL"} {
		if err := validLanguage(good); err != nil {
			t.Errorf("validLanguage(%q): %v", good, err)
		}
	}
	for _, bad := range []string{"e", "engl", "e1"} {
		if err := validLanguage(bad); err == nil {
			t.Errorf("validLanguage(%q) should fail", bad)
		}
	}
}
