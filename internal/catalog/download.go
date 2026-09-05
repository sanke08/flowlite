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
	"strconv"
	"strings"
	"time"
)

// Progress receives cumulative bytes and the total (0 if unknown).
type Progress func(done, total int64)

// Download fetches m into place. Interrupted downloads resume from a .part
// file via an HTTP Range request, so a 1.5 GB fetch on flaky Wi-Fi does not
// restart from zero. The final file only appears once it is complete and has
// passed a checksum, so a killed or corrupted download can never masquerade
// as a finished model.
func Download(ctx context.Context, m Model, progress Progress) error {
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
