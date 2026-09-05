//go:build windows

package overlay

import (
	"math"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/sanke08/flowlite/internal/mainloop"
)

// A small layered, top-most, non-activating black capsule painted with GDI.
// Same three looks as the macOS pill, and the same narrow exception to its
// no-text rule: a waveform while listening, short stubs with a sweeping
// shimmer while transcribing, red pulses on failure with a short status
// label (since the daemon runs detached with no terminal to explain a
// failure any other way); success and cancel just fade out, wordlessly —
// there is nothing to explain when there was simply nothing to transcribe.
// It sits centred on the chosen screen edge, edgeGap px in from the physical
// edge (upright on the left/right edges). Written against the Win32 API and
// cross-compiled from macOS; it has not yet been run on Windows.

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")
	kernel = syscall.NewLazyDLL("kernel32.dll")

	pRegisterClassExW      = user32.NewProc("RegisterClassExW")
	pCreateWindowExW       = user32.NewProc("CreateWindowExW")
	pDefWindowProcW        = user32.NewProc("DefWindowProcW")
	pShowWindow            = user32.NewProc("ShowWindow")
	pSetWindowPos          = user32.NewProc("SetWindowPos")
	pInvalidateRect        = user32.NewProc("InvalidateRect")
	pBeginPaint            = user32.NewProc("BeginPaint")
	pEndPaint              = user32.NewProc("EndPaint")
	pSetTimer              = user32.NewProc("SetTimer")
	pKillTimer             = user32.NewProc("KillTimer")
	pSetLayeredWindowAttrs = user32.NewProc("SetLayeredWindowAttributes")
	pGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	pSetWindowRgn          = user32.NewProc("SetWindowRgn")
	pFillRect              = user32.NewProc("FillRect")
	pDrawTextW             = user32.NewProc("DrawTextW")
	pGetModuleHandleW      = kernel.NewProc("GetModuleHandleW")

	pCreateSolidBrush      = gdi32.NewProc("CreateSolidBrush")
	pCreateRoundRectRgn    = gdi32.NewProc("CreateRoundRectRgn")
	pDeleteObject          = gdi32.NewProc("DeleteObject")
	pSelectObject          = gdi32.NewProc("SelectObject")
	pRoundRect             = gdi32.NewProc("RoundRect")
	pGetStockObject        = gdi32.NewProc("GetStockObject")
	pCreateFontW           = gdi32.NewProc("CreateFontW")
	pSetTextColor          = gdi32.NewProc("SetTextColor")
	pSetBkMode             = gdi32.NewProc("SetBkMode")
	pCreateCompatibleDC    = gdi32.NewProc("CreateCompatibleDC")
	pDeleteDC              = gdi32.NewProc("DeleteDC")
	pGetTextExtentPoint32W = gdi32.NewProc("GetTextExtentPoint32W")
)

const (
	pillLong, pillShort = 100, 30
	barCount            = 9
	edgeGap             = 20 // matches the macOS pill's EDGE_GAP
	fadeIn              = 0.16
	fadeOut             = 0.20
	errorHold           = 0.70 // two red pulses, then fade

	// The plain pill's show/hide slide offset is driven by a small, lightly
	// underdamped spring (not an eased curve) so arriving/departing reads as
	// a soft pop rather than a flat slide — matches overlay_darwin.m's
	// SLIDE_STIFFNESS/SLIDE_DAMPING (k=600/c=21): ~1.1pt overshoot over the
	// 6pt travel distance below, settling in ~0.3s. Alpha's fade stays a
	// plain ease (see tickWindow()) — opacity doesn't read as "springy,"
	// position does.
	slideStiffness = 600.0
	slideDamping   = 21.0

	wsPopup          = 0x80000000
	wsExTopmost      = 0x00000008
	wsExToolWindow   = 0x00000080
	wsExLayered      = 0x00080000
	wsExNoActivate   = 0x08000000
	wsExTransparent  = 0x00000020
	swHide           = 0
	swShowNoActivate = 4
	swpNoActivate    = 0x0010
	wmPaint          = 0x000F
	wmTimer          = 0x0113
	lwaAlpha         = 0x2
	smCxScreen       = 0
	smCyScreen       = 1
	nullPen          = 8

	// Font/text-drawing constants for the status label. It only ever
	// appears for the terminal Error state, and only grows the pill's own
	// footprint enough to hold a short (1-3 word) status without
	// wrapping — it is not a place for real text.
	fwNormal          = 400
	defaultCharset    = 1
	outDefaultPrecis  = 0
	clipDefaultPrecis = 0
	defaultQuality    = 0
	defaultPitch      = 0
	ffSwiss           = 0x20
	transparentBk     = 1

	dtCenter      = 0x00000001
	dtVCenter     = 0x00000004
	dtSingleLine  = 0x00000020
	dtNoPrefix    = 0x00000800
	dtEndEllipsis = 0x00008000

	labelFontPx  = -15   // ~11pt at 96 DPI; negative selects char height over cell height
	labelSidePad = 12.0  // horizontal padding around the label text
	labelThick   = 16    // extra room made below the bars, pill lying flat
	labelMaxText = 220.0 // widest the label's own text may lay out before truncating
)

type rect struct{ left, top, right, bottom int32 }
type gdiSize struct{ cx, cy int32 }
type paintStruct struct {
	hdc         uintptr
	erase       int32
	rcPaint     rect
	restore     int32
	incUpdate   int32
	rgbReserved [32]byte
}
type wndClassEx struct {
	size, style                   uint32
	wndProc, clsExtra, wndExtra   uintptr
	instance, icon, cursor, brush uintptr
	menuName, className           *uint16
	iconSm                        uintptr
}

var (
	mu         sync.Mutex
	hwnd       uintptr
	state      = Hidden
	stateAt    time.Time
	text       string // status text, shown only for Error
	shownAt    time.Time
	hideAt     time.Time // non-zero while fading out (may be in the future)
	slidePos   float64   // spring position of the pop in/out slide offset
	slideVel   float64   // spring velocity for slidePos
	slideLastT time.Time // previous tickWindow() time, for this spring's own dt
	lastTick   time.Time
	level      float64
	target     float64
	bars       [barCount]float64
	collapse   float64 // 0 = live waveform, 1 = equal stubs
	red        float64 // 0 = white, 1 = failure red
	shimmer    float64 // 0 = flat, 1 = sweeping band
	posCode    int     // 0 bottom, 1 top, 2 left, 3 right
	baseX      int32
	baseY      int32
	winW       int32
	winH       int32
	inX, inY   float64 // unit vector pointing away from the screen edge
	wndProcCB  = syscall.NewCallback(wndProc)

	labelFontOnce sync.Once
	labelFontH    uintptr
)

func rgb(r, g, b byte) uintptr    { return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16 }
func clamp01(v float64) float64   { return math.Max(0, math.Min(1, v)) }
func mix(a, b, k float64) float64 { return a + (b-a)*k }
func easeOut(t float64) float64   { t = clamp01(t); return 1 - math.Pow(1-t, 3) }
func easeInOut(t float64) float64 {
	t = clamp01(t)
	if t < 0.5 {
		return 4 * t * t * t
	}
	return 1 - math.Pow(-2*t+2, 3)/2
}
func approach(v *float64, want, dt, tau float64) { *v += (want - *v) * (1 - math.Exp(-dt/tau)) }

// blend mixes two COLORREFs: a*fg + (1-a)*bg.
func blend(fg, bg uintptr, a float64) uintptr {
	ch := func(shift uint) uintptr {
		f := float64((fg >> shift) & 0xff)
		b := float64((bg >> shift) & 0xff)
		return uintptr(a*f+(1-a)*b) & 0xff
	}
	return ch(0) | ch(8)<<8 | ch(16)<<16
}

func vertical() bool        { return posCode == 2 || posCode == 3 }
func terminal(s State) bool { return s == Pasted || s == Cancelled }

func applyPosition(code int) {
	mu.Lock()
	posCode = code
	mu.Unlock()
}

func wndProc(h uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case wmTimer:
		tickWindow()
		return 0
	case wmPaint:
		var ps paintStruct
		hdc, _, _ := pBeginPaint.Call(h, uintptr(unsafe.Pointer(&ps)))
		paint(hdc)
		pEndPaint.Call(h, uintptr(unsafe.Pointer(&ps)))
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(h, uintptr(m), w, l)
	return r
}

func tickWindow() {
	now := time.Now()
	mu.Lock()
	if state == Error && hideAt.IsZero() && now.Sub(stateAt).Seconds() >= errorHold {
		hideAt = now
	}
	alpha := easeOut(now.Sub(shownAt).Seconds() / fadeIn)
	slideTarget := 0.0 // resting position, onscreen
	hiding := !hideAt.IsZero()
	var f float64
	if hiding {
		f = easeInOut(now.Sub(hideAt).Seconds() / fadeOut)
		alpha *= 1 - f
		slideTarget = -4.0 * f // drifts back toward the edge
	}

	// A small underdamped spring carries the slide offset toward
	// slideTarget instead of an eased curve, so the pill overshoots its
	// resting position by a point or two before settling — a soft pop
	// rather than a flat slide. See slideStiffness/slideDamping above.
	dt := 1.0 / 60.0
	if !slideLastT.IsZero() {
		dt = math.Min(now.Sub(slideLastT).Seconds(), 0.1)
	}
	slideLastT = now
	accel := -slideStiffness*(slidePos-slideTarget) - slideDamping*slideVel
	slideVel += accel * dt
	slidePos += slideVel * dt

	x := baseX + int32(math.Round(inX*slidePos))
	y := baseY + int32(math.Round(inY*slidePos))
	w, h := winW, winH
	mu.Unlock()
	if hiding && f >= 1 {
		pKillTimer.Call(hwnd, 1)
		pShowWindow.Call(hwnd, swHide)
		mu.Lock()
		state = Hidden
		text = ""
		hideAt = time.Time{}
		mu.Unlock()
		return
	}
	pSetLayeredWindowAttrs.Call(hwnd, 0, uintptr(240*alpha), lwaAlpha)
	pSetWindowPos.Call(hwnd, ^uintptr(0) /* HWND_TOPMOST */, uintptr(x), uintptr(y), uintptr(w), uintptr(h), swpNoActivate)
	pInvalidateRect.Call(hwnd, 0, 0)
}

func paint(hdc uintptr) {
	now := time.Now()
	t := float64(now.UnixNano()) / 1e9

	mu.Lock()
	dt := 1.0 / 60
	if !lastTick.IsZero() {
		dt = math.Min(now.Sub(lastTick).Seconds(), 0.1)
	}
	lastTick = now
	st := now.Sub(stateAt).Seconds()
	appear := easeOut(now.Sub(shownAt).Seconds() / 0.18)
	vert := vertical()
	w, h := float64(winW), float64(winH)
	lbl := ""
	if showsLabel() {
		lbl = text
	}

	// Shape blends.
	wantCollapse, wantRed, wantShimmer := 1.0, 0.0, 0.0
	if state == Listening {
		wantCollapse = 0
	}
	if state == Error {
		wantRed = 1
	}
	if state == Transcribing {
		wantShimmer = 1
	}
	approach(&collapse, wantCollapse, dt, 0.10)
	approach(&red, wantRed, dt, 0.06)
	approach(&shimmer, wantShimmer, dt, 0.12)

	// Live waveform model (always runs so hand-offs are seamless).
	level += (target - level) * 0.5
	center := float64(barCount-1) / 2
	for i := 0; i < barCount; i++ {
		env := 1.0 - 0.55*math.Pow(math.Abs(float64(i)-center)/center, 1.6)
		wob := 0.72 + 0.28*math.Sin(t*(6.5+float64(i)*0.9)+float64(i)*1.7)
		desired := clamp01(level*1.9) * env * wob
		bars[i] += (desired - bars[i]) * 0.35
	}
	bh, col, rd, sh := bars, collapse, red, shimmer
	mu.Unlock()

	// Body: black; the layered-window alpha supplies the 0.94 translucency.
	bgc := rgb(0, 0, 0)
	full := rect{0, 0, int32(w), int32(h)}
	bg, _, _ := pCreateSolidBrush.Call(bgc)
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&full)), bg)
	pDeleteObject.Call(bg)
	if appear <= 0.001 {
		return
	}

	// Shimmer sweep 0..1 along the bars at ~0.7 Hz; failure pulse bright at 0, .35, .70.
	sweep := 0.5 - 0.5*math.Cos(2*math.Pi*st/1.4)
	pulse := 0.45 + 0.55*(0.5+0.5*math.Cos(2*math.Pi*st/(errorHold/2)))

	// The label (only drawn for Error) claims a strip of the
	// window — below the bars when the pill lies flat, beside them when it
	// stands upright — so the bars themselves keep their original size and
	// centring regardless of whether the window grew to fit the text.
	var labelRect rect
	coreW, coreH := w, h
	if lbl != "" {
		if vert {
			coreW = pillShort
			labelRect = rect{int32(pillShort), 0, int32(w), int32(h)}
		} else {
			coreH = pillShort
			labelRect = rect{0, int32(pillShort), int32(w), int32(h)}
		}
	}

	const barW, gap = 3.0, 3.0
	maxL := float64(pillShort - 12)
	span := barCount*(barW+gap) - gap
	cx, cy := coreW/2, coreH/2
	p := cx - span/2
	if vert {
		p = cy - span/2
	}
	np, _, _ := pGetStockObject.Call(nullPen)
	op, _, _ := pSelectObject.Call(hdc, np)
	for i := 0; i < barCount; i++ {
		u := float64(i) / float64(barCount-1)
		glow := math.Exp(-math.Pow((u-sweep)/0.16, 2)) * sh

		waveL := 3.0 + bh[i]*(maxL-3.0)
		stubL := 5.0 + 2.0*glow + 3.0*pulse*rd
		ln := mix(waveL, stubL, col) * (0.15 + 0.85*appear)

		a := mix(0.92, 0.30+0.62*glow, sh)
		a = mix(a, pulse, rd)
		c := rgb(255, byte(255*mix(1, 0.36, rd)), byte(255*mix(1, 0.36, rd)))
		br, _, _ := pCreateSolidBrush.Call(blend(c, bgc, a*appear))
		o, _, _ := pSelectObject.Call(hdc, br)
		var r rect
		if vert {
			r = rect{int32(cx - ln/2), int32(p), int32(cx + ln/2 + 0.5), int32(p + barW + 0.5)}
		} else {
			r = rect{int32(p), int32(cy - ln/2), int32(p + barW + 0.5), int32(cy + ln/2 + 0.5)}
		}
		pRoundRect.Call(hdc, uintptr(r.left), uintptr(r.top), uintptr(r.right), uintptr(r.bottom), 3, 3)
		pSelectObject.Call(hdc, o)
		pDeleteObject.Call(br)
		p += barW + gap
	}
	pSelectObject.Call(hdc, op)

	// ---- status label (Error only) -------------------------------------
	if lbl != "" {
		font := ensureLabelFont()
		oldFont, _, _ := pSelectObject.Call(hdc, font)
		pSetBkMode.Call(hdc, transparentBk)
		pSetTextColor.Call(hdc, blend(rgb(235, 235, 235), bgc, appear))
		u, _ := syscall.UTF16FromString(lbl)
		r := labelRect
		flags := uintptr(dtCenter | dtVCenter | dtSingleLine | dtEndEllipsis | dtNoPrefix)
		pDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&r)), flags)
		pSelectObject.Call(hdc, oldFont)
	}
}

func ensureWindow() {
	if hwnd != 0 {
		return
	}
	inst, _, _ := pGetModuleHandleW.Call(0)
	cls, _ := syscall.UTF16PtrFromString("FlowLitePill")
	wc := wndClassEx{size: uint32(unsafe.Sizeof(wndClassEx{})), wndProc: wndProcCB, instance: inst, className: cls}
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	hwnd, _, _ = pCreateWindowExW.Call(
		wsExLayered|wsExTopmost|wsExToolWindow|wsExNoActivate|wsExTransparent,
		uintptr(unsafe.Pointer(cls)), 0, wsPopup, 0, 0, pillLong, pillShort, 0, 0, inst, 0)
	pSetLayeredWindowAttrs.Call(hwnd, 0, 0, lwaAlpha)
}

// labelFor returns text unchanged if state s is allowed to show it — only
// the terminal Error state ever draws words — else "".
func labelFor(s State, text string) string {
	if text == "" || s != Error {
		return ""
	}
	return text
}

// showsLabel reports whether the pill should currently reserve room for
// and draw a status label. Must be called with mu held.
func showsLabel() bool {
	return text != "" && state == Error
}

// ensureLabelFont creates the small font the status label is drawn in, once,
// and reuses it for the process lifetime.
func ensureLabelFont() uintptr {
	labelFontOnce.Do(func() {
		name, _ := syscall.UTF16PtrFromString("Segoe UI")
		height := int32(labelFontPx) // a variable, not a constant: avoids a
		// compile-time "constant overflows uintptr" error on the negative
		// value below, which is fine as a runtime two's-complement wrap.
		labelFontH, _, _ = pCreateFontW.Call(
			uintptr(height), 0, 0, 0,
			uintptr(fwNormal),
			0, 0, 0,
			uintptr(defaultCharset),
			uintptr(outDefaultPrecis),
			uintptr(clipDefaultPrecis),
			uintptr(defaultQuality),
			uintptr(defaultPitch|ffSwiss),
			uintptr(unsafe.Pointer(name)),
		)
	})
	return labelFontH
}

// measureLabel returns the pixel width/height s takes in the label's font,
// via an off-screen DC — used only to size the pill; DrawTextW lays out the
// actual draw against the real window DC.
func measureLabel(s string) (w, h int32) {
	if s == "" {
		return 0, 0
	}
	dc, _, _ := pCreateCompatibleDC.Call(0)
	if dc == 0 {
		return 0, 0
	}
	defer pDeleteDC.Call(dc)
	old, _, _ := pSelectObject.Call(dc, ensureLabelFont())
	defer pSelectObject.Call(dc, old)
	u, _ := syscall.UTF16FromString(s)
	var sz gdiSize
	pGetTextExtentPoint32W.Call(dc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&sz)))
	return sz.cx, sz.cy
}

// pillDims returns the window's width/height for the current state and
// text, growing modestly to fit a short status label (Error only) on one
// line — capped so a long sentence truncates instead of growing the pill
// without bound. Must be called with mu held.
func pillDims() (w, h int32) {
	baseW, baseH := int32(pillLong), int32(pillShort)
	if vertical() {
		baseW, baseH = int32(pillShort), int32(pillLong)
	}
	if !showsLabel() {
		return baseW, baseH
	}
	tw, _ := measureLabel(text)
	want := tw + int32(2*labelSidePad)
	if capped := int32(2*labelSidePad + labelMaxText); want > capped {
		want = capped
	}
	if want > baseW {
		baseW = want
	}
	if !vertical() {
		baseH += labelThick
	}
	return baseW, baseH
}

// reposition centres the pill on the chosen edge of the primary screen and
// sizes it for that orientation, edgeGap px in from the physical edge on
// every side — on the bottom that is what lets the pill ride over the
// taskbar.
// Must be called with mu held.
func reposition() {
	sw, _, _ := pGetSystemMetrics.Call(smCxScreen)
	sh, _, _ := pGetSystemMetrics.Call(smCyScreen)
	scrW, scrH := int32(sw), int32(sh)
	winW, winH = pillDims()
	gap := int32(edgeGap)
	switch posCode {
	case 1: // top
		baseX, baseY = scrW/2-winW/2, gap
		inX, inY = 0, 1
	case 2: // left
		baseX, baseY = gap, scrH/2-winH/2
		inX, inY = 1, 0
	case 3: // right
		baseX, baseY = scrW-gap-winW, scrH/2-winH/2
		inX, inY = -1, 0
	default: // bottom
		baseX, baseY = scrW/2-winW/2, scrH-gap-winH
		inX, inY = 0, -1
	}
	pSetWindowPos.Call(hwnd, ^uintptr(0) /* HWND_TOPMOST */, uintptr(baseX), uintptr(baseY), uintptr(winW), uintptr(winH), swpNoActivate)
	rgn, _, _ := pCreateRoundRectRgn.Call(0, 0, uintptr(winW+1), uintptr(winH+1), pillShort, pillShort)
	pSetWindowRgn.Call(hwnd, rgn, 1)
}

// Show makes the pill visible in the given state. textArg is only ever drawn
// for the terminal Error state — every other state shows no words.
func Show(s State, textArg string) {
	mainloop.Dispatch(func() {
		ensureWindow()
		mu.Lock()
		fresh := state == Hidden || !hideAt.IsZero()
		if fresh && terminal(s) {
			// Nothing to show: success/cancel have no look of their own.
			mu.Unlock()
			return
		}
		state, stateAt = s, time.Now()
		text = labelFor(s, textArg)
		hideAt = time.Time{}
		if fresh {
			shownAt = time.Now()
			lastTick = time.Time{}
			level, target = 0, 0
			collapse, red, shimmer = 1, 0, 0
			if s == Listening {
				collapse = 0
			}
			for i := range bars {
				bars[i] = 0
			}
			// Seed the slide spring fresh so a rapid re-show never carries
			// stale velocity into a new appearance: starts off the edge, at
			// rest, same as the old flat ease's initial position.
			slidePos = -6.0
			slideVel = 0
			slideLastT = time.Time{}
		}
		// Windows always sizes off the primary screen (no per-monitor
		// mouse-follow like the macOS pill), so re-running this whenever the
		// label may have changed size is cheap and never "jumps" displays.
		reposition()
		mu.Unlock()
		if fresh {
			pSetLayeredWindowAttrs.Call(hwnd, 0, 0, lwaAlpha)
			pShowWindow.Call(hwnd, swShowNoActivate)
		}
		pSetTimer.Call(hwnd, 1, 16, 0)
		pInvalidateRect.Call(hwnd, 0, 0)
	})
}

// SetState changes appearance without moving screens. Pasted and Cancelled
// start the fade-out immediately; Error pulses red twice and then fades. t is
// only ever drawn for the terminal Error state.
func SetState(s State, t string) {
	mainloop.Dispatch(func() {
		mu.Lock()
		hidden := hwnd == 0 || state == Hidden
		mu.Unlock()
		if hidden {
			Show(s, t)
			return
		}
		mu.Lock()
		state, stateAt = s, time.Now()
		text = labelFor(s, t)
		hideAt = time.Time{}
		if terminal(s) {
			hideAt = stateAt
		}
		reposition()
		mu.Unlock()
		pSetTimer.Call(hwnd, 1, 16, 0)
		pInvalidateRect.Call(hwnd, 0, 0)
	})
}

// SetLevel pushes a 0–1 level into the waveform.
func SetLevel(l float64) {
	mu.Lock()
	target = clamp01(l)
	mu.Unlock()
}

// Hide fades the pill out; a failure finishes its two pulses first.
func Hide() {
	mainloop.Dispatch(func() {
		mu.Lock()
		if hwnd != 0 && state != Hidden && hideAt.IsZero() {
			hideAt = time.Now()
			if state == Error {
				if end := stateAt.Add(time.Duration(errorHold * float64(time.Second))); end.After(hideAt) {
					hideAt = end
				}
			}
		}
		mu.Unlock()
	})
}

// Snapshot is not implemented on Windows.
func Snapshot(path string) error { return nil }

// The history panel is not implemented on Windows: hotkey.ModifierHeld
// already stubs false on non-darwin, so the ShowHistory gesture never fires
// here, but overlay.go's shared API still needs these to satisfy the build.
func showHistory(entries []HistoryEntry, onPick func(int), onClose func()) {}
func hideHistory()                                                         {}
func isHistoryOpen() bool                                                  { return false }
