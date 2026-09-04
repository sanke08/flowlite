"""Speech model catalog, per-backend download specs and on-disk state.

The same Whisper weights ship in two incompatible formats: GGML for
whisper.cpp and CTranslate2 for faster-whisper. A ModelInfo therefore holds
one ModelSpec per backend, and every question about it ("how big is it?",
"is it downloaded?") is answered relative to the backend in use.
"""

from dataclasses import dataclass

from .paths import models_dir

WHISPERCPP = "whispercpp"
CT2 = "ct2"

GGML_REPO = "ggerganov/whisper.cpp"
MB = 1024 * 1024


@dataclass(frozen=True)
class ModelSpec:
    """How to fetch one model for one backend."""

    repo: str
    size_bytes: int
    filename: str | None = None   # None means "download the whole repo folder"

    @property
    def single_file(self) -> bool:
        return self.filename is not None


@dataclass(frozen=True)
class ModelInfo:
    key: str
    label: str
    blurb: str
    english_only: bool
    specs: dict[str, ModelSpec]
    recommended_for: tuple[str, ...] = ()

    # -- per-backend queries -------------------------------------------------

    def supports(self, backend: str) -> bool:
        return backend in self.specs

    def spec(self, backend: str) -> ModelSpec:
        return self.specs[backend]

    def is_recommended(self, backend: str) -> bool:
        return backend in self.recommended_for

    def local_dir(self, backend: str):
        return models_dir() / backend / self.key

    def local_path(self, backend: str):
        """The path handed to the backend: a file for GGML, a folder for CT2."""
        spec = self.spec(backend)
        base = self.local_dir(backend)
        return base / spec.filename if spec.single_file else base

    def downloaded(self, backend: str) -> bool:
        if not self.supports(backend):
            return False
        spec = self.spec(backend)
        if spec.single_file:
            path = self.local_path(backend)
            # A partial download would be smaller than the published size; the
            # 5% slack absorbs any metadata difference between formats.
            return path.exists() and path.stat().st_size >= spec.size_bytes * 0.95
        d = self.local_dir(backend)
        return (d / "model.bin").exists() and (d / "config.json").exists()

    def disk_bytes(self, backend: str) -> int:
        d = self.local_dir(backend)
        if not d.exists():
            return 0
        return sum(f.stat().st_size for f in d.rglob("*") if f.is_file())

    def size_bytes(self, backend: str) -> int:
        return self.spec(backend).size_bytes if self.supports(backend) else 0


def _ggml(name: str, mb: int) -> ModelSpec:
    return ModelSpec(repo=GGML_REPO, filename=f"ggml-{name}.bin", size_bytes=mb * MB)


def _ct2(repo: str, mb: int) -> ModelSpec:
    return ModelSpec(repo=repo, size_bytes=mb * MB)


CATALOG: list[ModelInfo] = [
    ModelInfo(
        key="tiny.en", label="Tiny (English)", english_only=True,
        blurb="Fastest and smallest. Usable for short, clearly spoken phrases, "
              "but it will mangle names and technical words.",
        specs={
            WHISPERCPP: _ggml("tiny.en", 74),
            CT2: _ct2("Systran/faster-whisper-tiny.en", 75),
        },
    ),
    ModelInfo(
        key="base.en", label="Base (English)", english_only=True,
        blurb="Still near-instant, and clearly better than Tiny. A good floor "
              "if disk space is tight.",
        specs={
            WHISPERCPP: _ggml("base.en", 141),
            CT2: _ct2("Systran/faster-whisper-base.en", 145),
        },
    ),
    ModelInfo(
        key="small.en", label="Small (English)", english_only=True,
        blurb="Strong everyday English accuracy and comfortably sub-second. "
              "The best speed-to-quality trade-off if you only dictate in English.",
        specs={
            WHISPERCPP: _ggml("small.en", 465),
            CT2: _ct2("Systran/faster-whisper-small.en", 480),
        },
        recommended_for=(CT2,),
    ),
    ModelInfo(
        key="large-v3-turbo-q5", label="Large v3 Turbo (compressed)", english_only=False,
        blurb="Near-best accuracy across 99 languages, handles accents well, and "
              "quantised down to a third of the full size with no noticeable "
              "quality loss. About 1.5 seconds per dictation.",
        specs={WHISPERCPP: _ggml("large-v3-turbo-q5_0", 547)},
        recommended_for=(WHISPERCPP,),
    ),
    ModelInfo(
        key="large-v3-turbo", label="Large v3 Turbo (full)", english_only=False,
        blurb="The uncompressed Turbo weights. Marginally more faithful than the "
              "compressed build, at three times the disk space.",
        specs={
            WHISPERCPP: _ggml("large-v3-turbo", 1549),
            CT2: _ct2("mobiuslabsgmbh/faster-whisper-large-v3-turbo", 1547),
        },
    ),
    ModelInfo(
        key="large-v3", label="Large v3", english_only=False,
        blurb="The most accurate Whisper model. Several times slower than Turbo "
              "for a small quality gain — worth it only for difficult audio.",
        specs={
            WHISPERCPP: _ggml("large-v3", 2952),
            CT2: _ct2("Systran/faster-whisper-large-v3", 3100),
        },
    ),
]

BY_KEY = {m.key: m for m in CATALOG}


def get(key: str) -> ModelInfo | None:
    return BY_KEY.get(key)


def for_backend(backend: str) -> list[ModelInfo]:
    return [m for m in CATALOG if m.supports(backend)]


def default_for(backend: str) -> ModelInfo:
    for m in CATALOG:
        if m.is_recommended(backend):
            return m
    return next(m for m in CATALOG if m.supports(backend))


def human_size(n: int) -> str:
    if n >= 1024 ** 3:
        return f"{n / 1024 ** 3:.2f} GB"
    return f"{n / MB:.0f} MB"
