// Package history remembers recent transcripts so the user can recover one
// that landed nowhere — a triple tap pastes the last again, and `flowlite
// history` / `flowlite last` print them. Entries live in an append-only
// history.jsonl in the config directory, one JSON object per line.
package history

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sanke08/flowlite/internal/config"
)

// FileName is the history file inside config.Dir().
const FileName = "history.jsonl"

const (
	// Keep is how many entries survive a compaction.
	Keep = 100
	// CompactAt is the file length that triggers a compaction.
	CompactAt = 150
)

// Entry is one transcript.
type Entry struct {
	Time         time.Time
	Text         string
	Pasted       bool // false when the paste failed or --no-paste was on
	AudioSeconds float64
}

// wire is the on-disk shape; time is RFC3339 so the file is readable by hand.
type wire struct {
	Time         string  `json:"time"`
	Text         string  `json:"text"`
	Pasted       bool    `json:"pasted"`
	AudioSeconds float64 `json:"audio_seconds"`
}

func (e Entry) toWire() wire {
	return wire{Time: e.Time.Format(time.RFC3339), Text: e.Text, Pasted: e.Pasted, AudioSeconds: e.AudioSeconds}
}

func (w wire) toEntry() Entry {
	t, _ := time.Parse(time.RFC3339, w.Time)
	return Entry{Time: t, Text: w.Text, Pasted: w.Pasted, AudioSeconds: w.AudioSeconds}
}

// Store is a history file. Methods are safe to call from several goroutines
// in one process.
type Store struct {
	mu   sync.Mutex
	path string
}

// Open returns the store in the FlowLite config directory.
func Open() (*Store, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	return OpenAt(filepath.Join(dir, FileName)), nil
}

// OpenAt returns a store backed by an arbitrary file (tests, mostly).
func OpenAt(path string) *Store { return &Store{path: path} }

// Path is the file the store writes to.
func (s *Store) Path() string { return s.path }

// Append records a transcript. Empty text is ignored. The file is compacted
// to the newest Keep entries once it grows past CompactAt.
func (s *Store) Append(e Entry) error {
	if e.Text == "" {
		return nil
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	line, err := json.Marshal(e.toWire())
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(line, '\n'))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}
	return s.compactLocked()
}

// Last returns the newest entry, or false when there is none.
func (s *Store) Last() (Entry, bool) {
	all, err := s.List(1)
	if err != nil || len(all) == 0 {
		return Entry{}, false
	}
	return all[0], true
}

// List returns up to n entries, newest first. n <= 0 means all of them. A
// missing file is an empty history, not an error.
func (s *Store) List(n int) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readLocked()
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	if n > 0 && len(all) > n {
		all = all[:n]
	}
	return all, nil
}

// readLocked returns every entry oldest first, skipping unreadable lines.
func (s *Store) readLocked() ([]Entry, error) {
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var w wire
		if err := json.Unmarshal(sc.Bytes(), &w); err != nil || w.Text == "" {
			continue // a torn write from a crash; don't lose the rest
		}
		out = append(out, w.toEntry())
	}
	return out, sc.Err()
}

// compactLocked rewrites the file with only the newest Keep entries when it
// has grown past CompactAt. The rewrite goes through a temp file and rename
// so a crash can never leave a half-written history.
func (s *Store) compactLocked() error {
	all, err := s.readLocked()
	if err != nil || len(all) <= CompactAt {
		return err
	}
	all = all[len(all)-Keep:]
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, e := range all {
		if err := enc.Encode(e.toWire()); err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, s.path)
}
