//go:build darwin

package sound

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AVFoundation -framework Foundation
int  flowlite_sound_load(int idx, const float *samples, int n, int rate);
void flowlite_sound_play(int idx);
void flowlite_sound_close(void);
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe"
)

// Player plays cues through AVAudioPlayer, macOS's system-managed playback
// path. Every cue is rendered and loaded once in NewPlayer; Play only
// triggers the matching preloaded player.
//
// Why not miniaudio here like the other platforms (player_malgo.go): the
// AUHAL output stream it opens stutters audibly on macOS 26 while its
// callback is provably on time and non-silent. The same samples written to
// a WAV and played by afplay were clean, so the fault is in that stream
// path, not the cues, and the fix is to let the system own the stream.
type Player struct {
	enabled bool
	loaded  bool

	workMu   sync.Mutex
	workStop chan struct{}

	logf func(format string, args ...any)
}

// NewPlayer renders and preloads every cue. Errors are non-fatal for the
// app: a Player that failed to load stays silent.
func NewPlayer(enabled bool) (*Player, error) {
	p := &Player{enabled: enabled}
	if !enabled {
		return p, nil
	}
	for c := Start; c <= Error; c++ {
		s := Samples(c)
		if rc := C.flowlite_sound_load(C.int(c), (*C.float)(unsafe.Pointer(&s[0])), C.int(len(s)), C.int(Rate)); rc != 0 {
			return p, fmt.Errorf("loading %s cue: AVAudioPlayer error %d", c, int(rc))
		}
	}
	p.loaded = true
	return p, nil
}

// SetLogger installs where diagnostics go. Nothing to report on this backend
// today; kept so callers are backend-agnostic.
func (p *Player) SetLogger(fn func(format string, args ...any)) {
	if p != nil {
		p.logf = fn
	}
}

// Reopen is a no-op here: AVAudioPlayer follows device and sleep/wake
// changes on its own.
func (p *Player) Reopen() {}

// Play triggers a cue. Safe on a nil or disabled Player.
func (p *Player) Play(c Cue) {
	if p == nil || !p.enabled || !p.loaded {
		return
	}
	C.flowlite_sound_play(C.int(c))
}

func (p *Player) ready() bool { return p != nil && p.enabled && p.loaded }

// Close releases every player.
func (p *Player) Close() {
	if p == nil {
		return
	}
	p.StopWorking()
	if p.loaded {
		p.loaded = false
		C.flowlite_sound_close()
	}
}
