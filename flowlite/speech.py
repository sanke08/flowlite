"""Gating and cleanup around raw Whisper output.

Whisper is trained on captioned video and will confidently caption silence —
"Thank you.", "Thanks for watching!" and similar phrases are its favourite
inventions. A mis-tapped hotkey must never paste one of those, so audio is
screened for actual speech before inference, and a known-hallucination filter
catches whatever slips past.
"""

import re
import unicodedata

import numpy as np

from .audio import SAMPLE_RATE

# Shorter than this and there is nothing worth sending to the model.
MIN_SECONDS = 0.35

# Measured against a normal speaking voice, which peaks around 0.2 RMS. Room
# tone and mic self-noise sit below 0.01, so this clears the noise floor with
# an order of magnitude to spare while still catching quiet speakers.
SPEECH_RMS = 0.015

FRAME = 1600            # 100 ms
MIN_LOUD_FRAMES = 3     # ~300 ms of actual sound

# Matched after lowercasing and stripping punctuation.
HALLUCINATIONS = {
    "", "you", "thank you", "thanks", "thank you very much",
    "thanks for watching", "thank you for watching", "thanks for watching!",
    "please subscribe", "like and subscribe", "subtitles by the amara org community",
    "bye", "bye bye", "goodbye", "okay", "ok", "so", "um", "uh", "hmm", "mm",
    "blank audio", "silence", "music", "applause", "beep",
}


def frame_rms(audio: np.ndarray, frame: int = FRAME) -> np.ndarray:
    if audio.size < frame:
        return np.array([float(np.sqrt(np.mean(np.square(audio))))]) if audio.size else np.array([0.0])
    usable = audio[: audio.size // frame * frame].reshape(-1, frame)
    return np.sqrt(np.mean(np.square(usable), axis=1))


def has_speech(audio: np.ndarray) -> bool:
    """Whether this buffer is worth running a model over."""
    if audio.size < MIN_SECONDS * SAMPLE_RATE:
        return False
    loud = frame_rms(audio) > SPEECH_RMS
    return int(loud.sum()) >= MIN_LOUD_FRAMES


def _normalise(text: str) -> str:
    text = unicodedata.normalize("NFKC", text).lower()
    text = re.sub(r"[^\w\s]", "", text)
    return re.sub(r"\s+", " ", text).strip()


def is_hallucination(text: str) -> bool:
    return _normalise(text) in HALLUCINATIONS


def join_segments(segments: list[str]) -> str:
    """Join model segments into one line.

    Whisper emits segments with inconsistent leading spaces, which is how
    "…since Monday." and "After that…" end up welded together.
    """
    parts = [s.strip() for s in segments]
    return " ".join(p for p in parts if p)


def clean(text: str) -> str:
    """Tidy whitespace and drop bracketed sound annotations."""
    # "[BLANK_AUDIO]", "(door closes)", "♪ music ♪"
    text = re.sub(r"[\[(](?:[^\])]*)[\])]", " ", text)
    text = text.replace("♪", " ")
    text = re.sub(r"\s+", " ", text)
    return text.strip()


def finalise(segments: list[str]) -> str:
    """Full post-processing pipeline: join, clean, reject hallucinations."""
    text = clean(join_segments(segments))
    if is_hallucination(text):
        return ""
    return text
