# Changelog

All notable changes to FlowLite. Versions follow [Semantic Versioning](https://semver.org):
MAJOR for incompatible changes, MINOR for new behaviour, PATCH for fixes.

## [0.4.0] — unreleased

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
- **Pill redesign:** pure black capsule with a fainter border, 100 px from the
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
