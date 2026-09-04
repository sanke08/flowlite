#!/usr/bin/env bash
# One-time setup to get the Windows installer built.
#
# Nothing here costs money. GitHub Actions is free with no usage limit on
# public repositories, and this workflow publishes nothing — it builds the
# installers and hands them back to you, into app/.
#
# GitHub refuses to let the gh CLI add a workflow file unless the token has
# the "workflow" scope, so the first step opens your browser once to grant it.
set -euo pipefail
cd "$(dirname "$0")"

VERSION="$(sed -n 's/^__version__ = "\(.*\)"/\1/p' flowlite/__init__.py)"
OUT="app/v${VERSION}"
WF=".github/workflows/release.yml"

command -v gh >/dev/null || { echo "GitHub CLI (gh) is not installed." >&2; exit 1; }

echo "==> 1/4  Granting the 'workflow' scope"
echo "    Your browser will open. Paste the code it shows, then come back here."
echo
gh auth refresh -s workflow

echo
echo "==> 2/4  Pushing the build pipeline"
if git diff --quiet HEAD -- "$WF" 2>/dev/null && git ls-files --error-unmatch "$WF" >/dev/null 2>&1; then
  echo "    already pushed"
else
  git add "$WF"
  git commit -q -m "Add CI to build the macOS and Windows installers"
  git push -q
  echo "    pushed"
fi

echo
echo "==> 3/4  Waiting for GitHub to build (about 5-10 minutes)"
sleep 5
RUN_ID="$(gh run list --workflow="Build installers" --limit 1 --json databaseId -q '.[0].databaseId')"
if [ -z "$RUN_ID" ]; then
  echo "    No run found yet. Start one with:  gh run watch"
  exit 0
fi
gh run watch "$RUN_ID" --exit-status || {
  echo
  echo "The build failed. See what happened with:"
  echo "    gh run view $RUN_ID --log-failed"
  exit 1
}

echo
echo "==> 4/4  Downloading the installers into ${OUT}/"
mkdir -p "$OUT"
TMP="$(mktemp -d)"
gh run download "$RUN_ID" --dir "$TMP"
find "$TMP" -type f \( -name "*.zip" -o -name "*.dmg" \) -exec mv -f {} "$OUT"/ \;
rm -rf "$TMP"

echo
echo "Done. Files you can share:"
ls -1sh "$OUT" | tail -n +2 | sed 's/^/  /'
