#!/bin/sh
# Build transcribe.cpp (Moonshine) as ONE static archive whose only global
# symbols are transcribe_* — its vendored, patched ggml is folded in and
# made private, so it links next to whisper.cpp's own ggml (static in a
# release, Homebrew's dylibs in a dev build) without a single clash.
#
#   scripts/build-transcribe.sh <dir>
#
# <dir> must have no spaces (cgo splits its flags on whitespace). Leaves:
#   <dir>/src/include                     headers
#   <dir>/private/libtranscribe_private.a the archive to link
#   <dir>/private/tag                     the TCPP_TAG it was built from
set -eu
here=$(cd "$(dirname "$0")/.." && pwd)
. "$here/third_party/transcribe-version.mk"
dir=${1:?usage: build-transcribe.sh <dir>}
case "$dir" in *" "*) echo "build-transcribe: $dir contains a space; cgo cannot use it" >&2; exit 1;; esac
mkdir -p "$dir"
cd "$dir"

if [ "$(cat private/tag 2>/dev/null)" = "$TCPP_TAG" ] && [ -f private/libtranscribe_private.a ]; then
  echo "transcribe.cpp $TCPP_TAG already built in $dir"
  exit 0
fi
if [ -d src ] && [ "$(git -C src describe --tags --exact-match 2>/dev/null)" != "$TCPP_TAG" ]; then
  rm -rf src build-static
fi
test -d src || git clone --depth 1 --branch "$TCPP_TAG" https://github.com/handy-computer/transcribe.cpp src

# -fno-common matters: without it ggml's lookup tables stay "common"
# symbols, which an export list cannot hide and the linker would silently
# merge with whisper.cpp's tables of the same name.
cmake -S src -B build-static -DCMAKE_BUILD_TYPE=Release \
  -DTRANSCRIBE_BUILD_SHARED=OFF -DBUILD_SHARED_LIBS=OFF \
  -DTRANSCRIBE_BUILD_TESTS=OFF -DTRANSCRIBE_BUILD_EXAMPLES=OFF -DTRANSCRIBE_BUILD_TOOLS=OFF \
  -DTRANSCRIBE_METAL=ON -DGGML_METAL_EMBED_LIBRARY=ON -DGGML_NATIVE=OFF -DGGML_CCACHE=OFF \
  -DCMAKE_OSX_DEPLOYMENT_TARGET=13.0 -DCMAKE_OSX_ARCHITECTURES=arm64 \
  -DCMAKE_C_FLAGS=-fno-common -DCMAKE_CXX_FLAGS=-fno-common
cmake --build build-static -j

B=build-static
mkdir -p private
echo '_transcribe_*' > private/exports.txt
ld -r -arch arm64 -platform_version macos 13.0 13.0 -o private/transcribe_private.o \
  -exported_symbols_list private/exports.txt \
  -force_load $B/src/libtranscribe.a -force_load $B/ggml/src/libggml.a \
  -force_load $B/ggml/src/libggml-cpu.a -force_load $B/ggml/src/ggml-metal/libggml-metal.a \
  -force_load $B/ggml/src/libggml-base.a
rm -f private/libtranscribe_private.a
ar rcs private/libtranscribe_private.a private/transcribe_private.o
bad=$(nm -gU private/libtranscribe_private.a | awk 'NF>=3{print $3}' | grep -vc '^_transcribe_' || true)
test "$bad" = 0 || { echo "build-transcribe: $bad non-transcribe globals leaked" >&2; exit 1; }
echo "$TCPP_TAG" > private/tag
echo "transcribe.cpp $TCPP_TAG built: $dir/private/libtranscribe_private.a"
