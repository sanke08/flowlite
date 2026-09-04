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
		e := Entry{Time: base.Add(time.Duration(i) * time.Minute), Text: fmt.Sprintf("line %d", i), Pasted: i%2 == 0, AudioSeconds: float64(i) + 0.5}
		if err := s.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	last, ok := s.Last()
	if !ok || last.Text != "line 4" || !last.Pasted || last.AudioSeconds != 4.5 || !last.Time.Equal(base.Add(4*time.Minute)) {
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
	if err := s.Append(Entry{Time: when, Text: "hello\nworld", Pasted: true, AudioSeconds: 1.25}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"time":"2026-09-05T12:34:56+05:30","text":"hello\nworld","pasted":true,"audio_seconds":1.25}` + "\n"
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
