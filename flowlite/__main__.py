"""Application entry point."""

import logging
import signal
import sys

from PySide6.QtCore import QTimer
from PySide6.QtWidgets import QApplication, QMessageBox, QSystemTrayIcon

from . import APP_NAME, backends, models, permissions
from .config import Settings
from .controller import Controller, State
from .paths import config_dir
from .ui.icons import app_icon
from .ui.overlay import Overlay
from .ui.tray import Tray
from .ui.window import MainWindow

log = logging.getLogger("flowlite")


def setup_logging() -> None:
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s %(levelname)-7s %(name)s: %(message)s",
        handlers=[
            logging.StreamHandler(sys.stderr),
            logging.FileHandler(config_dir() / "flowlite.log", encoding="utf-8"),
        ],
    )
    # pywhispercpp narrates every single dictation at INFO level.
    logging.getLogger("pywhispercpp").setLevel(logging.WARNING)


class App:
    def __init__(self, qapp: QApplication):
        self.qapp = qapp
        self.settings = Settings.load()

        self.controller = Controller(self.settings)
        self.backend = self.controller.backend_cls.id

        # Shown in the UI so it is obvious which engine and hardware are in use.
        probe = self.controller.backend_cls(models.default_for(self.backend))
        self.engine_note = f"{probe.label} — {probe.describe_device()}"
        log.info("engine: %s", self.engine_note)

        # A model chosen under a different engine will not exist for this one.
        if self.settings.model and not self._model_usable():
            log.info("model %r unavailable for %s; clearing",
                     self.settings.model, self.backend)
            self.settings.model = ""
            self.settings.save()

        self.overlay = Overlay()
        self.window = MainWindow(self.settings, self.backend, self.engine_note)
        self.tray = Tray(qapp)

        self.controller.stateChanged.connect(self._on_state)
        self.controller.levelChanged.connect(self.overlay.push_level)
        self.controller.durationChanged.connect(self.overlay.set_duration)
        self.controller.failed.connect(self._on_error)

        self.window.settingsChanged.connect(self._reload)
        self.tray.openSettings.connect(lambda: self.window.open_at(0))
        self.tray.quitRequested.connect(self.quit)

        self.tray.show()
        self._reload()

        if not self.settings.configured:
            self.window.open_at(0)
        elif permissions.needs_accessibility() and not permissions.has_accessibility():
            self.window.open_at(2)

    # -- wiring -------------------------------------------------------------

    def _model_usable(self) -> bool:
        info = models.get(self.settings.model)
        return info is not None and info.downloaded(self.backend)

    def _reload(self) -> None:
        self.controller.apply_settings(self.settings)
        self.controller.preload_model()
        self._sync_tray()

    def _sync_tray(self) -> None:
        info = models.get(self.settings.model)
        label = info.label if info and info.downloaded(self.backend) else None
        self.tray.set_state(self.controller.state, self.settings.hotkey, label)

    def _on_state(self, state: State) -> None:
        if state is State.RECORDING:
            self.overlay.show_recording()
        elif state is State.TRANSCRIBING:
            self.overlay.show_transcribing()
        else:
            self.overlay.hide_overlay()
        self._sync_tray()

    def _on_error(self, message: str) -> None:
        log.error(message)
        self.overlay.hide_overlay()
        if self.tray.supportsMessages():
            self.tray.showMessage(APP_NAME, message, app_icon(), 6000)
        else:
            QMessageBox.warning(self.window, APP_NAME, message)

    def quit(self) -> None:
        self.controller.shutdown()
        self.window.shutdown()
        self.overlay.hide_overlay()
        self.tray.hide()
        self.qapp.quit()


def main() -> int:
    setup_logging()

    qapp = QApplication(sys.argv)
    qapp.setApplicationName(APP_NAME)
    qapp.setWindowIcon(app_icon())
    # The tray is the app's real home; closing the settings window must not exit.
    qapp.setQuitOnLastWindowClosed(False)

    if sys.platform == "darwin":
        # Keep FlowLite out of the Dock and the app switcher.
        try:
            from AppKit import NSApp, NSApplicationActivationPolicyAccessory

            NSApp.setActivationPolicy_(NSApplicationActivationPolicyAccessory)
        except Exception:
            log.debug("could not set accessory activation policy", exc_info=True)

    if not QSystemTrayIcon.isSystemTrayAvailable():
        QMessageBox.critical(
            None, APP_NAME,
            "No system tray is available, so FlowLite has nowhere to live.",
        )
        return 1

    app = App(qapp)

    # Let Ctrl+C in a terminal actually kill the app despite the Qt event loop.
    signal.signal(signal.SIGINT, lambda *_: app.quit())
    heartbeat = QTimer()
    heartbeat.start(250)
    heartbeat.timeout.connect(lambda: None)

    return qapp.exec()


if __name__ == "__main__":
    sys.exit(main())
