// Package catalog knows which Whisper models exist, where they live on disk,
// and how to fetch them from Hugging Face.
package catalog

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sanke08/flowlite/internal/config"
)

const (
	repoBase = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/"
	mb       = 1024 * 1024
)

// Model is one downloadable GGML file.
type Model struct {
	Key         string
	Label       string
	Blurb       string
	File        string // filename inside the HF repo and on disk
	SizeBytes   int64  // published size; used for progress and completeness
	EnglishOnly bool
	Recommended bool
}

// Catalog is ordered smallest to largest.
var Catalog = []Model{
	{
		Key: "tiny.en", Label: "Tiny (English)", File: "ggml-tiny.en.bin", SizeBytes: 74 * mb, EnglishOnly: true,
		Blurb: "Fastest and smallest. Fine for short, clear phrases; mangles names and jargon.",
	},
	{
		Key: "base.en", Label: "Base (English)", File: "ggml-base.en.bin", SizeBytes: 141 * mb, EnglishOnly: true,
		Blurb: "Still near-instant and clearly better than Tiny.",
	},
	{
		Key: "small.en", Label: "Small (English)", File: "ggml-small.en.bin", SizeBytes: 465 * mb, EnglishOnly: true,
		Blurb: "Strong everyday English, comfortably under a second.",
	},
	{
		Key: "large-v3-turbo-q5", Label: "Large v3 Turbo (compressed)", File: "ggml-large-v3-turbo-q5_0.bin",
		SizeBytes: 547 * mb, Recommended: true,
		Blurb: "Near-best accuracy across 99 languages, handles accents well, a third the size of the full build. ~1.5 s per dictation.",
	},
	{
		Key: "large-v3-turbo", Label: "Large v3 Turbo (full)", File: "ggml-large-v3-turbo.bin", SizeBytes: 1549 * mb,
		Blurb: "Uncompressed Turbo. Marginally more faithful, three times the disk.",
	},
	{
		Key: "large-v3", Label: "Large v3", File: "ggml-large-v3.bin", SizeBytes: 2952 * mb,
		Blurb: "Most accurate. Several times slower than Turbo for a small gain.",
	},
}

// Get looks a model up by key.
func Get(key string) (Model, bool) {
	for _, m := range Catalog {
		if m.Key == key {
			return m, true
		}
	}
	return Model{}, false
}

// Default is the recommended model.
func Default() Model {
	for _, m := range Catalog {
		if m.Recommended {
			return m
		}
	}
	return Catalog[0]
}

// URL is the direct download link.
func (m Model) URL() string { return repoBase + m.File }

// Dir is this model's folder on disk.
func (m Model) Dir() (string, error) {
	base, err := config.ModelsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, m.Key), nil
}

// Path is the GGML file on disk.
func (m Model) Path() (string, error) {
	d, err := m.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, m.File), nil
}

// DiskBytes is the size of the file on disk, or 0.
func (m Model) DiskBytes() int64 {
	p, err := m.Path()
	if err != nil {
		return 0
	}
	fi, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// Downloaded reports whether the file is present and complete. A partial
// download is smaller than the published size; 5% slack absorbs the
// approximation in SizeBytes.
func (m Model) Downloaded() bool {
	got := m.DiskBytes()
	return got > 0 && float64(got) >= float64(m.SizeBytes)*0.95
}

// Remove deletes the downloaded file (and any partial).
func (m Model) Remove() error {
	d, err := m.Dir()
	if err != nil {
		return err
	}
	return os.RemoveAll(d)
}

// Human formats bytes for display.
func Human(n int64) string {
	switch {
	case n >= 1024*mb:
		return fmt.Sprintf("%.2f GB", float64(n)/float64(1024*mb))
	default:
		return fmt.Sprintf("%d MB", n/mb)
	}
}

// Installed returns every model present on disk. FlowLite keeps exactly one;
// more than one means a switch was interrupted, or the files predate that rule.
func Installed() []Model {
	var out []Model
	for _, m := range Catalog {
		if m.Downloaded() {
			out = append(out, m)
		}
	}
	return out
}

// Removed records one deleted model and the space it gave back.
type Removed struct {
	Model Model
	Bytes int64
}

// PruneExcept deletes every installed model other than keep. It is called
// only after a new model is completely on disk, so a failed download can
// never leave the user with nothing.
func PruneExcept(keep string) (removed []Removed, err error) {
	for _, m := range Installed() {
		if m.Key == keep {
			continue
		}
		size := m.DiskBytes() // before the file is gone
		if rerr := m.Remove(); rerr != nil {
			err = rerr
			continue
		}
		removed = append(removed, Removed{Model: m, Bytes: size})
	}
	return removed, err
}
