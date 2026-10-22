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
   those parts changed. `admin/dist` is built, not committed: `make admin` after changing `admin/ui`.
4. Fill in the [template](.github/pull_request_template.md):
   - **Principles assessment:** how the change measures against each relevant principle above, with numbers
     (size, memory, allocations, latency) where it touches a budget or a hot path.
   - **Compromises:** every place where the change bends a principle to fit an important capability, why it is worth
     it, and what keeps the cost bounded. Write "None" when there are none.
5. Update the public docs in `docs/` and add an entry under `[Unreleased]` in `CHANGELOG.md`.

**Review rule.** A pull request is rejected when it breaks a principle and doesn't list the compromise, or when the
compromises it lists don't fit: when the capability isn't worth what it costs, or a lighter design would do the same
job.

## Releasing

Releases, tags and npm publishing stay with the maintainers. The built admin (`admin/dist`) is not on `main`: the
release workflow builds it and records it in a release commit on the `release` branch, which the Go module tags
(`vX`, `modules/<name>/vX`) point at, so `go get` still gets a complete admin without Node. The release commit has
two parents, the source commit on `main` and the previous release, so `release` only fast-forwards.

1. `scripts/release.sh v0.3.0 --bump`, review, commit, push to `main` and wait for CI (`ci-ok`) on that commit.
2. `scripts/release.sh v0.3.0` (the gates; prints the next command), then
   `gh workflow run release.yml -f version=v0.3.0 -f ref=$(git rev-parse HEAD)`. The reviewer of the `release`
   environment approves the run. It refuses a source without a green `ci-ok`, creates the release commit and the
   tags, pushes them, and publishes the GitHub release (and the npm packages when the repository variable
   `NPM_PUBLISH` is `true`).

`scripts/release-commit.sh <version> <source-ref>` does the commit and tags in a clone without pushing (add `--push`
to publish), for trying the flow locally.

**Hotfix.** Branch `hotfix/v0.3.1` from the last release's source (`git switch -c hotfix/v0.3.1 v0.3.0^1`), push it,
open pull requests with the fix into it (CI runs on them), run `scripts/release.sh v0.3.1 --bump` on it and push, then
dispatch the workflow with `-f ref=hotfix/v0.3.1`. Afterwards merge the hotfix branch into `main` by pull request.

**One-time setup (repository admin).**

1. Create a deploy key with write access (`ssh-keygen -t ed25519 -N '' -f release_key`, add `release_key.pub` under
   Settings > Deploy keys, allow write access) and store the private key as the repository secret `RELEASE_DEPLOY_KEY`.
2. In the `release` branch ruleset keep "restrict updates" and "block force pushes" (fast-forward only), and add the
   deploy key as a bypass actor: a release commit has no checks of its own, the workflow's `ci-ok` check on the
   source stands in for them.
3. Create the environment `release` (Settings > Environments) with a required reviewer.
