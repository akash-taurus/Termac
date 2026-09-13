# build_windows.ps1 - Build Windows executable on Windows
# Usage: .\build_windows.ps1 -Version "1.0.0"

param(
    [string]$Version = "1.0.0"
)

$ErrorActionPreference = "Stop"

$AppName = "dashboard"
$OutputDir = Join-Path $PSScriptRoot "dist\windows"
$BuildDir = Join-Path $PSScriptRoot "cmd\$AppName"

Write-Host "Building $AppName v$Version for Windows..."

# Ensure we're in the project root
if (!(Test-Path (Join-Path $PSScriptRoot "go.mod"))) {
    Write-Error "go.mod not found. Run from project root."
    exit 1
}

# Sync dependencies
Write-Host "Running go mod tidy..."
Set-Location $PSScriptRoot
go mod tidy

# Lint
Write-Host "Running go vet..."
go vet ./...

# Check if golangci-lint is available
if (Get-Command golangci-lint -ErrorAction SilentlyContinue) {
    Write-Host "Running golangci-lint..."
    golangci-lint run ./...
} else {
    Write-Host "Warning: golangci-lint not found, skipping linting" -ForegroundColor Yellow
}

# Build
Write-Host "Building for Windows (amd64)..."
$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$ldflags = "-s -w -X main.version=$Version"
go build -ldflags "$ldflags" -o (Join-Path $OutputDir "$AppName.exe") "./cmd/$AppName"

Write-Host "Build complete: $(Join-Path $OutputDir "$AppName.exe")" -ForegroundColor Green

# Verify
if (Test-Path (Join-Path $OutputDir "$AppName.exe")) {
    $size = (Get-Item (Join-Path $OutputDir "$AppName.exe")).Length
    Write-Host "Binary size: $([math]::Round($size / 1MB, 2)) MB"
}