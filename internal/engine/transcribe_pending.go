//go:build !(darwin && moonshine)

package engine

import "errors"

// Builds without the `moonshine` tag (a plain `go build`, and Windows for
// now) leave transcribe.cpp out; `make build` and `make release` put it in.
func loadTranscribe(path string) (Engine, error) {
	return nil, errors.New("this FlowLite build has no Moonshine support — choose a Whisper model, or build with `make`")
}

// MoonshineVersion is "" when transcribe.cpp is not compiled in.
func MoonshineVersion() string { return "" }
