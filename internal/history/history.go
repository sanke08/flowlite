// Package history remembers recent transcripts so the user can recover one
// that landed nowhere — a triple tap pastes the last again, and `flowlite
// settings` → "Recent transcripts" lists and copies any recent one. Entries
// live in an append-only history.jsonl in the config directory, one JSON
// object per line.
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
	// Keep is the most entries the history ever exposes: List never returns
	// more than Keep, and every compaction trims the file to exactly the
	// newest Keep entries — a bounded FIFO where the oldest is evicted first.
	Keep = 50
	// CompactAt is the on-disk length that triggers a compaction. The small
	// slack over Keep means Append is a cheap O(1) append most of the time
	// and rewrites the whole file only once every CompactAt-Keep entries,
	// instead of on every single call. The file never holds more than
	// CompactAt lines; List hides the slack by clamping to Keep.
	CompactAt = Keep + 10
)

// Entry is one transcript.
type Entry struct {
	Time time.Time
	Text string
}

// wire is the on-disk shape; time is RFC3339 so the file is readable by hand.
//
// Older history files on disk may still have "pasted"/"audio_seconds" keys
// from before the schema was simplified down to just time+text —
// json.Unmarshal silently ignores unknown keys, so those files remain
// readable without any migration code.
type wire struct {
	Time string `json:"time"`
	Text string `json:"text"`
}

func (e Entry) toWire() wire {
	return wire{Time: e.Time.Format(time.RFC3339), Text: e.Text}
}

func (w wire) toEntry() Entry {
	t, _ := time.Parse(time.RFC3339, w.Time)
	return Entry{Time: t, Text: w.Text}
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
// to the newest Keep entries once it grows past CompactAt, so the oldest
// entries are evicted as new ones arrive.
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

// List returns up to n entries, newest first. n <= 0 or n > Keep means the
// newest Keep of them: the store never exposes more than Keep entries, even
// while the file is carrying the compaction slack. A missing file is an
// empty history, not an error.
func (s *Store) List(n int) ([]Entry, error) {
	if n <= 0 || n > Keep {
		n = Keep
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readLocked()
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	if len(all) > n {
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

// Clear erases every entry, leaving a valid, empty history file. Like
// compactLocked, the rewrite goes through a temp file and rename so a crash
// can never leave the path briefly missing or half-written.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.createTemp()
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return err
	}
	return os.Rename(f.Name(), s.path)
}

// createTemp opens a fresh, uniquely named temp file next to the history
// file for a rewrite. The name is unique per call rather than a fixed
// path+".tmp": during a settings reload the outgoing and the incoming daemon
// briefly run side by side, and with a shared name one's rewrite could
// truncate or rename away the other's half-written temp, losing transcripts.
// Same directory keeps the final os.Rename an atomic same-filesystem replace.
// os.CreateTemp creates the file 0600 (O_EXCL), matching Append's mode.
func (s *Store) createTemp() (*os.File, error) {
	return os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".*.tmp")
}

// compactLocked rewrites the file with only the newest Keep entries when it
// has grown past CompactAt. Entries stay oldest-first on disk, the order
// readLocked and List expect. The rewrite goes through a temp file and
// rename so a crash can never leave a half-written history.
func (s *Store) compactLocked() error {
	all, err := s.readLocked()
	if err != nil || len(all) <= CompactAt {
		return err
	}
	all = all[len(all)-Keep:]
	f, err := s.createTemp()
	if err != nil {
		return err
	}
	tmp := f.Name()
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
