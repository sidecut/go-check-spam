# Makefile for gocheckspam (Rust version)

BINARY_NAME := gocheckspam

# Color output
RED := \033[0;31m
GREEN := \033[0;32m
YELLOW := \033[0;33m
NC := \033[0m # No Color

# Default target
.PHONY: all
all: build

# Build the binary
.PHONY: build
build:
	@echo "Building $(BINARY_NAME) in debug mode..."
	@cargo build
	@echo "Build complete"

# Build optimized for production
.PHONY: build-prod
build-prod:
	@echo "Building $(BINARY_NAME) for production..."
	@cargo build --release
	@echo "Production build complete"

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning build artifacts..."
	@cargo clean
	@echo "Clean complete"

# Run the application
.PHONY: run
run:
	@echo "Running $(BINARY_NAME)..."
	@cargo run --

# Run with custom flags
.PHONY: run-debug
run-debug:
	@echo "Running $(BINARY_NAME) with debug enabled..."
	@cargo run -- --debug

# Run with custom worker count
.PHONY: run-workers
run-workers:
	@echo "Running $(BINARY_NAME) with 20 workers..."
	@cargo run -- --concurrency 20

# Test the application
.PHONY: test
test:
	@echo "Running tests..."
	@cargo test -- --nocapture

# Format code
.PHONY: fmt
fmt:
	@echo "Formatting code..."
	@cargo fmt

# Lint code (corresponds to vet/lint in Go)
.PHONY: lint
lint:
	@echo "Linting code..."
	@cargo clippy --all-targets --all-features -- -D warnings

.PHONY: vet
vet: lint

# Check/download dependencies
.PHONY: deps
deps:
	@echo "Fetching dependencies..."
	@cargo fetch

# Install the binary
.PHONY: install
install:
	@echo "Installing $(BINARY_NAME) to cargo bin..."
	@cargo install --path .
	@echo "Installation complete"

# Show build info
.PHONY: info
info:
	@echo "Build Information:"
	@echo "  Binary Name: $(BINARY_NAME)"
	@rustc --version
	@cargo --version

# Cross-compile for different platforms
.PHONY: build-linux
build-linux:
	@echo "Building for Linux (x86_64-unknown-linux-gnu)..."
	@cargo build --release --target x86_64-unknown-linux-gnu

.PHONY: build-windows
build-windows:
	@echo "Building for Windows (x86_64-pc-windows-gnu)..."
	@cargo build --release --target x86_64-pc-windows-gnu

.PHONY: build-mac
build-mac:
	@echo "Building for macOS..."
	@cargo build --release

# Build for all platforms
.PHONY: build-all
build-all: build-linux build-windows build-mac
	@echo "Cross-compilation complete"

# Development workflow
.PHONY: dev
dev: clean fmt vet build
	@echo "Development build complete"

# CI/CD workflow
.PHONY: ci
ci: clean fmt vet test build-prod
	@echo "CI build complete"

# Help target
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build        - Build the binary in debug mode"
	@echo "  build-prod   - Build optimized production binary"
	@echo "  clean        - Remove build artifacts"
	@echo "  run          - Run the application"
	@echo "  run-debug    - Run with debug enabled"
	@echo "  run-workers  - Run with 20 workers"
	@echo "  test         - Run tests"
	@echo "  fmt          - Format code"
	@echo "  lint / vet   - Lint code using clippy"
	@echo "  deps         - Fetch dependencies"
	@echo "  install      - Install binary to ~/.cargo/bin"
	@echo "  info         - Show build information"
	@echo "  build-linux  - Cross-compile for Linux"
	@echo "  build-windows- Cross-compile for Windows"
	@echo "  build-mac    - Cross-compile for macOS"
	@echo "  build-all    - Cross-compile for all platforms"
	@echo "  dev          - Development workflow (clean, fmt, clippy, build)"
	@echo "  ci           - CI workflow (clean, fmt, clippy, test, build-prod)"
	@echo "  help         - Show this help message"
