package main

// Status-bar severity handling, per-view list stash/restore, token-type
// descriptions, publish-modal opening, and the centralized view-switch logic.

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tui/pkg/theme"
)

func (m *DashboardModel) setStatus(level statusLevel, text string) {
	m.message = text
	m.statusLevel = level
}

// setStatusf is setStatus with Sprintf formatting.
func (m *DashboardModel) setStatusf(level statusLevel, format string, args ...any) {
	m.setStatus(level, fmt.Sprintf(format, args...))
}

// statusIconFor maps an explicit status level to its status-bar icon.
func statusIconFor(level statusLevel, pal theme.Palette) string {
	switch level {
	case statusSuccess:
		return lipgloss.NewStyle().Foreground(pal.Success).Bold(true).Render("✔ ")
	case statusWarn:
		return lipgloss.NewStyle().Foreground(pal.Warning).Bold(true).Render("▲ ")
	case statusError:
		return lipgloss.NewStyle().Foreground(pal.Danger).Bold(true).Render("✖ ")
	case statusLoading:
		return lipgloss.NewStyle().Foreground(pal.Warning).Render("◌ ")
	default:
		return lipgloss.NewStyle().Foreground(pal.Highlight).Render("ℹ ")
	}
}

// stashActiveList saves the active view's list before switching away.
func (m *DashboardModel) stashActiveList() {
	switch m.viewMode {
	case ViewLocal:
		m.localRepos = m.repos
		m.localSelected = m.selected
	case ViewGitHub:
		m.githubRepos = m.repos
		m.githubSelected = m.selected
	}
}

// restoreList swaps in the target view's list. Reports whether a stored
// list existed; callers fall back to fetch/scan when it did not.
func (m *DashboardModel) restoreList(v ViewMode) bool {
	switch v {
	case ViewLocal:
		if m.localRepos != nil {
			m.repos = m.localRepos
			m.selected = m.localSelected
			return true
		}
	case ViewGitHub:
		if m.githubRepos != nil {
			m.repos = m.githubRepos
			m.selected = m.githubSelected
			return true
		}
	}
	return false
}

// describeTokenType names the credential family from its prefix only.
// It never includes secret material, so it is safe to show in errors.
func describeTokenType(token string) string {
	switch {
	case strings.HasPrefix(token, "ghp_"):
		return "classic PAT (ghp_...)"
	case strings.HasPrefix(token, "github_pat_"):
		return "fine-grained PAT (github_pat_...)"
	case strings.HasPrefix(token, "ghu_"):
		return "device-flow user token (ghu_...)"
	case strings.HasPrefix(token, "gho_"):
		return "OAuth token (gho_...)"
	case strings.HasPrefix(token, "ghs_"):
		return "GitHub App token (ghs_...)"
	case strings.TrimSpace(token) == "":
		return "none"
	default:
		return "unrecognized format"
	}
}

// isPAT reports whether s looks like a PAT usable for repo creation
// (classic ghp_ or fine-grained github_pat_). Other tokens (device-flow
// ghu_/gho_, etc.) have unverifiable scopes offline, so creation routes
// through the auth modal first — [Esc] continues with the current token.
func isPAT(s string) bool {
	return strings.HasPrefix(s, "ghp_") || strings.HasPrefix(s, "github_pat_")
}

// logout clears every piece of GitHub session state from the model and
// returns the command that deletes the stored token from disk. It is the
// single implementation behind the [u] shortcut, the header Logout button,
// and the auth modal's [Ctrl+X], so all three behave identically.
func (m *DashboardModel) logout() tea.Cmd {
	// Cancelling an in-flight device-flow poll stops the goroutine from
	// hitting GitHub for the rest of the code's lifetime.
	if m.authCancel != nil {
		m.authCancel()
		m.authCancel = nil
	}
	m.authModalOpen = false
	m.authPolling = false
	m.deviceCode = nil
	m.authError = ""
	m.authNeedsPAT = false
	m.pendingPublish = false
	m.tokenInput.Blur()
	m.token = nil
	m.userName = ""
	m.githubRepos = nil
	m.githubSelected = 0
	if m.viewMode == ViewGitHub {
		m.repos = nil
		m.selected = 0
	}
	m.setStatus(statusLoading, "Logging out from GitHub...")
	return githubLogoutCommand()
}

// authButtonLabel is the text of the nav bar credential button. It is shared
// by the renderer and the mouse hit-test so the two can never disagree about
// the button's size or meaning.
func authButtonLabel(authenticated bool) string {
	if authenticated {
		return "⏻ Logout"
	}
	return "→ Login"
}

// authButtonWidth is the rendered cell width of the header button: the label
// plus the button style's horizontal padding of 1 on each side.
func authButtonWidth(authenticated bool) int {
	return lipgloss.Width(authButtonLabel(authenticated)) + 2
}

// navBarRow is the zero-based terminal row of the navigation (tabs) bar: the
// title bar is row 0, so the nav bar is row 1. The credential button is
// right-aligned on it.
const navBarRow = 1

// authButtonHit reports whether a terminal cell (x, y) falls inside the nav
// bar's credential button. The button is the last element of the bar, so it
// always ends at the right edge of the window.
func (m DashboardModel) authButtonHit(x, y int) bool {
	if y != navBarRow {
		return false
	}
	width := m.width
	if width <= 0 {
		width = 100
	}
	start := width - authButtonWidth(m.userName != "")
	return x >= start && x < width
}

// handleAuthButtonMouse acts on a left-click on the nav bar credential button:
// logout when authenticated, otherwise open the GitHub login modal.
func (m DashboardModel) handleAuthButtonMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	if !m.authButtonHit(msg.X, msg.Y) {
		return m, nil
	}
	if m.userName != "" {
		return m, m.logout()
	}
	m.authModalOpen = true
	m.tokenInput.Focus()
	m.tokenInput.Reset()
	m.authError = ""
	m.deviceCode = nil
	return m, textinput.Blink
}

// openPublishModal opens the create-and-push modal for the selected repo.
func (m *DashboardModel) openPublishModal(msg string) tea.Cmd {
	repo := m.repos[m.selected]
	cleanName := strings.ToLower(strings.ReplaceAll(repo.Name, " ", "-"))
	m.publishNameInput.SetValue(cleanName)
	m.publishNameInput.Focus()
	m.publishModalOpen = true
	m.publishPrivate = false
	m.message = msg
	return textinput.Blink
}

// switchView centralizes tab/view changes: stash the outgoing repo list,
// restore (or clear) the incoming one, then run the shared per-view side
// effects. Views other than Local/GitHub share the same slots and render no
// repo list, so their stale rows can never bleed across a switch.
func (m *DashboardModel) switchView(v ViewMode) tea.Cmd {
	m.stashActiveList()
	m.viewMode = v
	if v == ViewLocal || v == ViewGitHub {
		if !m.restoreList(v) {
			m.repos = nil
			m.selected = 0
		}
	}
	return m.handleViewChange()
}

func (m *DashboardModel) handleViewChange() tea.Cmd {
	// Panes, overlays, and modals are scoped to the previous view's repo;
	// drop them all so nothing bleeds across a switch. The prompt is kept
	// because the clone flow (global Ctrl+L) opens it without a repo.
	m.gitDiffActive = false
	m.gitLogActive = false
	m.syncOverlay = false
	m.hunkMode = false
	m.overlay = nil
	m.confirm = nil
	m.fileCursor = 0
	switch m.viewMode {
	case ViewLocal:
		m.message = "Showing local repositories"
		if !m.scanning && len(m.repos) == 0 {
			m.scanning = true
			return scanCommand(context.Background())
		}
	case ViewGitHub:
		if m.token == nil {
			m.setStatus(statusLoading, "Authenticating with GitHub...")
			return githubLoginCommand(context.Background())
		}
		if len(m.repos) == 0 {
			m.setStatus(statusLoading, "Fetching GitHub repositories...")
			return githubReposCommand(context.Background(), m.token)
		}
	case ViewSystem:
		m.message = "System Performance Monitor (Real-time polling active)"
		if m.sysCollector != nil {
			m.sysSnapshot = m.sysCollector.TakeSnapshot()
		}
	case ViewPlugins:
		m.message = "Interactive Plugin Manager (Named Pipe gRPC)"
		if m.pluginManager != nil {
			m.plugins, _ = m.pluginManager.DiscoverPlugins()
		}
	}
	return nil
}
