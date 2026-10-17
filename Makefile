# Go modules in this repository (core first). Every target runs over all of them.
MODULES := . modules/sqlite modules/postgres tests examples/minimal examples/content examples/headless examples/multisite examples/starter examples/fullstack

.PHONY: starter-fixture build test vet lint tidy tidy-check db-up db-down smoke check sdk next-e2e starter-next-e2e conformance site-e2e site-bridge site-bridge-check admin admin-types admin-dist-check admin-e2e example-admin example-admin-check

build:
	@for m in $(MODULES); do (cd $$m && go build ./...) || exit 1; done

test:
	@for m in $(MODULES); do (cd $$m && go test -race -count=1 ./...) || exit 1; done

vet:
	@for m in $(MODULES); do (cd $$m && go vet ./...) || exit 1; done

lint:
	@for m in $(MODULES); do (cd $$m && golangci-lint run ./...) || exit 1; done

# Each module must build on its own (GOWORK=off), with its own go.sum.
tidy:
	@for m in $(MODULES); do (cd $$m && GOWORK=off go mod tidy) || exit 1; done

tidy-check: tidy
	@git diff --exit-code -- '*go.mod' '*go.sum' || (echo "go.mod/go.sum not tidy: run make tidy"; exit 1)

# Rebuild examples/starter/fixture (deterministic): run after changing its schema or tools/fixture.
starter-fixture:
	cd examples/starter && go run ./tools/fixture

# Local Postgres 17 on localhost:5436 (hydrogen/hydrogen) for adapter tests and development.
db-up:
	docker compose -f scripts/docker-compose.yml -p hydrogen up -d --wait

db-down:
	docker compose -f scripts/docker-compose.yml -p hydrogen down

# hydrogen new → config:check → migrate → serve, with binary size, cold start and idle memory budgets.
smoke:
	./scripts/smoke.sh

# TypeScript SDK (packages/): install, build, test, gzip size budget.
sdk:
	cd packages && npm install --no-audit --no-fund && npm run build && npm test && npm run size

# examples/next-site against examples/multisite (SQLite): build, start, curl assertions, signed-webhook revalidation.
next-e2e:
	./scripts/next-e2e.sh

# packages/starters/next against examples/starter (SQLite, fixture), checked by the adapter conformance suite with a browser.
starter-next-e2e:
	./scripts/starter-next-e2e.sh

# The conformance suite (packages/adapter-test) against every frontend. A new frontend adds its own e2e target here
# (scripts/starter-next-e2e.sh is the model: it ends in `node packages/adapter-test/dist/cli.js --cms --token --frontend --browser`).
CONFORMANCE_TARGETS := starter-next-e2e site-e2e
conformance: $(CONFORMANCE_TARGETS)

# examples/fullstack served by one binary (admin, APIs, website), checked by the adapter conformance suite with a browser.
site-e2e:
	./scripts/site-e2e.sh

# modules/site/bridge.iife.js is the committed copy of packages/bridge's IIFE build, embedded in the binary.
site-bridge:
	cd packages && npm install --no-audit --no-fund >/dev/null && npm run build -w bridge >/dev/null
	cp packages/bridge/dist/bridge.iife.js modules/site/bridge.iife.js

site-bridge-check:
	cd packages && npm install --no-audit --no-fund >/dev/null && npm run build -w bridge >/dev/null
	@cmp -s packages/bridge/dist/bridge.iife.js modules/site/bridge.iife.js || (echo "modules/site/bridge.iife.js is stale: run make site-bridge"; exit 1)

# Admin app (admin/ui): install, typecheck, unit tests, build into the committed admin/dist, size budgets.
admin:
	scripts/npm-ci.sh admin/ui && cd admin/ui && npm run check && npm test && npm run build && npm run size

# Regenerate admin/ui/src/lib/api/schema.d.ts from a running examples/multisite's OpenAPI document.
admin-types:
	./scripts/admin-types.sh

# admin/dist is committed: fail when a rebuild changes it.
admin-dist-check: admin
	@test -z "$$(git status --porcelain -- admin/dist)" || (git status --short -- admin/dist; echo "admin/dist is stale: run make admin and commit it"; exit 1)

# Playwright against examples/multisite (SQLite) with a seeded administrator; E2E_KEEP=1 leaves the server up.
admin-e2e:
	./scripts/admin-e2e.sh

# examples/content/admin: custom elements on the admin SDK, built into the committed dist/elements.js.
example-admin:
	cd packages && npm install --no-audit --no-fund && npm run build -w admin-sdk
	scripts/npm-ci.sh examples/content/admin && cd examples/content/admin && npm run build

example-admin-check: example-admin
	@test -z "$$(git status --porcelain -- examples/content/admin/dist)" || (git status --short -- examples/content/admin/dist; echo "examples/content/admin/dist is stale: run make example-admin and commit it"; exit 1)

check: tidy-check vet lint test smoke admin admin-dist-check example-admin-check site-bridge-check
