# FlowLite

Free, local speech-to-text from the command line. Tap a key, talk, and the
words appear wherever your cursor is. Nothing leaves your computer.

- **macOS (Apple Silicon)** — built and tested. Runs whisper.cpp on the GPU via Metal.
- **Windows x64** — cross-compiled and linked from macOS, not yet run on Windows. See below.

```
flowlite setup     # once: permission, model download, dictation key
flowlite run       # then tap Right Option in any text field and speak
```

## Install (macOS)

```bash
brew install whisper-cpp
git clone https://github.com/sanke08/flowlite && cd flowlite
make install       # builds and copies to /opt/homebrew/bin — no sudo
flowlite setup
```

`brew install whisper-cpp` provides `libwhisper` with Metal enabled; FlowLite
links against it rather than shipping its own copy. Go 1.26 is needed to build (see go.mod).

## How a dictation works

| you | FlowLite |
| --- | --- |
| tap or hold the key | rising two-note cue · pill appears with a live waveform |
| speak | bars follow your voice |
| release / tap again | falling cue · bars collapse into a spinner · soft tick while the model works |
| — | text is pasted at the cursor · bright tick · spinner becomes a ✓ · pill fades |
| Esc while recording | low note · × · nothing pasted |

Recording starts the instant the key goes down, so no speech is lost while it
works out whether this is a tap or a hold. The cut-off is 400 ms.

Silence is never pasted: audio is gated on level before inference, and Whisper's
favourite hallucinations on silence ("Thank you.", "Thanks for watching!") are
filtered out afterwards.

## Commands

```
flowlite                 where things stand and the one thing to do next
flowlite setup           interactive: permission → model → key
flowlite doctor          checks engine, Metal, model, mic, clipboard, permission; says how to fix each
flowlite run             foreground daemon (recommended); --no-paste prints instead of pasting
flowlite start|stop|status   background daemon
flowlite test            record 4 s and print the transcript — no hotkey, no paste
                         --file x.wav to transcribe a 16 kHz mono WAV instead

flowlite models          list models; ● marks the active one
flowlite download <m>    resumable; Ctrl+C and rerun to continue
flowlite use <m>         switch model
flowlite remove <m>

flowlite key [name]      alt_r ctrl_r cmd_r shift_r f13 f14 f15
flowlite mic [list|name|default]
flowlite lang [code|auto]
flowlite sounds [on|off]
```

## The one permission that matters

macOS does not let any program see the keyboard globally without
**Accessibility**. Without it, the dictation key does nothing — silently. This
is the step almost everyone misses, and it is the *only* reason the earlier
version of this project "didn't work".

The permission attaches to the app that launched `flowlite` — normally
**Terminal** — not to the `flowlite` binary. That has a nice consequence: grant
it once and it survives every rebuild.

```bash
flowlite doctor --request   # macOS opens its prompt and adds Terminal to the list
# System Settings → Privacy & Security → Accessibility → switch on Terminal
flowlite doctor             # confirms
flowlite run
```

`flowlite doctor` names the exact app that needs the grant.

## Models

Downloaded on demand from Hugging Face into
`~/Library/Application Support/FlowLite/models/`. Nothing is bundled.

| model | size | notes |
| --- | --- | --- |
| tiny.en | 74 MB | fastest; mangles names |
| base.en | 141 MB | near-instant |
| small.en | 465 MB | strong everyday English, well under a second |
| **large-v3-turbo-q5** | **547 MB** | **recommended** — 99 languages, accents, ~1.5 s |
| large-v3-turbo | 1.5 GB | uncompressed Turbo |
| large-v3 | 2.9 GB | most accurate, several times slower |

## Performance

Apple M5, whisper.cpp via Metal, Large v3 Turbo (compressed):

| audio | time | speed |
| --- | --- | --- |
| 6.3 s | 1.48 s | 4.3× realtime |
| 25.4 s | 2.06 s | 12.3× realtime |

Turbo's cost is mostly fixed overhead, so long dictations are barely slower
than short ones. Model load is ~0.3 s warm. **The very first load on a machine
takes ~15 s** while Metal compiles the shaders; macOS caches them afterwards.

## Windows (experimental)

The Windows code — low-level keyboard hook, clipboard + `SendInput` paste, a
layered GDI pill — is written and **cross-compiles and links from macOS**. It
has **not been run on Windows**. Expect bugs; reports welcome.

```bash
brew install mingw-w64
scripts/fetch-windows-deps.sh   # official whisper.cpp DLLs + headers → third_party/windows
make windows                    # → dist/windows/flowlite.exe + DLLs
```

Ship the whole `dist/windows/` folder; the DLLs must sit next to the `.exe`.
Windows needs no special permission for the keyboard hook. The default key is
Right Control.

## Project layout

```
cmd/flowlite/        entry point
internal/cli/        every command
internal/daemon/     key → sound → pill → record → transcribe → paste
internal/whisper/    thin cgo wrapper over whisper.h (loads ggml's backend plugins)
internal/hotkey/     tap-vs-hold state machine (tested) + CGEventTap / WH_KEYBOARD_LL
internal/overlay/    the pill: NSPanel (macOS), layered GDI window (Windows)
internal/sound/      synthesised cues, one persistent output stream
internal/audio/      16 kHz mono capture via miniaudio
internal/speech/     silence gate, hallucination filter, segment join (tested)
internal/inject/     clipboard + paste keystroke
internal/catalog/    model table, resumable download
internal/config/     ~/Library/Application Support/FlowLite/config.json
internal/mainloop/   AppKit run loop bridge / Win32 message loop
scripts/             Windows deps fetcher
```

## Tests

```bash
make test
```

Pure-Go tests for the gesture machine and the speech gate run anywhere. The
whisper test loads a downloaded model, synthesises speech with macOS `say`,
transcribes it and asserts Metal was used; it skips where that is not possible.

## Recovering the previous version

The original Python/PySide6 implementation is tagged: `git checkout python-v0.1.0`.

## License

MIT.
