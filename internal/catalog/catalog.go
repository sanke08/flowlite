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

// Engine names the library that runs a model.
type Engine string

const (
	// Whisper is whisper.cpp, for OpenAI's Whisper models (GGML .bin files).
	// It is the zero value so every entry written before engines existed
	// keeps meaning what it always meant.
	Whisper Engine = ""
	// Transcribe is transcribe.cpp, for Moonshine (GGUF files).
	Transcribe Engine = "transcribe"
)

// Name is how the engine is shown to the user.
func (e Engine) Name() string {
	if e == Transcribe {
		return "transcribe.cpp"
	}
	return "whisper.cpp"
}

// dir is the engine's folder under models/. whisper.cpp keeps the folder
// name the previous version used, so existing downloads are still found.
func (e Engine) dir() string {
	if e == Transcribe {
		return "transcribe"
	}
	return "whispercpp"
}

// Model is one downloadable model file.
type Model struct {
	Key         string
	Label       string
	Blurb       string
	File        string // filename inside the HF repo and on disk
	SizeBytes   int64  // published size; used for progress and completeness
	SHA256      string // from HF's LFS metadata; verified against the finished download
	EnglishOnly bool
	Recommended bool
	Engine      Engine
	// Repo is the Hugging Face repo URL (ending in "/resolve/main/") the
	// file comes from; "" means whisper.cpp's own model repo.
	Repo string
	// Streaming models transcribe audio as it arrives, so live
	// transcription follows the voice closely instead of re-reading the
	// whole recording every half second.
	Streaming bool
}

// Catalog is Whisper then Moonshine, each ordered smallest to largest.
var Catalog = []Model{
	{
		Key: "tiny.en", Label: "Tiny (English)", File: "ggml-tiny.en.bin", SizeBytes: 74 * mb, EnglishOnly: true,
		SHA256: "921e4cf8686fdd993dcd081a5da5b6c365bfde1162e72b08d75ac75289920b1f",
		Blurb:  "Fastest and smallest. Fine for short, clear phrases; mangles names and jargon.",
	},
	{
		Key: "base.en", Label: "Base (English)", File: "ggml-base.en.bin", SizeBytes: 141 * mb, EnglishOnly: true,
		SHA256: "a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002",
		Blurb:  "Still near-instant and clearly better than Tiny.",
	},
	{
		Key: "small.en", Label: "Small (English)", File: "ggml-small.en.bin", SizeBytes: 465 * mb, EnglishOnly: true,
		SHA256: "c6138d6d58ecc8322097e0f987c32f1be8bb0a18532a3f88f734d1bbf9c41e5d",
		Blurb:  "Strong everyday English, comfortably under a second.",
	},
	{
		Key: "large-v3-turbo-q5", Label: "Large v3 Turbo (compressed)", File: "ggml-large-v3-turbo-q5_0.bin",
		SizeBytes: 547 * mb, Recommended: true,
		SHA256: "394221709cd5ad1f40c46e6031ca61bce88931e6e088c188294c6d5a55ffa7e2",
		Blurb:  "Near-best accuracy across 99 languages, handles accents well, a third the size of the full build. ~1.5 s per dictation.",
	},
	{
		Key: "large-v3-turbo", Label: "Large v3 Turbo (full)", File: "ggml-large-v3-turbo.bin", SizeBytes: 1549 * mb,
		SHA256: "1fc70f774d38eb169993ac391eea357ef47c88757ef72ee5943879b7e8e2bc69",
		Blurb:  "Uncompressed Turbo. Marginally more faithful, three times the disk.",
	},
	{
		Key: "large-v3", Label: "Large v3", File: "ggml-large-v3.bin", SizeBytes: 2952 * mb,
		SHA256: "64d182b440b98d5203c4f9bd541544d84c605196c4f7b845dfa11fb23594d1e2",
		Blurb:  "Most accurate. Several times slower than Turbo for a small gain.",
	},

	// Moonshine (transcribe.cpp), English only. The Streaming models
	// transcribe while you speak, so Live transcription follows the voice
	// instead of re-reading it; all of them also do plain dictation.
	{
		Key: "moonshine-base", Label: "Moonshine Base (English)", File: "moonshine-base-Q8_0.gguf",
		SizeBytes: 77476480, EnglishOnly: true, Engine: Transcribe, Repo: moonshineRepo("moonshine-base"),
		SHA256: "7f0027dfd857d310b63a85ef57cadf183da712cc374f85a648f8bc18aaa2efc8",
		Blurb:  "Light and quick for everyday English dictation.",
	},
	{
		Key: "moonshine-streaming-tiny", Label: "Moonshine Streaming Tiny (English)", File: "moonshine-streaming-tiny-Q8_0.gguf",
		SizeBytes: 50462816, EnglishOnly: true, Engine: Transcribe, Streaming: true, Repo: moonshineRepo("moonshine-streaming-tiny"),
		SHA256: "930e4622ad3a24158b91406c30c977fa6a26b34cb32d6ac3e57cfb23383a869e",
		Blurb:  "Smallest live model. Words appear as you speak; rougher on names.",
	},
	{
		Key: "moonshine-streaming-small", Label: "Moonshine Streaming Small (English)", File: "moonshine-streaming-small-Q8_0.gguf",
		SizeBytes: 198506848, EnglishOnly: true, Engine: Transcribe, Streaming: true, Repo: moonshineRepo("moonshine-streaming-small"),
		SHA256: "d03670f69629b649085d0f44a63d97668b4119117cc9611a4e4ad94341713dfc",
		Blurb:  "The pick for Live transcription: words appear as you speak, accuracy well above Whisper Base.",
	},
	{
		Key: "moonshine-streaming-medium", Label: "Moonshine Streaming Medium (English)", File: "moonshine-streaming-medium-Q8_0.gguf",
		SizeBytes: 295793568, EnglishOnly: true, Engine: Transcribe, Streaming: true, Repo: moonshineRepo("moonshine-streaming-medium"),
		SHA256: "f7c9564249b508f6012927ec4f9e536087da53a7047f858ca9975bea5f75299e",
		Blurb:  "Most accurate live model; uses noticeably more memory and GPU while you talk.",
	},
}

// moonshineRepo is where transcribe.cpp's tested Moonshine GGUF builds live.
func moonshineRepo(name string) string {
	return "https://huggingface.co/handy-computer/" + name + "-gguf/resolve/main/"
}

// enabled lists the engines compiled into this binary. Whisper always is;
// the others register themselves (see engine.init) when their build tag is on.
var enabled = map[Engine]bool{Whisper: true}

// EnableEngine marks an engine as compiled in. Called from init only.
func EnableEngine(e Engine) { enabled[e] = true }

// Supported reports whether this build can run the model.
func (m Model) Supported() bool { return enabled[m.Engine] }

// Available is the catalog minus models this build cannot run, in order.
func Available() []Model {
	out := make([]Model, 0, len(Catalog))
	for _, m := range Catalog {
		if m.Supported() {
			out = append(out, m)
		}
	}
	return out
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
func (m Model) URL() string {
	if m.Repo != "" {
		return m.Repo + m.File
	}
	return repoBase + m.File
}

// Dir is this model's folder on disk.
func (m Model) Dir() (string, error) {
	base, err := config.EngineModelsDir(m.Engine.dir())
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

// PruneExcept deletes every model on disk other than keep — complete or a
// leftover partial download, Whisper or Moonshine alike — so FlowLite only
// ever holds one. It is called only after keep is completely on disk, so a
// failed download can never leave the user with nothing.
func PruneExcept(keep string) (removed []Removed, err error) {
	for _, m := range Catalog {
		if m.Key == keep {
			continue
		}
		size := m.dirBytes() // before the files are gone
		if size == 0 {
			continue
		}
		if rerr := m.Remove(); rerr != nil {
			err = rerr
			continue
		}
		removed = append(removed, Removed{Model: m, Bytes: size})
	}
	return removed, err
}

// dirBytes is everything in the model's folder: the model, or a partial
// download of it.
func (m Model) dirBytes() int64 {
	d, err := m.Dir()
	if err != nil {
		return 0
	}
	var n int64
	entries, _ := os.ReadDir(d)
	for _, e := range entries {
		if fi, err := e.Info(); err == nil && !fi.IsDir() {
			n += fi.Size()
		}
	}
	return n
}
