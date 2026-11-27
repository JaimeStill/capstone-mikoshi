# capstone-mikoshi standards

The judgement calls the standards-reviewer applies to capstone-mikoshi, beyond what `mise run check`
enforces. Each is one line; the architecture pages hold the reasoning.

- The `web/` boundary mirrors the web stack: elements in its components layer, stateful client services in its app-SDK layer, each shaped to graduate (`mikoshi-web-graduation`); nothing outside `web/` imports from it but the composition root.
- `architecture/standards/go-elemental/principles/topology-and-naming.md`: `cmd/server`, `internal/app`'s layer files, and the domain packages.
- `architecture/principles/composition-root.md`: `internal/app` builds each domain service from the infrastructure nodes it uses.
- `architecture/principles/comments.md`: every comment.
