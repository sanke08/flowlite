# -*- mode: python ; coding: utf-8 -*-
"""PyInstaller build spec for FlowLite.

Produces a self-contained app: the user installs it and never sees Python,
pip or a virtualenv. The only thing downloaded afterwards is the speech model
they pick on first launch.

Build with:  .venv/bin/pyinstaller FlowLite.spec --noconfirm
"""

import re
import sys
from pathlib import Path

from PyInstaller.utils.hooks import collect_dynamic_libs

IS_MAC = sys.platform == "darwin"
IS_WIN = sys.platform == "win32"

BUNDLE_ID = "com.flowlite.app"

# Single source of truth, so a release cannot ship mismatched version numbers.
VERSION = re.search(
    r'__version__ = "([^"]+)"', Path("flowlite/__init__.py").read_text()
).group(1)

# PySide6 ships 145 Qt frameworks; FlowLite uses three. QtWebEngineCore alone
# is an entire Chromium at 602 MB. Everything below is dead weight for a tray
# app with a tabbed settings window, and excluding it is the difference
# between a ~100 MB download and a 1.4 GB one.
QT_EXCLUDES = [
    "PySide6.QtWebEngineCore", "PySide6.QtWebEngineWidgets", "PySide6.QtWebEngineQuick",
    "PySide6.QtWebChannel", "PySide6.QtWebSockets", "PySide6.QtWebView",
    "PySide6.QtQuick", "PySide6.QtQuick3D", "PySide6.QtQuickWidgets", "PySide6.QtQml",
    "PySide6.QtQmlModels", "PySide6.QtQmlWorkerScript",
    "PySide6.Qt3DCore", "PySide6.Qt3DRender", "PySide6.Qt3DInput",
    "PySide6.Qt3DLogic", "PySide6.Qt3DAnimation", "PySide6.Qt3DExtras",
    "PySide6.QtCharts", "PySide6.QtDataVisualization", "PySide6.QtGraphs",
    "PySide6.QtMultimedia", "PySide6.QtMultimediaWidgets", "PySide6.QtSpatialAudio",
    "PySide6.QtPdf", "PySide6.QtPdfWidgets", "PySide6.QtPrintSupport",
    "PySide6.QtSql", "PySide6.QtTest", "PySide6.QtDesigner", "PySide6.QtUiTools",
    "PySide6.QtHelp", "PySide6.QtBluetooth", "PySide6.QtNfc", "PySide6.QtPositioning",
    "PySide6.QtLocation", "PySide6.QtSerialPort", "PySide6.QtSerialBus",
    "PySide6.QtRemoteObjects", "PySide6.QtScxml", "PySide6.QtSensors",
    "PySide6.QtStateMachine", "PySide6.QtTextToSpeech", "PySide6.QtHttpServer",
    "PySide6.QtOpenGL", "PySide6.QtOpenGLWidgets", "PySide6.QtSvgWidgets",
    "PySide6.QtXml", "PySide6.QtConcurrent", "PySide6.QtUiPlugin",
    "PySide6.scripts", "PySide6.support",
]

# faster-whisper is an optional extra, not part of the shipped bundle. Naming
# it here keeps its 138 MB dependency chain out even if it is installed in the
# build environment.
ENGINE_EXCLUDES = [
    "faster_whisper", "ctranslate2", "onnxruntime", "av", "tokenizers", "transformers",
]

EXCLUDES = QT_EXCLUDES + ENGINE_EXCLUDES + [
    "tkinter", "unittest", "pydoc", "doctest", "pytest", "setuptools", "pip",
    "IPython", "matplotlib", "pandas", "scipy", "PIL", "pygments",
]

# whisper.cpp's compiled libraries live in pywhispercpp/.dylibs (macOS) and are
# loaded through an rpath, which PyInstaller's linkage scan can miss.
binaries = collect_dynamic_libs("pywhispercpp")

a = Analysis(
    ["launcher.py"],
    pathex=[],
    binaries=binaries,
    datas=[],
    # _pywhispercpp is a top-level extension imported from inside
    # pywhispercpp.model, so static analysis does not always see it.
    hiddenimports=[
        "_pywhispercpp",
        "flowlite.backends.whispercpp",
        "flowlite.backends.ct2",
    ],
    hookspath=[],
    hooksconfig={},
    runtime_hooks=[],
    excludes=EXCLUDES,
    noarchive=False,
    optimize=0,
)

pyz = PYZ(a.pure)

exe = EXE(
    pyz,
    a.scripts,
    [],
    exclude_binaries=True,
    name="FlowLite",
    debug=False,
    bootloader_ignore_signals=False,
    strip=False,
    upx=False,
    console=False,          # no terminal window on Windows
    disable_windowed_traceback=False,
    argv_emulation=False,
    target_arch=None,
    codesign_identity=None,
    entitlements_file=None,
    icon=None,
)

coll = COLLECT(
    exe,
    a.binaries,
    a.datas,
    strip=False,
    upx=False,
    upx_exclude=[],
    name="FlowLite",
)

if IS_MAC:
    app = BUNDLE(
        coll,
        name="FlowLite.app",
        icon=None,
        bundle_identifier=BUNDLE_ID,
        version=VERSION,
        info_plist={
            "CFBundleName": "FlowLite",
            "CFBundleDisplayName": "FlowLite",
            "CFBundleShortVersionString": VERSION,
            "CFBundleVersion": VERSION,
            # Menu-bar app: no Dock icon, no app-switcher entry.
            "LSUIElement": True,
            # Required, or macOS kills the process the moment it opens the mic.
            "NSMicrophoneUsageDescription":
                "FlowLite transcribes your speech on this device. Audio is held "
                "in memory and never leaves your computer.",
            "LSMinimumSystemVersion": "11.0",
            "NSHighResolutionCapable": True,
        },
    )
