# Sidekick MCP Server Makefile

.PHONY: build clean test test-race test-coverage fmt vet install help release dev

# Build variables
BINARY_NAME=sidekick
BUILD_DIR=./dist
SOURCE_DIR=./sidekick
VERSION?=$(shell git describe --tags --always --dirty)
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION)"

# Go settings
GOOS?=$(shell go env GOOS)
GOARCH?=$(shell go env GOARCH)

help: ## Show this help
	@echo "Available commands:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build the binary
	@echo "Building $(BINARY_NAME) for $(GOOS)/$(GOARCH)..."
	@mkdir -p $(BUILD_DIR)
	cd $(SOURCE_DIR) && go build $(LDFLAGS) -o ../$(BUILD_DIR)/$(BINARY_NAME) .
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

build-all: ## Build for all platforms
	@echo "Building for all platforms..."
	@mkdir -p $(BUILD_DIR)
	
	# macOS
	cd $(SOURCE_DIR) && GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o ../$(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 .
	cd $(SOURCE_DIR) && GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o ../$(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 .
	
	# Linux
	cd $(SOURCE_DIR) && GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o ../$(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 .
	cd $(SOURCE_DIR) && GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o ../$(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 .
	
	# Windows
	cd $(SOURCE_DIR) && GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o ../$(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe .
	
	@echo "Cross-platform build complete!"

clean: ## Clean build artifacts
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@rm -f $(SOURCE_DIR)/$(BINARY_NAME)

test: ## Run tests
	@echo "Running tests..."
	cd $(SOURCE_DIR) && go test -v ./...

test-race: ## Run tests with race detection
	@echo "Running tests with race detection..."
	cd $(SOURCE_DIR) && go test -v -race ./...

test-coverage: ## Run tests with coverage
	@echo "Running tests with coverage..."
	cd $(SOURCE_DIR) && go test -v -coverprofile=coverage.out ./...
	cd $(SOURCE_DIR) && go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: $(SOURCE_DIR)/coverage.html"


fmt: ## Format code
	@echo "Formatting code..."
	cd $(SOURCE_DIR) && go fmt ./...

vet: ## Run go vet
	@echo "Running go vet..."
	cd $(SOURCE_DIR) && go vet ./...

install: build ## Install binary to system
	@echo "Installing $(BINARY_NAME)..."
	@sudo cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/
	@echo "Installed to /usr/local/bin/$(BINARY_NAME)"

dev: ## Development setup
	@echo "Setting up development environment..."
	cd $(SOURCE_DIR) && go mod download
	cd $(SOURCE_DIR) && go mod verify
	@echo "Development environment ready!"

release: clean build-all ## Prepare release artifacts
	@echo "Preparing release artifacts..."
	cd $(BUILD_DIR) && \
	tar -czf $(BINARY_NAME)-darwin-arm64.tar.gz $(BINARY_NAME)-darwin-arm64 && \
	tar -czf $(BINARY_NAME)-darwin-amd64.tar.gz $(BINARY_NAME)-darwin-amd64 && \
	tar -czf $(BINARY_NAME)-linux-amd64.tar.gz $(BINARY_NAME)-linux-amd64 && \
	tar -czf $(BINARY_NAME)-linux-arm64.tar.gz $(BINARY_NAME)-linux-arm64 && \
	zip $(BINARY_NAME)-windows-amd64.zip $(BINARY_NAME)-windows-amd64.exe
	cd $(BUILD_DIR) && sha256sum *.tar.gz *.zip > checksums.txt
	@echo "Release artifacts ready in $(BUILD_DIR)/"

check: test ## Run all checks
	@echo "All checks passed!"

ci: test build ## CI pipeline
	@echo "CI pipeline completed successfully!"

.DEFAULT_GOAL := help