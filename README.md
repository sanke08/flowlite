# FlowLite

**Speak instead of typing — in any app, for free, without your voice leaving your computer.**

Tap a key, talk, release. A second later your words appear wherever your cursor
was: an email, a chat, a code comment, a search box. FlowLite runs OpenAI's
Whisper model locally on your Mac's GPU. There is no account, no subscription,
and no audio is ever uploaded anywhere.

- **Free and open source** (MIT)
- **Private** — the only network use is downloading the speech model, once
- **Fast** — about 1.5 seconds from release to text, on Apple Silicon
- **Works everywhere** — anywhere you can type, FlowLite can paste

> **Platforms:** macOS on Apple Silicon (M1 or newer) is supported and tested.
> A Windows build exists but has not been tested yet — see [Windows](#windows-experimental).

---

## Before you start

You need:

- A Mac with an **Apple Silicon** chip (M1, M2, M3, M4, M5) running macOS 13 or newer.
  *Intel Macs are not supported — the speech engine has no GPU path there.*
- About **600 MB of free disk** for the speech model.
- **5 minutes.** Two of those are the model download.

You do **not** need Homebrew, Python, Xcode, or anything else installed.

---

## Step 1 — Install

Open **Terminal** (press `⌘ Space`, type `Terminal`, press Enter) and paste this
one line:

```bash
curl -fsSL https://raw.githubusercontent.com/sanke08/flowlite/main/install.sh | sh
```

What it does: downloads a single 12 MB program, puts it where your terminal
can find it, and starts the setup wizard. If you'd rather see the file first,
it is here: <https://github.com/sanke08/flowlite/releases/latest>.

<details>
<summary>Downloaded the file by hand instead of using the line above?</summary>

macOS quarantines programs downloaded by a browser and — because FlowLite is not
signed with a paid Apple developer certificate — silently refuses to start it.
You will see nothing, or `zsh: killed`. Clear the flag once and run it:

```bash
xattr -d com.apple.quarantine ~/Downloads/flowlite-*-macos-arm64
~/Downloads/flowlite-*-macos-arm64
```

The `curl` line never has this problem, which is why it is the recommended way.
</details>

---

## Step 2 — The setup wizard

The wizard starts on its own after install (or run `flowlite setup` any time).
It asks four things:

**1. "Open the macOS Accessibility prompt now?"** — choose **Yes**.
macOS opens a small dialog. Click **Open System Settings**. In the list that
appears, switch on **Terminal**. This is explained in Step 3; it is the one
step people skip, and without it nothing works.

**2. "Speech model"** — press **Enter** to take the recommended one
(*Large v3 Turbo, compressed*, 547 MB). It downloads now; you'll see a progress
bar. If your connection drops, run `flowlite setup` again and it resumes.

**3. "Dictation key"** — press **Enter** for **Right Option** (the `⌥` key to
the right of your space bar). It does nothing on its own in macOS, which is
exactly why it makes a good dictation key. You can change it later.

**4. "Install flowlite so it works from any terminal?"** — **Yes**.

When it finishes you'll see `✓ saved` and what to do next.

---

## Step 3 — Allow keyboard access (the important one)

FlowLite has to notice your dictation key while you're working in *other* apps.
macOS only allows that for programs you explicitly approve under
**Accessibility**. Without it, pressing the key does nothing — no error, no
sound, nothing. This is by far the most common reason "it doesn't work".

If you said Yes in the wizard, Terminal is already in the list. Make sure its
switch is **on**:

> **System Settings → Privacy & Security → Accessibility → Terminal → on**

Two details worth knowing:

- The permission is granted to **Terminal** (the app you ran FlowLite from),
  not to FlowLite itself. That's why the list says Terminal. Grant it once and
  it stays granted, even when FlowLite updates.
- If you use **iTerm**, **Warp** or another terminal, that app is the one to
  switch on. `flowlite doctor` tells you exactly which.

Check it worked:

```bash
flowlite doctor
```

Every line should show ✓. If the last line says the keyboard is blocked, it
tells you the exact fix.

---

## Step 4 — Your first dictation

```bash
flowlite run
```

Leave that running. Now click into any text field — a Notes window is a good
first try — and:

1. **Press and hold Right Option.** You hear a short rising tone and a small
   dark pill appears at the bottom of the screen with a waveform. Speak.
2. **Release.** A falling tone; the waveform turns into a spinner and ticks
   quietly while the model works (about 1.5 s).
3. Your words appear at the cursor. A bright tick, the spinner becomes a ✓,
   the pill fades away.

That's it. Two other gestures:

- **Tap** the key instead of holding: recording starts and keeps going until
  you tap again. Better for long dictation — nothing to hold down.
- **Esc** while recording: throw the take away. A low note, an ×, nothing pasted.

Want to try it without pasting into anything? `flowlite run --no-paste` prints
transcripts in the terminal instead.

---

## Everyday use

**Keep it running.** `flowlite run` in a Terminal tab is the recommended way —
it shows a live log and Ctrl+C stops it. Or run it in the background:

```bash
flowlite start      # runs in the background, logs to a file
flowlite status
flowlite stop
```

**Type `flowlite` alone** at any time for a one-screen summary: model, key,
permission status, whether it's running, and the one thing to do next.

### What the pill and sounds mean

| you see | you hear | it means |
| --- | --- | --- |
| waveform | rising two notes | mic is live — speak |
| spinner | falling two notes, then soft ticks | recording ended, transcribing |
| ✓ (green) | bright tick | text was pasted |
| × (grey) | one low note | cancelled, or nothing was heard |
| × (red) | two low buzzes | something failed — check the terminal |

FlowLite never pastes on silence: a mis-tap produces the grey ×, not a stray
"Thank you." in your document.

### Changing settings

```bash
flowlite key ctrl_r          # dictation key: alt_r ctrl_r cmd_r shift_r f13 f14 f15
flowlite mic list            # see microphones;  flowlite mic "AirPods Pro"
flowlite lang hi             # force a language (en, hi, es, fr …) — or: flowlite lang auto
flowlite sounds off          # silence the cues
```

Each command with no argument shows the current value. Restart `flowlite run`
for changes to take effect.

### Choosing a different model

```bash
flowlite models              # what exists, what's installed
flowlite use small.en        # switch: downloads it, then removes the old one
```

| model | size | when to pick it |
| --- | --- | --- |
| tiny.en | 74 MB | very old hardware; mangles names |
| base.en | 141 MB | near-instant, English only |
| small.en | 465 MB | English only, well under a second, good accuracy |
| **large-v3-turbo-q5** | **547 MB** | **the default** — 99 languages, accents, ~1.5 s |
| large-v3-turbo | 1.5 GB | uncompressed version of the above |
| large-v3 | 2.9 GB | maximum accuracy, several times slower |

**FlowLite keeps exactly one model on disk.** Switching downloads the new one
first and deletes the old one only once the new file is complete — so an
interrupted download never leaves you without a working model.

---

## Troubleshooting

**I press the key and nothing happens at all.**
Accessibility isn't granted. Run `flowlite doctor` — it names the app to switch
on. After switching it on, **quit and reopen Terminal**, then `flowlite run`
again. macOS only re-checks the permission when the app starts.

**Terminal says `zsh: killed` or the file "cannot be opened".**
That's macOS quarantine on a browser-downloaded file. Use the `curl` install
line, or clear the flag as shown in Step 1.

**The very first run took 15+ seconds to load.**
Normal, once. Metal compiles the model's GPU shaders on first use and caches
them; after that, loading takes a fraction of a second.

**It heard me but pasted into the wrong place.**
FlowLite pastes wherever the cursor is *when the text is ready*, about 1.5 s
after you release. Don't click elsewhere in that moment.

**The pill appears but the text never arrives.**
Check the terminal running `flowlite run` — a paste failure shows there with a
red × on the pill. Some apps (secure password fields, some remote desktops)
refuse synthetic paste.

**"2 models on disk" warning.**
Left over from before the one-model rule. `flowlite use <name>` cleans it up.

**Microphone not working.**
`flowlite mic list` to see what macOS sees; `flowlite test` records 4 seconds
and prints the transcript, which isolates the microphone from everything else.
macOS asks for microphone permission the first time; grant it to Terminal.

**I have an Intel Mac.**
Not supported. The speech engine runs on the GPU through Metal, which Intel
Macs don't have in this form.

---

## Uninstall

```bash
flowlite uninstall
```

Removes the model, your settings, and the program. Nothing else was ever
installed. Then remove Terminal from Accessibility if you like.

---

## Windows (experimental)

The Windows version — keyboard hook, paste, and the pill — is written and
compiles, but **has never been run on a Windows machine**. If you try it, you
are the first. Download `flowlite-<version>-windows-x64.zip` from
[Releases](https://github.com/sanke08/flowlite/releases/latest), unzip it
anywhere, keep the DLLs next to `flowlite.exe`, and run `flowlite.exe setup`.
No special permissions are needed on Windows. The default key is Right Control.

---

## For developers

### Build from source

```bash
brew install cmake
git clone https://github.com/sanke08/flowlite && cd flowlite
make install       # static whisper.cpp + Go build → /opt/homebrew/bin/flowlite
```

Needs Go 1.26. `make build` is the quick developer build that links against
Homebrew's `whisper-cpp` (`brew install whisper-cpp`) instead of compiling it.
`make test` runs the unit tests; the whisper test additionally loads whatever
model is installed and asserts Metal was used.

### How it works

| step | where |
| --- | --- |
| global key press, tap-vs-hold decision | `internal/hotkey` — CGEventTap on macOS, WH_KEYBOARD_LL on Windows; the state machine is pure Go and tested |
| microphone at 16 kHz mono | `internal/audio` (miniaudio) |
| silence gate, hallucination filter | `internal/speech` |
| Whisper inference | `internal/whisper` — a thin cgo wrapper over `whisper.h`; loads ggml's backend plugins explicitly |
| the pill | `internal/overlay` — NSPanel + Core Graphics / layered GDI window |
| audio cues | `internal/sound` — synthesised, one persistent output stream |
| paste | `internal/inject` — clipboard + ⌘V / Ctrl+V keystroke, clipboard restored after |
| orchestration | `internal/daemon` |
| commands | `internal/cli` |

The macOS release binary statically links whisper.cpp and ggml with the Metal
library embedded, targets macOS 13+, and depends only on system frameworks.

### Versioning and releases

Semantic versioning; every release is a git tag with matching binaries on
[GitHub Releases](https://github.com/sanke08/flowlite/releases). Changes are
recorded in [CHANGELOG.md](CHANGELOG.md); `flowlite version` prints the exact
build. To cut a release:

```bash
# edit CHANGELOG.md, commit
git tag -a v0.4.0 -m "…"
make publish        # builds macOS (static) + Windows, uploads both
```

### Windows build

```bash
brew install mingw-w64
scripts/fetch-windows-deps.sh   # official whisper.cpp DLLs + headers
make windows                    # → dist/flowlite-<version>-windows-x64.zip
```

### The previous version

The original Python/PySide6 app is preserved at tag `python-v0.1.0`.

## License

MIT.
