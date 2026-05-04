# Frontend Agent Changelog

<!-- Entries are appended newest-first. Format:
## YYYY-MM-DD — Issue #NNN: <title>
**PR**: #NNN
**Files changed**:
- `path/to/file.tsx` — short description
**Summary**: One sentence summary of what was done and why.
-->

## 2026-05-04 — Issue #1139: feat(frontend): add Authorization header to all BFF requests
**PR**: #1150
**Files changed**:
- `frontend/src/adapters/` — added Authorization header injection to all BFF fetch calls via the REST API adapter layer
**Summary**: Wired the auth token into every outbound BFF request so authenticated endpoints receive the Authorization header; implemented at the adapter layer to keep components free of auth concerns.
