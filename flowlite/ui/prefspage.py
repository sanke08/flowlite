"""Hotkey, microphone and behaviour preferences."""

import sys

from PySide6.QtCore import Qt, Signal
from PySide6.QtWidgets import (
    QCheckBox, QComboBox, QFormLayout, QLabel, QSlider, QVBoxLayout, QWidget,
)

from ..audio import list_input_devices
from ..config import Settings
from ..hotkey import key_label

# Keys that are safe to grab globally: they do nothing on their own, and are
# rarely bound by other software.
HOTKEY_CHOICES = (
    ["alt_r", "ctrl_r", "cmd_r", "shift_r", "f13", "f14", "f15"]
    if sys.platform == "darwin"
    else ["ctrl_r", "alt_r", "shift_r", "f13", "f14", "f15", "pause"]
)

LANGUAGES = [
    ("Detect automatically", ""),
    ("English", "en"), ("Hindi", "hi"), ("Spanish", "es"), ("French", "fr"),
    ("German", "de"), ("Portuguese", "pt"), ("Italian", "it"), ("Dutch", "nl"),
    ("Russian", "ru"), ("Arabic", "ar"), ("Chinese", "zh"), ("Japanese", "ja"),
    ("Korean", "ko"), ("Tamil", "ta"), ("Telugu", "te"), ("Bengali", "bn"),
    ("Marathi", "mr"), ("Gujarati", "gu"), ("Urdu", "ur"),
]


class PrefsPage(QWidget):
    changed = Signal()

    def __init__(self, settings: Settings, parent=None):
        super().__init__(parent)
        self.settings = settings

        root = QVBoxLayout(self)
        root.setContentsMargins(14, 14, 14, 14)
        root.setSpacing(12)

        form = QFormLayout()
        form.setSpacing(10)
        form.setLabelAlignment(Qt.AlignmentFlag.AlignRight)

        self.hotkey = QComboBox()
        for key in HOTKEY_CHOICES:
            self.hotkey.addItem(key_label(key), key)
        idx = self.hotkey.findData(settings.hotkey)
        self.hotkey.setCurrentIndex(max(0, idx))
        self.hotkey.currentIndexChanged.connect(self._apply)
        form.addRow("Dictation key", self.hotkey)

        self.threshold = QSlider(Qt.Orientation.Horizontal)
        self.threshold.setRange(150, 900)
        self.threshold.setValue(int(settings.hold_threshold * 1000))
        self.threshold.valueChanged.connect(self._apply)
        self.threshold_label = QLabel()
        form.addRow("Tap / hold cutoff", self.threshold)
        form.addRow("", self.threshold_label)

        self.mic = QComboBox()
        self.mic.addItem("System default", None)
        for idx_, name in list_input_devices():
            self.mic.addItem(name, idx_)
        pos = self.mic.findData(settings.input_device)
        self.mic.setCurrentIndex(max(0, pos))
        self.mic.currentIndexChanged.connect(self._apply)
        form.addRow("Microphone", self.mic)

        self.language = QComboBox()
        for name, code in LANGUAGES:
            self.language.addItem(name, code)
        pos = self.language.findData(settings.language)
        self.language.setCurrentIndex(max(0, pos))
        self.language.currentIndexChanged.connect(self._apply)
        form.addRow("Language", self.language)

        root.addLayout(form)

        self.restore = QCheckBox("Put my previous clipboard contents back after pasting")
        self.restore.setChecked(settings.restore_clipboard)
        self.restore.toggled.connect(self._apply)
        root.addWidget(self.restore)

        hint = QLabel(
            "<b>Tap</b> the dictation key to start, then tap it again when you are "
            "done.<br><b>Hold</b> it down to dictate only while it is pressed.<br>"
            "Press <b>Esc</b> while recording to throw the take away."
        )
        hint.setWordWrap(True)
        hint.setObjectName("dim")
        root.addWidget(hint)
        root.addStretch(1)

        self._update_threshold_label()

    def _update_threshold_label(self) -> None:
        ms = self.threshold.value()
        self.threshold_label.setText(
            f"Released within {ms} ms counts as a tap; longer is push-to-talk."
        )
        self.threshold_label.setObjectName("dim")

    def _apply(self) -> None:
        self._update_threshold_label()
        self.settings.hotkey = self.hotkey.currentData()
        self.settings.hold_threshold = self.threshold.value() / 1000
        self.settings.input_device = self.mic.currentData()
        self.settings.language = self.language.currentData()
        self.settings.restore_clipboard = self.restore.isChecked()
        self.changed.emit()
