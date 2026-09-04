#!/usr/bin/env bash
# Double-click this after dragging FlowLite to Applications.
#
# FlowLite is not signed with a paid Apple Developer certificate, so macOS
# quarantines it on download and refuses to launch it — usually claiming the
# app is "damaged", which it is not. This removes the quarantine flag.
#
# You can do the same thing by hand:
#     xattr -cr /Applications/FlowLite.app
set -euo pipefail

APP="/Applications/FlowLite.app"

echo "FlowLite — clearing macOS quarantine"
echo

if [ ! -d "$APP" ]; then
  echo "FlowLite is not in your Applications folder yet."
  echo "Drag FlowLite onto the Applications shortcut first, then run this again."
  echo
  read -n 1 -s -r -p "Press any key to close…"
  exit 1
fi

xattr -cr "$APP"
echo "Done. FlowLite will now open normally."
echo "Open it from Applications or Spotlight — it lives in the menu bar."
echo
read -n 1 -s -r -p "Press any key to close…"
