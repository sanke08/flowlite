# Changelog

All notable changes to FlowLite. Versions follow [Semantic Versioning](https://semver.org):
MAJOR for incompatible changes, MINOR for new behaviour, PATCH for fixes.

## [Unreleased]

## [0.5.0] — 2026-09-06

### Added
- **Transcript history panel.** Hold the hotkey together with Right Shift and
  the pill morphs into a scrollable list of recent transcripts; click one to
  paste it again. Escape, or a click anywhere else, closes it.
- **"Remember transcripts" setting** in `flowlite settings`, and a "Clear all
  history" action under "Recent transcripts". Off means nothing new is
  recorded; existing history stays until cleared.

- **FlowLite always runs in the background now.** `flowlite` starts it and
  gives the terminal straight back; closing that window, or pressing Ctrl+C in
  it, no longer stops dictation. `flowlite stop` is the one way to stop it.
  (`flowlite --no-paste` still runs in the terminal, because printing
  transcripts instead of pasting them needs somewhere to print.)
- `flowlite reload` makes a running FlowLite pick up new settings or a newly
  installed binary, in place.

### Fixed
- **Sound cues stuttered or went silent on macOS.** Three causes, all fixed:
  the miniaudio output stream audibly stutters on macOS 26 even when it is fed
  on time, so cues now play through the system's own AVAudioPlayer, one
  preloaded player per cue; opening the microphone on every key-down briefly
  attached the capture unit to the speaker and reset the speaker session under
  the live cue stream, so the microphone unit is now opened once and only
  started and stopped per dictation (key-down is faster for it, and the mic
  indicator behaves as before); and the "still transcribing" tick had been
  set to fire twenty times a second, which the ear hears as one buzzing tone.
- The cue synthesiser renders at 48 kHz, the rate the built-in speakers run
  at, instead of 44.1 kHz resampled on the way out.
- On Windows and Linux, which keep the miniaudio player, cues are rendered
  before the output device opens, the real-time callback never blocks on a
  lock, and a stream that stops asking for samples is rebuilt automatically.
- The clipboard restore after a paste no longer blocks the daemon's event
  loop; shutdown waits for a pending restore so the transcript is never left
  on the clipboard.

### Changed
- **The "working" cue is a clock ticker**: a sharp click over a short woody
  knock, every 150 ms while transcription runs.
- History entries store only time and text. Older history files with the
  previous `pasted`/`audio_seconds` fields remain readable.

- **Quitting during a transcription could crash.** `Model.Close` called
  `whisper_free` without waiting for `whisper_full`, so Ctrl+C mid-dictation —
  or any restart triggered by `settings` or `update` — was a use-after-free.
- **A denied microphone looked like a working app.** Recording without the
  permission does not fail; it returns silence. Nothing checked for it, so
  every dictation ended in "Nothing heard" while `doctor` reported all green.
  The permission is now requested at startup, checked by `doctor` before it
  lists devices, and reported as "Microphone blocked" rather than blamed on
  the user.
- **A slow app could receive the previous clipboard instead of the
  transcript.** The old contents were restored on a fixed timer, whether or
  not the paste had landed. The restore now waits longer and only happens if
  the clipboard is still the one we wrote — so a Cmd+C during the wait is no
  longer overwritten either.
- **A paste that silently did nothing reported success.** Neither the
  clipboard write nor the Cmd+V keystroke checked its result, so revoking
  Accessibility mid-session produced the success chime, a "Pasted" history
  entry, and no text anywhere. Both now report failure.
- **Upgrading left the old version running.** Neither installer stopped the
  daemon first, so on macOS the new binary was installed under a still-running
  old one, and on Windows the copy failed outright against the locked `.exe`.
  Both now stop it, replace it, and start it again if it had been running.
- **`uninstall` recreated the directory it had just deleted** by racing the
  daemon it killed; it now waits for the process to exit.
- **Settings, updates and upgrades now apply themselves, without stopping
  anything.** A running FlowLite reloads in place: it replaces its own process
  image, keeping the same pid, the same terminal and the same Accessibility
  grant. Previously the only way to apply a change was to stop the daemon and
  respawn it detached, which took over the terminal tab someone was working in
  and moved it to the mode that can lose that grant. There is nothing to
  restart by hand in either mode now.
- **That in-place reload could leave sound stuttering or silent.** Replacing
  the process image hands CoreAudio a new audio client under the very pid
  whose previous one it was still tearing down, with no gap for that to
  settle — unlike a cold start, where the old pid is a separate process
  already on its way out. Reopening the output device now retries briefly on
  failure instead of giving up silently, and the reload pauses just long
  enough after closing the old device for the teardown to finish before
  opening the new one.
- **Applying settings or an update said "applying…" and handed the prompt
  back before FlowLite had actually reloaded.** Signalling the reload was
  instant; the model, sounds and hotkey coming back was not, so trying
  FlowLite in that window looked like it was broken. `settings`, `update` and
  `reload` now wait for the daemon to actually come back and confirm it
  before returning.

- `flowlite doctor` and the settings menu now say whether FlowLite is running
  in a terminal or in the background, because what you would do to it differs.
- Releases: editing `VERSION` by hand now releases that exact version, which is
  the only way to reach a new minor or major. Leaving it alone still bumps the
  patch automatically.

## [0.4.9] — 2026-09-05

### Fixed
- **The pill could stop appearing after the Mac woke from sleep.** The overlay
  window was built once per process and only ordered in and out after that, so
  a stale Space assignment left it drawing at full opacity on a Space nobody
  was looking at — recording, transcription and pasting all worked, and only
  the pill was missing. It now re-asserts its window traits on every show,
  checks it landed on the active Space and rebuilds the window if it did not,
  and drops the window on wake and on display changes.

### Changed
- The pill sits flush against the screen edge on the left and right, and
  clears the camera notch and menu bar at the top.
- `make dev`, `make run` and `make restart` build, install and restart FlowLite
  locally; `make dev` runs in the foreground so the Accessibility grant follows
  the terminal and survives rebuilds.

## [0.4.8] — 2026-09-04

### Added
- `install.ps1`, a one-line PowerShell installer for Windows.

## [0.4.7] — 2026-09-04

### Fixed
- Audio underruns during high CPU load: the playback period is now 25 ms.

## [0.4.6] — 2026-09-04

### Changed
- The Working cue is driven by state rather than played manually, and master
  peak volume is higher.

## [0.4.5] — 2026-09-04

### Added
- The background daemon restarts itself after a binary update.

## [0.4.4] — 2026-09-04

### Changed
- Darker, weightier synth cues, and a precise mechanical tick while working.

## [0.4.3] — 2026-09-04

### Added
- The background daemon restarts automatically when settings change.

## [0.4.2] — 2026-09-04

### Added
- `flowlite start` and `flowlite stop`.
- The command list is shown after first-time setup.

## [0.4.1] — 2026-09-04

### Added
- `flowlite uninstall` as a top-level command.
- Automatic patch-version bump and release on every push to `main`.

### Changed
- The pill sits closer to the screen edge.

## [0.4.0] — 2026-09-04

### Removed
- **Command surface reduced to four verbs: `flowlite`, `flowlite settings`,
  `flowlite doctor`, `flowlite update`.** All other subcommands are gone;
  their functions live in `settings` (models, key, pill position, microphone,
  language, sounds, test microphone, recent transcripts, background daemon,
  reset, uninstall). This is a breaking change for anyone who scripted the
  old commands (`run`, `setup`, `start`/`stop`/`status`, `test`, `models`,
  `use`/`download`/`remove`, `key`, `mic`, `lang`, `sounds`, `pill`,
  `history`, `last`, `version`, `install`, `uninstall`, `pill-demo`,
  `sound-demo`). The status page that bare `flowlite` used to print is gone
  too; its facts moved to the listening banner and to `doctor`'s header.

### Changed
- **`flowlite` now starts dictating.** On first use it runs the setup wizard
  first; if the keyboard permission is missing it prints the fix and stops;
  if a daemon is already listening it says so and exits instead of starting a
  second one. The foreground is the default; running in the background is a
  `settings` option. `flowlite --no-paste` prints transcripts instead of
  pasting.
- `install.sh` copies the binary into place itself and ends by launching
  `flowlite`, which runs setup.
- **Redesigned audio cues:** all six sounds are now one instrument family in
  D major — additive struck-body timbres, detuned layers, a short room tail and
  warm saturation. Start/Stop rise and fall over a soft sub body, Done chimes,
  Working is a barely-there wood tock, Cancel sighs downward, Error is a dull
  tritone descent. `flowlite settings → Sounds → Play the cues` plays them.
- **Pill redesign:** pure black capsule with a fainter border, set in from the
  physical screen edge (over the Dock at the bottom). No text or glyphs at all:
  a waveform while recording, a shimmer sweeping across settled bars while
  processing (the spinner is gone), two red pulses on failure; success and
  cancel simply fade out.
- A lone tap no longer starts a session: it is discarded silently, and the pill
  and start sound only appear once a gesture is confirmed (hold threshold
  reached, or second tap).
- Esc before a gesture is confirmed discards quietly instead of flashing
  "Cancelled".

### Added
- **`flowlite update`** downloads the latest release for this machine, verifies
  it and that it runs, then swaps it in atomically (`--check` to only look).
  The listening banner and `doctor` mention a newer release at most once a
  day, only in a terminal; `FLOWLITE_NO_UPDATE_CHECK=1` silences it.
- **`flowlite settings`:** one interactive menu showing every setting's current
  value — model, key, hold threshold, pill edge, microphone, language, sounds —
  plus test microphone, recent transcripts (pick one to copy it), background
  daemon start/stop/restart, reset to defaults and uninstall.
- **Hands-free dictation:** double-tap the dictation key and recording continues
  with nothing held; one press stops it. Holding the key is still push-to-talk.
- **Triple-tap pastes your last transcript again** — recovery for when no text
  field had focus when the words arrived.
- **Transcript history:** the last 100 transcripts are kept in `history.jsonl`,
  including ones whose paste failed; browse and copy them under
  `flowlite settings → Recent transcripts`.
- `flowlite doctor` opens with version, build, whisper.cpp version, update
  status and daemon state before the slower checks.
- Terminal spinners for slow steps: `doctor` checks update in place, the model
  load and transcription animate, downloads show "Connecting…" until the bar
  starts. Piped output stays clean.
- `VERSION` file as the single source of truth for the version; `make publish`
  refuses to run unless HEAD is tagged exactly `v<VERSION>`.
- **Automatic releases:** pushing to `main` with a new `VERSION` builds both
  installers on GitHub Actions, tags, and publishes a Release with
  `SHA256SUMS`. No manual step.
- **Pill position** setting (`pill_position`: bottom, top, left, right) with a
  live preview from `settings`. On the left or right edge the pill stands
  upright.

### Fixed
- Installer shows a real progress readout (MB downloaded / total / percent)
  instead of curl's unknown-size animation.

## [0.3.1] — 2026-09-05

### Fixed
- `flowlite version` now reports the real whisper.cpp version in release
  builds instead of "homebrew".
- The v0.2.1 release binary was built with a minimum macOS of 26 and would not
  start on earlier systems; that release has been withdrawn. Builds from 0.3.0
  on target macOS 13+.

## [0.3.0] — 2026-09-05

### Changed
- **One model on disk.** `flowlite use <name>` downloads the model if needed,
  makes it active, and removes every other installed model. The previous
  model is deleted only once the new one is completely on disk, so an
  interrupted download can never leave you without a working model.
- `flowlite download` is now an alias of `flowlite use`.
- `flowlite models`, the status page and `doctor` warn when more than one
  model is present.

### Added
- `flowlite version`: version, commit, build date, whisper.cpp version.
- `make publish`: tag-gated upload to GitHub Releases.

## [0.2.1] — 2026-09-05

### Added
- Self-contained release binary: whisper.cpp + ggml statically linked with the
  Metal library embedded. One 12 MB file, no Homebrew.
- One-line installer (`install.sh`) that avoids Gatekeeper quarantine.
- Running an unconfigured binary goes straight into setup; setup offers to
  install onto PATH.

## [0.2.0] — 2026-09-05

### Changed
- Rewritten as a Go CLI. The Python/PySide6 app is retired (tag `python-v0.1.0`).

### Added
- Minimal animated pill: waveform → spinner → check/×, no text.
- Synthesised audio cues: start, stop, working tick, done, cancel, error.
- `flowlite doctor` names the app that must hold Accessibility and how to grant it.
- Windows build (hook, paste, GDI pill) — cross-compiled, unverified.

## [python-v0.1.0] — 2026-09-04

- Original Python/Qt implementation. Recoverable with `git checkout python-v0.1.0`.
