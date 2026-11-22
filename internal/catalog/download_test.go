package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"
)

// A second download takes over from one in progress: the first stops and
// deletes its partial file, the second completes, and only it is on disk.
func TestDownloadTakesOver(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("AppData", t.TempDir())

	slowBody := make([]byte, 8<<20)
	fastBody := []byte("the replacement model")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slow.bin":
			w.Header().Set("Content-Length", strconv.Itoa(len(slowBody)))
			for i := 0; i < len(slowBody); i += 64 << 10 {
				if _, err := w.Write(slowBody[i : i+64<<10]); err != nil {
					return
				}
				w.(http.Flusher).Flush()
				time.Sleep(20 * time.Millisecond)
			}
		case "/fast.bin":
			w.Header().Set("Content-Length", strconv.Itoa(len(fastBody)))
			w.Write(fastBody)
		}
	}))
	defer srv.Close()

	sum := sha256.Sum256(fastBody)
	slow := Model{Key: "test-slow", Label: "Slow", File: "slow.bin", SizeBytes: int64(len(slowBody)), Repo: srv.URL + "/"}
	fast := Model{Key: "test-fast", Label: "Fast", File: "fast.bin", SizeBytes: int64(len(fastBody)),
		SHA256: hex.EncodeToString(sum[:]), Repo: srv.URL + "/", Engine: Transcribe}
	saved := Catalog
	Catalog = append(append([]Model(nil), saved...), slow, fast)
	defer func() { Catalog = saved }()

	slowErr := make(chan error, 1)
	go func() { slowErr <- Download(context.Background(), slow, nil) }()

	slowPath, _ := slow.Path()
	for deadline := time.Now().Add(5 * time.Second); ; {
		if fi, err := os.Stat(slowPath + ".part"); err == nil && fi.Size() > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("slow download never started")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := Download(context.Background(), fast, nil); err != nil {
		t.Fatalf("taking over: %v", err)
	}
	var taken errTakenOver
	if err := <-slowErr; !errors.As(err, &taken) {
		t.Fatalf("first download: got %v, want it taken over", err)
	}
	if _, err := os.Stat(slowPath + ".part"); !os.IsNotExist(err) {
		t.Fatal("the stopped download's partial file is still there")
	}
	if !fast.Downloaded() {
		t.Fatal("the new download is not on disk")
	}
	if _, takeover, _ := lockPaths(); takeover != "" {
		if _, err := os.Stat(takeover); !os.IsNotExist(err) {
			t.Fatal("the takeover note was left behind")
		}
	}
}

// PruneExcept removes other models whether complete or partial, across
// engines.
func TestPruneExceptRemovesPartials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	keep := Model{Key: "test-keep", File: "keep.bin", SizeBytes: 4}
	partial := Model{Key: "test-partial", File: "partial.gguf", SizeBytes: 100, Engine: Transcribe}
	saved := Catalog
	Catalog = []Model{keep, partial}
	defer func() { Catalog = saved }()

	for _, f := range []struct {
		m    Model
		ext  string
		size int
	}{{keep, "", 4}, {partial, ".part", 10}} {
		p, _ := f.m.Path()
		os.MkdirAll(filepathDir(p), 0o755)
		os.WriteFile(p+f.ext, make([]byte, f.size), 0o644)
	}
	removed, err := PruneExcept(keep.Key)
	if err != nil || len(removed) != 1 || removed[0].Model.Key != partial.Key {
		t.Fatalf("removed %v, err %v", removed, err)
	}
	if d, _ := partial.Dir(); func() bool { _, err := os.Stat(d); return err == nil }() {
		t.Fatal("partial model folder still there")
	}
	if !keep.Downloaded() {
		t.Fatal("kept model was removed")
	}
}
