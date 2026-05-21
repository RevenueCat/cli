.PHONY: build test check fmt fmt-check vet lint cover install-hooks lefthook

build:
	go build ./...

test:
	go test -race ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

fmt:
	gofmt -w ./internal ./cmd

fmt-check:
	@out=$$(gofmt -l ./internal ./cmd); \
	if [ -n "$$out" ]; then \
		echo "unformatted files:"; echo "$$out"; exit 1; \
	fi

vet:
	go vet ./...

lint:
	@command -v staticcheck >/dev/null || { echo "staticcheck not installed: go install honnef.co/go/tools/cmd/staticcheck@latest"; exit 1; }
	staticcheck ./...

check: fmt-check vet test

tidy:
	go mod tidy

install-hooks:
	@command -v lefthook >/dev/null || go install github.com/evilmartians/lefthook@latest
	lefthook install
