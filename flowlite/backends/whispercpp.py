"""whisper.cpp backend.

Preferred everywhere it will load. On Apple Silicon it runs the encoder on the
GPU through Metal, which is roughly seven times faster than the CPU-only
CTranslate2 path for the large models — the difference between a usable
dictation tool and an unusable one.
"""

import logging
import os
import sys

import numpy as np

from ..models import WHISPERCPP, ModelInfo
from ..speech import finalise, has_speech
from .base import Backend

log = logging.getLogger(__name__)


def _thread_count() -> int:
    """CPU threads for the decoder.

    On Apple Silicon the encoder runs on the GPU, so this only feeds the
    decoder. Beyond about eight threads there is nothing left to gain, and
    oversubscribing a shared machine costs more than it returns.
    """
    return max(2, min(8, os.cpu_count() or 4))


class WhisperCppBackend(Backend):
    id = WHISPERCPP
    label = "whisper.cpp"

    def __init__(self, info: ModelInfo, language: str = ""):
        super().__init__(info, language)
        self._model = None
        self._threads = _thread_count()

    @staticmethod
    def is_available() -> bool:
        try:
            import pywhispercpp.model  # noqa: F401
        except Exception:
            return False
        return True

    def describe_device(self) -> str:
        if sys.platform == "darwin":
            import platform

            if platform.machine() == "arm64":
                return f"Apple GPU via Metal, {self._threads} CPU threads"
        return f"CPU, {self._threads} threads"

    @property
    def loaded(self) -> bool:
        return self._model is not None

    def load(self) -> None:
        if self._model is not None:
            return
        from pywhispercpp.model import Model

        path = self.info.local_path(WHISPERCPP)
        log.info("loading %s (%s)", self.info.key, self.describe_device())
        self._model = Model(
            str(path),
            redirect_whispercpp_logs_to=None,   # whisper.cpp is extremely chatty
            n_threads=self._threads,
            print_progress=False,
            print_realtime=False,
            print_timestamps=False,
            single_segment=False,
            no_context=True,        # each dictation is independent
            suppress_nst=True,      # drop "(door closes)" style annotations
            **self._language_params(),
        )
        # Warm the Metal pipelines so the first real dictation isn't the one
        # that pays for shader compilation.
        self._model.transcribe(np.zeros(16_000, dtype=np.float32))

    def unload(self) -> None:
        self._model = None

    def _language_params(self) -> dict:
        """whisper.cpp's sentinel for autodetect is the literal string "auto".

        Do not be tempted by `detect_language`: that flag makes whisper.cpp
        report the language and return no segments at all.
        """
        return {"language": self.language or "auto", "detect_language": False}

    def transcribe(self, audio: np.ndarray) -> str:
        if not has_speech(audio):
            return ""
        self.load()
        for key, value in self._language_params().items():
            setattr(self._model, key, value)
        segments = self._model.transcribe(audio)
        return finalise([s.text for s in segments])
