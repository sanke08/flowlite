"""Microphone capture.

Whisper expects 16 kHz mono float32, so the stream is opened at exactly that
and no resampling is needed downstream.
"""

import threading

import numpy as np
import sounddevice as sd

SAMPLE_RATE = 16_000
CHANNELS = 1
BLOCKSIZE = 1024


class Recorder:
    """Records to memory until stopped. One recording at a time."""

    def __init__(self, device: int | None = None, max_seconds: int = 300):
        self.device = device
        self.max_seconds = max_seconds
        self._chunks: list[np.ndarray] = []
        self._stream: sd.InputStream | None = None
        self._lock = threading.Lock()
        self._level = 0.0
        self._overrun = False

    @property
    def recording(self) -> bool:
        return self._stream is not None

    @property
    def level(self) -> float:
        """Most recent RMS amplitude, 0.0-1.0, for a live meter."""
        return self._level

    @property
    def duration(self) -> float:
        with self._lock:
            frames = sum(len(c) for c in self._chunks)
        return frames / SAMPLE_RATE

    def _callback(self, indata, frames, time_info, status):  # noqa: ARG002
        with self._lock:
            if sum(len(c) for c in self._chunks) >= self.max_seconds * SAMPLE_RATE:
                self._overrun = True
                return
            self._chunks.append(indata.copy().reshape(-1))
        # Smooth the meter a little so the UI doesn't strobe.
        rms = float(np.sqrt(np.mean(np.square(indata))))
        self._level = 0.6 * self._level + 0.4 * min(rms * 4.0, 1.0)

    def start(self) -> None:
        if self._stream is not None:
            return
        with self._lock:
            self._chunks = []
        self._overrun = False
        self._level = 0.0
        self._stream = sd.InputStream(
            samplerate=SAMPLE_RATE,
            channels=CHANNELS,
            dtype="float32",
            blocksize=BLOCKSIZE,
            device=self.device,
            callback=self._callback,
        )
        self._stream.start()

    def stop(self) -> np.ndarray:
        """Stop and return the captured audio as float32 mono."""
        stream, self._stream = self._stream, None
        if stream is not None:
            stream.stop()
            stream.close()
        self._level = 0.0
        with self._lock:
            chunks, self._chunks = self._chunks, []
        if not chunks:
            return np.zeros(0, dtype=np.float32)
        return np.concatenate(chunks).astype(np.float32)

    def cancel(self) -> None:
        self.stop()


def list_input_devices() -> list[tuple[int, str]]:
    out = []
    for idx, dev in enumerate(sd.query_devices()):
        if dev.get("max_input_channels", 0) > 0:
            out.append((idx, dev["name"]))
    return out


def default_input_name() -> str:
    try:
        return sd.query_devices(kind="input")["name"]
    except Exception:
        return "Unknown"
