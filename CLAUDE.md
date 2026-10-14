# CLAUDE.md

hydrogencms is a headless, composable CMS in Go (SQLite or Postgres through swappable adapters, Svelte 5 admin,
TypeScript frontend SDK).

**Core principle: lightweight and powerful.** Judge every change by both: does it keep the binary, memory, startup,
admin bundle and dependency list small, and does it add real capability? Prefer the standard library, no cgo,
no web framework or ORM, lazy-load heavy admin features, and put anything most projects don't need in a
module.

If `planning/` exists, read [`planning/README.md`](planning/README.md) first. It is private and gitignored:
never commit it, quote it in commit messages or public docs, or move its content into tracked files unless asked.

## Rules

- General-purpose headless: core never renders a public site and has no web assumptions; URLs, the page tree and
  web matchers live in the `pages` module; fullstack rendering lives in the `site` module.
- Core stays small; features that a project can live without are modules registered through the kernel.
- Every new dependency (Go or npm) needs a reason; prefer writing a small piece over importing a large one.
- No `init()` registration and no package-level mutable state.
- Database access only through the storage interfaces. No queries inside JSON documents; filter through indexed
  fields. Every storage change passes the conformance suite on every adapter.
- No cgo in default builds.
- Every mutation: one transaction, validated documents, audit event and outbox event in the same transaction.
  Outbox consumers are idempotent.
- Time and randomness go through injectable interfaces; never let map iteration order reach output.
- Schema lives in project files; never hand-edit generated code.
- No raw CSS or HTML in content; style through design tokens.
- Comments: minimal; only where the implementation is non-obvious.
- Branch `main`. Commit when asked; push only when asked. Commits: no "Generated with Claude Code" footer.
