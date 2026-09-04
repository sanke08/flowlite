"""Main settings window."""

import sys

from PySide6.QtCore import Qt, QTimer, Signal
from PySide6.QtWidgets import (
    QFrame, QHBoxLayout, QLabel, QPushButton, QTabWidget, QVBoxLayout, QWidget,
)

from .. import models, permissions
from ..config import Settings
from ..hotkey import key_label
from . import theme
from .icons import app_icon
from .modelspage import ModelsPage
from .prefspage import PrefsPage



class PermissionsPage(QWidget):
    def __init__(self, parent=None):
        super().__init__(parent)
        root = QVBoxLayout(self)
        root.setContentsMargins(14, 14, 14, 14)
        root.setSpacing(12)

        if not permissions.needs_accessibility():
            note = QLabel(
                "No special permissions are needed on this operating system. "
                "If your antivirus or corporate policy blocks synthetic keystrokes, "
                "FlowLite will be able to record but not paste."
            )
            note.setWordWrap(True)
            root.addWidget(note)
            root.addStretch(1)
            return

        frozen = permissions.is_frozen()
        if frozen:
            intro = (
                "<p>macOS requires two permissions before FlowLite can work.</p>"
                "<p><b>Accessibility</b> lets FlowLite see the dictation key while "
                "you are in another app, and paste text back into it. Without it "
                "the hotkey silently does nothing.</p>"
                "<p><b>Microphone</b> is requested automatically the first time "
                "you record.</p>"
                "<p>Use the button below and macOS will add FlowLite to the list "
                "for you — then switch it on and relaunch.</p>"
            )
        else:
            host = permissions.host_app_name()
            intro = (
                f"<p>You are running FlowLite from source, so macOS assigns these "
                f"permissions to <b>{host}</b> — the program that launched it — "
                f"and not to FlowLite. Build the app bundle to have them attach "
                f"to FlowLite itself.</p>"
                "<p><b>Accessibility</b> lets FlowLite see the dictation key from "
                "any app and paste text back into it. Without it the hotkey "
                "silently does nothing.</p>"
                "<p><b>Microphone</b> is requested automatically the first time "
                "you record.</p>"
            )

        body = QLabel(
            intro + "<p>After granting Accessibility you must quit and relaunch "
            "— macOS only re-checks at startup.</p>"
        )
        body.setWordWrap(True)
        root.addWidget(body)

        self.status = QFrame()
        self.status.setObjectName("banner")
        row = QHBoxLayout(self.status)
        row.setContentsMargins(12, 10, 12, 10)
        self.status_label = QLabel()
        self.status_label.setWordWrap(True)
        row.addWidget(self.status_label, 1)
        root.addWidget(self.status)

        buttons = QHBoxLayout()
        ask = QPushButton("Grant Accessibility…")
        ask.clicked.connect(self._request)
        buttons.addWidget(ask)
        acc = QPushButton("Open Accessibility settings")
        acc.clicked.connect(permissions.open_accessibility_settings)
        buttons.addWidget(acc)
        mic = QPushButton("Open Microphone settings")
        mic.clicked.connect(permissions.open_microphone_settings)
        buttons.addWidget(mic)
        buttons.addStretch(1)
        root.addLayout(buttons)
        root.addStretch(1)

        self._poll = QTimer(self)
        self._poll.setInterval(1500)
        self._poll.timeout.connect(self.refresh)
        self._poll.start()
        self.refresh()

    def _request(self) -> None:
        """Trigger the system dialog, which registers FlowLite in the list."""
        if not permissions.request_accessibility():
            permissions.open_accessibility_settings()
        self.refresh()

    def refresh(self) -> None:
        if not permissions.needs_accessibility():
            return
        granted = permissions.has_accessibility()
        self.status.setObjectName("ok" if granted else "banner")
        self.status_label.setText(
            "Accessibility is granted. You're all set."
            if granted else
            "Accessibility is <b>not</b> granted yet — the dictation key will not work."
        )
        self.status.setStyleSheet(theme.STYLESHEET)  # re-evaluate #ok / #banner


class MainWindow(QWidget):
    settingsChanged = Signal()

    def __init__(self, settings: Settings, backend: str, engine_note: str, parent=None):
        super().__init__(parent)
        self.settings = settings
        self.backend = backend
        self.setWindowTitle("FlowLite")
        self.setWindowIcon(app_icon())
        self.resize(720, 700)
        self.setMinimumSize(620, 520)

        root = QVBoxLayout(self)
        root.setContentsMargins(20, 18, 20, 18)
        root.setSpacing(14)

        title = QLabel("FlowLite")
        title.setObjectName("title")
        root.addWidget(title)

        self.summary = QLabel()
        self.summary.setObjectName("dim")
        self.summary.setWordWrap(True)
        root.addWidget(self.summary)

        self.tabs = QTabWidget()
        self.models_page = ModelsPage(settings.model, backend, engine_note)
        self.models_page.modelChosen.connect(self._on_model)
        self.models_page.changed.connect(self._refresh_summary)
        self.prefs_page = PrefsPage(settings)
        self.prefs_page.changed.connect(self._on_prefs)

        self.tabs.addTab(self.models_page, "Speech models")
        self.tabs.addTab(self.prefs_page, "Preferences")
        self.tabs.addTab(PermissionsPage(), "Permissions")
        root.addWidget(self.tabs, 1)

        bottom = QHBoxLayout()
        bottom.addStretch(1)
        close = QPushButton("Close")
        close.setObjectName("primary")
        close.setMinimumWidth(96)
        close.clicked.connect(self.hide)
        bottom.addWidget(close)
        root.addLayout(bottom)

        self._refresh_summary()

    # -- handlers -----------------------------------------------------------

    def _on_model(self, key: str) -> None:
        self.settings.model = key
        self.settings.save()
        self._refresh_summary()
        self.settingsChanged.emit()

    def _on_prefs(self) -> None:
        self.settings.save()
        self._refresh_summary()
        self.settingsChanged.emit()

    def _refresh_summary(self) -> None:
        info = models.get(self.settings.model)
        key = key_label(self.settings.hotkey)
        if info is None or not info.downloaded(self.backend):
            self.summary.setText(
                "<b>No model installed.</b> Download one below to start dictating."
            )
        else:
            self.summary.setText(
                f"Ready — <b>{info.label}</b>. Tap <b>{key}</b> to start and stop, "
                f"or hold it to dictate while pressed. Text is pasted wherever your "
                f"cursor is."
            )

    def open_at(self, tab: int = 0) -> None:
        self.tabs.setCurrentIndex(tab)
        self.models_page.refresh()
        self.show()
        self.raise_()
        self.activateWindow()

    def closeEvent(self, event):
        # Closing the window leaves the app running in the tray.
        event.ignore()
        self.hide()

    def shutdown(self) -> None:
        self.models_page.stop_all()
