"""Wires the hotkey, recorder, transcriber and text injector together."""

import logging
import threading
import time
from enum import Enum

from PySide6.QtCore import QObject, QTimer, Signal

from . import backends, models
from .audio import Recorder
from .backends.base import Backend
from .config import Settings
from .hotkey import HotkeyListener
from .inject import paste_text

log = logging.getLogger(__name__)


class State(str, Enum):
    IDLE = "idle"
    RECORDING = "recording"
    TRANSCRIBING = "transcribing"
    ERROR = "error"


class Controller(QObject):
    stateChanged = Signal(State)
    levelChanged = Signal(float)
    durationChanged = Signal(float)
    transcribed = Signal(str)      # final text, after it has been pasted
    failed = Signal(str)

    def __init__(self, settings: Settings, parent=None):
        super().__init__(parent)
        self.settings = settings
        self.state = State.IDLE

        self.recorder = Recorder(
            device=settings.input_device, max_seconds=settings.max_seconds
        )
        self.backend_cls = backends.pick()
        self.engine: Backend | None = None
        self.hotkey: HotkeyListener | None = None
        self._busy = threading.Lock()

        # Drives the recording overlay's level meter and elapsed timer.
        self._ticker = QTimer(self)
        self._ticker.setInterval(50)
        self._ticker.timeout.connect(self._tick)

    # -- setup --------------------------------------------------------------

    def apply_settings(self, settings: Settings) -> None:
        """Re-read settings; safe to call while idle."""
        self.settings = settings
        self.recorder = Recorder(
            device=settings.input_device, max_seconds=settings.max_seconds
        )
        backend = self.backend_cls.id
        info = models.get(settings.model)
        if info is None or not info.downloaded(backend):
            self.engine = None
        elif self.engine is None or self.engine.info.key != info.key:
            self.engine = self.backend_cls(info, settings.language)
        else:
            self.engine.language = settings.language
        self.restart_hotkey()

    def restart_hotkey(self) -> None:
        if self.hotkey is not None:
            self.hotkey.stop()
        self.hotkey = HotkeyListener(
            hotkey=self.settings.hotkey,
            hold_threshold=self.settings.hold_threshold,
            on_start=self._start_recording,
            on_finish=self._finish_recording,
            on_cancel=self._cancel_recording,
        )
        self.hotkey.start()

    def preload_model(self) -> None:
        """Load weights in the background so the first dictation isn't slow."""
        if self.engine is None or self.engine.loaded:
            return

        def work():
            try:
                self.engine.load()
            except Exception:
                log.exception("model preload failed")

        threading.Thread(target=work, daemon=True, name="preload").start()

    def shutdown(self) -> None:
        if self.hotkey is not None:
            self.hotkey.stop()
        self._ticker.stop()
        if self.recorder.recording:
            self.recorder.cancel()

    # -- recording lifecycle (called from the hotkey thread) ----------------

    def _set_state(self, state: State) -> None:
        self.state = state
        self.stateChanged.emit(state)

    def _start_recording(self) -> None:
        if self.state is not State.IDLE:
            return
        if self.engine is None:
            self.failed.emit("No model installed yet — open Settings to download one.")
            return
        try:
            self.recorder.start()
        except Exception as exc:
            log.exception("could not open the microphone")
            self._set_state(State.IDLE)
            self.failed.emit(f"Could not open the microphone: {exc}")
            return
        self._set_state(State.RECORDING)
        QTimer.singleShot(0, self._ticker.start)

    def _cancel_recording(self) -> None:
        if self.state is not State.RECORDING:
            return
        self.recorder.cancel()
        QTimer.singleShot(0, self._ticker.stop)
        self._set_state(State.IDLE)

    def _finish_recording(self) -> None:
        if self.state is not State.RECORDING:
            return
        audio = self.recorder.stop()
        QTimer.singleShot(0, self._ticker.stop)

        if audio.size == 0:
            self._set_state(State.IDLE)
            return

        self._set_state(State.TRANSCRIBING)
        threading.Thread(
            target=self._transcribe_and_paste, args=(audio,),
            daemon=True, name="transcribe",
        ).start()

    def _transcribe_and_paste(self, audio) -> None:
        with self._busy:
            started = time.monotonic()
            try:
                text = self.engine.transcribe(audio)
            except Exception as exc:
                log.exception("transcription failed")
                self._set_state(State.IDLE)
                self.failed.emit(f"Transcription failed: {exc}")
                return

            if not text:
                self._set_state(State.IDLE)
                return

            log.info(
                "%.1fs audio -> %d chars in %.2fs",
                audio.size / 16_000, len(text), time.monotonic() - started,
            )
            try:
                paste_text(text, restore_clipboard=self.settings.restore_clipboard)
            except Exception as exc:
                log.exception("paste failed")
                self._set_state(State.IDLE)
                self.failed.emit(f"Could not paste the text: {exc}")
                return

            self._set_state(State.IDLE)
            self.transcribed.emit(text)

    # -- UI ticker ----------------------------------------------------------

    def _tick(self) -> None:
        self.levelChanged.emit(self.recorder.level)
        duration = self.recorder.duration
        self.durationChanged.emit(duration)
        if duration >= self.settings.max_seconds:
            # Stopping ourselves leaves the listener believing a toggle session
            # is still open, which would swallow the user's next tap.
            if self.hotkey is not None:
                self.hotkey.reset()
            self._finish_recording()
