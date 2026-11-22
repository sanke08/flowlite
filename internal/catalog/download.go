package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sanke08/flowlite/internal/config"
)

// One model downloads at a time, machine-wide, whatever its engine. A second
// download does not wait its turn or give up: it takes over. It leaves a
// takeover note naming the model it wants, the download in progress sees the
// note within a moment, stops, deletes its partial file (unless the note asks
// for that same model, which then simply resumes from it) and lets go of the
// lock, and the newcomer proceeds.

// ErrDownloadBusy means another FlowLite process holds the download lock.
var ErrDownloadBusy = errors.New("another FlowLite window is downloading a model")

// errTakenOver is what a download returns when another one replaced it.
type errTakenOver struct{ by string }

func (e errTakenOver) Error() string {
	return fmt.Sprintf("download stopped: another FlowLite window switched to %s", e.by)
}

// takeoverWait is how long a new download waits for the old one to stop.
const takeoverWait = 20 * time.Second

func lockPaths() (lock, takeover string, err error) {
	root, err := config.ModelsRoot()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(root, ".download.lock"), filepath.Join(root, ".download.takeover"), nil
}

// lockDownloads takes the machine-wide download lock for model key, taking
// it over from a download already in progress if there is one.
func lockDownloads(ctx context.Context, key string) (release func(), err error) {
	lock, takeover, err := lockPaths()
	if err != nil {
		return nil, err
	}
	release, err = tryLock(lock)
	if errors.Is(err, ErrDownloadBusy) {
		if werr := os.WriteFile(takeover, []byte(key), 0o644); werr != nil {
			return nil, werr
		}
		deadline := time.Now().Add(takeoverWait)
		for errors.Is(err, ErrDownloadBusy) && time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				os.Remove(takeover)
				return nil, ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
			release, err = tryLock(lock)
		}
		if errors.Is(err, ErrDownloadBusy) {
			os.Remove(takeover)
			return nil, errors.New("another FlowLite window is downloading a model and did not stop — close it and try again")
		}
	}
	if err != nil {
		return nil, err
	}
	// Holding the lock, any note left is either ours or from a newcomer
	// that gave up; neither is meant for this download.
	os.Remove(takeover)
	return release, nil
}

// takenOver reports which model a takeover note asks for, if one is there.
func takenOver() (string, bool) {
	_, takeover, err := lockPaths()
	if err != nil {
		return "", false
	}
	b, err := os.ReadFile(takeover)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

// dropPartials deletes the half-finished downloads of every model but keep.
// Completed models are left alone: the one in use stays until its
// replacement is fully on disk (see PruneExcept).
func dropPartials(keep string) {
	for _, m := range Catalog {
		if m.Key == keep {
			continue
		}
		if p, err := m.Path(); err == nil {
			os.Remove(p + ".part")
			os.Remove(p + ".part.etag")
		}
	}
}

// Progress receives cumulative bytes and the total (0 if unknown).
type Progress func(done, total int64)

// Download fetches m into place. Interrupted downloads resume from a .part
// file via an HTTP Range request, so a 1.5 GB fetch on flaky Wi-Fi does not
// restart from zero. The final file only appears once it is complete and has
// passed a checksum, so a killed or corrupted download can never masquerade
// as a finished model.
func Download(ctx context.Context, m Model, progress Progress) error {
	// One download at a time, machine-wide, whatever the engine: FlowLite
	// keeps a single model, and two settings windows fetching two models at
	// once would end with both on disk.
	release, err := lockDownloads(ctx, m.Key)
	if err != nil {
		return err
	}
	defer release()
	dropPartials(m.Key)

	final, err := m.Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepathDir(final), 0o755); err != nil {
		return err
	}
	part := final + ".part"
	// Remembers the ETag of the response the .part file was started from,
	// so a resume can tell the server "only continue if this is still that
	// exact file" via If-Range, instead of trusting a Range request against
	// whatever the URL now serves.
	metaPath := part + ".etag"

	var have int64
	var etag string
	if fi, err := os.Stat(part); err == nil {
		have = fi.Size()
		if b, err := os.ReadFile(metaPath); err == nil {
			etag = strings.TrimSpace(string(b))
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.URL(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "flowlite")
	if have > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(have, 10)+"-")
		if etag != "" {
			req.Header.Set("If-Range", etag)
		}
	}

	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connecting to Hugging Face: %w", err)
	}
	defer resp.Body.Close()

	var total int64
	flags := os.O_CREATE | os.O_WRONLY
	switch resp.StatusCode {
	case http.StatusPartialContent:
		flags |= os.O_APPEND
		total = have + resp.ContentLength
	case http.StatusOK:
		// Either a fresh download, or If-Range decided the .part file no
		// longer matches what the server has and sent the whole thing over.
		have = 0
		flags |= os.O_TRUNC
		total = resp.ContentLength
	default:
		return fmt.Errorf("Hugging Face returned HTTP %d for %s", resp.StatusCode, m.File)
	}
	sizeKnown := total > 0
	if !sizeKnown {
		total = m.SizeBytes
	}
	if newTag := resp.Header.Get("ETag"); newTag != "" {
		_ = os.WriteFile(metaPath, []byte(newTag), 0o600)
	}

	// The running checksum has to cover the whole file, not just what this
	// attempt appends: prime it with whatever is already on disk before any
	// new bytes are written.
	hash := sha256.New()
	if have > 0 {
		existing, err := os.Open(part)
		if err != nil {
			return err
		}
		_, err = io.Copy(hash, existing)
		existing.Close()
		if err != nil {
			return err
		}
	}

	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return err
	}

	buf := make([]byte, 1<<20)
	done := have
	last := time.Now()
	lastCheck := time.Now()
	if progress != nil {
		progress(done, total)
	}
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return werr
			}
			hash.Write(buf[:n])
			done += int64(n)
			if progress != nil && time.Since(last) > 100*time.Millisecond {
				progress(done, total)
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if time.Since(lastCheck) > 250*time.Millisecond {
			lastCheck = time.Now()
			if by, ok := takenOver(); ok {
				f.Close()
				if by != m.Key {
					os.Remove(part)
					os.Remove(metaPath)
				}
				label := by
				if other, ok := Get(by); ok {
					label = other.Label
				}
				return errTakenOver{by: label}
			}
		}
		if rerr != nil {
			f.Close()
			if errors.Is(rerr, context.Canceled) {
				return rerr
			}
			return fmt.Errorf("download interrupted (%v) — run the same command again to resume", rerr)
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if progress != nil {
		progress(done, total)
	}

	fail := func(format string, a ...any) error {
		os.Remove(part)
		os.Remove(metaPath)
		return fmt.Errorf(format, a...)
	}

	// The server's own Content-Length for this exact request is exact,
	// unlike the catalog's hardcoded SizeBytes; prefer it when we have it.
	// Only when the server never sent a length do we fall back to the old
	// approximate check.
	switch {
	case sizeKnown && done != total:
		return fail("%s came back truncated (%s of %s) — run the same command again to resume",
			m.File, Human(done), Human(total))
	case !sizeKnown && float64(done) < float64(m.SizeBytes)*0.95:
		return fail("%s came back truncated (%s of ~%s); try again",
			m.File, Human(done), Human(m.SizeBytes))
	}

	if m.SHA256 != "" {
		if sum := hex.EncodeToString(hash.Sum(nil)); sum != m.SHA256 {
			return fail("%s failed checksum verification — the download is corrupt; run the same command again", m.File)
		}
	}

	os.Remove(metaPath)
	return os.Rename(part, final)
}

func filepathDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i]
		}
	}
	return "."
}
