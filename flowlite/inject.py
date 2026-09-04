"""Insert transcribed text into whatever field currently has focus.

Pasting is used rather than synthesising one key event per character: it is
near-instant regardless of length, and it does not mangle non-ASCII text or
trigger autocomplete on every keystroke.
"""

import logging
import sys
import time

from pynput.keyboard import Controller, Key

log = logging.getLogger(__name__)

_keyboard = Controller()

# How long to leave our text on the clipboard before restoring the previous
# contents. The target app reads the clipboard synchronously when it handles
# the paste, but that handling is asynchronous from our point of view.
RESTORE_DELAY = 0.35


# --- clipboard -------------------------------------------------------------

if sys.platform == "darwin":
    from AppKit import NSPasteboard, NSPasteboardTypeString

    def get_clipboard() -> str | None:
        return NSPasteboard.generalPasteboard().stringForType_(NSPasteboardTypeString)

    def set_clipboard(text: str) -> None:
        pb = NSPasteboard.generalPasteboard()
        pb.clearContents()
        pb.setString_forType_(text, NSPasteboardTypeString)

elif sys.platform == "win32":
    import ctypes
    from ctypes import wintypes

    CF_UNICODETEXT = 13
    GMEM_MOVEABLE = 0x0002

    _user32 = ctypes.WinDLL("user32", use_last_error=True)
    _kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)
    _kernel32.GlobalLock.restype = ctypes.c_void_p
    _kernel32.GlobalAlloc.restype = wintypes.HGLOBAL

    def _with_clipboard(fn):
        for _ in range(10):  # another process may hold it open briefly
            if _user32.OpenClipboard(None):
                try:
                    return fn()
                finally:
                    _user32.CloseClipboard()
            time.sleep(0.02)
        raise OSError("Could not open the Windows clipboard")

    def get_clipboard() -> str | None:
        def read():
            handle = _user32.GetClipboardData(CF_UNICODETEXT)
            if not handle:
                return None
            ptr = _kernel32.GlobalLock(ctypes.c_void_p(handle))
            try:
                return ctypes.c_wchar_p(ptr).value
            finally:
                _kernel32.GlobalUnlock(ctypes.c_void_p(handle))

        return _with_clipboard(read)

    def set_clipboard(text: str) -> None:
        def write():
            _user32.EmptyClipboard()
            buf = ctypes.create_unicode_buffer(text)
            size = ctypes.sizeof(buf)
            handle = _kernel32.GlobalAlloc(GMEM_MOVEABLE, size)
            ptr = _kernel32.GlobalLock(handle)
            ctypes.memmove(ptr, buf, size)
            _kernel32.GlobalUnlock(handle)
            _user32.SetClipboardData(CF_UNICODETEXT, handle)

        _with_clipboard(write)

else:
    import subprocess

    def _tool(read: bool) -> list[str] | None:
        import shutil

        if shutil.which("wl-copy") and shutil.which("wl-paste"):
            return ["wl-paste", "-n"] if read else ["wl-copy"]
        if shutil.which("xclip"):
            return ["xclip", "-selection", "clipboard", "-o"] if read else \
                   ["xclip", "-selection", "clipboard"]
        return None

    def get_clipboard() -> str | None:
        cmd = _tool(read=True)
        if not cmd:
            return None
        try:
            return subprocess.run(cmd, capture_output=True, text=True, timeout=2).stdout
        except Exception:
            return None

    def set_clipboard(text: str) -> None:
        cmd = _tool(read=False)
        if not cmd:
            raise OSError("Install xclip or wl-clipboard to paste text")
        subprocess.run(cmd, input=text, text=True, timeout=2)


# --- keystroke -------------------------------------------------------------

PASTE_MODIFIER = Key.cmd if sys.platform == "darwin" else Key.ctrl


def _tap_paste() -> None:
    with _keyboard.pressed(PASTE_MODIFIER):
        _keyboard.press("v")
        _keyboard.release("v")


def paste_text(text: str, restore_clipboard: bool = True) -> None:
    """Put `text` on the clipboard and send the paste shortcut."""
    if not text:
        return

    previous = None
    if restore_clipboard:
        try:
            previous = get_clipboard()
        except Exception as exc:
            log.warning("could not read clipboard: %s", exc)

    set_clipboard(text)
    # Give the OS a moment to register the new clipboard owner before the
    # paste; without this, fast machines occasionally paste the old contents.
    time.sleep(0.05)
    _tap_paste()

    if restore_clipboard and previous is not None:
        time.sleep(RESTORE_DELAY)
        try:
            set_clipboard(previous)
        except Exception as exc:
            log.warning("could not restore clipboard: %s", exc)


def type_text(text: str) -> None:
    """Character-by-character fallback for fields that reject paste."""
    _keyboard.type(text)
