package catalog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

// Progress receives cumulative bytes and the total (0 if unknown).
type Progress func(done, total int64)

// Download fetches m into place. Interrupted downloads resume from a .part
// file via an HTTP Range request, so a 1.5 GB fetch on flaky Wi-Fi does not
// restart from zero. The final file only appears once it is complete, so a
// killed download can never masquerade as a finished model.
func Download(ctx context.Context, m Model, progress Progress) error {
	final, err := m.Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepathDir(final), 0o755); err != nil {
		return err
	}
	part := final + ".part"

	var have int64
	if fi, err := os.Stat(part); err == nil {
		have = fi.Size()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.URL(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "flowlite")
	if have > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(have, 10)+"-")
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
		have = 0 // server ignored the range; start over
		flags |= os.O_TRUNC
		total = resp.ContentLength
	default:
		return fmt.Errorf("Hugging Face returned HTTP %d for %s", resp.StatusCode, m.File)
	}
	if total <= 0 {
		total = m.SizeBytes
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

	if float64(done) < float64(m.SizeBytes)*0.95 {
		os.Remove(part)
		return fmt.Errorf("%s came back truncated (%s of ~%s); try again",
			m.File, Human(done), Human(m.SizeBytes))
	}
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
