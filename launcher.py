"""Frozen-app entry point.

PyInstaller runs its entry script as __main__ with no package context, so
flowlite/__main__.py cannot be used directly — its relative imports would
fail. This module keeps the package import intact.
"""

import multiprocessing
import sys


def self_test() -> int:
    """Diagnose a packaged build: `FlowLite --self-test`.

    Checks the things that break when an app is frozen but work fine from
    source — a missing SSL module, an engine whose native library did not get
    bundled, an unwritable model directory.
    """
    ok = True

    def check(label, fn):
        nonlocal ok
        try:
            detail = fn()
        except Exception as exc:
            ok = False
            print(f"  FAIL  {label}: {type(exc).__name__}: {exc}")
        else:
            print(f"  ok    {label}{f': {detail}' if detail else ''}")

    print(f"FlowLite self-test (frozen={getattr(sys, 'frozen', False)})")

    def _ssl():
        import ssl
        return ssl.OPENSSL_VERSION

    def _certs():
        import certifi
        from pathlib import Path
        path = Path(certifi.where())
        assert path.exists(), f"cacert.pem missing at {path}"
        return f"{path.stat().st_size // 1024} KB"

    def _network():
        import urllib.request
        with urllib.request.urlopen("https://huggingface.co/api/models/ggerganov/whisper.cpp",
                                    timeout=15) as r:
            return f"HTTP {r.status} from huggingface.co"

    def _engine():
        from flowlite import backends
        picked = backends.pick()
        from flowlite import models
        return f"{picked.label} — {picked(models.default_for(picked.id)).describe_device()}"

    def _audio():
        from flowlite.audio import default_input_name
        return default_input_name()

    def _paths():
        from flowlite.paths import models_dir
        d = models_dir()
        probe = d / ".writable"
        probe.write_text("x")
        probe.unlink()
        return str(d)

    def _clipboard():
        from flowlite.inject import get_clipboard, set_clipboard
        before = get_clipboard()
        set_clipboard("flowlite-self-test")
        assert get_clipboard() == "flowlite-self-test"
        set_clipboard(before or "")
        return "read/write round-trip"

    check("ssl module", _ssl)
    check("CA certificates", _certs)
    check("network to huggingface.co", _network)
    check("speech engine", _engine)
    check("microphone device", _audio)
    check("model directory writable", _paths)
    check("clipboard", _clipboard)

    print("PASS" if ok else "FAIL")
    return 0 if ok else 1


if __name__ == "__main__":
    # Without this, any accidental process spawn in a frozen app re-runs the
    # whole GUI instead of the child target.
    multiprocessing.freeze_support()
    if "--self-test" in sys.argv:
        sys.exit(self_test())
    from flowlite.__main__ import main

    sys.exit(main())
