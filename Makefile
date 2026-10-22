.PHONY: build test test-integration vet fmt-check discovery auth-check up down migrate sync-full sync-incremental

DEV_COMPOSE = docker compose -f deploy/compose.dev.yml
# Integration tests create and drop their own databases on this server.
TEST_DATABASE_URL ?= postgres://planner:planner-dev-only@localhost:5432/postgres?sslmode=disable

build:
	go build -o bin/redmine-discovery ./cmd/redmine-discovery
	go build -o bin/csv-compare ./cmd/csv-compare
	go build -o bin/sync-worker ./cmd/sync-worker

# Unit tests only; database tests skip without TEST_DATABASE_URL.
test:
	go test ./...

# Needs `make up` first.
test-integration:
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -count=1 ./...

vet:
	go vet ./...

fmt-check:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && echo "Run gofmt -w ." && exit 1)

up:
	$(DEV_COMPOSE) up -d --wait

down:
	$(DEV_COMPOSE) down

# Reads DATABASE_URL from .env or the environment.
migrate: build
	./bin/sync-worker -migrate

# Read REDMINE_URL, REDMINE_API_KEY and DATABASE_URL from .env or the environment.
sync-full: build
	./bin/sync-worker -once full

sync-incremental: build
	./bin/sync-worker -once incremental

auth-check: build
	./bin/redmine-discovery -auth-only

discovery: build
	./bin/redmine-discovery -out discovery-out
