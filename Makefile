# Go modules in this repository (core first). Every target runs over all of them.
MODULES := . modules/sqlite modules/postgres tests examples/minimal examples/content examples/headless examples/multisite

.PHONY: build test vet lint tidy tidy-check db-up db-down smoke check sdk next-e2e admin admin-types admin-dist-check admin-e2e example-admin example-admin-check

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

# Local Postgres 17 on localhost:5436 (hydrogen/hydrogen) for adapter tests and development.
db-up:
	docker compose -p hydrogen up -d --wait

db-down:
	docker compose -p hydrogen down

# hydrogen new → config:check → migrate → serve, with binary size, cold start and idle memory budgets.
smoke:
	./scripts/smoke.sh

# TypeScript SDK (packages/): install, build, test, gzip size budget.
sdk:
	cd packages && npm install --no-audit --no-fund && npm run build && npm test && npm run size

# examples/next-site against examples/multisite (SQLite): build, start, curl assertions, signed-webhook revalidation.
next-e2e:
	./scripts/next-e2e.sh

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

check: tidy-check vet lint test smoke admin admin-dist-check example-admin-check
