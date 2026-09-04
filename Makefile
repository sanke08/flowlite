# FlowLite — build, test, install.
#
# whisper.cpp comes from Homebrew on macOS and is found through pkg-config,
# so a plain `go build` works once `brew install whisper-cpp` has run.

BINARY   := flowlite
MODULE   := github.com/sanke08/flowlite
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X $(MODULE)/internal/cli.Version=$(VERSION)
# Homebrew's bin is already on PATH and user-writable on Apple Silicon, so
# `make install` needs no sudo there.
PREFIX   ?= $(shell test -w /opt/homebrew/bin && echo /opt/homebrew || echo /usr/local)

.PHONY: all build test install uninstall clean deps windows

all: build

deps:            ## Install native dependencies (macOS)
	brew list whisper-cpp >/dev/null 2>&1 || brew install whisper-cpp

build:           ## Build ./bin/flowlite
	@mkdir -p bin
	CGO_ENABLED=1 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/flowlite
	@echo "built bin/$(BINARY) ($(VERSION))"

test:            ## Unit tests (pure Go; no mic, model or permissions needed)
	go test ./...

install: build   ## Copy to $(PREFIX)/bin
	install -d $(PREFIX)/bin
	install -m 0755 bin/$(BINARY) $(PREFIX)/bin/$(BINARY)
	@echo "installed $(PREFIX)/bin/$(BINARY) — run: flowlite setup"

uninstall:
	rm -f $(PREFIX)/bin/$(BINARY)

clean:
	rm -rf bin dist

# Cross-link a Windows binary from macOS to prove the Windows code compiles
# and links. It cannot be run here. Needs: brew install mingw-w64, and the
# whisper.cpp release DLLs + headers under third_party/windows (see README).
WINDIR  := third_party/windows
# cgo cannot cope with spaces in -I/-L paths (it splits CGO_*FLAGS on
# whitespace and ignores quoting), so the deps are reached through a symlink
# at a space-free location.
WINLINK := $(HOME)/.cache/flowlite-windeps
windows:
	@test -d $(WINDIR)/include || { echo "missing $(WINDIR)/include — see README 'Windows build'"; exit 1; }
	@mkdir -p $(HOME)/.cache dist/windows
	@ln -sfn "$(CURDIR)/$(WINDIR)" $(WINLINK)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 \
	CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
	CGO_CFLAGS="-I$(WINLINK)/include" \
	CGO_LDFLAGS="-L$(WINLINK)/lib -lwhisper -lggml -lggml-base" \
	go build -trimpath -ldflags '$(LDFLAGS)' -o dist/windows/$(BINARY).exe ./cmd/flowlite
	cp $(WINDIR)/lib/whisper.dll $(WINDIR)/lib/ggml*.dll dist/windows/
	@echo "linked dist/windows/$(BINARY).exe (unverified at runtime)"
