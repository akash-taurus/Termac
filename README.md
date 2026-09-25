# Terminal Dashboard

A Windows-native Terminal User Interface (TUI) dashboard for managing and monitoring system processes, plugins, and services.

## Features

- **Windows-Optimized**: Uses Windows Named Pipes for secure plugin communication (no firewall prompts)
- **Plugin System**: Supports Python, Node.js, Batch (`.bat`/`.cmd`), and native `.exe` plugins via gRPC
- **Process Management**: Robust process tree killing using `taskkill /F /T /PID` to prevent zombies
- **Terminal Integration**: Proper Virtual Terminal Processing (VTP) and mouse support
- **Configuration**: Stores config and plugins in `%AppData%\Dashboard\`
- **TUI Interface**: Built with Bubble Tea for responsive, keyboard-navigable interface
- **Cross-Compilation**: Can be built on Linux/macOS for Windows targets

## Quick Start

### Prerequisites
- Go 1.24+ (matching the version in go.mod)
- Windows 10/11 (for native execution)
- Python/Node.js (optional, for plugin support)

### Build

```powershell
# On Windows PowerShell
.\build_windows.ps1

# Or on Unix-like systems (Linux/macOS/WSL)
./build_windows.sh
```

The executable will be created at `dist/windows/dashboard.exe`.

### Run

```powershell
.\dist\windows\dashboard.exe
```

## Architecture

- **`cmd/dashboard`**: Bubble Tea TUI (model, update, views, modals)
- **`pkg/launcher`**: Plugin process launcher (Python/Node/Batch/`.exe`/`.cmd`)
- **`pkg/plugin`**: Plugin lifecycle manager (discover, start/stop, fetch/render)
- **`pkg/transport`**: gRPC communication over Windows Named Pipes (Unix sockets elsewhere)
- **`pkg/process`**: Process management with `taskkill /F /T /PID` and Windows Job Objects
- **`pkg/verify`**: Zero-TCP socket verification for security
- **`pkg/term`**: Windows console initialization (VTP, mouse)
- **`pkg/system`**: CPU / memory / disk / process metrics
- **`pkg/git`**: Git operations (scan, status, diff, log, push/pull)
- **`pkg/github`**: GitHub REST API client (repositories, PRs, commits, create repo)
- **`pkg/auth`**: GitHub authentication (PAT, device flow, token storage)
- **`pkg/explorer`**: In-repo file/directory browser and preview
- **`pkg/shell`**: Windows GUI dialogs and Windows Terminal profile registration
- **`pkg/theme`**: TUI color palettes
- **`pkg/config`**: Application configuration using `os.UserConfigDir()`

## Design Specifications

Detailed Windows-specific design guidelines are in the `exe/` directory:
- `exe/01_windows_console_and_rendering.md` - Console & TUI rendering
- `exe/02_windows_file_system_and_config.md` - File system & config management  
- `exe/03_windows_plugin_execution.md` - Plugin execution & process management
- `exe/00_windows_build_and_cross_compilation.md` - Build & packaging strategy

## Development

Run tests:
```bash
go test ./...
```

Build and lint:
```bash
make vet   # go vet + optional golangci-lint
make test  # go test
```

## License

MIT License - see `LICENSE` file for details.