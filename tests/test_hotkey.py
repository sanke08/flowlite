"""Tap-vs-hold state machine, driven directly with no OS listener attached."""

import time

import pytest
from pynput import keyboard

from flowlite.hotkey import Gesture, HotkeyListener

TARGET = keyboard.Key.alt_r
OTHER = keyboard.Key.alt_l
HOLD = 0.4
PAST_HOLD = 0.45


@pytest.fixture
def machine():
    events: list[str] = []
    listener = HotkeyListener(
        "alt_r", HOLD,
        on_start=lambda: events.append("start"),
        on_finish=lambda: events.append("finish"),
        on_cancel=lambda: events.append("cancel"),
    )
    return listener, events


def test_hold_is_push_to_talk(machine):
    h, events = machine
    h._on_press(TARGET)
    time.sleep(PAST_HOLD)
    h._on_release(TARGET)
    assert events == ["start", "finish"]


def test_tap_opens_a_toggle_session(machine):
    h, events = machine
    h._on_press(TARGET)
    h._on_release(TARGET)
    assert events == ["start"], "a quick tap must keep recording"
    assert h._state is Gesture.TOGGLE


def test_second_tap_finishes(machine):
    h, events = machine
    h._on_press(TARGET); h._on_release(TARGET)
    h._on_press(TARGET); h._on_release(TARGET)
    assert events == ["start", "finish"]
    assert h._state is Gesture.IDLE


def test_escape_cancels_an_open_session(machine):
    h, events = machine
    h._on_press(TARGET); h._on_release(TARGET)
    h._on_press(keyboard.Key.esc)
    assert events == ["start", "cancel"]
    assert h._state is Gesture.IDLE


def test_key_autorepeat_does_not_restart(machine):
    h, events = machine
    for _ in range(3):
        h._on_press(TARGET)
    time.sleep(PAST_HOLD)
    h._on_release(TARGET)
    assert events == ["start", "finish"]


def test_unrelated_keys_are_ignored(machine):
    h, events = machine
    h._on_press(OTHER)
    h._on_release(OTHER)
    h._on_press(keyboard.KeyCode.from_char("a"))
    assert events == []


def test_escape_while_idle_is_inert(machine):
    h, events = machine
    h._on_press(keyboard.Key.esc)
    assert events == []


def test_a_raising_callback_does_not_wedge_the_machine(machine):
    h, _ = machine

    def boom():
        raise RuntimeError("boom")

    h.on_start = boom
    h._on_press(TARGET)
    assert h._state is Gesture.IDLE, "a failed callback must not strand the listener"


def test_reset_lets_the_next_gesture_start_cleanly(machine):
    """The controller calls reset() when it stops recording on its own."""
    h, events = machine
    h._on_press(TARGET); h._on_release(TARGET)
    h.reset()
    events.clear()
    h._on_press(TARGET)
    time.sleep(PAST_HOLD)
    h._on_release(TARGET)
    assert events == ["start", "finish"]
