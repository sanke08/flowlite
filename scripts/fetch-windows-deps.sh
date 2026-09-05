#!/usr/bin/env bash
# Fetch whisper.cpp's official Windows DLLs and matching headers into
# third_party/windows so `make windows` can cross-link from macOS.
#
# The Windows binaries are published under whisper.cpp's old "bNNNN"
# build-number tag, not the "vX.Y.Z" tag the Makefile's WCPP_TAG pins the
# macOS static build to — two different names that need to resolve to the
# same commit, or Windows silently ships an older whisper.cpp than macOS
# under the same FlowLite version number. Both tags live in
# third_party/whisper-version.mk so they get bumped together; this script
# also checks they actually agree before fetching anything.
set -euo pipefail
cd "$(dirname "$0")/.."

REPO=ggml-org/whisper.cpp
# shellcheck source=/dev/null
source third_party/whisper-version.mk
TAG="${1:-$WCPP_WIN_TAG}"

commit_of() {
  # Resolves a tag to the commit it points at. An annotated tag's ref points
  # at a tag object, not the commit, so follow that one extra hop; a
  # lightweight tag (which is what the "b" build tags are) points straight
  # at the commit already.
  local obj
  obj="$(gh api "repos/$REPO/git/refs/tags/$1" --jq '.object.sha,.object.type')"
  local sha=$(sed -n 1p <<<"$obj") type=$(sed -n 2p <<<"$obj")
  if [ "$type" = "tag" ]; then
    gh api "repos/$REPO/git/tags/$sha" --jq .object.sha
  else
    echo "$sha"
  fi
}

if winsha="$(commit_of "$TAG" 2>/dev/null)" && srcsha="$(commit_of "$WCPP_TAG" 2>/dev/null)"; then
  if [ "$winsha" != "$srcsha" ]; then
    echo "error: $TAG (Windows DLLs) and $WCPP_TAG (macOS source, from third_party/whisper-version.mk) are different whisper.cpp commits." >&2
    echo "       find the \"bNNNN\" release published alongside $WCPP_TAG and update WCPP_WIN_TAG to it." >&2
    exit 1
  fi
else
  echo "warning: could not verify $TAG matches $WCPP_TAG (gh api failed — rate limit or offline); proceeding anyway" >&2
fi

DIR=third_party/windows
mkdir -p "$DIR/lib" "$DIR/include"

echo "==> whisper.cpp $TAG: Windows x64 binaries"
gh release download "$TAG" -R "$REPO" -p "whisper-bin-x64.zip" -D "$DIR" --clobber
unzip -o -q "$DIR/whisper-bin-x64.zip" -d "$DIR/bin-x64"
find "$DIR/bin-x64" -iname "*.dll" -exec cp {} "$DIR/lib/" \;

echo "==> headers at the same tag"
for h in include/whisper.h ggml/include/ggml.h ggml/include/ggml-alloc.h \
         ggml/include/ggml-backend.h ggml/include/ggml-cpu.h; do
  curl -fsSL "https://raw.githubusercontent.com/$REPO/$TAG/$h" \
    -o "$DIR/include/$(basename "$h")"
done
echo "done: $(ls "$DIR/lib" | wc -l | tr -d ' ') DLLs, $(ls "$DIR/include" | wc -l | tr -d ' ') headers"
