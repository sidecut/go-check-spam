# Makefile for the rcheckspam project (Rust)

BINARY_NAME := rcheckspam

.PHONY: all
all: build

# Build a debug binary.
.PHONY: build
build:
	@cargo build

# Build an optimized release binary.
.PHONY: build-prod
build-prod:
	@cargo build --release

# Remove build artifacts.
.PHONY: clean
clean:
	@cargo clean

# Build and run the application.
.PHONY: run
run:
	@cargo run

# Run with debug output enabled.
.PHONY: run-debug
run-debug:
	@cargo run -- --debug

# Run the tests.
.PHONY: test
test:
	@cargo test

# Format the code.
.PHONY: fmt
fmt:
	@cargo fmt

# Lint the code.
.PHONY: lint
lint:
	@cargo clippy --all-targets -- -D warnings

# Development workflow.
.PHONY: dev
dev: fmt lint build

# CI workflow.
.PHONY: ci
ci: fmt lint test build

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build       - Build a debug binary"
	@echo "  build-prod  - Build an optimized release binary"
	@echo "  clean       - Remove build artifacts"
	@echo "  run         - Build and run the application"
	@echo "  run-debug   - Run with debug output enabled"
	@echo "  test        - Run the tests"
	@echo "  fmt         - Format the code"
	@echo "  lint        - Lint the code with clippy"
	@echo "  dev         - fmt + lint + build"
	@echo "  ci          - fmt + lint + test + build"
