//go:build darwin && !static

package whisper

// Default developer build: link against Homebrew's whisper-cpp and ggml.
// `go build -tags static` drops these and expects CGO_CFLAGS/CGO_LDFLAGS to
// point at a static whisper.cpp build instead (see `make release`).

/*
#cgo CFLAGS:  -I/opt/homebrew/opt/whisper-cpp/include -I/opt/homebrew/opt/ggml/include
#cgo LDFLAGS: -L/opt/homebrew/opt/whisper-cpp/lib -L/opt/homebrew/opt/ggml/lib -lwhisper -lggml -lggml-base
*/
import "C"
