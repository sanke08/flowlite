// C side of the whisper.cpp bridge.
//
// Lives in its own file because a Go file that uses //export may only
// *declare* C symbols in its preamble, never define them.

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
