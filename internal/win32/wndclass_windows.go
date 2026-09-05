// Package win32 holds the handful of Win32 declarations shared by more than
// one package, so a struct layout is checked once rather than copied.
package win32

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	pRegisterClassExW = user32.NewProc("RegisterClassExW")
	pCreateWindowExW  = user32.NewProc("CreateWindowExW")
	pGetModuleHandleW = syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW")
	errClassExists    = syscall.Errno(1410) // ERROR_CLASS_ALREADY_EXISTS
)

// wndClassExSize is sizeof(WNDCLASSEXW): four 32-bit fields plus eight
// pointer-sized ones — 80 on amd64, 48 on 386.
const wndClassExSize = 16 + 8*unsafe.Sizeof(uintptr(0))

// Compile-time check that WndClassEx matches the C layout; a mismatch makes
// the constant expression negative and the build fails.
const _ = uint(wndClassExSize - unsafe.Sizeof(WndClassEx{}))
const _ = uint(unsafe.Sizeof(WndClassEx{}) - wndClassExSize)

// WndClassEx mirrors WNDCLASSEXW exactly. cbClsExtra and cbWndExtra are
// `int` (4 bytes) in the Windows headers, not pointer-sized: declaring them
// as uintptr grew the struct to 88 bytes on amd64 and shifted every field
// from hInstance on, which made RegisterClassExW reject cbSize outright.
// It is 80 bytes on amd64 (16 + 8 pointers), 48 on 386; see wndClassExSize.
type WndClassEx struct {
	Size, Style                   uint32
	WndProc                       uintptr
	ClsExtra, WndExtra            int32
	Instance, Icon, Cursor, Brush uintptr
	MenuName, ClassName           *uint16
	IconSm                        uintptr
}

// ModuleHandle is GetModuleHandleW(NULL): this executable's HINSTANCE.
func ModuleHandle() uintptr {
	h, _, _ := pGetModuleHandleW.Call(0)
	return h
}

// RegisterClass registers a window class with the given WndProc callback
// (from syscall.NewCallback) and no icon, cursor, brush or menu. It returns
// the UTF-16 class name to pass to CreateWindowExW. A class that this
// process already registered is not an error, so callers may retry.
func RegisterClass(name string, wndProc uintptr) (*uint16, error) {
	cls, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	wc := WndClassEx{Size: uint32(unsafe.Sizeof(WndClassEx{})), WndProc: wndProc, Instance: ModuleHandle(), ClassName: cls}
	atom, _, e := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 && e != errClassExists {
		return nil, fmt.Errorf("RegisterClassExW(%s): %w", name, e)
	}
	return cls, nil
}

// CreateHiddenWindow is CreateWindowExW for a window that is never shown by
// default: no parent, no menu, no title, positioned at the origin with the
// given size, owned by this module. className comes from RegisterClass.
// exStyle and style are the WS_EX_* and WS_* bits the caller wants.
func CreateHiddenWindow(exStyle uintptr, className *uint16, style uintptr, width, height int) (hwnd uintptr, err error) {
	hwnd, _, e := pCreateWindowExW.Call(exStyle, uintptr(unsafe.Pointer(className)), 0, style,
		0, 0, uintptr(width), uintptr(height), 0, 0, ModuleHandle(), 0)
	if hwnd == 0 {
		return 0, fmt.Errorf("CreateWindowExW: %w", e)
	}
	return hwnd, nil
}
