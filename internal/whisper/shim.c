// C side of the whisper.cpp bridge.
//
// Lives in its own file because a Go file that uses //export may only
// *declare* C symbols in its preamble, never define them.

#include <stdbool.h>
#include <stdlib.h>
#include "whisper.h"

extern void flowliteWhisperLog(int level, char *text);

static void log_bridge(enum ggml_log_level level, const char *text, void *user_data) {
    (void)user_data;
    flowliteWhisperLog((int)level, (char *)text);
}

// Route every whisper.cpp and ggml log line into Go. whisper_log_set also
// installs the ggml logger, which is where the Metal device-init lines
// come from — `flowlite doctor` reads those to prove the GPU is in use.
void flowlite_whisper_install_logger(void) {
    whisper_log_set(log_bridge, NULL);
}

// Abort flags are plain C ints the Go side owns (see whisper.Flag): whisper.cpp
// polls the callback between graph computations, on its own thread, so the
// flag has to live in memory Go's collector will never move or free under it.
static bool abort_bridge(void *data) {
    return __atomic_load_n((int *)data, __ATOMIC_RELAXED) != 0;
}

void flowlite_whisper_set_abort(struct whisper_full_params *p, int *flag) {
    p->abort_callback = abort_bridge;
    p->abort_callback_user_data = flag;
}

void flowlite_flag_store(int *flag, int v) {
    __atomic_store_n(flag, v, __ATOMIC_RELAXED);
}

bool flowlite_flag_load(int *flag) {
    return __atomic_load_n(flag, __ATOMIC_RELAXED) != 0;
}
