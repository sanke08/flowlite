"""Floating recording indicator.

Critically, this window must never take focus: the whole point of the app is
that text lands in whatever field the user was already typing in.
"""

import math

from PySide6.QtCore import Qt, QTimer
from PySide6.QtGui import (
    QColor, QCursor, QFont, QGuiApplication, QPainter, QPainterPath, QPen,
)
from PySide6.QtWidgets import QWidget

BAR_COUNT = 13
WIDTH = 208
HEIGHT = 54


class Overlay(QWidget):
    def __init__(self):
        super().__init__(
            None,
            Qt.WindowType.FramelessWindowHint
            | Qt.WindowType.WindowStaysOnTopHint
            | Qt.WindowType.Tool
            | Qt.WindowType.WindowDoesNotAcceptFocus,
        )
        self.setAttribute(Qt.WidgetAttribute.WA_TranslucentBackground)
        self.setAttribute(Qt.WidgetAttribute.WA_ShowWithoutActivating)
        self.setFixedSize(WIDTH, HEIGHT)

        self._levels = [0.04] * BAR_COUNT
        self._label = "Listening"
        self._duration = 0.0
        self._busy = False
        self._phase = 0.0

        self._spin = QTimer(self)
        self._spin.setInterval(33)
        self._spin.timeout.connect(self._advance)

    # -- placement ----------------------------------------------------------

    def reposition(self) -> None:
        # Prefer the screen holding the cursor, so the pill follows the user
        # across a multi-monitor setup. Falls back to the primary screen,
        # since self.screen() is unreliable before the first show().
        app = QGuiApplication.instance()
        screen = QGuiApplication.screenAt(QCursor.pos()) or app.primaryScreen()
        if screen is None:
            return
        area = screen.availableGeometry()
        self.move(
            area.center().x() - self.width() // 2,
            area.bottom() - self.height() - 72,
        )

    # -- state --------------------------------------------------------------

    def show_recording(self) -> None:
        self._busy = False
        self._label = "Listening"
        self._levels = [0.04] * BAR_COUNT
        self._duration = 0.0
        self.reposition()
        self.show()
        self.raise_()
        self._spin.start()

    def show_transcribing(self) -> None:
        self._busy = True
        self._label = "Transcribing"
        self.update()

    def push_level(self, level: float) -> None:
        if self._busy:
            return
        self._levels = self._levels[1:] + [max(0.04, min(level, 1.0))]
        self.update()

    def set_duration(self, seconds: float) -> None:
        self._duration = seconds

    def _advance(self) -> None:
        self._phase += 0.14
        if self._busy:
            self.update()

    def hide_overlay(self) -> None:
        self._spin.stop()
        self.hide()

    # -- painting -----------------------------------------------------------

    def paintEvent(self, event):  # noqa: ARG002
        p = QPainter(self)
        p.setRenderHint(QPainter.RenderHint.Antialiasing)

        path = QPainterPath()
        path.addRoundedRect(self.rect().adjusted(1, 1, -1, -1), 16, 16)
        p.fillPath(path, QColor(22, 22, 26, 235))
        p.setPen(QPen(QColor(255, 255, 255, 28), 1))
        p.drawPath(path)

        accent = QColor(255, 92, 92) if not self._busy else QColor(120, 170, 255)

        # Status dot: steady while recording, pulsing while transcribing.
        alpha = 255
        if self._busy:
            alpha = int(120 + 110 * (0.5 + 0.5 * math.sin(self._phase * 1.8)))
        dot = QColor(accent)
        dot.setAlpha(alpha)
        p.setPen(Qt.PenStyle.NoPen)
        p.setBrush(dot)
        p.drawEllipse(16, HEIGHT // 2 - 4, 8, 8)

        p.setPen(QColor(238, 238, 242))
        font = QFont(self.font())
        font.setPointSize(11)
        font.setWeight(QFont.Weight.Medium)
        p.setFont(font)
        p.drawText(32, HEIGHT // 2 + 4, self._label)

        if self._busy:
            return

        # Level meter, newest sample on the right.
        p.setPen(Qt.PenStyle.NoPen)
        p.setBrush(QColor(255, 255, 255, 205))
        right = WIDTH - 16
        bar_w, gap = 3, 4
        span = BAR_COUNT * (bar_w + gap) - gap
        x = right - span
        mid = HEIGHT / 2
        for lv in self._levels:
            h = max(3.0, lv * (HEIGHT - 24))
            p.drawRoundedRect(int(x), int(mid - h / 2), bar_w, int(h), 1.5, 1.5)
            x += bar_w + gap
