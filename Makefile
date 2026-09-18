# Makefile for Terminal Dashboard

APP_NAME := dashboard
VERSION := 1.0.0
MODULE := tui
OUTPUT_DIR := dist/windows
BUILD_DIR := cmd/$(APP_NAME)

.PHONY: all build build-windows build-linux test vet clean help

all: build

build: build-windows

build-windows: ## Build for Windows (native)
	@echo "Building $(APP_NAME) v$(VERSION) for Windows..."
	go vet ./...
	mkdir -p $(OUTPUT_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w -X main.version=$(VERSION)" -o $(OUTPUT_DIR)/$(APP_NAME).exe ./cmd/$(APP_NAME)
	@echo "Build complete: $(OUTPUT_DIR)/$(APP_NAME).exe"

build-linux: ## Build for Linux
	@echo "Building $(APP_NAME) v$(VERSION) for Linux..."
	go vet ./...
	mkdir -p dist/linux
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X main.version=$(VERSION)" -o dist/linux/$(APP_NAME) ./cmd/$(APP_NAME)

test: ## Run tests
	go test ./... -v

vet: ## Run go vet
	go vet ./...

clean: ## Clean build artifacts
	go clean
	rm -rf dist/
	@echo "Cleaned build artifacts"

help: ## Show this help
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  all               Build for Windows (default)"
	@echo "  build             Alias for build-windows"
	@echo "  build-windows     Build Windows executable"
	@echo "  build-linux       Build Linux executable"
	@echo "  test              Run all tests"
	@echo "  vet               Run go vet"
	@echo "  clean             Remove dist/ directory"
	@echo "  help              Show this help message"