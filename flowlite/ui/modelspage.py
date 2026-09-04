"""Model download / selection page."""

import threading

from PySide6.QtCore import QObject, QThread, Qt, Signal
from PySide6.QtGui import QFont
from PySide6.QtWidgets import (
    QFrame, QHBoxLayout, QLabel, QMessageBox, QProgressBar, QPushButton,
    QRadioButton, QScrollArea, QVBoxLayout, QWidget,
)

from .. import models
from ..download import DownloadCancelled, delete_model, download_model
from ..models import ModelInfo, human_size as _human


class DownloadWorker(QObject):
    progress = Signal(int, int, str)
    finished = Signal(bool, str)   # success, message

    def __init__(self, info: ModelInfo, backend: str):
        super().__init__()
        self.info = info
        self.backend = backend
        self.cancel = threading.Event()

    def run(self) -> None:
        try:
            download_model(self.info, self.backend, self.progress.emit, self.cancel)
        except DownloadCancelled:
            self.finished.emit(False, "")
        except Exception as exc:
            self.finished.emit(False, str(exc))
        else:
            self.finished.emit(True, "")


class ModelRow(QFrame):
    selected = Signal(str)
    changed = Signal()

    def __init__(self, info: ModelInfo, backend: str, parent=None):
        super().__init__(parent)
        self.info = info
        self.backend = backend
        self._thread: QThread | None = None
        self._worker: DownloadWorker | None = None

        self.setObjectName("modelRow")
        self.setFrameShape(QFrame.Shape.StyledPanel)

        outer = QVBoxLayout(self)
        outer.setContentsMargins(14, 12, 14, 12)
        outer.setSpacing(6)

        top = QHBoxLayout()
        top.setSpacing(10)

        self.radio = QRadioButton(info.label)
        f = QFont(self.radio.font())
        f.setWeight(QFont.Weight.DemiBold)
        self.radio.setFont(f)
        self.radio.toggled.connect(
            lambda on: self.selected.emit(self.info.key) if on else None
        )
        top.addWidget(self.radio)

        if info.is_recommended(backend):
            tag = QLabel("Recommended")
            tag.setObjectName("tag")
            top.addWidget(tag)

        top.addStretch(1)

        self.size_label = QLabel(_human(info.size_bytes(backend)))
        self.size_label.setObjectName("dim")
        top.addWidget(self.size_label)

        self.action = QPushButton()
        self.action.setFixedWidth(104)
        self.action.clicked.connect(self._on_action)
        top.addWidget(self.action)

        self.delete_btn = QPushButton("Remove")
        self.delete_btn.setFixedWidth(80)
        self.delete_btn.clicked.connect(self._on_delete)
        top.addWidget(self.delete_btn)

        outer.addLayout(top)

        blurb = QLabel(info.blurb)
        blurb.setWordWrap(True)
        blurb.setObjectName("dim")
        outer.addWidget(blurb)

        self.bar = QProgressBar()
        self.bar.setTextVisible(True)
        self.bar.setFixedHeight(16)
        self.bar.hide()
        outer.addWidget(self.bar)

        self.refresh()

    # -- state --------------------------------------------------------------

    @property
    def downloading(self) -> bool:
        return self._thread is not None

    def refresh(self) -> None:
        have = self.info.downloaded(self.backend)
        self.radio.setEnabled(have)
        self.delete_btn.setVisible(have and not self.downloading)
        if self.downloading:
            self.action.setText("Cancel")
            self.action.setEnabled(True)
        elif have:
            self.action.setText("Downloaded")
            self.action.setEnabled(False)
            self.size_label.setText(_human(self.info.disk_bytes(self.backend)))
        else:
            self.action.setText("Download")
            self.action.setEnabled(True)
            self.size_label.setText(_human(self.info.size_bytes(self.backend)))

    def set_checked(self, on: bool) -> None:
        self.radio.blockSignals(True)
        self.radio.setChecked(on)
        self.radio.blockSignals(False)

    # -- actions ------------------------------------------------------------

    def _on_action(self) -> None:
        if self.downloading:
            self._worker.cancel.set()
            self.action.setEnabled(False)
            self.action.setText("Cancelling…")
            return
        self._start_download()

    def _start_download(self) -> None:
        self.bar.setRange(0, 0)
        self.bar.setFormat("Starting…")
        self.bar.show()

        self._thread = QThread(self)
        self._worker = DownloadWorker(self.info, self.backend)
        self._worker.moveToThread(self._thread)
        self._thread.started.connect(self._worker.run)
        self._worker.progress.connect(self._on_progress)
        self._worker.finished.connect(self._on_finished)
        self._thread.start()
        self.refresh()
        self.changed.emit()

    def _on_progress(self, done: int, total: int, status: str) -> None:
        if total <= 0:
            self.bar.setRange(0, 0)
            self.bar.setFormat(status)
            return
        self.bar.setRange(0, 1000)
        self.bar.setValue(int(done / total * 1000))
        self.bar.setFormat(f"{_human(done)} of {_human(total)}  —  %p%")

    def _on_finished(self, ok: bool, message: str) -> None:
        thread, self._thread = self._thread, None
        self._worker = None
        if thread is not None:
            thread.quit()
            thread.wait(3000)

        self.bar.hide()
        self.refresh()
        self.changed.emit()

        if ok:
            self.radio.setChecked(True)
        elif message:
            QMessageBox.warning(
                self, "Download failed",
                f"{self.info.label} could not be downloaded.\n\n{message}",
            )

    def _on_delete(self) -> None:
        reply = QMessageBox.question(
            self, "Remove model",
            f"Delete the downloaded weights for {self.info.label}?\n"
            f"This frees {_human(self.info.disk_bytes(self.backend))}. "
            f"You can download it again later.",
        )
        if reply != QMessageBox.StandardButton.Yes:
            return
        delete_model(self.info, self.backend)
        self.set_checked(False)
        self.refresh()
        self.changed.emit()

    def stop(self) -> None:
        if self._worker is not None:
            self._worker.cancel.set()
        if self._thread is not None:
            self._thread.quit()
            self._thread.wait(2000)


class ModelsPage(QWidget):
    modelChosen = Signal(str)
    changed = Signal()

    def __init__(self, current: str, backend: str, engine_note: str, parent=None):
        super().__init__(parent)
        self.backend = backend
        layout = QVBoxLayout(self)
        layout.setContentsMargins(0, 0, 0, 0)
        layout.setSpacing(10)

        intro = QLabel(
            "Pick a speech model and download it once. Everything runs on this "
            "computer afterwards — no account, no subscription, and no audio "
            f"ever leaves the machine.<br><br>Running on <b>{engine_note}</b>."
        )
        intro.setWordWrap(True)
        intro.setObjectName("dim")
        layout.addWidget(intro)

        scroll = QScrollArea()
        scroll.setWidgetResizable(True)
        scroll.setFrameShape(QFrame.Shape.NoFrame)
        inner = QWidget()
        col = QVBoxLayout(inner)
        col.setContentsMargins(0, 0, 8, 0)
        col.setSpacing(8)

        self.rows: list[ModelRow] = []
        for info in models.for_backend(backend):
            row = ModelRow(info, backend)
            row.selected.connect(self._on_selected)
            row.changed.connect(self.changed)
            row.set_checked(info.key == current and info.downloaded(backend))
            col.addWidget(row)
            self.rows.append(row)
        col.addStretch(1)

        scroll.setWidget(inner)
        layout.addWidget(scroll, 1)

    def _on_selected(self, key: str) -> None:
        for row in self.rows:
            if row.info.key != key:
                row.set_checked(False)
        self.modelChosen.emit(key)

    def refresh(self) -> None:
        for row in self.rows:
            row.refresh()

    def stop_all(self) -> None:
        for row in self.rows:
            row.stop()
