"""Inference backend selection."""

import logging

from ..models import CT2, WHISPERCPP, ModelInfo
from .base import Backend
from .ct2 import FasterWhisperBackend, has_cuda
from .whispercpp import WhisperCppBackend

log = logging.getLogger(__name__)

ALL: list[type[Backend]] = [WhisperCppBackend, FasterWhisperBackend]


def available() -> list[type[Backend]]:
    return [b for b in ALL if b.is_available()]


def pick() -> type[Backend]:
    """Choose the fastest engine this machine can actually run.

    whisper.cpp wins by a wide margin on Apple Silicon (Metal) and is fine
    elsewhere, but CTranslate2's CUDA path beats it on an NVIDIA machine.
    """
    usable = available()
    if not usable:
        raise RuntimeError(
            "No speech engine is installed. Reinstall the dependencies with "
            "'uv pip install -e .'."
        )
    if FasterWhisperBackend in usable and has_cuda():
        return FasterWhisperBackend
    if WhisperCppBackend in usable:
        return WhisperCppBackend
    return usable[0]


def make(info: ModelInfo, language: str = "") -> Backend:
    return pick()(info, language)


__all__ = [
    "CT2", "WHISPERCPP", "Backend", "FasterWhisperBackend", "WhisperCppBackend",
    "available", "make", "pick",
]
