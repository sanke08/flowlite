package cli

import (
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

// spinner is a one-line "working…" indicator for the slow bits of a command
// (loading ggml backends, enumerating CoreAudio devices, opening an HTTP
// connection). It draws on stderr with \r so it never lands in stdout, and it
// draws nothing at all unless stderr is a terminal — `flowlite doctor | cat`
// and log files stay free of control characters.
type spinner struct {
	text string
	tty  bool

	once sync.Once
	quit chan struct{}
	done chan struct{}
}

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// startSpinner begins animating "  ⠋ text" on stderr. Always pair it with
// Stop or Done — both are idempotent and safe from any goroutine, so it is
// fine to `defer s.Stop()` and also call s.Done(...) on the happy path.
func startSpinner(text string) *spinner {
	s := &spinner{
		text: text,
		tty:  term.IsTerminal(int(os.Stderr.Fd())),
		quit: make(chan struct{}),
		done: make(chan struct{}),
	}
	if !s.tty {
		close(s.done)
		return s
	}
	go s.run()
	return s
}

func (s *spinner) run() {
	defer close(s.done)
	t := time.NewTicker(80 * time.Millisecond)
	defer t.Stop()
	i := 0
	s.draw(i)
	for {
		select {
		case <-s.quit:
			// \r + erase-line so whatever prints next starts on a clean line.
			fmt.Fprint(os.Stderr, "\r\033[2K")
			return
		case <-t.C:
			i++
			s.draw(i)
		}
	}
}

func (s *spinner) draw(i int) {
	fmt.Fprintf(os.Stderr, "\r\033[2K  %s %s", dim(spinFrames[i%len(spinFrames)]), dim(s.text))
}

// Stop halts the animation and clears its line. Returns once the line is
// clean, so the caller can print immediately afterwards without interleaving.
func (s *spinner) Stop() {
	if s == nil {
		return
	}
	s.once.Do(func() { close(s.quit) })
	<-s.done
}

// Done stops the spinner and prints line to stdout in its place.
func (s *spinner) Done(line string) {
	s.Stop()
	fmt.Println(line)
}
