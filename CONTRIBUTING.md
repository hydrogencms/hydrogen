# Contributing to Hydrogen

Every change reaches `main` through a pull request, and a pull request solves one issue. Each pull request is
assessed against Hydrogen's core principles before it is merged. This applies to people and to coding agents alike.

To run a working copy, see [DEVELOPMENT.md](DEVELOPMENT.md).

## Core principles

**Lightweight and powerful.** A change is judged on both counts:

- **Lightweight:** it keeps the binary, memory, start-up time, admin bundle and dependency list small.
- **Powerful:** it adds real capability.

In practice:

- **Dependencies.** Prefer the standard library. Every new Go or npm dependency needs a reason, and a small piece of
  code beats a large import. Default builds use no cgo, and Hydrogen uses no web framework or ORM.
- **Core stays small.** Anything a project can live without is a module registered through the kernel. Heavy admin
  features load lazily.
- **Headless.** Core never renders a public site and makes no web assumptions. URLs, the page tree and web matchers
  live in the `pages` module, and fullstack rendering lives in the `site` module.
- **Storage.** The database is reached only through the storage interfaces. Nothing queries inside JSON documents:
  filters go through indexed fields. Every storage change passes the conformance suite on every adapter.
- **Mutations.** Each mutation is one transaction with validated documents, plus an audit event and an outbox event
  in that same transaction. Outbox consumers are idempotent.
- **Determinism.** There is no `init()` registration and no package-level mutable state. Time and randomness go through
  injectable interfaces, and map iteration order never reaches output.
- **Content.** Schema lives in project files, and generated code is never edited by hand. Content holds no raw CSS or
  HTML: styling goes through design tokens.
- **Budgets.** The binary, cold start, idle memory and npm gzip sizes have budgets, checked by `make smoke` and
  `make sdk`. A change that needs more room makes the case for it.
- **Comments.** Keep them minimal: only where the implementation is not obvious.

## Pull requests

1. Branch from `main` and name the branch after the issue: `issue-25-delivery-fields`.
2. Keep to one issue per pull request, and put `Closes #<n>` in the description.
3. Run the full gate before asking for review: `make tidy vet lint test smoke`, plus `make sdk` or `make admin` when
   those parts changed. Commit `admin/dist` with any change under `admin/ui`.
4. Fill in the [template](.github/pull_request_template.md):
   - **Principles assessment:** how the change measures against each relevant principle above, with numbers
     (size, memory, allocations, latency) where it touches a budget or a hot path.
   - **Compromises:** every place where the change bends a principle to fit an important capability, why it is worth
     it, and what keeps the cost bounded. Write "None" when there are none.
5. Update the public docs in `docs/` and add an entry under `[Unreleased]` in `CHANGELOG.md`.

**Review rule.** A pull request is rejected when it breaks a principle and doesn't list the compromise, or when the
compromises it lists don't fit: when the capability isn't worth what it costs, or a lighter design would do the same
job.

Releases, tags and npm publishing stay with the maintainers (`scripts/release.sh`). The `release` branch moves only
through that script or a hotfix pull request.
