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
> A Windows build exists and has a one-line installer too, but has not been
> tested on real hardware yet — see [Windows](#windows-experimental).

Four commands cover everyday use:

| command | what it does |
| --- | --- |
| `flowlite` | start dictating (the first time, it sets itself up) |
| `flowlite settings` | everything you can change, in one menu |
| `flowlite doctor` | check what FlowLite needs and how to fix what is missing |
| `flowlite update` | get the latest version |

Three more exist when you need them: `flowlite start` and `flowlite stop` run it
detached from the terminal, and `flowlite uninstall` removes it. (`flowlite
reload` is there too, but you should never need it — settings and updates apply
themselves.)

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

> On Windows, open **PowerShell** instead and paste:
> ```powershell
> irm https://raw.githubusercontent.com/sanke08/flowlite/main/install.ps1 | iex
> ```
> See [Windows (experimental)](#windows-experimental) — the Windows build is
> untested on real hardware.

What it does: downloads a single 12 MB program, puts it where your terminal
can find it, and starts `flowlite`, which runs the setup wizard. If you'd
rather see the file first, it is here:
<https://github.com/sanke08/flowlite/releases/latest>.

<details>
<summary>Downloaded the file by hand instead of using the line above?</summary>

macOS quarantines programs downloaded by a browser and — because FlowLite is not
signed with a paid Apple developer certificate — silently refuses to start it.
You will see nothing, or `zsh: killed`. Clear the flag once and run it:

```bash
xattr -d com.apple.quarantine ~/Downloads/flowlite-*-macos-arm64
~/Downloads/flowlite-*-macos-arm64
```

It will offer to copy itself onto your PATH so plain `flowlite` works from then
on. The `curl` line never has the quarantine problem, which is why it is the
recommended way.
</details>

---

## Step 2 — The setup wizard

The wizard starts on its own after install — it is what `flowlite` does when
it has no model yet. It asks three things:

**1. "Open the macOS Accessibility prompt now?"** — choose **Yes**.
macOS opens a small dialog. Click **Open System Settings**. In the list that
appears, switch on **Terminal**. This is explained in Step 3; it is the one
step people skip, and without it nothing works.

**2. "Speech model"** — press **Enter** to take the recommended one
(*Large v3 Turbo, compressed*, 547 MB). It downloads now; you'll see a progress
bar. If your connection drops, run `flowlite` again and it resumes.

**3. "Dictation key"** — press **Enter** for **Right Option** (the `⌥` key to
the right of your space bar). It does nothing on its own in macOS, which is
exactly why it makes a good dictation key. You can change it later.

When it finishes you'll see `✓ saved`. If the keyboard permission is not
granted yet, FlowLite then prints the exact steps to grant it and stops —
that is Step 3.

---

## Step 3 — Allow keyboard access (the important one)

FlowLite has to notice your dictation key while you're working in *other* apps.
macOS only allows that for programs you explicitly approve under
**Accessibility**. Without it, pressing the key does nothing — no error, no
sound, nothing. This is by far the most common reason "it doesn't work".

If you said Yes in the wizard, Terminal is already in the list. Make sure its
switch is **on**:

> **System Settings → Privacy & Security → Accessibility → Terminal → on**

Then **quit and reopen Terminal** — macOS only re-checks the permission when
the app starts.

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

Every line should show ✓. If the keyboard line says it is blocked, it tells
you the exact fix.

---

## Step 4 — Your first dictation

```bash
flowlite
```

You'll see a banner like

```
FlowLite v0.4.0 is listening.   Large v3 Turbo (compressed) · Right Option · Metal
  hold Right Option to talk · double-tap for hands-free, tap to stop · triple-tap pastes your last transcript · Esc cancels
  settings: flowlite settings (in another tab)
```

Leave that running. Now click into any text field — a Notes window is a good
first try — and:

1. **Press and hold Right Option.** You hear a short rising tone and a small
   black pill appears near the bottom of the screen with a waveform. Speak.
2. **Release.** A falling tone; the bars settle and a light sweeps across them
   while the model works (about 1.5 s), ticking quietly.
3. Your words appear at the cursor. A bright tick, and the pill fades away.

That's holding. The other gestures, all on the same key:

| gesture | what it does |
| --- | --- |
| **hold** | record while held, release to finish |
| **double-tap** | hands-free: recording continues with nothing held |
| **one press** (while hands-free) | stop and transcribe |
| **triple-tap** | paste your **last transcript** again |
| **Esc** | cancel — a low note, nothing pasted |
| single tap | nothing (on purpose — see Troubleshooting) |

Hands-free is for long dictation. Triple-tap is your safety net: if nothing
was focused when the words arrived, click into the right field and triple-tap.

Want to try it without pasting into anything? `flowlite --no-paste` prints
transcripts in the terminal instead.

---

## Everyday use

**`flowlite` once, then dictate.** That is the whole loop.

FlowLite runs in the background. `flowlite` starts it and hands your terminal
straight back — you can close the window, and it keeps listening. Closing the
terminal or pressing Ctrl+C in it does not stop it, because by then dictation
is part of how you type and losing it by accident would be worse than useless.

```bash
flowlite          # start listening (does nothing if it already is)
flowlite stop     # the one way to stop it
```

Running `flowlite` while it is already listening tells you so and does nothing
— two listeners would paste twice. Every transcript is logged to
`~/Library/Application Support/FlowLite/flowlite.log`; `tail -f` it if you want
to watch. `flowlite --no-paste` is the exception that stays in your terminal:
it prints transcripts instead of pasting them, for when you want to see what it
heard without it typing anywhere.

**Nothing is ever lost.** Every transcript — including ones that had nowhere
to paste — is kept. `flowlite settings → Recent transcripts` lists the last
ten; pick one and it is copied to the clipboard, ready for ⌘V.

### What the pill and sounds mean

| you see | you hear | it means |
| --- | --- | --- |
| waveform | rising two notes | mic is live — speak |
| bars settle, a light sweeps across them | falling two notes, then soft ticks | recording ended, transcribing |
| pill fades out | bright tick | text was pasted |
| pill fades out | one low note | cancelled, or nothing was heard |
| bars pulse **red** twice | two low buzzes | something failed — check the terminal |

The pill never shows text. FlowLite never pastes on silence either: a mis-tap
produces nothing, not a stray "Thank you." in your document.

### Changing settings

```bash
flowlite settings
```

One menu; every row shows its current value. Pick a row, change it, it is saved
at once — and if FlowLite is running, it applies the change to itself on the
way out. That works whether it is running in the background or in a terminal
tab you are sitting in: it reloads in place, keeping its pid, its tab and its
keyboard permission. There is nothing to restart.

| row | what it is |
| --- | --- |
| Speech model ⟳ | which Whisper model — see below |
| Dictation key ⟳ | Right Option by default; also Right Control, Right Command, Right Shift, F13–F15 |
| Hold threshold ⟳ | how long a press must last to count as a hold rather than a tap (400 ms) |
| Pill position ⟳ | bottom, top, left or right edge — with a live preview |
| Microphone ⟳ | system default (follows AirPods and headsets), or a specific device |
| Language ⟳ | auto-detect, or fix one (en, hi, es, fr, …) for short phrases |
| Sounds ⟳ | on or off — "Play the cues" lets you hear them first |
| Test microphone | records 4 seconds and prints the transcript — no key, no paste |
| Recent transcripts | the last ten; pick one to copy it |
| Background daemon | start, stop or restart the detached daemon |
| Reset to defaults | every setting back to how it shipped; the model stays |
| Uninstall FlowLite | removes everything (asks you to type `yes`) |

### Choosing a different model

`flowlite settings → Speech model` lists them; choosing one downloads it and
removes the old one.

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

## Updating FlowLite

```bash
flowlite update            # fetch the latest release and swap it in
flowlite update --check    # only say whether there is one
```

`update` downloads the release for this Mac from GitHub, checks that the file
is complete and actually runs, then replaces the program in place. Your
settings and model are untouched, and **a running FlowLite moves to the new
version by itself** — including one you are running in a terminal, which keeps
its tab, its pid and its keyboard permission. You do not have to stop or start
anything. The same is true of the one-line installer when you use it to
upgrade, and of every change you make in `flowlite settings`.

FlowLite also looks for a newer release on its own, at most once a day, and
mentions it in the listening banner and in `flowlite doctor`. That check is
the only network request FlowLite ever makes apart from the model download
and the update itself; set `FLOWLITE_NO_UPDATE_CHECK=1` to turn it off. The
explicit `flowlite update` still works either way.

How releases are cut is under [Versioning and releases](#versioning-and-releases).

---

## Troubleshooting

Start with `flowlite doctor`: it checks the engine, model, microphone,
clipboard, config directory and — the one that matters — the keyboard
permission, and prints the fix for anything that fails.

**I tapped the key once and nothing happened.**
That's by design: a single tap does nothing, so an accidental brush of the key
never starts a recording. **Hold** it to talk, or **double-tap** for hands-free.

**I press the key and nothing happens at all — not even when holding.**
Accessibility isn't granted. Run `flowlite doctor` — it names the app to switch
on. After switching it on, **quit and reopen Terminal**, then `flowlite`
again. macOS only re-checks the permission when the app starts.

**Terminal says `zsh: killed` or the file "cannot be opened".**
That's macOS quarantine on a browser-downloaded file. Use the `curl` install
line, or clear the flag as shown in Step 1.

**The very first run took 15+ seconds to load.**
Normal, once. Metal compiles the model's GPU shaders on first use and caches
them; after that, loading takes a fraction of a second.

**It heard me but pasted into the wrong place — or nowhere.**
FlowLite pastes wherever the cursor is *when the text is ready*, about 1.5 s
after you release. The transcript is never lost: click into the right field and
**triple-tap** the key to paste it again, or copy it from
`flowlite settings → Recent transcripts`.

**The pill appears but the text never arrives.**
Check the terminal running `flowlite` — a paste failure shows there and the
pill pulses red. Some apps (secure password fields, some remote desktops)
refuse synthetic paste.

**"2 models installed" in doctor.**
Left over from before the one-model rule. Re-choose your model under
`flowlite settings → Speech model` and the extra one is removed.

**Microphone not working.**
`flowlite settings → Test microphone` records 4 seconds and prints the
transcript, which isolates the microphone from everything else; the
Microphone row shows every device macOS sees. macOS asks for microphone
permission the first time; grant it to Terminal.

**`flowlite` says it is already listening, but I can't find it.**
It is running in the background. `flowlite settings → Background daemon → Stop`.

**I have an Intel Mac.**
Not supported. The speech engine runs on the GPU through Metal, which Intel
Macs don't have in this form.

---

## Uninstall

```bash
flowlite settings
```

Choose **Uninstall FlowLite** and type `yes`. It removes the model, your
settings and transcript history, and the program. Nothing else was ever
installed. Then remove Terminal from Accessibility if you like.

---

## Windows (experimental)

Windows is a **preview**. The keyboard hook, the paste and the pill are all
written, and every release cross-compiles and links them on CI — but they have
not yet been exercised on real Windows hardware, so treat this as a build to
try and report on rather than one to rely on. Bugs and successes are both
useful: please open an issue either way.

Open **PowerShell** and paste this one line:

```powershell
irm https://raw.githubusercontent.com/sanke08/flowlite/main/install.ps1 | iex
```

It downloads `flowlite-<version>-windows-x64.zip`, unzips it to
`%LOCALAPPDATA%\FlowLite`, adds that folder to your user `PATH`, and starts
`flowlite.exe`, which sets itself up the first time. No admin rights are
needed. If you'd rather do it by hand, download the zip from
[Releases](https://github.com/sanke08/flowlite/releases/latest), unzip it
anywhere, keep the DLLs next to `flowlite.exe`, and run `flowlite.exe`.
The default key is Right Control.

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

#### Testing your changes locally

`make install` and the curl installer write the **same file** — so a local
build replaces the downloaded one in place, and nothing needs uninstalling
first. (They agree whenever `/opt/homebrew/bin` or `/usr/local/bin` is
writable, which is the normal case. If neither is, the installer falls back to
`~/.local/bin` while `make` still uses `/usr/local`, leaving two copies for
`PATH` to choose between — pass `PREFIX=$HOME/.local` to `make` to match.)

```bash
make dev       # build, install, restart — this is the daily loop
```

`make dev` takes seconds because it links Homebrew's `whisper-cpp` rather than
compiling it. It stops whatever is listening, installs the new binary and
starts it again in the background, then prints the `tail -f` command for the
log so you can watch what it does.

One thing to expect: every rebuild changes the binary, and macOS ties the
Accessibility grant to the binary it was given to. So a fresh build can find
itself without keyboard access and say so on startup — `flowlite doctor` will
tell you, and you re-allow it in **System Settings → Privacy & Security →
Accessibility**.

```bash
make run       # full static release build, installed and started in background
make restart   # stop the daemon and start it again, no rebuild
```

Use `make run` once before tagging, to confirm the real release binary behaves
the same as your dev build. Its first run is slow — it clones and compiles
whisper.cpp — and cached after that.

Three hidden root flags exist for plumbing and are not part of the user
interface: `--daemon` (what "Start in background" spawns), `--pill-preview
<edge>` and `--play-cues` (what the settings menu spawns, because the pill
needs a fresh AppKit main loop).

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
| transcript history | `internal/history` — `history.jsonl`, last 100 |
| orchestration | `internal/daemon` |
| commands | `internal/cli` |

The macOS release binary statically links whisper.cpp and ggml with the Metal
library embedded, targets macOS 13+, and depends only on system frameworks.

### Versioning and releases

Semantic versioning, and **releases are automatic**.
`.github/workflows/release.yml` runs on every push to `main` that touches code.
GitHub's own Apple Silicon runner runs the tests, builds the self-contained
macOS binary and the Windows zip, writes `SHA256SUMS`, creates the tag and the
Release. The curl installer and `flowlite update` pick it up within minutes.

Everything below follows from one rule: **the version is decided by whether
`VERSION` already has a git tag.**

| `VERSION` says | Meaning | What ships |
| --- | --- | --- |
| a version that is already tagged | an ordinary code push | the next patch — 0.4.8 → 0.4.9 |
| a version with no tag yet | only a human writes this | exactly that version |

So a patch release needs no thought, and a minor or major release is always a
deliberate act. Nothing can reach 1.0.0 by accident.

#### A normal release — patch

**Do not touch `VERSION`.** Just push code:

```bash
git add -A && git commit -m "fix: pill not rendering after wake" && git push
```

The runner tags `v0.4.9`, publishes the Release, and commits the new `VERSION`
back to `main` for you.

#### A major or minor release

**Edit `VERSION` first.** That is the one deliberate step:

```bash
echo 1.0.0 > VERSION                                  # or 0.5.0 for a minor
$EDITOR CHANGELOG.md                                  # what changed
$EDITOR release-notes.md                              # what the Release page shows
git add -A && git commit -m "v1.0.0" && git push
```

A `VERSION` that is not `X.Y.Z`, or that is not newer than the latest tag,
**fails the build** rather than publishing something wrong.

#### Three things worth knowing

- **Pull before you start work again.** Every patch release pushes a `VERSION`
  commit to `main`. Without `git pull` your next push conflicts.
- **Doc-only pushes release nothing.** `**.md` and `.github/**` are ignored, so
  a README fix on its own ships no binary. That is usually what you want — but
  it is why "nothing happened" is sometimes the correct outcome.
- **`release-notes.md` is the text on every Release page.** It is not generated
  from the CHANGELOG, so edit it *before* you push a version people will read.

Watch a run on the repository's **Actions** tab. `make publish` does the same
from your machine if you ever need to release by hand (it insists HEAD is
tagged `v<VERSION>` and the tree is clean). Untagged local builds report
`v0.5.0-dev+<sha>` so they can never be mistaken for the release.

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
