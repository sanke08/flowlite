# FlowLite

A free, local, cross-platform alternative to Wispr Flow.

Press one key, talk, and your words appear in whatever text field you were
already using. No account, no subscription, no audio leaves your computer.

Runs on **macOS** and **Windows** from a single codebase.

---

## How it works

| Step | What happens |
| --- | --- |
| 1 | A global hotkey listener watches for your dictation key from any app |
| 2 | Your microphone records to memory at 16 kHz — nothing is written to disk |
| 3 | Whisper runs locally — on the GPU where one is available |
| 4 | The text is placed on the clipboard and pasted at your cursor |
| 5 | Your previous clipboard contents are put back |

## The dictation key

The default is **Right Option** on macOS and **Right Control** on Windows.
Both gestures work on the same key:

- **Tap it** — recording starts. Tap again when you're finished.
- **Hold it** — recording lasts only while the key is down (push-to-talk).
- **Esc while recording** — discard the take and paste nothing.

Recording begins the instant the key goes down, so nothing is clipped while
the app works out which gesture you meant. The cutoff between the two is
adjustable in Preferences.

## Choosing a model

On first launch FlowLite opens the model picker. Nothing is bundled — you pick
what to download, and it is fetched from Hugging Face once. Models can be
added and removed at any time from **Settings → Speech models**.

| Model | Size | Notes |
| --- | --- | --- |
| Tiny (English) | 74 MB | Fastest, least accurate. Mangles names and jargon. |
| Base (English) | 141 MB | Near-instant, clearly better than Tiny. |
| Small (English) | 465 MB | Strong everyday English, comfortably sub-second. |
| **Large v3 Turbo (compressed)** | **547 MB** | **Recommended.** 99 languages, handles accents well, ~1.5 s per dictation. |
| Large v3 Turbo (full) | 1.5 GB | Uncompressed Turbo. Marginally better, 3× the disk. |
| Large v3 | 3.0 GB | Most accurate, several times slower than Turbo. |

The compressed Turbo build is the sweet spot: quantisation cuts it to a third
of the full size with no transcript difference in testing, and because most of
its latency is fixed overhead, the cost is roughly the same whether you dictate
five seconds or thirty.

Weights live in:
- macOS — `~/Library/Application Support/FlowLite/models/<engine>/`
- Windows — `%LOCALAPPDATA%\FlowLite\models\<engine>\`

## Two inference engines

The same Whisper weights ship in two incompatible formats, and FlowLite picks
the faster one for your machine automatically. The engine in use is shown at
the top of the model picker.

| Engine | Used when | Runs on |
| --- | --- | --- |
| **whisper.cpp** | Default everywhere it loads | Apple GPU via Metal; CPU elsewhere |
| **faster-whisper** | NVIDIA GPU present, or Intel Mac | CUDA float16, or CPU int8 |

This matters more than it sounds. CTranslate2 — what `faster-whisper` uses —
has no Metal backend, so on Apple Silicon it is CPU-only. Measured on this
M5, Large v3 Turbo ran at **0.6× realtime** on CTranslate2 and **4× realtime**
on whisper.cpp with Metal. The first is unusable for dictation; the second is
not.

## Installing

Built installers live in [`app/`](app/), one folder per version. Grab the file
for your machine — that single file is everything. No Python, no `pip`, no
virtualenv. The speech model is downloaded from inside the app on first
launch, which keeps the download small.

| Platform | File |
| --- | --- |
| macOS (Apple Silicon) | `FlowLite-<version>-macOS-arm64.dmg` — 57 MB |
| Windows 10/11 (x64) | `FlowLite-<version>-Windows-x64.zip` |

### macOS

Open the `.dmg`, drag FlowLite to Applications, then **double-click
`Fix-Gatekeeper.command`** in the same window.

That third step is not optional. FlowLite is not signed with a paid Apple
Developer certificate, so macOS quarantines it and reports that the app is
**"damaged and can't be opened"**. Nothing is damaged — macOS just cannot
verify the publisher. The helper runs `xattr -cr /Applications/FlowLite.app`,
which clears the quarantine flag.

### Windows

Unzip and run `FlowLite.exe`. If SmartScreen warns about an unrecognised app,
choose **More info → Run anyway** — same cause, no signing certificate.

### First launch

1. FlowLite opens the model picker — choose one and press **Download**.
2. macOS only: **Permissions** tab → **Grant Accessibility**, switch FlowLite
   on in System Settings, then **quit and relaunch**.
3. Tap your dictation key in any text field and talk.

Full instructions, checksums and per-version notes: [`app/README.md`](app/README.md).

## Building the installers

Requires a checkout and [uv](https://github.com/astral-sh/uv) (or `venv`).
Build on the OS you are targeting — there is no cross-compilation.

```bash
./run.sh          # one-time: creates .venv and installs dependencies
./build-macos.sh  # -> app/v<version>/FlowLite-<version>-macOS-arm64.dmg
```

```bat
run.bat
build-windows.bat :: -> app\v<version>\FlowLite-<version>-Windows-x64.zip
```

Or push a tag and let CI build both — `.github/workflows/release.yml` runs the
tests, builds on macOS and Windows runners, self-tests each bundle, and
attaches them to a GitHub Release:

```bash
git tag v0.1.0 && git push origin v0.1.0
```

The version comes from `flowlite/__init__.py` alone; `pyproject.toml`, the
PyInstaller spec, the macOS bundle version and the artifact filenames all read
from it, so a release cannot ship mismatched numbers.

To check a build without a GUI:

```bash
./dist/FlowLite.app/Contents/MacOS/FlowLite --self-test
```

That verifies SSL, CA certificates, network reach to Hugging Face, the speech
engine and its GPU, the microphone, the model directory, and the clipboard —
the things that work from source but silently break once an app is frozen.

### Signing

The build ad-hoc signs by default, which is fine on your own machine. Ad-hoc
signatures change on every rebuild, so macOS forgets the Accessibility grant
each time you rebuild. For a stable identity — and to give the app to anyone
else — sign with a Developer ID and notarise:

```bash
SIGN_ID="Developer ID Application: Your Name (TEAMID)" ./build-macos.sh
xcrun notarytool submit dist/FlowLite.dmg --keychain-profile YOUR_PROFILE --wait
xcrun stapler staple dist/FlowLite.dmg
```

Without notarisation, other people will get Gatekeeper warnings.

## Running from source

```bash
./run.sh      # macOS / Linux
run.bat       # Windows
```

Note that macOS then grants permissions to your *terminal*, not to FlowLite.
Use a built `.app` for daily use.

## macOS permissions

FlowLite needs two permissions, and **which app they attach to depends on how
you launched it** — this is the most confusing part of setup.

| Launched from | Permission goes to |
| --- | --- |
| `FlowLite.app` | FlowLite |
| `./run.sh` | Your terminal or IDE |

1. **Accessibility** — required to see the dictation key from other apps and
   to paste. Without it the hotkey silently does nothing. Use the **Grant
   Accessibility** button in the Permissions tab: macOS then registers
   FlowLite in the list for you, so you only have to flip the switch.
2. **Microphone** — requested automatically the first time you record.

**After granting Accessibility you must quit and relaunch.** macOS only
re-checks at startup.

Windows needs no special permissions, though some antivirus software blocks
synthetic keystrokes — FlowLite will record but fail to paste if so.

## Performance

Measured on an Apple M5 (16 GB) with whisper.cpp + Metal, on a 6-second and a
25-second clip. Times are wall-clock from audio in to text out.

| Model | 6 s clip | 25 s clip |
| --- | --- | --- |
| Small (English) | 0.28 s | 0.65 s |
| Large v3 Turbo (compressed) | ~1.6 s | ~1.6 s |

Turbo's cost is almost entirely fixed overhead, which is why the 25-second clip
is no slower than the 6-second one. Small scales with length but is far below
one second either way.

Two caveats on these numbers:

- They were taken on an otherwise quiet machine. Repeating them later while an
  unrelated process held about two cores roughly doubled Turbo's time to ~3 s,
  so expect real-world latency to track whatever else your machine is doing.
- CTranslate2 on the same hardware took **12 s** for the 6-second clip. If you
  are comparing against other local Whisper tools, check which engine they use.

## Privacy

Audio is held in memory and discarded after transcription. The only network
request FlowLite ever makes is downloading a model you explicitly asked for.

## Configuration

`settings.json` and `flowlite.log` live in:
- macOS — `~/Library/Application Support/FlowLite/`
- Windows — `%APPDATA%\FlowLite\`

## Project layout

```
app/              built installers, one folder per version
.github/          CI that builds macOS + Windows on a tag
FlowLite.spec     PyInstaller build spec (Qt exclusions, macOS bundle)
launcher.py       frozen entry point, plus --self-test diagnostics
build-macos.sh    builds .app + .dmg, signs
build-windows.bat builds .exe
flowlite/
  paths.py        cross-platform app directories
  config.py       persisted settings
  models.py       model catalog, per-engine download specs
  download.py     model fetching with progress
  audio.py        microphone capture
  speech.py       silence gating, hallucination filtering, text cleanup
  inject.py       clipboard + paste keystroke, per-platform
  hotkey.py       global hotkey, tap vs hold state machine
  permissions.py  macOS Accessibility / Microphone checks
  controller.py   ties it all together
  backends/       whisper.cpp and faster-whisper engines
  ui/             tray, overlay, settings window
tests/            state machine and gating tests
```

## Tests

```bash
.venv/bin/python -m pytest tests/ -q
```

26 tests cover the tap/hold state machine and the silence gating. Neither
needs a microphone, a model download, or Accessibility permission.

## What ships in the bundle

PySide6 bundles 145 Qt frameworks; FlowLite uses three. The build spec
excludes the rest — QtWebEngineCore alone is an entire Chromium at 602 MB —
and leaves out `faster-whisper`'s 138 MB dependency chain, since the fallback
engine is an optional extra rather than part of the shipped app.

| | Size |
| --- | --- |
| Unstripped dev environment | 1.4 GB |
| `FlowLite.app` | 128 MB |
| `FlowLite.dmg` (compressed) | 57 MB |

## Not yet built

- LLM cleanup of filler words and grammar (planned for v2)
- Notarised, Developer-ID-signed releases
- Custom vocabulary for names and jargon
- Launch at login
