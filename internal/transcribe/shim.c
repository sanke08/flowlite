//go:build darwin && moonshine

// C side of the transcribe.cpp bridge. Lives in its own file because a Go
// file that uses //export may only *declare* C symbols in its preamble.

#include <stdbool.h>
#include "transcribe.h"
#include "transcribe/moonshine_streaming.h"

extern void flowliteTranscribeLog(int level, char *text);

static void log_bridge(transcribe_log_level level, const char *msg, void *userdata) {
    (void)userdata;
    flowliteTranscribeLog((int)level, (char *)msg);
}

// Route every transcribe.cpp and (its private) ggml log line into Go instead
// of stderr. transcribe.h asks for this exactly once, before any model loads.
void flowlite_transcribe_install_logger(void) {
    transcribe_log_set(log_bridge, NULL);
}

// The abort flag is the same C int whisper.cpp polls (see whisper.Flag), so
// one Abort cancels a run on either engine.
static bool abort_bridge(void *data) {
    return __atomic_load_n((int *)data, __ATOMIC_RELAXED) != 0;
}

void flowlite_transcribe_set_abort(struct transcribe_session *s, int *flag) {
    if (flag) transcribe_set_abort_callback(s, abort_bridge, flag);
    else transcribe_set_abort_callback(s, NULL, NULL);
}

// Begins a Moonshine stream with the family's default decode cadence. The
// parameter structs point into each other, so they are built here, on the C
// stack: cgo forbids handing C a Go struct that holds a Go pointer, and the
// library copies what it needs before begin returns.
transcribe_status flowlite_transcribe_stream_begin(struct transcribe_session *s) {
    struct transcribe_moonshine_streaming_stream_ext ext;
    transcribe_moonshine_streaming_stream_ext_init(&ext);
    struct transcribe_stream_params spp;
    transcribe_stream_params_init(&spp);
    spp.family = &ext.ext;
    spp.commit_policy = TRANSCRIBE_STREAM_COMMIT_AUTO;
    return transcribe_stream_begin(s, NULL, &spp);
}
