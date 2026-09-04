"""Menu bar / system tray presence."""

from PySide6.QtCore import Signal
from PySide6.QtGui import QAction
from PySide6.QtWidgets import QMenu, QSystemTrayIcon

from ..controller import State
from ..hotkey import key_label
from .icons import tray_icon


class Tray(QSystemTrayIcon):
    openSettings = Signal()
    quitRequested = Signal()

    def __init__(self, parent=None):
        super().__init__(tray_icon(False), parent)
        self.setToolTip("FlowLite")

        menu = QMenu()
        self.status_action = QAction("Ready")
        self.status_action.setEnabled(False)
        menu.addAction(self.status_action)
        menu.addSeparator()

        settings = QAction("Open FlowLite Settings…")
        settings.triggered.connect(self.openSettings.emit)
        menu.addAction(settings)
        # Make the primary action obvious when the menu is opened by a click.
        menu.setDefaultAction(settings)

        menu.addSeparator()
        quit_action = QAction("Quit FlowLite")
        quit_action.triggered.connect(self.quitRequested.emit)
        menu.addAction(quit_action)

        self.setContextMenu(menu)
        self._menu = menu  # keep a reference; Qt does not own it
        self.activated.connect(self._on_activated)

    def _on_activated(self, reason) -> None:
        if reason == QSystemTrayIcon.ActivationReason.Trigger:
            self.openSettings.emit()

    def set_state(self, state: State, hotkey: str, model_label: str | None) -> None:
        self.setIcon(tray_icon(state is State.RECORDING))
        if model_label is None:
            text = "No model installed"
        elif state is State.RECORDING:
            text = "Listening…"
        elif state is State.TRANSCRIBING:
            text = "Transcribing…"
        else:
            text = f"Ready — tap {key_label(hotkey)}"
        self.status_action.setText(text)
        self.setToolTip(f"FlowLite — {text}")
