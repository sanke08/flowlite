"""Persisted user settings."""

import json
import sys
from dataclasses import asdict, dataclass, field

from .paths import config_dir

SETTINGS_FILE = config_dir() / "settings.json"

# Tap vs hold is decided by how long the key is held down.
DEFAULT_HOLD_THRESHOLD = 0.4


def _default_hotkey() -> str:
    # Right-Option on macOS, Right-Ctrl on Windows/Linux. Both are keys almost
    # nothing else binds, and both sit under a resting hand.
    return "alt_r" if sys.platform == "darwin" else "ctrl_r"


@dataclass
class Settings:
    model: str = ""                      # empty until first-run setup completes
    hotkey: str = field(default_factory=_default_hotkey)
    hold_threshold: float = DEFAULT_HOLD_THRESHOLD
    language: str = ""                   # "" = autodetect
    input_device: int | None = None      # None = system default mic
    restore_clipboard: bool = True
    play_sounds: bool = True
    max_seconds: int = 300               # hard stop so a stuck toggle can't record forever

    @classmethod
    def load(cls) -> "Settings":
        if SETTINGS_FILE.exists():
            try:
                data = json.loads(SETTINGS_FILE.read_text(encoding="utf-8"))
                known = {f for f in cls.__dataclass_fields__}
                return cls(**{k: v for k, v in data.items() if k in known})
            except (json.JSONDecodeError, TypeError, ValueError):
                pass  # corrupt settings shouldn't brick startup
        return cls()

    def save(self) -> None:
        SETTINGS_FILE.write_text(
            json.dumps(asdict(self), indent=2), encoding="utf-8"
        )

    @property
    def configured(self) -> bool:
        return bool(self.model)
