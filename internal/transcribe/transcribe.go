//go:build darwin && moonshine

// Package transcribe is a thin cgo wrapper over transcribe.cpp's C API, for
// the Moonshine models.
//
// transcribe.cpp carries its own, patched copy of ggml — a different version
// from whisper.cpp's — so it is linked as one pre-built archive whose only
// global symbols are transcribe_*; its ggml is private to it and cannot
// collide with whisper.cpp's (see scripts/build-transcribe.sh). cgo flags
// come from the Makefile, which is why this package only builds with the
// `moonshine` tag: a plain `go build` simply leaves Moonshine out.
package transcribe

/*
#include <stdlib.h>
#include <stdbool.h>
#include "transcribe.h"
#include "transcribe/moonshine_streaming.h"

void flowlite_transcribe_install_logger(void);
void flowlite_transcribe_set_abort(struct transcribe_session *s, int *flag);
transcribe_status flowlite_transcribe_stream_begin(struct transcribe_session *s);
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"unsafe"
)

// ---- logging ------------------------------------------------------------

const logKeep = 200

var (
	logOnce  sync.Once
	logMu    sync.Mutex
	logLines []string
)

//export flowliteTranscribeLog
func flowliteTranscribeLog(level C.int, text *C.char) {
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

// Logs returns the most recent transcribe.cpp log lines.
func Logs() []string {
	logMu.Lock()
	defer logMu.Unlock()
	return append([]string(nil), logLines...)
}

// Version is the linked transcribe.cpp release.
func Version() string { return C.GoString(C.transcribe_version()) }

func status(st C.transcribe_status) error {
	if st == C.TRANSCRIBE_OK {
		return nil
	}
	return errors.New(C.GoString(C.transcribe_status_string(C.int(st))))
}

// ---- model --------------------------------------------------------------

// ErrAborted is returned by a Run whose abort flag was raised.
var ErrAborted = errors.New("transcription aborted")

// Model is one loaded model and the single session FlowLite runs it with.
//
// transcribe.cpp allows one run or one active stream per session at a time,
// from one thread at a time; mu enforces that, and makes Close wait for a
// call in flight. A stream spans many calls, so streaming additionally
// blocks Run until the stream ends.
type Model struct {
	mu        sync.Mutex
	model     *C.struct_transcribe_model
	sess      *C.struct_transcribe_session
	canStream bool
	streaming bool
	maxAudio  int64 // ms of audio one Run accepts; 0 = no limit reported
	device    string
}

// Load reads a GGUF model file.
func Load(path string) (*Model, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("model file: %w", err)
	}
	logOnce.Do(func() { C.flowlite_transcribe_install_logger() })

	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var lp C.struct_transcribe_model_load_params
	C.transcribe_model_load_params_init(&lp)
	lp.backend = C.TRANSCRIBE_BACKEND_AUTO

	m := &Model{}
	if err := status(C.transcribe_model_load_file(cpath, &lp, &m.model)); err != nil {
		return nil, fmt.Errorf("transcribe.cpp could not load the model: %w", err)
	}
	var sp C.struct_transcribe_session_params
	C.transcribe_session_params_init(&sp)
	if err := status(C.transcribe_session_init(m.model, &sp, &m.sess)); err != nil {
		C.transcribe_model_free(m.model)
		return nil, fmt.Errorf("transcribe.cpp session: %w", err)
	}

	var caps C.struct_transcribe_capabilities
	C.transcribe_capabilities_init(&caps)
	if C.transcribe_model_get_capabilities(m.model, &caps) == C.TRANSCRIBE_OK {
		m.maxAudio = int64(caps.max_audio_ms)
	}
	m.canStream = bool(C.transcribe_model_accepts_ext_kind(m.model,
		C.TRANSCRIBE_EXT_SLOT_STREAM, C.TRANSCRIBE_EXT_KIND_MOONSHINE_STREAMING_STREAM))

	backend := C.GoString(C.transcribe_model_backend(m.model))
	var di C.struct_transcribe_device_info
	C.transcribe_device_info_init(&di)
	m.device = backend
	if C.transcribe_device_get_info(C.transcribe_model_device(m.model), &di) == C.TRANSCRIBE_OK && di.description != nil {
		m.device = strings.TrimSpace(backend + " " + C.GoString(di.description))
	}
	return m, nil
}

// CanStream reports whether the model transcribes audio as it arrives.
func (m *Model) CanStream() bool { return m.canStream }

// MaxAudioMS is the longest recording one Run accepts, or 0 for no limit.
func (m *Model) MaxAudioMS() int64 { return m.maxAudio }

// Device says where the model runs, e.g. "Metal Apple M3".
func (m *Model) Device() string { return m.device }

// Close frees the model, waiting for any call in flight. Safe to call twice.
func (m *Model) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sess != nil {
		C.transcribe_session_free(m.sess)
		m.sess = nil
	}
	if m.model != nil {
		C.transcribe_model_free(m.model)
		m.model = nil
	}
}

// Run transcribes one whole 16 kHz mono recording. abort, if not nil, is a
// C int (see whisper.Flag.Pointer) that cuts the run short once non-zero.
func (m *Model) Run(samples []float32, abort unsafe.Pointer) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sess == nil {
		return "", errors.New("model is closed")
	}
	if m.streaming {
		return "", errors.New("model is busy streaming")
	}
	if len(samples) == 0 {
		return "", nil
	}
	C.flowlite_transcribe_set_abort(m.sess, (*C.int)(abort))
	defer C.flowlite_transcribe_set_abort(m.sess, nil)

	var rp C.struct_transcribe_run_params
	C.transcribe_run_params_init(&rp)
	rp.timestamps = C.TRANSCRIBE_TIMESTAMPS_NONE
	st := C.transcribe_run(m.sess, (*C.float)(unsafe.Pointer(&samples[0])), C.int(len(samples)), &rp)
	if bool(C.transcribe_was_aborted(m.sess)) {
		return "", ErrAborted
	}
	// A truncated run still returns everything decoded up to the limit;
	// callers keep recordings short enough that it should not happen.
	if st != C.TRANSCRIBE_OK && st != C.TRANSCRIBE_ERR_OUTPUT_TRUNCATED {
		return "", status(st)
	}
	return C.GoString(C.transcribe_full_text(m.sess)), nil
}

// ---- streaming ----------------------------------------------------------

// BeginStream starts a live transcription. Until EndStream, the session is
// the stream's: Run refuses to run.
func (m *Model) BeginStream() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sess == nil {
		return errors.New("model is closed")
	}
	if !m.canStream {
		return errors.New("this model cannot stream")
	}
	if err := status(C.flowlite_transcribe_stream_begin(m.sess)); err != nil {
		return fmt.Errorf("begin stream: %w", err)
	}
	m.streaming = true
	return nil
}

// Feed appends audio to the stream; the model decodes when it has enough.
func (m *Model) Feed(samples []float32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.streaming || m.sess == nil {
		return errors.New("no stream")
	}
	if len(samples) == 0 {
		return nil
	}
	st := C.transcribe_stream_feed(m.sess, (*C.float)(unsafe.Pointer(&samples[0])), C.int(len(samples)), nil)
	if st != C.TRANSCRIBE_OK && st != C.TRANSCRIBE_ERR_OUTPUT_TRUNCATED {
		return status(st)
	}
	return nil
}

// Text is the stream's current state: full is the model's whole current
// hypothesis (authoritative, but it may rewrite anywhere), committed the
// append-only prefix it has settled on.
func (m *Model) Text() (full, committed string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.streaming || m.sess == nil {
		return "", ""
	}
	var tx C.struct_transcribe_stream_text
	C.transcribe_stream_text_init(&tx)
	if C.transcribe_stream_get_text(m.sess, &tx) != C.TRANSCRIBE_OK {
		return "", ""
	}
	return goStr(tx.full_text, tx.full_text_bytes), goStr(tx.committed_text, tx.committed_text_bytes)
}

// Finalize decodes what is left and ends the stream, returning the final
// full text. The session is free for Run again afterwards.
func (m *Model) Finalize() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.streaming || m.sess == nil {
		return "", errors.New("no stream")
	}
	m.streaming = false
	st := C.transcribe_stream_finalize(m.sess, nil)
	if st != C.TRANSCRIBE_OK && st != C.TRANSCRIBE_ERR_OUTPUT_TRUNCATED {
		C.transcribe_stream_reset(m.sess)
		return "", status(st)
	}
	var tx C.struct_transcribe_stream_text
	C.transcribe_stream_text_init(&tx)
	C.transcribe_stream_get_text(m.sess, &tx)
	text := goStr(tx.full_text, tx.full_text_bytes)
	C.transcribe_stream_reset(m.sess)
	return text, nil
}

// EndStream discards a stream without decoding the rest. Safe to call when
// no stream is active.
func (m *Model) EndStream() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.streaming && m.sess != nil {
		C.transcribe_stream_reset(m.sess)
	}
	m.streaming = false
}

func goStr(p *C.char, n C.uint64_t) string {
	if p == nil || n == 0 {
		return ""
	}
	return C.GoStringN(p, C.int(n))
}
