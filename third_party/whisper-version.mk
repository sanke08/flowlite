# The one place both whisper.cpp pins live. Read by the Makefile (as Make
# syntax) and by scripts/fetch-windows-deps.sh (sourced as shell), so the
# macOS static build and the Windows DLL fetch can never drift apart without
# someone touching this file — no spaces around '=' so both parsers agree.
#
# WCPP_TAG is the source tag the macOS build compiles from (make
# whisper-static / make release). WCPP_WIN_TAG is whisper.cpp's separate
# "bNNNN" build-number tag that carries prebuilt Windows DLLs for a release —
# a different name for what must be the same commit. fetch-windows-deps.sh
# checks that at fetch time; update both together, verify the script reports
# a match, and only then bump WCPP_TAG here.
WCPP_TAG=v1.9.3
WCPP_WIN_TAG=b4938
