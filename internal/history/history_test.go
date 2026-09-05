package history

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	return OpenAt(filepath.Join(t.TempDir(), FileName))
}

func TestEmptyHistory(t *testing.T) {
	s := newStore(t)
	if _, ok := s.Last(); ok {
		t.Fatal("Last on a missing file must report nothing")
	}
	list, err := s.List(10)
	if err != nil || len(list) != 0 {
		t.Fatalf("List = %v, %v; want empty, nil", list, err)
	}
}

func TestAppendListLast(t *testing.T) {
	s := newStore(t)
	base := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		e := Entry{Time: base.Add(time.Duration(i) * time.Minute), Text: fmt.Sprintf("line %d", i)}
		if err := s.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	last, ok := s.Last()
	if !ok || last.Text != "line 4" || !last.Time.Equal(base.Add(4*time.Minute)) {
		t.Fatalf("Last = %+v, %v", last, ok)
	}
	list, err := s.List(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].Text != "line 4" || list[2].Text != "line 2" {
		t.Fatalf("List(3) = %+v, want newest first", list)
	}
	all, _ := s.List(0)
	if len(all) != 5 {
		t.Fatalf("List(0) returned %d entries, want 5", len(all))
	}
}

func TestEmptyTextIsNotRecorded(t *testing.T) {
	s := newStore(t)
	if err := s.Append(Entry{Text: ""}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Path()); !os.IsNotExist(err) {
		t.Fatal("empty transcript must not create the file")
	}
}

func TestFileIsOneRFC3339JSONObjectPerLine(t *testing.T) {
	s := newStore(t)
	when := time.Date(2026, 9, 5, 12, 34, 56, 0, time.FixedZone("IST", 5*3600+1800))
	if err := s.Append(Entry{Time: when, Text: "hello\nworld"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"time":"2026-09-05T12:34:56+05:30","text":"hello\nworld"}` + "\n"
	if string(b) != want {
		t.Fatalf("file = %q\nwant   %q", b, want)
	}
}

func TestCompaction(t *testing.T) {
	s := newStore(t)
	for i := 0; i < CompactAt; i++ {
		if err := s.Append(Entry{Text: fmt.Sprintf("t%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	if n := countLines(t, s.Path()); n != CompactAt {
		t.Fatalf("at the limit the file has %d lines, want %d", n, CompactAt)
	}
	if err := s.Append(Entry{Text: "over"}); err != nil {
		t.Fatal(err)
	}
	if n := countLines(t, s.Path()); n != Keep {
		t.Fatalf("after compaction the file has %d lines, want %d", n, Keep)
	}
	all, _ := s.List(0)
	if len(all) != Keep || all[0].Text != "over" || all[Keep-1].Text != fmt.Sprintf("t%d", CompactAt-Keep+1) {
		t.Fatalf("compaction kept the wrong entries: newest %q oldest %q", all[0].Text, all[len(all)-1].Text)
	}
	if _, err := os.Stat(s.Path() + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file left behind")
	}
}

func TestClear(t *testing.T) {
	s := newStore(t)
	if err := s.Append(Entry{Text: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Entry{Text: "two"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(0)
	if err != nil || len(list) != 0 {
		t.Fatalf("List after Clear = %v, %v; want empty, nil", list, err)
	}
	if _, ok := s.Last(); ok {
		t.Fatal("Last after Clear must report nothing")
	}
	assertNoTemp(t, s.Path())
	// The store must still be usable afterward.
	if err := s.Append(Entry{Text: "three"}); err != nil {
		t.Fatal(err)
	}
	last, ok := s.Last()
	if !ok || last.Text != "three" {
		t.Fatalf("Last after Clear+Append = %+v, %v", last, ok)
	}
}

func TestTornLinesAreSkipped(t *testing.T) {
	s := newStore(t)
	_ = s.Append(Entry{Text: "good"})
	f, _ := os.OpenFile(s.Path(), os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`{"time":"2026-09-05T00:00:00Z","te`)
	f.Close()
	last, ok := s.Last()
	if !ok || last.Text != "good" {
		t.Fatalf("Last = %+v, %v; a torn tail must not hide earlier entries", last, ok)
	}
}

func TestConcurrentAppends(t *testing.T) {
	s := newStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.Append(Entry{Text: strings.Repeat("x", i+1)}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	all, err := s.List(0)
	if err != nil || len(all) != 40 {
		t.Fatalf("got %d entries, %v; want 40", len(all), err)
	}
}

// TestTwoStoresCompactConcurrently is the settings-reload scenario: the
// outgoing and the incoming daemon each hold their own Store (separate
// mutexes) on the same history file and both trigger a compaction at the same
// moment. With a fixed path+".tmp" the two rewrites truncated and renamed each
// other's temp file (garbled entries, or one rename failing with ENOENT). Each
// rewrite must use its own temp file, so the outcome is always a valid file
// holding exactly the newest Keep entries and no stray temp file.
func TestTwoStoresCompactConcurrently(t *testing.T) {
	for round := 0; round < 25; round++ {
		path := filepath.Join(t.TempDir(), FileName)
		seed := OpenAt(path)
		// Just past the threshold: the next Append on either store compacts.
		for i := 0; i < CompactAt; i++ {
			if err := seed.Append(Entry{Text: fmt.Sprintf("r%d-t%d", round, i)}); err != nil {
				t.Fatal(err)
			}
		}
		a, b := OpenAt(path), OpenAt(path)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for _, st := range []*Store{a, b} {
			wg.Add(1)
			go func(st *Store) {
				defer wg.Done()
				<-start
				st.mu.Lock()
				defer st.mu.Unlock()
				// Both files are over CompactAt, so both compact for real.
				if err := st.compactLocked(); err != nil {
					t.Errorf("round %d: compact: %v", round, err)
				}
			}(st)
		}
		// Force the compaction path on both: one extra line, written without
		// compacting so both stores see CompactAt+1 entries.
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(f, `{"time":"2026-09-05T00:00:00Z","text":"r%d-over"}`+"\n", round)
		f.Close()
		close(start)
		wg.Wait()

		all, err := a.List(0)
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		if len(all) != Keep {
			t.Fatalf("round %d: %d entries after concurrent compaction, want %d", round, len(all), Keep)
		}
		if all[0].Text != fmt.Sprintf("r%d-over", round) {
			t.Fatalf("round %d: newest = %q, want the last appended entry", round, all[0].Text)
		}
		// Every kept entry must be intact and in order: an interleaved write
		// into a shared temp file shows up here as a torn or missing line.
		for i := 1; i < Keep; i++ {
			want := fmt.Sprintf("r%d-t%d", round, CompactAt-i)
			if all[i].Text != want {
				t.Fatalf("round %d: entry %d = %q, want %q", round, i, all[i].Text, want)
			}
		}
		assertNoTemp(t, path)
	}
}

// TestTwoStoresAppendConcurrently: two stores in (what stands in for) two
// processes appending to the same file below the compaction threshold must
// keep every entry, since each line is a single O_APPEND write.
func TestTwoStoresAppendConcurrently(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	a, b := OpenAt(path), OpenAt(path)
	var wg sync.WaitGroup
	// Stay under CompactAt in total so no compaction runs and every line
	// must survive.
	const per = CompactAt/2 - 1
	for _, st := range []*Store{a, b} {
		for i := 0; i < per; i++ {
			wg.Add(1)
			go func(st *Store, i int) {
				defer wg.Done()
				if err := st.Append(Entry{Text: fmt.Sprintf("%p-%d", st, i)}); err != nil {
					t.Error(err)
				}
			}(st, i)
		}
	}
	wg.Wait()
	// Count lines on disk rather than via List, which clamps to Keep.
	if n := countLines(t, path); n != 2*per {
		t.Fatalf("file has %d lines, want %d", n, 2*per)
	}
	if all, err := a.List(0); err != nil || len(all) != Keep {
		t.Fatalf("List returned %d entries, %v; want %d", len(all), err, Keep)
	}
	assertNoTemp(t, path)
}

// assertNoTemp fails if any rewrite temp file (history.jsonl.*.tmp, or the
// old fixed history.jsonl.tmp) was left next to the history file.
func assertNoTemp(t *testing.T, path string) {
	t.Helper()
	stray, err := filepath.Glob(path + "*.tmp")
	if err != nil {
		t.Fatal(err)
	}
	if len(stray) != 0 {
		t.Fatalf("temp file left behind: %v", stray)
	}
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		n++
	}
	return n
}

// TestBoundedAtKeep is the bounded-FIFO contract: after many more appends
// than Keep, List returns exactly the newest Keep in newest-first order, the
// file never holds more than CompactAt lines, and Clear still empties it.
func TestBoundedAtKeep(t *testing.T) {
	s := OpenAt(filepath.Join(t.TempDir(), FileName))
	const total = 60
	for i := 1; i <= total; i++ {
		if err := s.Append(Entry{Text: fmt.Sprintf("t%d", i)}); err != nil {
			t.Fatal(err)
		}
		if n := countLines(t, s.Path()); n > CompactAt {
			t.Fatalf("after %d appends the file has %d lines, over the %d cap", i, n, CompactAt)
		}
		all, err := s.List(0)
		if err != nil {
			t.Fatal(err)
		}
		if len(all) > Keep {
			t.Fatalf("after %d appends List returned %d entries, want at most %d", i, len(all), Keep)
		}
	}
	all, err := s.List(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != Keep {
		t.Fatalf("List returned %d entries, want exactly %d", len(all), Keep)
	}
	for i, e := range all {
		if want := fmt.Sprintf("t%d", total-i); e.Text != want {
			t.Fatalf("entry %d is %q, want %q (newest first)", i, e.Text, want)
		}
	}
	// Asking for more than Keep is clamped, not honoured.
	over, err := s.List(Keep + 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(over) != Keep {
		t.Fatalf("List(Keep+100) returned %d entries, want %d", len(over), Keep)
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if n := countLines(t, s.Path()); n != 0 {
		t.Fatalf("after Clear the file has %d lines, want 0", n)
	}
	if all, _ := s.List(0); len(all) != 0 {
		t.Fatalf("after Clear List returned %d entries, want 0", len(all))
	}
}
