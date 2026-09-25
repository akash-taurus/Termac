#!/usr/bin/env bash
# build_windows.sh - Build Windows executable from Unix-like systems
# Usage: ./build_windows.sh [version]

set -euo pipefail

VERSION="${1:-1.1.0}"
APP_NAME="dashboard"
MODULE="tui"
OUTPUT_DIR="dist/windows"
BUILD_DIR="cmd/${APP_NAME}"

echo "Building ${APP_NAME} v${VERSION} for Windows..."

# Ensure we're in the project root
if [[ ! -f "go.mod" ]]; then
    echo "Error: go.mod not found. Run from project root."
    exit 1
fi

# Sync dependencies
echo "Running go mod tidy..."
go mod tidy

# Lint
echo "Running go vet..."
go vet ./...

# Check if golangci-lint is available
if command -v golangci-lint &> /dev/null; then
    echo "Running golangci-lint..."
    golangci-lint run ./...
else
    echo "Warning: golangci-lint not found, skipping linting"
fi

# Build
echo "Building for Windows (amd64)..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o "${OUTPUT_DIR}/${APP_NAME}.exe" \
    "${BUILD_DIR}"

echo "Build complete: ${OUTPUT_DIR}/${APP_NAME}.exe"

# Verify
if [[ -f "${OUTPUT_DIR}/${APP_NAME}.exe" ]]; then
    echo "Binary size: $(du -h "${OUTPUT_DIR}/${APP_NAME}.exe" | cut -f1)"
fi