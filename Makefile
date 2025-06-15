# AIDevTools Makefile

.PHONY: all build-sidekick build-stdiobridge clean test help

help: ## Show this help
	@echo "Available commands:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

all: build-sidekick build-stdiobridge ## Build all components

build-sidekick: ## Build sidekick
	@echo "Building sidekick..."
	@cd sidekick && go build -o sidekick .
	@echo "✓ sidekick built"

build-stdiobridge: ## Build stdiobridge
	@echo "Building stdiobridge..."
	@cd stdiobridge && go build -o stdiobridge .
	@echo "✓ stdiobridge built"

clean: ## Clean all build artifacts
	@echo "Cleaning..."
	@rm -f sidekick/sidekick
	@rm -f stdiobridge/stdiobridge
	@rm -rf dist/
	@echo "✓ Cleaned"

test: ## Run all tests
	@echo "Testing sidekick..."
	@cd sidekick && go test -v ./...
	@echo "Testing stdiobridge..."
	@cd stdiobridge && go test -v ./...

install-sidekick: build-sidekick ## Install sidekick
	@echo "Installing sidekick..."
	@mkdir -p ~/.local/bin
	@cp sidekick/sidekick ~/.local/bin/
	@echo "✓ Installed to ~/.local/bin/sidekick"

install-stdiobridge: build-stdiobridge ## Install stdiobridge
	@echo "Installing stdiobridge..."
	@mkdir -p ~/.local/bin
	@cp stdiobridge/stdiobridge ~/.local/bin/
	@echo "✓ Installed to ~/.local/bin/stdiobridge"

install: install-sidekick install-stdiobridge ## Install all components

.DEFAULT_GOAL := help