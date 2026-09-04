"""A single, forced dark theme.

FlowLite does not follow the system appearance. Qt's default palette roles
collapse to near-unreadable greys in dark mode — `palette(mid)` in particular
lands almost on top of the window background — so every colour here is stated
explicitly rather than inherited.
"""

import sys

from PySide6.QtCore import Qt
from PySide6.QtGui import QColor, QPalette

# --- palette ---------------------------------------------------------------

BG = "#1b1b1f"            # window
SURFACE = "#242429"       # cards, inputs
SURFACE_HI = "#2c2c33"    # hover
BORDER = "#3a3a44"
BORDER_HI = "#4d4d59"

TEXT = "#f2f2f5"          # primary
TEXT_DIM = "#a8a8b6"      # secondary — still comfortably readable
TEXT_FAINT = "#7d7d8c"    # disabled

ACCENT = "#4a8cff"
ACCENT_HI = "#5f9bff"
ACCENT_TEXT = "#ffffff"

DANGER = "#ff5c5c"
SUCCESS = "#3ecf8e"
WARNING = "#e0a44a"


def _qpalette() -> QPalette:
    """Explicit palette so nothing falls back to the system appearance."""
    p = QPalette()
    c = QColor

    p.setColor(QPalette.ColorRole.Window, c(BG))
    p.setColor(QPalette.ColorRole.WindowText, c(TEXT))
    p.setColor(QPalette.ColorRole.Base, c(SURFACE))
    p.setColor(QPalette.ColorRole.AlternateBase, c(SURFACE_HI))
    p.setColor(QPalette.ColorRole.Text, c(TEXT))
    p.setColor(QPalette.ColorRole.Button, c(SURFACE))
    p.setColor(QPalette.ColorRole.ButtonText, c(TEXT))
    p.setColor(QPalette.ColorRole.BrightText, c(DANGER))
    p.setColor(QPalette.ColorRole.Highlight, c(ACCENT))
    p.setColor(QPalette.ColorRole.HighlightedText, c(ACCENT_TEXT))
    p.setColor(QPalette.ColorRole.ToolTipBase, c(SURFACE))
    p.setColor(QPalette.ColorRole.ToolTipText, c(TEXT))
    p.setColor(QPalette.ColorRole.PlaceholderText, c(TEXT_FAINT))
    p.setColor(QPalette.ColorRole.Link, c(ACCENT))

    for role in (
        QPalette.ColorRole.WindowText, QPalette.ColorRole.Text,
        QPalette.ColorRole.ButtonText,
    ):
        p.setColor(QPalette.ColorGroup.Disabled, role, c(TEXT_FAINT))

    return p


STYLESHEET = f"""
QWidget {{
    background: {BG};
    color: {TEXT};
    font-size: 13px;
}}

/* ---- text roles ---- */
QLabel {{ background: transparent; color: {TEXT}; }}
QLabel#title {{ font-size: 22px; font-weight: 700; color: {TEXT}; }}
QLabel#dim   {{ color: {TEXT_DIM}; }}
QLabel#tag {{
    background: {ACCENT}; color: {ACCENT_TEXT};
    border-radius: 8px; padding: 2px 9px;
    font-size: 10px; font-weight: 700;
}}

/* ---- model rows ---- */
QFrame#modelRow {{
    background: {SURFACE};
    border: 1px solid {BORDER};
    border-radius: 10px;
}}
QFrame#modelRowActive {{
    background: {SURFACE};
    border: 1px solid {ACCENT};
    border-radius: 10px;
}}

/* ---- status banners ---- */
QFrame#banner {{
    background: rgba(224, 164, 74, 0.14);
    border: 1px solid {WARNING};
    border-radius: 10px;
}}
QFrame#ok {{
    background: rgba(62, 207, 142, 0.14);
    border: 1px solid {SUCCESS};
    border-radius: 10px;
}}

/* ---- buttons ---- */
QPushButton {{
    background: {SURFACE_HI};
    color: {TEXT};
    border: 1px solid {BORDER};
    border-radius: 7px;
    padding: 6px 14px;
}}
QPushButton:hover  {{ background: {BORDER}; border-color: {BORDER_HI}; }}
QPushButton:pressed {{ background: {BORDER_HI}; }}
QPushButton:disabled {{
    background: {SURFACE}; color: {TEXT_FAINT}; border-color: {BORDER};
}}
QPushButton#primary {{
    background: {ACCENT}; color: {ACCENT_TEXT}; border: none; font-weight: 600;
}}
QPushButton#primary:hover {{ background: {ACCENT_HI}; }}

/* ---- radio ---- */
QRadioButton {{ background: transparent; color: {TEXT}; spacing: 8px; }}
QRadioButton:disabled {{ color: {TEXT_DIM}; }}
QRadioButton::indicator {{
    width: 14px; height: 14px;
    border-radius: 9px;
    border: 2px solid {BORDER_HI};
    background: {BG};
}}
/* A thick border defeats border-radius and renders as a rounded square, so
   the inner dot is painted with a radial gradient instead. */
QRadioButton::indicator:checked {{
    border: 2px solid {ACCENT};
    background: qradialgradient(cx:0.5, cy:0.5, radius:0.5, fx:0.5, fy:0.5,
        stop:0 {ACCENT}, stop:0.5 {ACCENT},
        stop:0.56 rgba(0,0,0,0), stop:1 rgba(0,0,0,0));
}}
QRadioButton::indicator:hover {{ border-color: {ACCENT}; }}
QRadioButton::indicator:disabled {{ border-color: {BORDER}; }}

/* ---- checkbox ---- */
QCheckBox {{ background: transparent; color: {TEXT}; spacing: 8px; }}
QCheckBox::indicator {{
    width: 15px; height: 15px;
    border-radius: 4px;
    border: 2px solid {BORDER_HI};
    background: {BG};
}}
QCheckBox::indicator:checked {{
    background: {ACCENT}; border-color: {ACCENT};
}}
QCheckBox::indicator:hover {{ border-color: {ACCENT}; }}

/* ---- combo ---- */
QComboBox {{
    background: {SURFACE_HI};
    border: 1px solid {BORDER};
    border-radius: 7px;
    padding: 6px 10px;
    color: {TEXT};
    min-height: 20px;
}}
QComboBox:hover {{ border-color: {BORDER_HI}; }}
QComboBox::drop-down {{ border: none; width: 22px; }}
QComboBox::down-arrow {{
    image: none;
    border-left: 4px solid transparent;
    border-right: 4px solid transparent;
    border-top: 5px solid {TEXT_DIM};
    margin-right: 8px;
}}
QComboBox QAbstractItemView {{
    background: {SURFACE};
    color: {TEXT};
    border: 1px solid {BORDER};
    border-radius: 7px;
    selection-background-color: {ACCENT};
    selection-color: {ACCENT_TEXT};
    outline: none;
    padding: 4px;
}}

/* ---- tabs ---- */
QTabWidget::pane {{
    border: 1px solid {BORDER};
    border-radius: 10px;
    background: {BG};
    top: -1px;
    padding: 6px;
}}
QTabBar {{ background: transparent; qproperty-drawBase: 0; }}
QTabBar::tab {{
    background: transparent;
    color: {TEXT_DIM};
    border: 1px solid transparent;
    border-top-left-radius: 8px; border-top-right-radius: 8px;
    padding: 8px 18px;
    margin-right: 2px;
}}
QTabBar::tab:hover {{ color: {TEXT}; }}
QTabBar::tab:selected {{
    background: {BG};
    color: {TEXT};
    border-color: {BORDER};
    border-bottom-color: {BG};
    font-weight: 600;
}}

/* ---- slider ---- */
QSlider::groove:horizontal {{
    height: 4px; border-radius: 2px; background: {BORDER};
}}
QSlider::sub-page:horizontal {{ background: {ACCENT}; border-radius: 2px; }}
QSlider::handle:horizontal {{
    width: 16px; height: 16px; margin: -7px 0;
    border-radius: 8px; background: {TEXT};
}}
QSlider::handle:horizontal:hover {{ background: #ffffff; }}

/* ---- progress ---- */
QProgressBar {{
    background: {BG};
    border: 1px solid {BORDER};
    border-radius: 7px;
    text-align: center;
    color: {TEXT};
    font-size: 11px;
}}
QProgressBar::chunk {{ background: {ACCENT}; border-radius: 6px; }}

/* ---- scrolling ---- */
QScrollArea {{ background: transparent; border: none; }}
QScrollArea > QWidget > QWidget {{ background: transparent; }}
QScrollBar:vertical {{
    background: transparent; width: 10px; margin: 2px;
}}
QScrollBar::handle:vertical {{
    background: {BORDER_HI}; border-radius: 5px; min-height: 30px;
}}
QScrollBar::handle:vertical:hover {{ background: {TEXT_FAINT}; }}
QScrollBar::add-line, QScrollBar::sub-line {{ height: 0; }}
QScrollBar::add-page, QScrollBar::sub-page {{ background: transparent; }}

/* ---- dialogs ---- */
QMessageBox {{ background: {BG}; }}
QMessageBox QLabel {{ color: {TEXT}; }}
QToolTip {{
    background: {SURFACE}; color: {TEXT};
    border: 1px solid {BORDER}; padding: 4px 8px; border-radius: 5px;
}}
"""


def apply(app) -> None:
    """Force the dark theme, ignoring whatever the OS is set to."""
    # Fusion is the only style that honours a custom palette consistently on
    # both macOS and Windows; the native styles override large parts of it.
    app.setStyle("Fusion")
    app.setPalette(_qpalette())
    app.setStyleSheet(STYLESHEET)

    if sys.platform == "darwin":
        # Without this the window's title bar stays light while its contents
        # are dark, which looks broken.
        try:
            from AppKit import NSApp, NSAppearance

            NSApp.setAppearance_(
                NSAppearance.appearanceNamed_("NSAppearanceNameDarkAqua")
            )
        except Exception:
            pass
