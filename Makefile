.PHONY: build test check fmt fmt-check vet lint cover install-hooks gen

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
	@command -v golangci-lint >/dev/null || { echo "golangci-lint not installed: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2"; exit 1; }
	golangci-lint run ./...

check: fmt-check vet test

tidy:
	go mod tidy

install-hooks:
	@command -v lefthook >/dev/null || go install github.com/evilmartians/lefthook@latest
	@if command -v lefthook >/dev/null; then \
		lefthook install; \
	else \
		PATH="$(shell go env GOPATH)/bin:$$PATH" lefthook install; \
	fi

gen: ## Regenerate API types from OpenAPI spec
	@if ! python3 -c "import yaml" 2>/dev/null; then \
		echo "Installing pyyaml into /tmp/pyyaml-env ..."; \
		python3 -m venv /tmp/pyyaml-env && /tmp/pyyaml-env/bin/pip install -q pyyaml; \
		PYTHON=/tmp/pyyaml-env/bin/python3; \
	else \
		PYTHON=python3; \
	fi; \
	$$PYTHON scripts/preprocess-spec.py docs/specs/v2-developer.yaml /tmp/v2-clean.yaml docs/specs/v2-beta-overlay.yaml && \
	$$PYTHON scripts/gen-paths.py /tmp/v2-clean.yaml docs/specs/cli-coverage.yaml internal/api/paths_gen.go
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config oapi-codegen.yaml /tmp/v2-clean.yaml

preview: ## Render output snapshots to SVGs in docs/previews (brew install charmbracelet/tap/freeze)
	./scripts/preview.sh
