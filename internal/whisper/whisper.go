// Package whisper is a thin cgo wrapper over whisper.cpp's C API.
//
// It deliberately does not use the official Go bindings: they live inside the
// whisper.cpp repository with awkward module versioning and expect a locally
// built static library. whisper.h is small; wrapping the handful of calls we
// need lets the binary link against whatever libwhisper the system has —
// Homebrew's on macOS, the release DLLs on Windows.
package whisper

/*
#cgo darwin CFLAGS:  -I/opt/homebrew/opt/whisper-cpp/include -I/opt/homebrew/opt/ggml/include
#cgo darwin LDFLAGS: -L/opt/homebrew/opt/whisper-cpp/lib -L/opt/homebrew/opt/ggml/lib -lwhisper -lggml -lggml-base
#cgo windows LDFLAGS: -lwhisper -lggml -lggml-base
#cgo linux LDFLAGS: -lwhisper -lggml -lggml-base
#include <stdlib.h>
#include "ggml-backend.h"
#include "whisper.h"

void flowlite_whisper_install_logger(void);
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"unsafe"
)

// ---- runtime ------------------------------------------------------------

var runtimeOnce sync.Once

// initRuntime wires logging and loads the compute backends.
//
// Homebrew builds ggml with GGML_BACKEND_DL: Metal, BLAS and the CPU variants
// are plugins in a directory compiled into libggml, and *nothing loads them
// automatically*. whisper-cli calls ggml_backend_load_all() itself; a library
// user that forgets hits `GGML_ASSERT(device) failed` inside whisper_init.
func initRuntime() {
	runtimeOnce.Do(func() {
		C.flowlite_whisper_install_logger()
		C.ggml_backend_load_all()
	})
}

// ---- logging ------------------------------------------------------------

const logKeep = 200

var (
	logMu    sync.Mutex
	logLines []string
)

//export flowliteWhisperLog
func flowliteWhisperLog(level C.int, text *C.char) {
	line := strings.TrimRight(C.GoString(text), "\n")
	if line == "" {
		return
	}
	logMu.Lock()
	logLines = append(logLines, line)
	if len(logLines) > logKeep {
		logLines = logLines[len(logLines)-logKeep:]
	}
	logMu.Unlock()
}

// Logs returns the most recent whisper.cpp/ggml log lines.
func Logs() []string {
	logMu.Lock()
	defer logMu.Unlock()
	return append([]string(nil), logLines...)
}

// UsingMetal reports whether ggml initialised a Metal device in this process.
func UsingMetal() bool {
	for _, l := range Logs() {
		if strings.Contains(l, "ggml_metal_device_init: GPU name") {
			return true
		}
	}
	return false
}

// GPUName returns the device ggml reported, or "".
func GPUName() string {
	for _, l := range Logs() {
		if i := strings.Index(l, "GPU name:"); i >= 0 {
			return strings.TrimSpace(l[i+len("GPU name:"):])
		}
	}
	return ""
}

// Backends lists the compute backends ggml has registered (after loading).
func Backends() []string {
	initRuntime()
	n := int(C.ggml_backend_reg_count())
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		reg := C.ggml_backend_reg_get(C.size_t(i))
		out = append(out, C.GoString(C.ggml_backend_reg_name(reg)))
	}
	return out
}

// ---- model --------------------------------------------------------------

// Model is a loaded whisper.cpp context. Not safe for concurrent Transcribe
// calls; the daemon serialises them.
type Model struct {
	ctx  *C.struct_whisper_context
	path string
}

// Options control one transcription.
type Options struct {
	// Language is an ISO code such as "en", or "" / "auto" to detect.
	Language string
	// Threads for the decoder; 0 picks a sensible default.
	Threads int
}

// DefaultThreads: on Apple Silicon the encoder runs on the GPU, so these only
// feed the decoder; beyond about eight there is nothing left to gain.
func DefaultThreads() int {
	n := runtime.NumCPU()
	if n > 8 {
		n = 8
	}
	if n < 2 {
		n = 2
	}
	return n
}

// Load reads a GGML model file. Logs are captured from this point on.
func Load(path string) (*Model, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("model file: %w", err)
	}
	initRuntime()

	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))

	cparams := C.whisper_context_default_params()
	cparams.use_gpu = C.bool(true)
	cparams.flash_attn = C.bool(true)

	ctx := C.whisper_init_from_file_with_params(cpath, cparams)
	if ctx == nil {
		return nil, errors.New("whisper.cpp could not load the model (see logs)")
	}
	return &Model{ctx: ctx, path: path}, nil
}

// Path is the model file this context was loaded from.
func (m *Model) Path() string { return m.path }

// Close frees the context. Safe to call twice.
func (m *Model) Close() {
	if m.ctx != nil {
		C.whisper_free(m.ctx)
		m.ctx = nil
	}
}

// Transcribe runs 16 kHz mono float32 samples and returns the raw segment
// texts. Callers pass these through speech.Finalise.
func (m *Model) Transcribe(samples []float32, opt Options) ([]string, error) {
	if m.ctx == nil {
		return nil, errors.New("model is closed")
	}
	if len(samples) == 0 {
		return nil, nil
	}

	lang := opt.Language
	if lang == "" {
		lang = "auto" // whisper.cpp's sentinel for autodetect
	}
	clang := C.CString(lang)
	defer C.free(unsafe.Pointer(clang))

	threads := opt.Threads
	if threads <= 0 {
		threads = DefaultThreads()
	}

	p := C.whisper_full_default_params(C.WHISPER_SAMPLING_GREEDY)
	p.n_threads = C.int(threads)
	p.language = clang
	p.detect_language = C.bool(false)
	p.no_context = C.bool(true)   // each dictation is independent
	p.single_segment = C.bool(false)
	p.suppress_nst = C.bool(true) // drop "(door closes)" style tokens
	p.print_progress = C.bool(false)
	p.print_realtime = C.bool(false)
	p.print_timestamps = C.bool(false)
	p.print_special = C.bool(false)

	rc := C.whisper_full(m.ctx, p, (*C.float)(unsafe.Pointer(&samples[0])), C.int(len(samples)))
	if rc != 0 {
		return nil, fmt.Errorf("whisper_full failed (%d)", int(rc))
	}

	n := int(C.whisper_full_n_segments(m.ctx))
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, C.GoString(C.whisper_full_get_segment_text(m.ctx, C.int(i))))
	}
	return out, nil
}

// SystemInfo returns whisper.cpp's compiled feature summary.
func SystemInfo() string {
	return C.GoString(C.whisper_print_system_info())
}
