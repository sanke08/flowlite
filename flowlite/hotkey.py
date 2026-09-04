"""Global hotkey listener with combined tap-to-toggle and hold-to-talk.

A single key drives both gestures. Recording always begins the instant the key
goes down, so no speech is lost while we work out which gesture this is:

  * released quickly  -> treat as a tap; keep recording until the next tap
  * held past the threshold -> treat as push-to-talk; stop on release

Callbacks fire on the listener thread, so they must return promptly.
"""

import logging
import time
from collections.abc import Callable
from enum import Enum, auto

from pynput import keyboard

log = logging.getLogger(__name__)


class Gesture(Enum):
    IDLE = auto()
    UNDECIDED = auto()   # key is down, too early to tell
    TOGGLE = auto()      # tapped; recording until the next tap


def parse_key(name: str):
    """Turn a settings string such as 'alt_r' or 'f13' into a pynput key."""
    key = getattr(keyboard.Key, name, None)
    if key is not None:
        return key
    if len(name) == 1:
        return keyboard.KeyCode.from_char(name)
    raise ValueError(f"Unrecognised hotkey: {name!r}")


def key_label(name: str) -> str:
    import sys

    pretty = {
        "alt_r": "Right Option" if sys.platform == "darwin" else "Right Alt",
        "alt_l": "Left Option" if sys.platform == "darwin" else "Left Alt",
        "ctrl_r": "Right Control",
        "ctrl_l": "Left Control",
        "cmd_r": "Right Command",
        "shift_r": "Right Shift",
    }
    return pretty.get(name, name.replace("_", " ").title())


class HotkeyListener:
    def __init__(
        self,
        hotkey: str,
        hold_threshold: float,
        on_start: Callable[[], None],
        on_finish: Callable[[], None],
        on_cancel: Callable[[], None],
    ):
        self.hold_threshold = hold_threshold
        self.on_start = on_start
        self.on_finish = on_finish
        self.on_cancel = on_cancel

        self._target = parse_key(hotkey)
        self._state = Gesture.IDLE
        self._key_down = False
        self._pressed_at = 0.0
        self._listener: keyboard.Listener | None = None

    # -- lifecycle ----------------------------------------------------------

    def start(self) -> None:
        if self._listener is not None:
            return
        self._listener = keyboard.Listener(
            on_press=self._on_press, on_release=self._on_release
        )
        self._listener.start()

    def stop(self) -> None:
        listener, self._listener = self._listener, None
        if listener is not None:
            listener.stop()
        self._state = Gesture.IDLE
        self._key_down = False

    @property
    def running(self) -> bool:
        return self._listener is not None and self._listener.running

    def reset(self) -> None:
        """Return to idle without firing callbacks (after an error)."""
        self._state = Gesture.IDLE
        self._key_down = False

    # -- event handling -----------------------------------------------------

    def _matches(self, key) -> bool:
        return key == self._target

    def _safe(self, fn) -> None:
        try:
            fn()
        except Exception:
            log.exception("hotkey callback failed")
            self.reset()

    def _on_press(self, key):
        if key == keyboard.Key.esc and self._state is not Gesture.IDLE:
            self._state = Gesture.IDLE
            self._key_down = False
            self._safe(self.on_cancel)
            return

        if not self._matches(key):
            return

        if self._key_down:
            return  # auto-repeat while held

        self._key_down = True

        if self._state is Gesture.TOGGLE:
            # Second tap of a toggle session: finish it.
            self._state = Gesture.IDLE
            self._safe(self.on_finish)
            return

        self._state = Gesture.UNDECIDED
        self._pressed_at = time.monotonic()
        self._safe(self.on_start)

    def _on_release(self, key):
        if not self._matches(key) or not self._key_down:
            return
        self._key_down = False

        if self._state is not Gesture.UNDECIDED:
            return  # this release ended a toggle session's second tap

        held = time.monotonic() - self._pressed_at
        if held >= self.hold_threshold:
            self._state = Gesture.IDLE
            self._safe(self.on_finish)
        else:
            self._state = Gesture.TOGGLE  # keep recording until the next tap
