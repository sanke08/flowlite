#!/usr/bin/env bash
# Launch FlowLite from source (macOS / Linux).
set -euo pipefail
cd "$(dirname "$0")"

if [ ! -d .venv ]; then
  echo "Setting up for the first time…"
  if command -v uv >/dev/null 2>&1; then
    uv venv --python 3.12 && uv pip install -e .
  else
    python3 -m venv .venv && .venv/bin/pip install -e .
  fi
fi

exec .venv/bin/python -m flowlite "$@"
