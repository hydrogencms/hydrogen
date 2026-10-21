Closes #

## Summary

<!-- What changes and why; one issue per pull request. -->

## Principles assessment

<!-- Measure the change against Hydrogen's core principles (CONTRIBUTING.md): lightweight (binary, memory,
start-up, admin bundle, dependencies) and powerful (the capability it adds), plus each rule it touches (core vs
module, storage interfaces, transactions and outbox, determinism, budgets). Give numbers for hot paths and budgets. -->

## Compromises

<!-- Every principle this change bends to fit an important capability: what, why it is worth it, what bounds the
cost. "None" if there are none. Pull requests that break a principle without listing it here, or whose compromises
don't fit, are rejected. -->

## Checks

- [ ] `make tidy vet lint test smoke`
- [ ] `make sdk` / `make admin` (when packages or the admin changed)
- [ ] Docs in `docs/` and a `CHANGELOG.md` entry
