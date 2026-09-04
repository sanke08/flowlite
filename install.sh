#!/bin/sh
# FlowLite installer — one line, no Homebrew, no Python:
#   curl -fsSL https://raw.githubusercontent.com/sanke08/flowlite/main/install.sh | sh
#
# Downloading with curl (rather than a browser) means macOS does not attach a
# quarantine flag, so Gatekeeper never gets in the way.
set -e
REPO="sanke08/flowlite"

case "$(uname -s)-$(uname -m)" in
  Darwin-arm64) ASSET="macos-arm64" ;;
  *) echo "FlowLite's one-line installer currently supports Apple Silicon Macs."; echo "Windows: download the zip from https://github.com/$REPO/releases"; exit 1 ;;
esac

echo "FlowLite: finding the latest release…"
URL=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
  | grep '"browser_download_url"' | grep "$ASSET" | head -1 | sed 's/.*"\(https[^"]*\)".*/\1/')
[ -n "$URL" ] || { echo "no release asset found for $ASSET"; exit 1; }

TMP=$(mktemp -t flowlite)
echo "FlowLite: downloading $(basename "$URL")…"
curl -fL --progress-bar "$URL" -o "$TMP"
chmod +x "$TMP"

"$TMP" install
rm -f "$TMP"

echo
# When piped through `sh`, stdin is the script itself, not the keyboard; the
# interactive wizard needs the real terminal. FLOWLITE_NO_SETUP=1 skips it.
if [ -n "${FLOWLITE_NO_SETUP:-}" ]; then
  echo "Installed. Next:  flowlite setup"
elif command -v flowlite >/dev/null 2>&1 && [ -r /dev/tty ]; then
  exec flowlite setup < /dev/tty
else
  echo "Open a new terminal and run:  flowlite setup"
fi
