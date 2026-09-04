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

// A small layered, top-most, non-activating capsule painted with GDI: bars
// while listening, a spinner while transcribing, a check or × when done.
// Written against the Win32 API and cross-compiled from macOS; it has not
// yet been run on Windows.

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
	pSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
	pSetWindowRgn          = user32.NewProc("SetWindowRgn")
	pFillRect              = user32.NewProc("FillRect")
	pGetModuleHandleW      = kernel.NewProc("GetModuleHandleW")

	pCreateSolidBrush   = gdi32.NewProc("CreateSolidBrush")
	pCreateRoundRectRgn = gdi32.NewProc("CreateRoundRectRgn")
	pDeleteObject       = gdi32.NewProc("DeleteObject")
	pSelectObject       = gdi32.NewProc("SelectObject")
	pRoundRect          = gdi32.NewProc("RoundRect")
	pGetStockObject     = gdi32.NewProc("GetStockObject")
	pExtCreatePen       = gdi32.NewProc("ExtCreatePen")
	pMoveToEx           = gdi32.NewProc("MoveToEx")
	pLineTo             = gdi32.NewProc("LineTo")
	pArc                = gdi32.NewProc("Arc")
	pSetArcDirection    = gdi32.NewProc("SetArcDirection")
)

const (
	pillW, pillH = 100, 30
	barCount     = 9
	bottomGap    = 64

	wsPopup          = 0x80000000
	wsExTopmost      = 0x00000008
	wsExToolWindow   = 0x00000080
	wsExLayered      = 0x00080000
	wsExNoActivate   = 0x08000000
	wsExTransparent  = 0x00000020
	swHide           = 0
	swShowNoActivate = 4
	wmPaint          = 0x000F
	wmTimer          = 0x0113
	lwaAlpha         = 0x2
	spiGetWorkArea   = 0x0030
	nullPen          = 8
	nullBrush        = 5
	psGeometric      = 0x00010000
	psEndcapRound    = 0x00000000
	psJoinRound      = 0x00000000
	adCounterClock   = 1
)

type rect struct{ left, top, right, bottom int32 }
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
type logBrush struct {
	style uint32
	color uintptr
	hatch uintptr
}

var (
	mu        sync.Mutex
	hwnd      uintptr
	state     = Hidden
	stateAt   time.Time
	shownAt   time.Time
	hideAt    time.Time
	level     float64
	target    float64
	bars      [barCount]float64
	wndProcCB = syscall.NewCallback(wndProc)
)

func rgb(r, g, b byte) uintptr { return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16 }
func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }
func easeOut(t float64) float64 { t = clamp01(t); return 1 - math.Pow(1-t, 3) }
func easeInOut(t float64) float64 {
	t = clamp01(t)
	if t < 0.5 {
		return 4 * t * t * t
	}
	return 1 - math.Pow(-2*t+2, 3)/2
}

// blend mixes two COLORREFs: a*fg + (1-a)*bg.
func blend(fg, bg uintptr, a float64) uintptr {
	ch := func(shift uint) uintptr {
		f := float64((fg >> shift) & 0xff)
		b := float64((bg >> shift) & 0xff)
		return uintptr(a*f+(1-a)*b) & 0xff
	}
	return ch(0) | ch(8)<<8 | ch(16)<<16
}

func pen(color uintptr, width int) uintptr {
	lb := logBrush{style: 0, color: color}
	p, _, _ := pExtCreatePen.Call(psGeometric|psEndcapRound|psJoinRound, uintptr(width), uintptr(unsafe.Pointer(&lb)), 0, 0)
	return p
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
	alpha := easeOut(now.Sub(shownAt).Seconds() / 0.16)
	mu.Lock()
	hiding := !hideAt.IsZero()
	var f float64
	if hiding {
		f = easeInOut(now.Sub(hideAt).Seconds() / 0.24)
		alpha *= 1 - f
	}
	mu.Unlock()
	if hiding && f >= 1 {
		pKillTimer.Call(hwnd, 1)
		pShowWindow.Call(hwnd, swHide)
		mu.Lock()
		state = Hidden
		hideAt = time.Time{}
		mu.Unlock()
		return
	}
	pSetLayeredWindowAttrs.Call(hwnd, 0, uintptr(240*alpha), lwaAlpha)
	pInvalidateRect.Call(hwnd, 0, 0)
}

func paint(hdc uintptr) {
	mu.Lock()
	st, sAt, lv, tg := state, stateAt, level, target
	mu.Unlock()
	now := time.Now()
	t := float64(now.UnixNano()) / 1e9
	stT := now.Sub(sAt).Seconds()

	bgc := rgb(23, 23, 28)
	full := rect{0, 0, pillW, pillH}
	bg, _, _ := pCreateSolidBrush.Call(bgc)
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&full)), bg)
	pDeleteObject.Call(bg)

	var barsA, spinA, markA float64
	switch st {
	case Listening:
		barsA = easeOut(stT / 0.18)
	case Transcribing:
		barsA = 1 - easeInOut(stT/0.22)
		spinA = easeOut((stT - 0.08) / 0.22)
	case Pasted, Cancelled, Error:
		spinA = 1 - easeInOut(stT/0.15)
		markA = easeOut((stT - 0.05) / 0.30)
	}

	cx, cy := float64(pillW)/2, float64(pillH)/2

	// waveform
	lv += (tg - lv) * 0.5
	center := float64(barCount-1) / 2
	mu.Lock()
	level = lv
	for i := 0; i < barCount; i++ {
		env := 1.0 - 0.55*math.Pow(math.Abs(float64(i)-center)/center, 1.6)
		wob := 0.72 + 0.28*math.Sin(t*(6.5+float64(i)*0.9)+float64(i)*1.7)
		desired := clamp01(lv*1.9) * env * wob
		bars[i] += (desired - bars[i]) * 0.35
	}
	bh := bars
	mu.Unlock()
	if barsA > 0.001 {
		const barW, gap = 3.0, 3.0
		span := barCount*(barW+gap) - gap
		x := cx - span/2
		maxH := float64(pillH - 12)
		br, _, _ := pCreateSolidBrush.Call(blend(rgb(255, 255, 255), bgc, 0.92*barsA))
		o, _, _ := pSelectObject.Call(hdc, br)
		np, _, _ := pGetStockObject.Call(nullPen)
		op, _, _ := pSelectObject.Call(hdc, np)
		for i := 0; i < barCount; i++ {
			h := (3.0 + bh[i]*(maxH-3.0)) * (0.15 + 0.85*barsA)
			pRoundRect.Call(hdc, uintptr(int32(x)), uintptr(int32(cy-h/2)), uintptr(int32(x+barW+0.5)), uintptr(int32(cy+h/2+0.5)), 3, 3)
			x += barW + gap
		}
		pSelectObject.Call(hdc, op)
		pSelectObject.Call(hdc, o)
		pDeleteObject.Call(br)
	}

	// spinner: 270° arc rotating at 1.1 rev/s
	if spinA > 0.001 {
		r := 7.5 * (0.7 + 0.3*spinA)
		start := math.Mod(-t*396.0, 360.0) * math.Pi / 180
		end := start - 270*math.Pi/180
		p := pen(blend(rgb(120, 170, 255), bgc, spinA), 2)
		o, _, _ := pSelectObject.Call(hdc, p)
		nb, _, _ := pGetStockObject.Call(nullBrush)
		ob, _, _ := pSelectObject.Call(hdc, nb)
		pSetArcDirection.Call(hdc, adCounterClock)
		pArc.Call(hdc,
			uintptr(int32(cx-r)), uintptr(int32(cy-r)), uintptr(int32(cx+r)), uintptr(int32(cy+r)),
			uintptr(int32(cx+r*math.Cos(end))), uintptr(int32(cy-r*math.Sin(end))),
			uintptr(int32(cx+r*math.Cos(start))), uintptr(int32(cy-r*math.Sin(start))))
		pSelectObject.Call(hdc, ob)
		pSelectObject.Call(hdc, o)
		pDeleteObject.Call(p)
	}

	// check / cross
	if markA > 0.001 {
		var color uintptr
		switch st {
		case Pasted:
			color = rgb(62, 207, 142)
		case Error:
			color = rgb(255, 92, 92)
		default:
			color = rgb(158, 158, 158)
		}
		p := pen(blend(color, bgc, clamp01(markA*2)), 2)
		o, _, _ := pSelectObject.Call(hdc, p)
		line := func(x0, y0, x1, y1 float64) {
			pMoveToEx.Call(hdc, uintptr(int32(x0)), uintptr(int32(y0)), 0)
			pLineTo.Call(hdc, uintptr(int32(x1)), uintptr(int32(y1)))
		}
		if st == Pasted {
			// GDI y grows downward, so the check is flipped relative to Cocoa.
			ax, ay := cx-6.5, cy-0.5
			mx, my := cx-2.0, cy+4.0
			zx, zy := cx+6.5, cy-5.0
			s1 := clamp01(markA / 0.4)
			s2 := clamp01((markA - 0.4) / 0.6)
			line(ax, ay, ax+(mx-ax)*s1, ay+(my-ay)*s1)
			if s2 > 0 {
				line(mx, my, mx+(zx-mx)*s2, my+(zy-my)*s2)
			}
		} else {
			k := 5.0 * markA
			line(cx-k, cy-k, cx+k, cy+k)
			line(cx-k, cy+k, cx+k, cy-k)
		}
		pSelectObject.Call(hdc, o)
		pDeleteObject.Call(p)
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
		uintptr(unsafe.Pointer(cls)), 0, wsPopup, 0, 0, pillW, pillH, 0, 0, inst, 0)
	rgn, _, _ := pCreateRoundRectRgn.Call(0, 0, pillW+1, pillH+1, pillH, pillH)
	pSetWindowRgn.Call(hwnd, rgn, 1)
	pSetLayeredWindowAttrs.Call(hwnd, 0, 0, lwaAlpha)
}

func reposition() {
	var work rect
	pSystemParametersInfoW.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&work)), 0)
	x := (work.left+work.right)/2 - pillW/2
	y := work.bottom - bottomGap - pillH
	pSetWindowPos.Call(hwnd, ^uintptr(0) /* HWND_TOPMOST */, uintptr(int32(x)), uintptr(int32(y)), pillW, pillH, 0x0010 /* SWP_NOACTIVATE */)
}

// Show makes the pill visible in the given state (text is ignored).
func Show(s State, _ string) {
	mainloop.Dispatch(func() {
		ensureWindow()
		mu.Lock()
		fresh := state == Hidden || !hideAt.IsZero()
		state, stateAt = s, time.Now()
		hideAt = time.Time{}
		if fresh {
			shownAt = time.Now()
			level, target = 0, 0
			for i := range bars {
				bars[i] = 0
			}
		}
		mu.Unlock()
		if fresh {
			reposition()
			pSetLayeredWindowAttrs.Call(hwnd, 0, 0, lwaAlpha)
			pShowWindow.Call(hwnd, swShowNoActivate)
		}
		pSetTimer.Call(hwnd, 1, 16, 0)
		pInvalidateRect.Call(hwnd, 0, 0)
	})
}

// SetState changes appearance without repositioning.
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
		hideAt = time.Time{}
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

// Hide fades the pill out.
func Hide() {
	mainloop.Dispatch(func() {
		mu.Lock()
		if hwnd != 0 && state != Hidden && hideAt.IsZero() {
			hideAt = time.Now()
		}
		mu.Unlock()
	})
}

// Snapshot is not implemented on Windows.
func Snapshot(path string) error { return nil }
