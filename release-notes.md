Local, free speech-to-text. Tap a key, speak, the words land at your cursor. Everything runs on your machine.

**What changed:** see [CHANGELOG.md](https://github.com/sanke08/flowlite/blob/main/CHANGELOG.md).

## macOS (Apple Silicon) — one line, nothing else to install

    curl -fsSL https://raw.githubusercontent.com/sanke08/flowlite/main/install.sh | sh

Downloading the file by hand instead? macOS quarantines browser downloads of unsigned binaries and silently refuses to run them. Clear it once:

    xattr -d com.apple.quarantine ~/Downloads/flowlite-*-macos-arm64 && ~/Downloads/flowlite-*-macos-arm64

The binary then walks you through the keyboard permission, the model download, and installing itself.

## Windows x64 — experimental

`flowlite-<version>-windows-x64.zip`: unzip anywhere, run `flowlite.exe`. Cross-compiled and linked against the official whisper.cpp DLLs but **not yet run on Windows**. Reports welcome.
