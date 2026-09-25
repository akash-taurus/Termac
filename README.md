# Termac — Terminal Dashboard

A **Windows-native Terminal User Interface (TUI)** for managing and monitoring local git repositories, GitHub, system resources, and gRPC plugins — all from one keyboard-driven dashboard.

```
 ⚡ TERMINAL DASHBOARD  v1.1.0         🎨 Midnight  •  ● @you [classic PAT]
 ┌──────────────────┐ ┌───────────────────────────────────────────┐
 │ 📦 Local Repos(3)│ │ 📦 termac                        ● Clean   │
 │ ▸ termac         │ │    Branch  : main   ↑0 ↓0                  │
 │   other-proj     │ │    Remote  : github.com/you/termac         │
 │   scripts        │ │    Last    : feat: add git actions         │
 └──────────────────┘ └───────────────────────────────────────────┘
 Tab: views • t: theme • q: quit   🔒 Zero-TCP Named Pipe IPC  •  🛡️ Job Object Guard
```

- **Windows-optimized** — plugin IPC uses Windows Named Pipes (no firewall prompts), processes are killed via `taskkill /F /T` with Job Object containment, and the console is initialized with VT processing + mouse support.
- **Keyboard-driven** — every action (stage, commit, push, pull, diff, log, explore, publish) is a keypress.
- **Zero-TCP plugin IPC** — gRPC over `\\.\pipe\dashboard_plugin_*`; plugins never open network sockets.

---

## Table of Contents

1. [Prerequisites](#1-prerequisites)
2. [Install from Source](#2-install-from-source)
   - [Option A — Build on Windows (PowerShell)](#option-a--build-on-windows-powershell)
   - [Option B — Cross-compile from Linux/macOS/WSL](#option-b--cross-compile-from-linuxmacoswsl)
   - [Option C — Makefile](#option-c--makefile)
3. [First Run & Setup](#3-first-run--setup)
4. [GitHub Authentication](#4-github-authentication)
   - [Option 1 — Personal Access Token (recommended)](#option-1--personal-access-token-recommended)
   - [Option 2 — Device Flow (read-only)](#option-2--device-flow-read-only)
   - [Option 3 — Environment Variables](#option-3--environment-variables)
   - [Managing / removing credentials](#managing--removing-credentials)
5. [Using the Dashboard](#5-using-the-dashboard)
   - [Navigation](#navigation)
   - [Local Repos tab](#local-repos-tab)
   - [GitHub tab](#github-tab)
   - [System tab](#system-tab)
   - [Plugins tab](#plugins-tab)
   - [File Explorer](#file-explorer)
6. [Publishing a New Repo to GitHub](#6-publishing-a-new-repo-to-github)
7. [Plugins](#7-plugins)
   - [Installing](#installing)
   - [Writing your own plugin](#writing-your-own-plugin)
8. [Configuration & File Locations](#8-configuration--file-locations)
9. [Troubleshooting](#9-troubleshooting)
10. [Development](#10-development)
11. [License](#11-license)

---

## 1. Prerequisites

| Requirement | Minimum | Notes |
|---|---|---|
| OS | Windows 10/11 | Required for native execution (Named Pipes, metrics, VTP) |
| Go toolchain | **1.24+** | Must match `go.mod`; only needed to build from source |
| Git | Any recent version | Needed for repo scan/status/push/pull features |
| Terminal | Windows Terminal recommended | Legacy conhost works too (VT is enabled automatically) |
| Python | 3.8+ *(optional)* | Only if you use `.py` plugins |
| Node.js | 16+ *(optional)* | Only if you use `.js` plugins |

Check your toolchain:

```powershell
go version      # expect go1.24 or newer
git --version
```

> **Cross-compiling?** You can build the Windows binary on Linux/macOS/WSL — see [Option B](#option-b--cross-compile-from-linuxmacoswsl). The binary itself still *runs* on Windows only.

---

## 2. Install from Source

```bash
git clone https://github.com/akash-taurus/Termac.git
cd Termac
```

### Option A — Build on Windows (PowerShell)

```powershell
.\build_windows.ps1
# custom version stamp:
.\build_windows.ps1 -Version "1.2.0"
```

The script runs `go mod tidy`, `go vet`, optional `golangci-lint`, then builds:

```
dist\windows\dashboard.exe
```

### Option B — Cross-compile from Linux/macOS/WSL

```bash
./build_windows.sh
# or: ./build_windows.sh 1.2.0
```

### Option C — Makefile

```bash
make build-windows   # go vet + CGO_ENABLED=0 build → dist/windows/dashboard.exe
make test            # go test ./... -v
make vet             # go vet ./...
make clean           # remove dist/
```

All builds produce a **static, CGO-free** binary (`-s -w` stripped) — a single self-contained `.exe` with no runtime DLL dependencies beyond Windows itself.

### Run it

```powershell
.\dist\windows\dashboard.exe
```

Optional CLI flags (no TUI — useful for scripts):

```powershell
dashboard.exe -version          # print version and exit
dashboard.exe -logout           # delete the stored GitHub token
dashboard.exe -token <PAT>      # validate + store a PAT non-interactively
```

---

## 3. First Run & Setup

Launch `dashboard.exe`. On startup it will:

1. **Create its config directory** at `%AppData%\Dashboard\` (token storage, plugins).
2. **Load the current working directory** as the first local repo row instantly (git details fill in asynchronously — the UI never blocks on git).
3. **Adopt a stored GitHub token** *only if it live-validates* against `GET /user`. A provably dead token is not kept; if you're offline, the token is kept so the UI doesn't flap.
4. **Discover plugins** in `%AppData%\Dashboard\plugins\` (or `./plugins` if it exists).
5. **Start real-time system polling** (CPU/RAM/disk/processes every 1.5 s).

You start on the **Local Repos** tab with your current directory selected. Press `r` to scan your home directory (depth 3) for more repositories, or `o` to open any folder by path.

> **Tip:** press `t` to cycle color themes; the choice applies instantly.

---

## 4. GitHub Authentication

Press **`l`** (from any tab) to open the auth modal. Three options:

### Option 1 — Personal Access Token (recommended)

Required for **creating repositories** and pushing to GitHub via the dashboard.

1. In the auth modal press **`Ctrl+O`** — this opens `github.com/settings/tokens/new` with the `repo` + `read:org` scopes pre-selected.
2. Generate a **classic PAT** and copy it (`ghp_...`).
3. Back in the modal, **paste the token** (bracketed paste is supported and shortcut letters in the token are safe) and press **`Enter`**.
4. The token is validated against the GitHub API and stored at `%AppData%\Dashboard\github_token.json` with `0600` permissions.

Fine-grained PATs (`github_pat_...`) are accepted; they need **Administration (read & write)** on the account to create repos. Device-flow tokens (`ghu_...`) **cannot** create user repositories — the UI detects this and will ask for a PAT when you try to publish.

### Option 2 — Device Flow (read-only)

Press **`Ctrl+D`** in the auth modal. The user code is copied to your clipboard and the browser opens automatically; approve at github.com/login/device. Good for browsing your repos; not sufficient for repo creation.

### Option 3 — Environment Variables

Most reliable paste path:

```powershell
$env:GITHUB_TOKEN = "ghp_xxxxxxxxxxxxxxxxxxxx"
# then press Ctrl+E in the auth modal to verify & import
```

`GITHUB_TOKEN` / `GH_TOKEN` are also picked up automatically as a fallback when no stored token exists. Env tokens are **read-only** — they are never persisted to disk unless you explicitly import or submit them.

### Managing / removing credentials

| Action | How |
|---|---|
| **Logout button** (header) | click **⏻ Logout** in the top-right corner |
| Logout (anywhere) | press `u` |
| Logout (auth modal) | `Ctrl+X` |
| Logout (CLI) | `dashboard.exe -logout` |

Security notes:

- Pushes authenticate via a **one-shot `http.extraHeader`** (`Authorization: Bearer …`) — the token is never written to `.git/config` nor embedded in remote URLs, and `-u` records the *remote name* (so plain `git push` keeps working later). This is guarded by a regression test.
- Token *type* (prefix only) is shown in the UI/errors — never the secret itself.

---

## 5. Using the Dashboard

### Navigation

| Key | Action |
|---|---|
| `Tab` / `Shift+Tab` | Next / previous tab |
| `1` `2` `3` `4` | Jump to Local / GitHub / System / Plugins |
| `t` | Cycle theme |
| `q` / `Ctrl+C` | Quit (stops plugins, restores console) |

The **navigation bar** (the tabs row) ends with the credential button —
**⏻ Logout** when signed in, **→ Login** otherwise. It is a real mouse click
target (mouse input is enabled) and shares one implementation with the `u`
shortcut and the auth modal's `Ctrl+X`.

Each view keeps its **own repo list and selection** — switching tabs never wipes or mixes lists.

### Local Repos tab

| Key | Action |
|---|---|
| `r` | Scan home dir (depth 3) for git repos |
| `o` | Open folder by path (modal) |
| `b` | Native Windows GUI folder picker |
| `↑/↓` or `k/j` | Move selection (details load async) |
| `←/→` | Move the changed-file cursor in the detail pane |
| `Enter` / `f` | Open in-repo file explorer |
| `i` | `git init` on a non-repo folder |
| `a` | `git add -A` (stage all) |
| `c` | Commit (modal; auto-stages first) |
| `P` | `git push` (falls back to header-auth push with your stored token if credentials fail) |
| `F` | `git pull` |
| `d` | Toggle diff pane (syntax-colored +/-/@@) |
| `g` | Toggle commit log pane |
| `e` | Open current repo in Windows File Explorer |
| `p` | Open a terminal at the repo path (wt.exe or PowerShell) |
| `n` | Publish this folder as a **new GitHub repo** (see [§6](#6-publishing-a-new-repo-to-github)) |

The detail pane shows branch, ahead/behind, upstream, staged/unstaged/untracked counts, remote, and last commit — plus a **changed-files list** with a cursor.

#### Per-file staging (Local tab)

With the changed-files list visible:

| Key | Action |
|---|---|
| `S` | Stage the highlighted file (`git add -- <path>`) |
| `U` | Unstage the highlighted file (keeps working-tree changes) |
| `Ctrl+U` | Unstage everything |
| `Ctrl+X` | **Discard** the highlighted file (confirmation required — irreversible) |
| `h` | Hunk mode: stage individual hunks of the highlighted file (`n`/`p` select, `s` stage hunk) |
| `Ctrl+Y` | File history (`git log --follow`) for the highlighted file |
| `Ctrl+B` | Blame view for the highlighted file |

#### Stash, branches, merge/rebase (Local tab)

| Key | Action |
|---|---|
| `z` | Stash changes (optional message, includes untracked) |
| `Z` | Stash list overlay — `Enter` pop, `a` apply (keep entry), `d` drop (confirmed) |
| `B` | Branch list overlay — `Enter` switch, `n` new branch, `d` delete (confirmed) |
| `Ctrl+N` | Create + switch to a new branch |
| `M` | Merge a branch into the current one |
| `Ctrl+R` | Rebase the current branch onto another |
| `Ctrl+A` | Abort the in-progress merge/rebase (confirmed) |
| `Ctrl+G` | Reflog overlay — `Enter` checks out the highlighted state |
| `Ctrl+F` | `git fetch --all --prune` |
| `Ctrl+P` | **Create PR from current branch** (see below) |

In the **commit log pane** (`g`): `j`/`k` move the highlighted commit, `Enter` shows that commit's diff, and pressing `Enter` again cycles reset options (`--mixed`, then `--hard` — both confirmed).

#### Create a PR from the current branch (`Ctrl+P`)

Pressing `Ctrl+P` on the Local tab runs a **preflight check** (async, never blocks) and only proceeds when everything needed for a PR is true:

1. Current branch resolves (not detached HEAD)
2. An `origin` remote exists and its URL parses to `owner/repo`
3. The branch is **pushed** — if not, you're told to press `P` first, then retry
4. The branch is not **behind** its upstream — if it is, pull first (`F`)
5. The default base branch is read from `origin`'s HEAD symref (no API call needed)

Then two prompts appear:

- **PR title** — prefilled with the latest commit subject (editable)
- **Base branch** — prefilled with the remote's default (e.g. `main`); base == head is rejected

The body is auto-generated as a bulleted commit list of `base..head`. Creation runs async and reports the PR number + URL in the status bar. Failures (no scope, no commits between branches, etc.) surface the GitHub API message verbatim with the flow cancelled cleanly.

### GitHub tab

Requires authentication. Lists all your repositories (paginated, up to 500): stars, forks, language, open PR count, and latest commit per repo. `r` refreshes; `Enter` loads details.

| Key | Action |
|---|---|
| `I` | List open issues for the selected repo (overlay) |
| `Ctrl+M` | Merge the highlighted PR (confirmed) |
| `Ctrl+E` | Close the highlighted PR (confirmed) |

### Global shortcuts (any tab)

| Key | Action |
|---|---|
| `Ctrl+S` | **Sync all local repos** — `git pull --ff-only` in every scanned repo; per-repo results overlay (skips repos without upstream, never auto-merges) |
| `Ctrl+L` | **Clone by URL** — clones into a subdirectory of your current directory; open it afterwards with `o` |

### System tab

Live CPU load + trend sparkline, RAM, fixed/removable disks (CD-ROM & network drives skipped), and top processes by thread count. `r` forces a refresh. Real metrics are collected via `GetSystemTimes`, `GlobalMemoryStatusEx`, drive APIs, and the Toolhelp snapshot — no external dependencies.

### Plugins tab

| Key | Action |
|---|---|
| `j/k` | Select plugin |
| `s` | Start (launches process, dials its Named Pipe, 4 s budget) |
| `x` | Stop (kills the whole process tree) |
| `r` / `Enter` | Re-fetch + re-render |
| Re-scan | Deleting a plugin file and pressing `r` cleans it up automatically |

Statuses: `Stopped → Starting → Running / Error`. Re-discovery also stops instances whose files were deleted — kills happen outside the manager lock so the UI stays responsive.

### File Explorer

Press `f` or `Enter` on a local repo:

| Key | Action |
|---|---|
| `j/k`, arrows | Navigate (git status badges `[M] [A] [D] [?]` per file) |
| `Enter` / `→` | Enter dir / preview file (line numbers, binary detection, size guard) |
| `Backspace` / `←` | Parent directory |
| `e` | Open in Windows File Explorer |
| `p` | Open terminal here |
| `v` | Open in VS Code |
| `r` | Refresh |
| `t` | Theme |
| `Esc` / `b` | Back to repos |

All navigation is **sandboxed to the repo root** — traversal and symlink escapes are rejected; delete/confirm operations verify the target stays inside the repo.

---

## 6. Publishing a New Repo to GitHub

Press **`n`** on a local folder. The flow is fully automated:

1. **Pre-flight validation** — token is live-checked and its scopes inspected (`X-OAuth-Scopes`). Insufficient scope fails *before* your working tree is touched, with a precise message (and `Ctrl+L` to switch tokens right from the publish modal).
2. `git init` if needed; sets a local git identity from your GitHub login if none configured.
3. Creates an initial commit (writes a `README.md` if the folder is empty) or commits pending changes.
4. Ensures branch is `main`.
5. Creates the repo via `POST /user/repos` (public by default — **`Tab` toggles private**). If it already exists, the dashboard connects to it instead.
6. Sets `origin` and pushes with **header-based auth** (token never lands in `.git/config`).

**Token ↔ capability cheat sheet:**

| Token | Browse repos | Create + push |
|---|---|---|
| Classic PAT `ghp_` with `repo` | ✅ | ✅ |
| Fine-grained `github_pat_` w/ Administration RW | ✅ | ✅ |
| Device-flow `ghu_` / OAuth `gho_` | ✅ | ❌ (push still OK via git credentials) |

---

## 7. Plugins

Plugins are separate processes the dashboard talks to over gRPC on a **Named Pipe** (`\\.\pipe\dashboard_plugin_<sanitized-name>_<uuid>`). No TCP, no firewall prompts.

### Installing

Drop plugin files into `%AppData%\Dashboard\plugins\` (or a `./plugins` folder next to the exe):

| Type | Requires |
|---|---|
| `.exe` | — (native) |
| `.py` | Python in PATH (`python` or `py`) |
| `.js` | Node.js in PATH (`node`) |
| `.bat` / `.cmd` | — (cmd.exe) |

Sample plugins are included in the repo's `plugins/` directory: `system_info.py`, `quick_status.bat`, `services.ps1` (note: `.ps1` is listed as sample material but is not a launchable type — rename to `.bat`/`.cmd` or ship an `.exe`).

### Writing your own plugin

A plugin implements two RPCs — `FetchData` (state) and `Render` (ANSI-styled string) — and serves gRPC **on the pipe path the host passes via the `PLUGIN_PIPE` environment variable**. See `exe/plugin_template.py` for a working Python template:

```python
class MyPlugin(plugin_pb2_grpc.WidgetPluginServicer):
    def FetchData(self, request, context):
        return plugin_pb2.FetchResponse(data="Plugin Data Here")
    def Render(self, request, context):
        return plugin_pb2.RenderResponse(rendered_string="\033[32mPlugin Active\033[0m")
```

Contract details:

- The host launches your process with `PLUGIN_PIPE=\\.\pipe\...` set; listen on `pipe:<that path>`.
- Dial budget is 4 s from launch; keep startup fast.
- `FetchData`/`Render` calls time out after 2 s; render dimensions arrive as `width`/`height` (clamped 1–1000).
- Only the **first 20 alphanumeric characters** of the filename become the pipe prefix; invalid characters become `_`.

---

## 8. Configuration & File Locations

| Path | Purpose |
|---|---|
| `%AppData%\Dashboard\` | Root config dir (created on first run) |
| `%AppData%\Dashboard\github_token.json` | Stored GitHub token (mode 0600) |
| `%AppData%\Dashboard\github_config.json` | Optional OAuth client config |
| `%AppData%\Dashboard\plugins\` | Plugin directory (scan target) |
| `%AppData%\Dashboard\last_login` | Last login timestamp (RFC3339) |

`%AppData%` is `%USERPROFILE%\AppData\Roaming`. On non-Windows builds, `os.UserConfigDir()` resolves to `~/.config/Dashboard/` — but note system metrics are **Windows-only** and are clearly flagged *“Simulated metrics”* in the UI elsewhere.

---

## 9. Troubleshooting

| Symptom | Cause & fix |
|---|---|
| Repo list empty | Press `r` to scan home dir, or `o`/`b` to open a folder directly. |
| Push fails with auth error | Push retries automatically with your stored token over HTTPS. If the remote is SSH, header-auth can't apply — the error will say so. Re-login with `l`. |
| *"token lacks the required scope"* on publish | Device-flow/fine-grained tokens can't create user repos. Press `Ctrl+L` in the modal and paste a classic PAT with `repo` scope. |
| *"Python interpreter not found"* / *"Node.js..."* | Install Python/Node or remove `.py`/`.js` plugins. |
| Plugin stuck on *Starting* → *Error* | Plugin must listen on `PLUGIN_PIPE` within 4 s. Check the plugin runs standalone; see [§7](#7-plugins). |
| Metric panel shows *"Simulated metrics"* | You're on a non-Windows build; metrics collectors are Windows-only by design. |
| Terminal renders garbage on exit | Use `q` or `Ctrl+C` — the app restores VTP/mouse state. Legacy consoles occasionally need a `cls`. |
| Status bar shows `✖` / `▲` / `✔` | Explicit severities (error / warn / success) set at the message source — not guesses from text. |
| `go build` fails | Go ≥ 1.24 required; run `go mod tidy` first. |
| Antivirus flags the exe | Unsigned Go binaries occasionally do. Build locally with `build_windows.ps1` to self-verify, or sign with Authenticode. |

Where to look when filing an issue: exact status-bar message (icon included), `dashboard.exe -version`, OS build, terminal (Windows Terminal vs conhost).

---

## 10. Development

```bash
go test ./...        # full suite (unit + integration; real git round-trips included)
go test ./pkg/git/ -run TestGitPushUpstreamAuth -v
make vet             # go vet ./...  (golangci-lint runs automatically if installed)
make build-windows
```

**Project layout:**

```
cmd/dashboard/   Bubble Tea app: model.go, update.go, views.go,
                 commands.go, helpers.go, main.go
pkg/git/         scan/status/diff/log/push + staging/stash/branches/
                 clone/sync/blame/hunks/merge-rebase (header-auth push!)
pkg/github/      REST client (repos, PRs incl. create/merge/diff, issues)
pkg/github/      REST client (repos, PRs, commits, create repo)
pkg/auth/        PAT + device flow, token storage/validation, scopes
pkg/plugin/      lifecycle manager (discover/start/stop/fetch/render)
pkg/launcher/    .exe/.py/.js/.bat/.cmd process launch
pkg/transport/   gRPC over Named Pipes (Unix sockets elsewhere)
pkg/process/     taskkill /F /T + Job Object containment
pkg/system/      CPU/RAM/disk/process metrics (Windows) or simulated
pkg/verify/      Zero-TCP assertion (GetExtendedTcpTable)
pkg/explorer/    in-repo file browser/preview
pkg/config/      %AppData% path helpers
pkg/shell/       GUI dialogs, Windows Terminal profile registration
pkg/term/        VTP + mouse init/restore
pkg/theme/       color palettes
exe/             Windows architecture & engineering specs (00–10)
test/e2e/        fixtures (dummy gRPC plugin, scripts)
```

**Conventions:** every async git/API call runs in a `tea.Cmd` (UI never blocks); status messages carry an explicit severity level; git subprocesses run with `GIT_TERMINAL_PROMPT=0` and a 30 s timeout (clones get 10 min); secrets never appear in argv, URLs, or logs; destructive operations (discard, reset --hard, branch delete, stash drop, PR close/merge, abort) always route through a confirmation modal.

---

## 11. License

MIT — see `LICENSE`.
