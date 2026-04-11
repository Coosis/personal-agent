"""Personal Agent graph package skeleton."""

__all__ = ["build_graph"]


def build_graph(*args, **kwargs):
    from .graph import build_graph as _build_graph

    return _build_graph(*args, **kwargs)
