"""Icons drawn at runtime, so the app ships without binary assets."""

from PySide6.QtCore import QRectF, Qt
from PySide6.QtGui import QColor, QIcon, QPainter, QPixmap

SIZE = 64


def _mic_pixmap(color: QColor, filled: bool = True) -> QPixmap:
    pm = QPixmap(SIZE, SIZE)
    pm.fill(Qt.GlobalColor.transparent)
    p = QPainter(pm)
    p.setRenderHint(QPainter.RenderHint.Antialiasing)

    pen_w = 5
    p.setPen(Qt.PenStyle.NoPen)
    p.setBrush(color if filled else Qt.BrushStyle.NoBrush)

    # Capsule
    capsule = QRectF(23, 11, 18, 30)
    if filled:
        p.drawRoundedRect(capsule, 9, 9)
    else:
        p.setPen(color)
        p.setBrush(Qt.BrushStyle.NoBrush)
        p.drawRoundedRect(capsule, 9, 9)

    # Cradle arc + stand
    pen = p.pen()
    pen.setColor(color)
    pen.setWidth(pen_w)
    pen.setCapStyle(Qt.PenCapStyle.RoundCap)
    p.setPen(pen)
    p.setBrush(Qt.BrushStyle.NoBrush)
    p.drawArc(QRectF(15, 22, 34, 32), 0, -180 * 16)
    p.drawLine(32, 48, 32, 55)
    p.end()
    return pm


def tray_icon(active: bool = False) -> QIcon:
    """Monochrome on macOS so the menu bar tints it for light/dark."""
    color = QColor(255, 92, 92) if active else QColor(0, 0, 0)
    icon = QIcon(_mic_pixmap(color))
    if not active:
        icon.setIsMask(True)
    return icon


def app_icon() -> QIcon:
    return QIcon(_mic_pixmap(QColor(64, 110, 240)))
