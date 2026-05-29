.PHONY: all
all: build

.PHONY: build
build:
	@cargo build

.PHONY: build-release
build-release:
	@cargo build --release

.PHONY: run
run:
	@cargo run -- --oauth-port 8080 --concurrency 8 --days 30

.PHONY: run-debug
run-debug:
	@cargo run -- --debug

.PHONY: test
test:
	@cargo test

.PHONY: fmt
fmt:
	@cargo fmt

.PHONY: lint
lint:
	@cargo clippy -- -D warnings

.PHONY: clean
clean:
	@cargo clean

.PHONY: ci
ci: fmt lint test build

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build         - cargo build"
	@echo "  build-release - cargo build --release"
	@echo "  run           - run with common defaults"
	@echo "  run-debug     - run with --debug"
	@echo "  test          - cargo test"
	@echo "  fmt           - cargo fmt"
	@echo "  lint          - cargo clippy -- -D warnings"
	@echo "  clean         - cargo clean"
	@echo "  ci            - fmt + lint + test + build"
