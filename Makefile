# Go modules in this repository (core first). Every target runs over all of them.
MODULES := . modules/sqlite modules/postgres tests examples/minimal examples/content examples/headless examples/multisite

.PHONY: build test vet lint tidy tidy-check db-up db-down smoke check sdk next-e2e

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

check: tidy-check vet lint test smoke
