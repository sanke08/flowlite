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

.PHONY: all build test install uninstall clean deps windows whisper-static release

all: build

deps:            ## Install native dependencies (macOS)
	brew list whisper-cpp >/dev/null 2>&1 || brew install whisper-cpp

build:           ## Build ./bin/flowlite
	@mkdir -p bin
	CGO_ENABLED=1 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/flowlite
	@echo "built bin/$(BINARY) ($(VERSION))"

test:            ## Unit tests (pure Go; no mic, model or permissions needed)
	go test ./...

install: release ## Install the self-contained binary to $(PREFIX)/bin
	install -d $(PREFIX)/bin
	install -m 0755 $(RELEASE) $(PREFIX)/bin/$(BINARY)
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
	@cd dist && rm -f $(BINARY)-$(VERSION)-windows-x64.zip && zip -q -r $(BINARY)-$(VERSION)-windows-x64.zip windows
	@echo "linked dist/$(BINARY)-$(VERSION)-windows-x64.zip (unverified at runtime)"

# ---- self-contained release binary ------------------------------------------
# Statically links whisper.cpp + ggml (Metal library embedded) so the result is
# ONE file that runs on any Apple Silicon Mac with nothing installed.
WCPP     := third_party/whisper.cpp
WCPP_TAG := v1.9.3
# Space-free path for cgo (the project dir may contain spaces). No inline
# comment here: make keeps the whitespace before a '#'.
WCPP_LNK := $(HOME)/.cache/flowlite-whisper
STATIC_LIBS = $(WCPP_LNK)/build/src/libwhisper.a \
              $(shell find $(WCPP)/build -name libggml.a       | head -1 | sed 's|^$(WCPP)|$(WCPP_LNK)|') \
              $(shell find $(WCPP)/build -name libggml-metal.a | head -1 | sed 's|^$(WCPP)|$(WCPP_LNK)|') \
              $(shell find $(WCPP)/build -name libggml-blas.a  | head -1 | sed 's|^$(WCPP)|$(WCPP_LNK)|') \
              $(shell find $(WCPP)/build -name libggml-cpu.a   | head -1 | sed 's|^$(WCPP)|$(WCPP_LNK)|') \
              $(shell find $(WCPP)/build -name libggml-base.a  | head -1 | sed 's|^$(WCPP)|$(WCPP_LNK)|')
RELEASE  := dist/flowlite-$(VERSION)-macos-arm64

whisper-static:   ## Build whisper.cpp as static libs with Metal (needs cmake)
	@test -d $(WCPP) || git clone --depth 1 --branch $(WCPP_TAG) https://github.com/ggml-org/whisper.cpp $(WCPP)
	@test -f $(WCPP)/build/src/libwhisper.a || ( \
	  cmake -S $(WCPP) -B $(WCPP)/build -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF \
	    -DGGML_METAL=ON -DGGML_METAL_EMBED_LIBRARY=ON -DGGML_BLAS=ON -DGGML_BLAS_VENDOR=Apple \
	    -DGGML_NATIVE=OFF -DWHISPER_BUILD_EXAMPLES=OFF -DWHISPER_BUILD_TESTS=OFF \
	    -DWHISPER_BUILD_SERVER=OFF -DWHISPER_SDL2=OFF -DGGML_CCACHE=OFF && \
	  cmake --build $(WCPP)/build -j )

release: whisper-static   ## One shareable file: dist/flowlite-<version>-macos-arm64
	@mkdir -p $(HOME)/.cache dist
	@ln -sfn "$(CURDIR)/$(WCPP)" $(WCPP_LNK)
	CGO_ENABLED=1 \
	CGO_CFLAGS="-I$(WCPP_LNK)/include -I$(WCPP_LNK)/ggml/include" \
	CGO_LDFLAGS="$(STATIC_LIBS) -framework Metal -framework MetalKit -framework Foundation -framework Accelerate -lc++" \
	go build -tags static -trimpath -ldflags '$(LDFLAGS)' -o $(RELEASE) ./cmd/flowlite
	@codesign --force --sign - $(RELEASE) 2>/dev/null || true
	@echo "release: $(RELEASE) ($$(du -h $(RELEASE) | cut -f1)) — depends only on macOS system libraries:"
	@otool -L $(RELEASE) | grep -vE "^dist|/usr/lib/|/System/" | sed 's/^/  UNEXPECTED: /' || true
