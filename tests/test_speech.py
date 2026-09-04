"""Silence gating and hallucination filtering."""

import numpy as np

from flowlite.audio import SAMPLE_RATE
from flowlite.speech import clean, finalise, has_speech, is_hallucination, join_segments


def tone(amplitude: float, seconds: float = 1.0) -> np.ndarray:
    rng = np.random.default_rng(0)
    return (rng.standard_normal(int(SAMPLE_RATE * seconds)) * amplitude).astype(np.float32)


class TestHasSpeech:
    def test_digital_silence_is_rejected(self):
        assert not has_speech(np.zeros(SAMPLE_RATE, dtype=np.float32))

    def test_mic_noise_floor_is_rejected(self):
        assert not has_speech(tone(0.001))

    def test_room_tone_is_rejected(self):
        assert not has_speech(tone(0.01))

    def test_speech_level_is_accepted(self):
        assert has_speech(tone(0.15))

    def test_a_mistap_is_too_short_to_count(self):
        assert not has_speech(tone(0.15, seconds=0.1))

    def test_empty_buffer_is_rejected(self):
        assert not has_speech(np.zeros(0, dtype=np.float32))


class TestHallucinations:
    def test_thank_you_on_silence_is_dropped(self):
        assert finalise([" Thank you."]) == ""

    def test_thanks_for_watching_is_dropped(self):
        assert finalise(["Thanks for watching!"]) == ""

    def test_blank_audio_marker_is_dropped(self):
        assert finalise(["[BLANK_AUDIO]"]) == ""

    def test_real_speech_survives(self):
        assert finalise(["Ship the release on Friday."]) == "Ship the release on Friday."

    def test_matching_ignores_case_and_punctuation(self):
        assert is_hallucination("  THANK YOU!!  ")


class TestJoining:
    def test_segments_get_a_separating_space(self):
        """Whisper's inconsistent leading spaces weld sentences together."""
        assert join_segments(["since Monday.", "After that"]) == "since Monday. After that"

    def test_leading_spaces_are_normalised(self):
        assert join_segments([" Hello", " world"]) == "Hello world"

    def test_empty_segments_are_skipped(self):
        assert join_segments(["Hello", "", "  ", "world"]) == "Hello world"


class TestClean:
    def test_sound_annotations_are_removed(self):
        assert clean("Hello (door closes) world.") == "Hello world."

    def test_bracketed_markers_are_removed(self):
        assert clean("[MUSIC] Let's begin.") == "Let's begin."

    def test_whitespace_is_collapsed(self):
        assert clean("too    many\n\nspaces") == "too many spaces"
