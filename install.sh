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

# curl's own progress bar degrades to a bouncing marker when it cannot work
# out the total size, which reads as broken. Download silently in the
# background, ask for the size separately (the asset URL redirects; the last
# Content-Length is the real file), and draw our own single-line readout.
file_size() { stat -f%z "$1" 2>/dev/null || wc -c < "$1" | tr -d ' '; }
progress() {
  # $1 = bytes so far, $2 = total bytes (0 = unknown)
  awk -v got="$1" -v total="$2" 'BEGIN {
    if (total > 0) {
      pct = int(got * 100 / total); if (pct > 100) pct = 100
      filled = int(pct / 5); bar = ""
      for (i = 0; i < 20; i++) bar = bar (i < filled ? "\342\226\210" : "\342\226\221")
      printf "\r  %.1f MB / %.1f MB  %3d%%  [%s]", got/1048576, total/1048576, pct, bar
    } else {
      printf "\r  %.1f MB downloaded\342\200\246", got/1048576
    }
  }' >&2
}

curl -fsSL "$URL" -o "$TMP" &
DL_PID=$!
TOTAL=$(curl -sIL "$URL" 2>/dev/null | tr -d '\r' \
  | awk 'tolower($1)=="content-length:" {n=$2} END {print n}')
case "$TOTAL" in ''|*[!0-9]*) TOTAL=0 ;; esac
while kill -0 "$DL_PID" 2>/dev/null; do
  progress "$(file_size "$TMP" 2>/dev/null || echo 0)" "$TOTAL"
  sleep 0.2
done
if ! wait "$DL_PID"; then
  printf '\n' >&2
  rm -f "$TMP"
  echo "FlowLite: download failed ($URL)" >&2
  exit 1
fi
FINAL=$(file_size "$TMP")
[ "$TOTAL" -gt 0 ] || TOTAL=$FINAL
progress "$FINAL" "$TOTAL"
printf '\n' >&2
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
