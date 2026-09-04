# FlowLite releases

Built installers live here, one folder per version.

```
app/
  Fix-Gatekeeper.command                       helper shipped inside the DMG
  v0.1.0/
    FlowLite-0.1.0-macOS-arm64.dmg             Apple Silicon      [built]
    FlowLite-0.1.0-macOS-arm64.dmg.sha256                         [built]
    FlowLite-0.1.0-Windows-x64.zip             Windows 10/11 x64  [see below]
    FlowLite-0.1.0-Windows-x64.zip.sha256                         [see below]
```

### The Windows build is not here yet

PyInstaller cannot cross-compile — a Windows `.exe` has to be built on
Windows. Two ways to get it:

- **Push a tag.** `.github/workflows/release.yml` builds macOS *and* Windows
  on GitHub's runners, self-tests both, and attaches them to a GitHub Release:
  ```bash
  git tag v0.1.0 && git push origin v0.1.0
  ```
  This is the easiest route for an open-source project — no Windows machine
  needed, and it is free for public repos.
- **Or run `build-windows.bat`** on any Windows 10/11 PC with Python 3.12.
  It writes the `.zip` into this same folder.

### Apple Silicon only

The macOS build targets arm64 (M1 and later). Intel Macs have no
`pywhispercpp` wheel and would need the `ct2` extra with the slower
CPU-only engine, so they are not currently a supported target.

**Share the `.dmg` with Mac friends and the `.zip` with Windows friends.**
That single file is everything — no Python, no installer chain. The speech
model is downloaded from inside the app on first launch, so the download stays
small.

---

## Installing — macOS

1. Open the `.dmg`.
2. Drag **FlowLite** onto the **Applications** shortcut.
3. **Double-click `Fix-Gatekeeper.command`** (it is in the same window).
4. Open FlowLite from Applications. It appears in the menu bar — there is no
   Dock icon.

### Why step 3 exists

FlowLite is not signed with a paid Apple Developer certificate ($99/year), so
macOS quarantines it and refuses to launch it. The error message says the app
is **"damaged and can't be opened"**. That message is wrong — nothing is
damaged, macOS simply cannot verify who built it.

`Fix-Gatekeeper.command` runs one command:

```bash
xattr -cr /Applications/FlowLite.app
```

That clears the quarantine flag. Anyone who prefers to type it themselves can
skip the helper entirely.

> Right-click → Open is the usual advice for unsigned apps, but it often does
> not work for ad-hoc-signed apps on Apple Silicon. Use the helper.

### First launch

1. FlowLite opens the model picker — choose one and press **Download**.
2. Go to the **Permissions** tab, press **Grant Accessibility**, switch
   FlowLite on in System Settings.
3. **Quit and relaunch.** macOS only re-checks permissions at startup.
4. Tap **Right Option** in any text field and start talking.

---

## Installing — Windows

1. Unzip anywhere (e.g. `C:\Program Files\FlowLite`).
2. Run `FlowLite.exe`. It appears in the system tray.
3. Pick a model, download it, and tap **Right Control** to dictate.

SmartScreen may warn about an unrecognised app — choose **More info → Run
anyway**. Same cause as the macOS warning: no paid code-signing certificate.
Some antivirus software blocks synthetic keystrokes, in which case FlowLite
records but cannot paste.

---

## Verifying a download

Each artifact ships with a `.sha256` file. To confirm a download is intact:

```bash
shasum -a 256 -c FlowLite-0.1.0-macOS-arm64.dmg.sha256    # macOS
```

```powershell
(Get-FileHash FlowLite-0.1.0-Windows-x64.zip -Algorithm SHA256).Hash
```

---

## Versioning

Semantic versioning, with `flowlite/__init__.py` as the single source of
truth — `pyproject.toml`, the PyInstaller spec, the macOS bundle version and
these filenames all read from it, so a release cannot ship mismatched numbers.

To cut a release: bump `__version__`, run the build script on each OS, and the
artifacts land in `app/v<version>/`.

| Version | Notes |
| --- | --- |
| 0.1.0 | First release. Tap/hold dictation, six selectable models, whisper.cpp with Metal on Apple Silicon. |
