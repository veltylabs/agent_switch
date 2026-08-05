# agent-switch — Database Diagram

```mermaid
flowchart TD
    A[agent_switch]
    A --> B[id: string PK]
    A --> C[is_enabled: bool NOT NULL]
    A --> D[changed_by: string NOT NULL]
    A --> E[changed_at: int64 NOT NULL]
    A --> F[reason: string nullable]
```

> **Read strategy:** `SELECT ... ORDER BY changed_at DESC LIMIT 1` — latest row = current state.
> INSERT only. No UPDATE. No DELETE.
