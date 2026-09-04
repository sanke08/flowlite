"""Model downloading with progress reporting.

Progress is measured by polling the size of the destination directory rather
than by hooking huggingface_hub's tqdm internals, which change between
versions and are awkward to route into a Qt signal.
"""

import logging
import shutil
import threading
from collections.abc import Callable

from huggingface_hub import hf_hub_download, snapshot_download
from huggingface_hub.utils import disable_progress_bars

from .models import ModelInfo

log = logging.getLogger(__name__)

# The GUI draws its own progress bar; the hub's would only spam the log file.
disable_progress_bars()

# CTranslate2 needs only these. Repos often also carry a PyTorch copy of the
# weights, which would double the download for nothing.
CT2_PATTERNS = [
    "config.json", "preprocessor_config.json", "model.bin",
    "tokenizer.json", "vocabulary.*",
]

# (downloaded_bytes, total_bytes, status_text)
ProgressFn = Callable[[int, int, str], None]

POLL_SECONDS = 0.25


class DownloadCancelled(Exception):
    pass


def download_model(
    info: ModelInfo,
    backend: str,
    on_progress: ProgressFn | None = None,
    cancel: threading.Event | None = None,
) -> None:
    """Fetch `info` for `backend`, reporting progress as it goes.

    Raises DownloadCancelled if `cancel` is set before the download finishes.
    A partial directory is always removed, so a cancelled or failed download
    never looks complete on the next launch.
    """
    if not info.supports(backend):
        raise ValueError(f"{info.label} is not available for the {backend} engine")

    cancel = cancel or threading.Event()
    spec = info.spec(backend)
    dest = info.local_dir(backend)
    dest.mkdir(parents=True, exist_ok=True)

    if on_progress:
        on_progress(0, spec.size_bytes, "Starting…")

    error: list[BaseException] = []

    def work():
        try:
            if spec.single_file:
                hf_hub_download(
                    repo_id=spec.repo, filename=spec.filename, local_dir=str(dest)
                )
            else:
                snapshot_download(
                    repo_id=spec.repo, local_dir=str(dest),
                    allow_patterns=CT2_PATTERNS, max_workers=4,
                )
        except BaseException as exc:  # re-raised on the calling thread
            error.append(exc)

    worker = threading.Thread(target=work, daemon=True, name=f"dl-{info.key}")
    worker.start()

    while worker.is_alive():
        if cancel.is_set():
            # There is no cancel hook in huggingface_hub. The worker is a
            # daemon so it dies with the process; drop the partial directory
            # now so the model is not mistaken for a complete one.
            shutil.rmtree(dest, ignore_errors=True)
            raise DownloadCancelled()
        if on_progress:
            done = info.disk_bytes(backend)
            on_progress(min(done, spec.size_bytes), spec.size_bytes, "Downloading…")
        worker.join(timeout=POLL_SECONDS)

    if error:
        shutil.rmtree(dest, ignore_errors=True)
        raise error[0]

    if not info.downloaded(backend):
        shutil.rmtree(dest, ignore_errors=True)
        raise RuntimeError(
            f"{info.label} finished downloading but the weights are incomplete. "
            "Check your connection and try again."
        )

    if on_progress:
        on_progress(spec.size_bytes, spec.size_bytes, "Done")


def delete_model(info: ModelInfo, backend: str) -> None:
    shutil.rmtree(info.local_dir(backend), ignore_errors=True)
