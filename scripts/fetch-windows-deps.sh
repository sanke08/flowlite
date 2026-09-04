#!/usr/bin/env bash
# Fetch whisper.cpp's official Windows DLLs and matching headers into
# third_party/windows so `make windows` can cross-link from macOS.
set -euo pipefail
cd "$(dirname "$0")/.."

TAG="${1:-b4938}"
DIR=third_party/windows
mkdir -p "$DIR/lib" "$DIR/include"

echo "==> whisper.cpp $TAG: Windows x64 binaries"
gh release download "$TAG" -R ggml-org/whisper.cpp -p "whisper-bin-x64.zip" -D "$DIR" --clobber
unzip -o -q "$DIR/whisper-bin-x64.zip" -d "$DIR/bin-x64"
find "$DIR/bin-x64" -iname "*.dll" -exec cp {} "$DIR/lib/" \;

echo "==> headers at the same tag"
for h in include/whisper.h ggml/include/ggml.h ggml/include/ggml-alloc.h \
         ggml/include/ggml-backend.h ggml/include/ggml-cpu.h; do
  curl -fsSL "https://raw.githubusercontent.com/ggml-org/whisper.cpp/$TAG/$h" \
    -o "$DIR/include/$(basename "$h")"
done
echo "done: $(ls "$DIR/lib" | wc -l | tr -d ' ') DLLs, $(ls "$DIR/include" | wc -l | tr -d ' ') headers"
