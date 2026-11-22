package daemon

import (
	"errors"
	"sync"
	"time"

	"github.com/sanke08/flowlite/internal/audio"
	"github.com/sanke08/flowlite/internal/engine"
	"github.com/sanke08/flowlite/internal/inject"
	"github.com/sanke08/flowlite/internal/overlay"
	"github.com/sanke08/flowlite/internal/speech"
)

// Live transcription puts the words where they are going while the user
// still speaks. A goroutine keeps re-hearing the audio so far and, depending
// on config.LiveStyle, either
//
//   - types each word into the focused field once two hearings in a row
//     agree on it (see speech.AgreedPrefix) — never taking anything back
//     while the user talks, or
//   - shows everything heard so far in a caption next to the pill.
//
// Neither decides the final text. Release always runs one full pass over the
// whole recording, exactly as with live transcription off; typed words are
// then corrected to match it (see transcribe), and a caption is replaced by
// the usual paste. A preview that lags, stumbles or is cut short can never
// make the result worse.

// Whisper reads a whole recording at a time, so its preview re-reads the
// most recent previewWindow of audio on every pass. A pass costs about the
// same whatever its length (whisper.cpp pads every input to 30 s), so the
// pause between passes follows how long the last one took: a small model
// refreshes every few hundred milliseconds, a large one never queues work
// behind itself.
const (
	previewWindow = 25 * audio.SampleRate
	previewMinNew = audio.SampleRate / 4 // skip a pass for less new audio than this
	// Under a second of audio, models invent more than they hear —
	// Moonshine Base answers half a second with a wall of "hello, hello".
	previewMinAudio = audio.SampleRate
	previewMinGap   = 300 * time.Millisecond
	previewMaxGap   = 1500 * time.Millisecond
)

// A streaming model is fed whatever the microphone captured since the last
// feed this often.
const streamEvery = 100 * time.Millisecond

// liveSession is one preview, from the moment a recording is confirmed to
// the moment it ends.
type liveSession struct {
	mu      sync.Mutex
	stopped bool
	quit    chan struct{}
	done    chan struct{} // closed once the goroutine is gone and the model is free
	abort   *engine.Abort

	// typing: live words go into the focused field rather than a caption.
	typing bool
	// typed is everything this dictation has typed so far. Only the live
	// goroutine writes it; anyone else reads it only after done is closed.
	typed string
	// stream, when not nil after done, is a typing session's live stream,
	// handed over with fed (samples it has had) so the final text can come
	// from the very stream whose words were typed — see transcribe. Whoever
	// waits on done owns it and must release it.
	stream engine.Stream
	fed    int
}

// release frees a handed-over stream. Call only after done is closed.
func (l *liveSession) release() {
	if l.stream != nil {
		l.stream.Close()
		l.stream = nil
	}
}

// stop tells the preview to end and aborts a pass it has in flight. It does
// not wait; done says when it has.
func (l *liveSession) stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.stopped {
		l.stopped = true
		close(l.quit)
		l.abort.Raise()
	}
}

// show puts text in the caption unless the session has been stopped. The
// post happens under mu, so it is always queued before the HideLiveText
// that follows a stop, never after it — a late preview cannot bring the
// caption back once it has gone.
func (l *liveSession) show(committed, tentative string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stopped {
		return false
	}
	overlay.SetLiveText(committed, tentative)
	return true
}

// beginLive starts the preview for a recording that has just been confirmed.
func (d *Daemon) beginLive() {
	l := &liveSession{
		quit:  make(chan struct{}),
		done:  make(chan struct{}),
		abort: engine.NewAbort(),
		// With --no-paste nothing may be typed, so the caption stands in.
		typing: d.cfg.LiveTyping() && !d.NoPaste,
	}
	if old := d.live.Swap(l); old != nil {
		old.stop()
	}
	go func() {
		defer close(l.done)
		defer l.abort.Free()
		if st, ok := d.model.(engine.Streamer); ok {
			d.streamLive(l, st)
		} else {
			d.rereadLive(l)
		}
	}()
}

// detachLive stops the preview, if one is running, and hands it back so the
// caller can wait for it. The caption is left showing the last words.
func (d *Daemon) detachLive() *liveSession {
	l := d.live.Swap(nil)
	if l != nil {
		l.stop()
	}
	return l
}

// endLive stops the preview and takes the caption down with it.
func (d *Daemon) endLive() {
	if l := d.detachLive(); l != nil {
		overlay.HideLiveText()
		go func() { <-l.done; l.release() }()
	}
}

// eraseLive stops the preview and deletes whatever it typed — the dictation
// was cancelled, so its words should not stay behind. The backspaces wait
// for the preview goroutine to be gone, so nothing types after them.
func (d *Daemon) eraseLive() {
	if l := d.detachLive(); l != nil {
		d.erase(l)
	}
}

// erase takes down a stopped session's caption and deletes what it typed.
func (d *Daemon) erase(l *liveSession) {
	overlay.HideLiveText()
	go func() {
		<-l.done
		l.release()
		if n := len([]rune(l.typed)); n > 0 {
			if err := inject.Backspace(n); err != nil {
				d.log.Printf("live: could not take back the typed words: %v", err)
			}
		}
	}()
}

// publish hands one update to wherever live words go. settled is the part
// two hearings agree on, tentative the rest of the latest one. It returns
// false once the session has been stopped (or typing has failed), so the
// caller can wind down.
func (d *Daemon) publish(l *liveSession, settled, tentative string) bool {
	if !l.typing {
		return l.show(settled, tentative)
	}
	l.mu.Lock()
	stopped := l.stopped
	l.mu.Unlock()
	if stopped {
		return false
	}
	back, insert, next := speech.Converge(l.typed, settled, speech.LiveMaxBack)
	if back == 0 && insert == "" {
		return true
	}
	err := inject.Backspace(back)
	if err == nil {
		err = inject.Type(insert)
	}
	if err != nil {
		// What is in the field is now unknown; leave it to the final pass,
		// which pastes rather than correcting (typed stays as it was).
		d.log.Printf("live: typing failed: %v", err)
		return false
	}
	l.typed = next
	return true
}

// rereadLive is the preview for models that cannot stream.
func (d *Daemon) rereadLive(l *liveSession) {
	gap := previewMinGap
	last := 0
	prev := ""
	for {
		select {
		case <-l.quit:
			return
		case <-time.After(gap):
		}
		samples, total := d.rec.Tail(previewWindow)
		if total < previewMinAudio || total-last < previewMinNew || !speech.HasSpeech(samples) {
			continue
		}
		last = total
		t0 := time.Now()
		text, err := d.model.Transcribe(samples, engine.Options{Language: d.cfg.Language, Abort: l.abort})
		if err != nil {
			if !errors.Is(err, engine.ErrAborted) {
				d.log.Printf("live preview: %v", err)
			}
			return
		}
		if speech.Implausible(text, len(samples)) {
			// A decoding loop, not speech: keep showing the last good pass.
			gap = min(max(time.Since(t0), previewMinGap), previewMaxGap)
			continue
		}
		gap = min(max(time.Since(t0), previewMinGap), previewMaxGap)
		if text == "" {
			continue
		}
		if total > previewWindow {
			// The pass no longer starts where the dictation did, so its
			// words cannot be lined up with what was typed; typing waits
			// for the final pass from here on.
			if l.typing {
				continue
			}
			text = "… " + text
		}
		settled := speech.AgreedPrefix(prev, text)
		prev = text
		tentative := text[len(settled):]
		if !l.typing {
			// A whole-window re-read is the caption's best guess; show it
			// all as settled, as before.
			settled, tentative = text, ""
		}
		if !d.publish(l, settled, tentative) {
			return
		}
	}
}

// streamLive is the preview for models that transcribe as audio arrives.
func (d *Daemon) streamLive(l *liveSession, st engine.Streamer) {
	s, err := st.NewStream(engine.Options{Language: d.cfg.Language})
	if err != nil {
		d.log.Printf("live preview: %v", err)
		return
	}
	kept := false
	defer func() {
		if !kept {
			s.Close()
		}
	}()
	tick := time.NewTicker(streamEvery)
	defer tick.Stop()
	fed := 0
	var shownC, shownT string
	for {
		select {
		case <-l.quit:
			if l.typing {
				// Hand the stream to the final pass: it finishes this
				// stream rather than starting over, so the text it ends
				// with is the one whose words are already in the field.
				l.stream, l.fed, kept = s, fed, true
			}
			return
		case <-tick.C:
		}
		chunk, total := d.rec.Since(fed)
		fed = total
		if len(chunk) == 0 {
			continue
		}
		if err := s.Feed(chunk); err != nil {
			d.log.Printf("live preview: %v", err)
			return
		}
		c, t := s.Text()
		if c == shownC && t == shownT {
			continue
		}
		shownC, shownT = c, t
		if speech.Implausible(c+" "+t, fed) {
			continue // a decoding loop, not speech
		}
		if !d.publish(l, c, t) {
			return
		}
	}
}
