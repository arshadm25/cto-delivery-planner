.PHONY: build test vet fmt-check discovery auth-check

build:
	go build -o bin/redmine-discovery ./cmd/redmine-discovery

test:
	go test ./...

vet:
	go vet ./...

fmt-check:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && echo "Run gofmt -w ." && exit 1)

# Reads REDMINE_URL and REDMINE_API_KEY from the environment.
auth-check: build
	./bin/redmine-discovery -auth-only

discovery: build
	./bin/redmine-discovery -out discovery-out
