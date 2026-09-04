"""OS permission checks.

macOS gates synthetic keystrokes and global key monitoring behind
Accessibility, and the microphone behind its own TCC prompt. Windows and Linux
need neither.
"""

import subprocess
import sys


def needs_accessibility() -> bool:
    return sys.platform == "darwin"


def has_accessibility() -> bool:
    if sys.platform != "darwin":
        return True
    try:
        from ApplicationServices import AXIsProcessTrusted

        return bool(AXIsProcessTrusted())
    except Exception:
        return True  # can't tell; don't block the user on a bad check


def request_accessibility() -> bool:
    """Ask macOS to show its own Accessibility prompt.

    This is worth far more than sending the user to System Settings unaided:
    the system dialog registers the app in the Accessibility list, so the user
    only has to flip the switch instead of hunting for the app with the "+"
    button. Returns the current trust state, which is almost always False on
    the first call — granting happens after the dialog, out of band.
    """
    if sys.platform != "darwin":
        return True
    try:
        from ApplicationServices import (
            AXIsProcessTrustedWithOptions,
            kAXTrustedCheckOptionPrompt,
        )

        return bool(AXIsProcessTrustedWithOptions({kAXTrustedCheckOptionPrompt: True}))
    except Exception:
        return has_accessibility()


def open_accessibility_settings() -> None:
    if sys.platform != "darwin":
        return
    subprocess.Popen([
        "open",
        "x-apple.systempreferences:com.apple.preference.security"
        "?Privacy_Accessibility",
    ])


def open_microphone_settings() -> None:
    if sys.platform != "darwin":
        return
    subprocess.Popen([
        "open",
        "x-apple.systempreferences:com.apple.preference.security"
        "?Privacy_Microphone",
    ])


def is_frozen() -> bool:
    """True when running from a packaged .app / .exe rather than source."""
    return bool(getattr(sys, "frozen", False))


def host_app_name() -> str:
    """The app the user must grant permission to.

    In a packaged build this is FlowLite itself. Running from source it is the
    terminal or IDE that launched Python, which is the single most confusing
    part of first-run setup.
    """
    if is_frozen():
        return "FlowLite"
    import os

    return os.environ.get("TERM_PROGRAM", "your terminal app").replace("_", " ")
