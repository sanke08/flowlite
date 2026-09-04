"""Cross-platform application directories."""

import os
import sys
from pathlib import Path

from . import APP_NAME


def config_dir() -> Path:
    """Where settings.json lives."""
    if sys.platform == "darwin":
        base = Path.home() / "Library" / "Application Support"
    elif sys.platform == "win32":
        base = Path(os.environ.get("APPDATA", Path.home() / "AppData" / "Roaming"))
    else:
        base = Path(os.environ.get("XDG_CONFIG_HOME", Path.home() / ".config"))
    d = base / APP_NAME
    d.mkdir(parents=True, exist_ok=True)
    return d


def models_dir() -> Path:
    """Where downloaded Whisper weights live.

    Kept separate from the config dir so a user can wipe settings without
    re-downloading gigabytes of weights.
    """
    if sys.platform == "darwin":
        base = Path.home() / "Library" / "Application Support"
    elif sys.platform == "win32":
        base = Path(os.environ.get("LOCALAPPDATA", Path.home() / "AppData" / "Local"))
    else:
        base = Path(os.environ.get("XDG_DATA_HOME", Path.home() / ".local" / "share"))
    d = base / APP_NAME / "models"
    d.mkdir(parents=True, exist_ok=True)
    return d
