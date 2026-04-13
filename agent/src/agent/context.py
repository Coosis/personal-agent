from agent.db import AgentDB
from agent.embedding import AgentEmbeddingService


class AppContext:
    db: AgentDB
    embd_svc: AgentEmbeddingService

    def __init__(self, cfg):
        self.db = AgentDB(cfg)
        self.embd_svc = AgentEmbeddingService(cfg)
