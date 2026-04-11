"""Personal Agent Processor - Document processing pipeline."""
__version__ = "0.1.0"

__all__ = ["main"]


def main(*args, **kwargs):
    from processor.__main__ import main as _main

    return _main(*args, **kwargs)
