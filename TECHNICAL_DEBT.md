# Technical debt

This file tracks known development and maintenance costs that are intentionally
deferred. Add new items with an identifier, status, impact, and target outcome.

## TD-001: Live development workflow

- Status: open
- Priority: medium
- Area: developer experience, console, Go backend

### Problem

The console is copied into `engine/internal/api/static` and embedded in the Go
binary. Every HTML, CSS, or JavaScript edit therefore requires rebuilding and
restarting Kapkan. Backend edits also require a manual build and restart.

### Target outcome

- Add a development-only console directory option so UI assets are read from
  `console/` on every request and become visible after a browser refresh.
- Keep `go:embed` as the production and release default.
- Add a Go file watcher that performs an incremental build and restarts only the
  DEV process after backend changes.
- Ensure UI-only changes neither rebuild nor restart the backend.
- Document and test the development workflow without changing release builds.
