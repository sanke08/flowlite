//go:build windows

// Windows main loop: a low-level keyboard hook only fires while the thread
// that installed it pumps messages, and the pill window needs a WndProc.
// Run owns that loop on the main thread; Dispatch queues closures and wakes
// the loop with a thread message so they execute there.
package mainloop

import (
	"log"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/sanke08/flowlite/internal/win32"
)

var (
	user32                         = syscall.NewLazyDLL("user32.dll")
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procGetMessageW                = user32.NewProc("GetMessageW")
	procTranslateMessage           = user32.NewProc("TranslateMessage")
	procDispatchMessageW           = user32.NewProc("DispatchMessageW")
	procPostThreadMessage          = user32.NewProc("PostThreadMessageW")
	procDefWindowProcW             = user32.NewProc("DefWindowProcW")
	procRegisterDeviceNotification = user32.NewProc("RegisterDeviceNotificationW")
	procGetCurrentThread           = kernel32.NewProc("GetCurrentThreadId")
)

const (
	wmQuit = 0x0012
	wmApp  = 0x8000 // WM_APP: "drain the dispatch queue"

	// Sleep/wake and device-change plumbing for OnWake; see wakeWndProc.
	wmPowerBroadcast         = 0x0218
	pbtAPMResumeSuspend      = 0x0007 // resume triggered by the user
	pbtAPMResumeAutomatic    = 0x0012 // any resume; always sent, so the one to rely on
	wmDeviceChange           = 0x0219
	dbtDevNodesChanged       = 0x0007
	dbtDeviceArrival         = 0x8000
	dbtDeviceRemoveComplete  = 0x8004
	dbtDevTypDeviceInterface = 0x0005
	deviceNotifyWindowHandle = 0x0000
	wsOverlapped             = 0x00000000

	// wakeDebounce coalesces the burst of notifications one real event
	// produces — plugging in headphones yields several DBT_DEVNODES_CHANGED
	// plus an arrival per interface class, and a resume is followed by the
	// audio stack re-enumerating everything — into a single wake callback.
	// The registered fn (Player.Reopen) tears down and rebuilds a stream, so
	// it should run once per event, not once per message.
	wakeDebounce = 500 * time.Millisecond
)

type point struct{ x, y int32 }

type msg struct {
	hwnd     uintptr
	message  uint32
	_        uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       point
	lPrivate uint32
}

type guid struct {
	data1 uint32
	data2 uint16
	data3 uint16
	data4 [8]byte
}

// devBroadcastDeviceInterface is DEV_BROADCAST_DEVICEINTERFACE_W with the
// trailing name shortened to its minimum; only the header is read.
type devBroadcastDeviceInterface struct {
	size       uint32
	deviceType uint32
	reserved   uint32
	classGUID  guid
	name       [1]uint16
}

var (
	// KSCATEGORY_AUDIO {6994AD04-93EF-11D0-A3CC-00A0C9223196}: every kernel
	// streaming audio device, which is what appears and disappears when a
	// USB/Bluetooth/display audio device comes or goes.
	ksCategoryAudio = guid{0x6994AD04, 0x93EF, 0x11D0, [8]byte{0xA3, 0xCC, 0x00, 0xA0, 0xC9, 0x22, 0x31, 0x96}}
	// DEVINTERFACE_AUDIO_RENDER {E6327CAD-DCEC-4949-AE8A-991E976A79D2}: the
	// MMDevice render endpoints, i.e. the things "default output device"
	// is chosen from.
	devInterfaceAudioRender = guid{0xE6327CAD, 0xDCEC, 0x4949, [8]byte{0xAE, 0x8A, 0x99, 0x1E, 0x97, 0x6A, 0x79, 0xD2}}
)

var (
	runOnce sync.Once
	mainTID uint32
	queue   = make(chan func(), 256)

	wakeMu    sync.Mutex
	wakeFns   []func()
	wakeTimer *time.Timer // pending debounced wake, nil when none
	wakeGen   uint64      // bumped by every scheduleWake; a fire for an older gen is stale

	wakeWndProcCB = syscall.NewCallback(wakeWndProc)
)

func init() { runtime.LockOSThread() }

// Run pumps messages on the main thread until Stop.
func Run(ready func()) {
	runOnce.Do(func() {
		tid, _, _ := procGetCurrentThread.Call()
		mainTID = uint32(tid)
		createWakeWindow()
		go ready()
		var m msg
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 { // WM_QUIT or error
				return
			}
			if m.message == wmApp && m.hwnd == 0 {
				drain()
				continue
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
	})
}

func drain() {
	for {
		select {
		case fn := <-queue:
			fn()
		default:
			return
		}
	}
}

// Stop ends the loop.
func Stop() {
	procPostThreadMessage.Call(uintptr(mainTID), wmQuit, 0, 0)
}

// Dispatch runs fn on the loop thread.
func Dispatch(fn func()) {
	queue <- fn
	procPostThreadMessage.Call(uintptr(mainTID), wmApp, 0, 0)
}

// DispatchSync runs fn on the loop thread and waits.
func DispatchSync(fn func()) {
	done := make(chan struct{})
	Dispatch(func() { fn(); close(done) })
	<-done
}

// ThreadID is the loop thread, for callers that need to install hooks on it.
func ThreadID() uint32 { return mainTID }

// OnWake registers fn to run whenever the system resumes from sleep or the
// set of audio devices changes (headphones, Bluetooth, display audio). Both
// leave an open output stream alive but inaudible — it keeps asking for
// samples, so the Player's dead-stream watchdog cannot see it — and only a
// rebuild recovers it. fn runs on its own goroutine, not the loop thread;
// call Dispatch from inside it if it needs the window thread. Bursts of
// notifications are coalesced (see wakeDebounce) so fn runs once per event.
func OnWake(fn func()) {
	wakeMu.Lock()
	wakeFns = append(wakeFns, fn)
	wakeMu.Unlock()
}

// createWakeWindow makes the hidden top-level window that receives
// WM_POWERBROADCAST and WM_DEVICECHANGE. It must be a real top-level window,
// not an HWND_MESSAGE one: broadcast messages are only delivered to
// top-level windows. It is never shown. Runs on the loop thread, before the
// pump starts, so its WndProc is serviced by the same GetMessage loop.
func createWakeWindow() {
	cls, err := win32.RegisterClass("FlowLiteWake", wakeWndProcCB)
	if err != nil {
		// Without the window there are no wake/device notifications, which
		// degrades sound cues after sleep but is not fatal to dictation.
		log.Printf("mainloop: %v; sleep/device wake disabled", err)
		return
	}
	hwnd, err := win32.CreateHiddenWindow(0, cls, wsOverlapped, 0, 0)
	if err != nil {
		log.Printf("mainloop: %v; sleep/device wake disabled", err)
		return
	}
	// WM_DEVICECHANGE's DBT_DEVNODES_CHANGED is broadcast to every top-level
	// window on its own; DBT_DEVICEARRIVAL / DBT_DEVICEREMOVECOMPLETE for a
	// specific interface class only arrive after registering for it. Both
	// audio classes are registered so a device that shows up under only one
	// of them still counts.
	for _, g := range []guid{ksCategoryAudio, devInterfaceAudioRender} {
		filter := devBroadcastDeviceInterface{deviceType: dbtDevTypDeviceInterface, classGUID: g}
		filter.size = uint32(unsafe.Sizeof(filter))
		procRegisterDeviceNotification.Call(hwnd, uintptr(unsafe.Pointer(&filter)), deviceNotifyWindowHandle)
	}
}

// wakeWndProc turns power and device messages into a debounced wake.
func wakeWndProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case wmPowerBroadcast:
		if w == pbtAPMResumeAutomatic || w == pbtAPMResumeSuspend {
			scheduleWake()
		}
		return 1 // TRUE
	case wmDeviceChange:
		if w == dbtDevNodesChanged || w == dbtDeviceArrival || w == dbtDeviceRemoveComplete {
			scheduleWake()
		}
		return 1 // TRUE
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(m), w, l)
	return r
}

// scheduleWake (re)arms the debounce timer: the wake fires wakeDebounce
// after the last message in a burst, and a burst that keeps going keeps
// pushing it back. Cheap enough to call from the WndProc on every message.
//
// Each call takes a new generation and starts a fresh timer rather than
// Reset-ing the old one: a timer whose callback has already started cannot
// be stopped, and its fireWake would otherwise run alongside the rearmed
// one — two wakes, two device reopens. A stale generation simply returns.
func scheduleWake() {
	wakeMu.Lock()
	defer wakeMu.Unlock()
	if wakeTimer != nil {
		wakeTimer.Stop()
	}
	wakeGen++
	gen := wakeGen
	wakeTimer = time.AfterFunc(wakeDebounce, func() { fireWake(gen) })
}

// fireWake runs every registered wake fn, each on its own goroutine, like
// the macOS implementation does for NSWorkspaceDidWakeNotification. It does
// nothing if scheduleWake has run again since this timer was armed.
func fireWake(gen uint64) {
	wakeMu.Lock()
	if gen != wakeGen {
		wakeMu.Unlock()
		return
	}
	wakeTimer = nil
	fns := append([]func(){}, wakeFns...)
	wakeMu.Unlock()
	for _, fn := range fns {
		go fn()
	}
}
