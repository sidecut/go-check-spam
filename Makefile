# Makefile for go-check-spam project
# Build settings

# Configuration - Change these variables as needed
BINARY_NAME := gocheckspam
GO_VERSION := 1.25
MAIN_FILE := main.go

# Build flags
LDFLAGS := -s -w
BUILD_FLAGS := -ldflags "$(LDFLAGS)"

# Color output (optional)
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
	@echo "Building $(BINARY_NAME)..."
	@go build $(BUILD_FLAGS) -o $(BINARY_NAME) .
	@echo "Build complete: $(BINARY_NAME)"

# Build the binary (standard, no experiments)
.PHONY: build-standard
build-standard:
	@echo "Building $(BINARY_NAME) (standard build)..."
	@go build $(BUILD_FLAGS) -o $(BINARY_NAME) .
	@echo "Build complete: $(BINARY_NAME)"

# Build for production (optimized)
.PHONY: build-prod
build-prod:
	@echo "Building $(BINARY_NAME) for production..."
	@CGO_ENABLED=0 go build $(BUILD_FLAGS) -a -installsuffix cgo -o $(BINARY_NAME) .
	@echo "Production build complete: $(BINARY_NAME)"

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning build artifacts..."
	@rm -f $(BINARY_NAME)
	@echo "Clean complete"

# Run the application
.PHONY: run
run: build
	@echo "Running $(BINARY_NAME)..."
	@./$(BINARY_NAME)

# Run with custom flags
.PHONY: run-debug
run-debug: build
	@echo "Running $(BINARY_NAME) with debug enabled..."
	@./$(BINARY_NAME) -debug

# Run with custom worker count
.PHONY: run-workers
run-workers: build
	@echo "Running $(BINARY_NAME) with a 20-request concurrency cap..."
	@./$(BINARY_NAME) -workers 20

# Test the application
.PHONY: test
test:
	@echo "Running tests..."
	@go test -v ./...

# Format code
.PHONY: fmt
fmt:
	@echo "Formatting code..."
	@go fmt ./...

# Lint code
.PHONY: lint
lint:
	@echo "Linting code..."
	@golangci-lint run

# Vet code
.PHONY: vet
vet:
	@echo "Vetting code..."
	@go vet ./...

# Check dependencies
.PHONY: deps
deps:
	@echo "Downloading dependencies..."
	@go mod download
	@go mod verify

# Update dependencies
.PHONY: update-deps
update-deps:
	@echo "Updating dependencies..."
	@go mod tidy
	@go mod download

# Install the binary
.PHONY: install
install:
	@echo "Installing $(BINARY_NAME)..."
	@go install $(BUILD_FLAGS) .
	@echo "Installation complete"

# Show build info
.PHONY: info
info:
	@echo "Build Information:"
	@echo "  Binary Name: $(BINARY_NAME)"
	@echo "  Go Version: $(GO_VERSION)"
	@echo "  Build Flags: $(BUILD_FLAGS)"
	@echo "  Go Environment:"
	@go env

# Cross-compile for different platforms
.PHONY: build-linux
build-linux:
	@echo "Building for Linux..."
	@GOOS=linux GOARCH=amd64 go build $(BUILD_FLAGS) -o $(BINARY_NAME)-linux .

.PHONY: build-windows
build-windows:
	@echo "Building for Windows..."
	@GOOS=windows GOARCH=amd64 go build $(BUILD_FLAGS) -o $(BINARY_NAME)-windows.exe .

.PHONY: build-mac
build-mac:
	@echo "Building for macOS..."
	@GOOS=darwin GOARCH=amd64 go build $(BUILD_FLAGS) -o $(BINARY_NAME)-mac .

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
ci: clean fmt vet test build
	@echo "CI build complete"

# Help target
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build        - Build the binary with experimental GC (fallback to standard)"
	@echo "  build-exp    - Build with GOEXPERIMENT (forced, may fail)"
	@echo "  build-standard - Build without any experiments"
	@echo "  build-custom - Build with custom experiment (use EXP=<experiment>)"
	@echo "  build-prod   - Build optimized production binary"
	@echo "  clean        - Remove build artifacts"
	@echo "  run          - Build and run with default flags"
	@echo "  run-debug    - Run with debug logging enabled"
	@echo "  run-workers  - Run with -workers 20 to cap concurrent Gmail fetches"
	@echo "  test         - Run tests (with fallback)"
	@echo "  test-exp     - Run tests with GOEXPERIMENT (forced)"
	@echo "  fmt          - Format code"
	@echo "  lint         - Lint code (requires golangci-lint)"
	@echo "  vet          - Vet code"
	@echo "  deps         - Download dependencies"
	@echo "  update-deps  - Update dependencies"
	@echo "  install      - Install binary to GOPATH/bin"
	@echo "  info         - Show build information and experiment availability"
	@echo "  build-linux  - Cross-compile for Linux"
	@echo "  build-windows - Cross-compile for Windows"
	@echo "  build-mac    - Cross-compile for macOS"
	@echo "  build-all    - Cross-compile for all platforms"
	@echo "  dev          - Development workflow (clean, fmt, vet, build)"
	@echo "  ci           - CI workflow (clean, fmt, vet, test, build)"
	@echo "  help         - Show this help message"
	@echo ""
	@echo "$(YELLOW)Examples:$(NC)"
	@echo "  make run-workers"
	@echo "  ./$(BINARY_NAME) -days 3 -workers 50"
	@echo "  make build-custom EXP=rangefunc"
	@echo "  make build-custom EXP=newinliner"
	@echo "  make GOEXP=rangefunc build-exp"
