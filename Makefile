# Makefile for go-check-spam project
# Build settings

# Makefile for go-check-spam project

BINARY_NAME := go-check-spam

.PHONY: all
all: build

.PHONY: build
build:
	cargo build

.PHONY: build-release
build-release:
	cargo build --release

.PHONY: clean
clean:
	cargo clean

.PHONY: run
run:
	cargo run --

.PHONY: run-debug
run-debug:
	cargo run -- --debug

.PHONY: test
test:
	cargo test

.PHONY: fmt
fmt:
	cargo fmt

.PHONY: lint
lint:
	cargo clippy --all-targets --all-features

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build        - Build the binary"
	@echo "  build-release- Build the optimized binary"
	@echo "  clean        - Remove build artifacts"
	@echo "  run          - Build and run the application"
	@echo "  run-debug    - Run with debug enabled"
	@echo "  test         - Run tests"
	@echo "  fmt          - Format code"
	@echo "  lint         - Run clippy"
	@echo "Running $(BINARY_NAME)..."
