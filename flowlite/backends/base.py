"""Backend interface."""

from abc import ABC, abstractmethod

import numpy as np

from ..models import ModelInfo


class Backend(ABC):
    """Wraps one inference engine around one model.

    Instances are created per model and kept alive between dictations so the
    weights stay resident.
    """

    id: str = ""
    label: str = ""

    def __init__(self, info: ModelInfo, language: str = ""):
        self.info = info
        self.language = language

    @staticmethod
    @abstractmethod
    def is_available() -> bool:
        """Whether this engine can run on this machine at all."""

    @abstractmethod
    def describe_device(self) -> str:
        """Short human-readable note about what the engine will run on."""

    @abstractmethod
    def load(self) -> None: ...

    @abstractmethod
    def unload(self) -> None: ...

    @property
    @abstractmethod
    def loaded(self) -> bool: ...

    @abstractmethod
    def transcribe(self, audio: np.ndarray) -> str:
        """Transcribe 16 kHz mono float32 audio."""
