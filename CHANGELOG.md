# Changelog

All notable changes to FlowLite. Versions follow [Semantic Versioning](https://semver.org):
MAJOR for incompatible changes, MINOR for new behaviour, PATCH for fixes.

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
