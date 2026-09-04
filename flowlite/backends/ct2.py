"""faster-whisper (CTranslate2) backend.

Used where whisper.cpp has no wheel — notably Intel Macs — and preferred on
machines with an NVIDIA GPU, where CTranslate2's CUDA path is excellent.
CTranslate2 has no Metal backend, so on Apple Silicon this is a slow fallback.
"""

import logging
import os

import numpy as np

from ..models import CT2, ModelInfo
from ..speech import finalise, has_speech
from .base import Backend

log = logging.getLogger(__name__)


def has_cuda() -> bool:
    try:
        import ctranslate2

        return ctranslate2.get_cuda_device_count() > 0
    except Exception:
        return False


class FasterWhisperBackend(Backend):
    id = CT2
    label = "faster-whisper"

    def __init__(self, info: ModelInfo, language: str = ""):
        super().__init__(info, language)
        self._model = None
        if has_cuda():
            self.device, self.compute_type = "cuda", "float16"
        else:
            self.device, self.compute_type = "cpu", "int8"
        self._threads = 0 if self.device == "cuda" else max(4, min(8, os.cpu_count() or 4))

    @staticmethod
    def is_available() -> bool:
        try:
            import faster_whisper  # noqa: F401
        except Exception:
            return False
        return True

    def describe_device(self) -> str:
        if self.device == "cuda":
            return "NVIDIA GPU via CUDA, float16"
        return f"CPU, {self._threads} threads, int8"

    @property
    def loaded(self) -> bool:
        return self._model is not None

    def load(self) -> None:
        if self._model is not None:
            return
        from faster_whisper import WhisperModel

        log.info("loading %s (%s)", self.info.key, self.describe_device())
        self._model = WhisperModel(
            str(self.info.local_path(CT2)),
            device=self.device,
            compute_type=self.compute_type,
            cpu_threads=self._threads,
        )
        self._model.transcribe(np.zeros(16_000, dtype=np.float32))

    def unload(self) -> None:
        self._model = None

    def transcribe(self, audio: np.ndarray) -> str:
        if not has_speech(audio):
            return ""
        self.load()
        segments, _info = self._model.transcribe(
            audio,
            language=self.language or None,
            # Greedy decoding: measurably faster and, for short dictation,
            # indistinguishable in quality from a beam search.
            beam_size=1,
            vad_filter=True,
            vad_parameters={"min_silence_duration_ms": 500},
            condition_on_previous_text=False,  # avoids runaway repetition loops
        )
        return finalise([s.text for s in segments])
