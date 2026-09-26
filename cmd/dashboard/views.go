package main

// All rendering: the root View, tab chrome, modals, per-view panes, and
// shared text/progress render helpers.

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"tui/pkg/explorer"
	"tui/pkg/git"
	"tui/pkg/plugin"
	"tui/pkg/system"
	"tui/pkg/theme"
)

func (m DashboardModel) View() string {
	pal := theme.GetThemeByIndex(m.themeIndex)

	termWidth := m.width
	if termWidth <= 0 {
		termWidth = 100
	}
	termHeight := m.height
	if termHeight <= 0 {
		termHeight = 30
	}

	// Responsive breakpoints
	isNarrow := termWidth < 100
	isVeryNarrow := termWidth < 80

	// 1. Top Title Bar & Header
	logo := lipgloss.NewStyle().
		Bold(true).
		Background(pal.Primary).
		Foreground(pal.Background).
		Padding(0, 1).
		Render("⚡ TERMINAL DASHBOARD")

	verPill := lipgloss.NewStyle().
		Bold(true).
		Background(pal.Border).
		Foreground(pal.Foreground).
		Padding(0, 1).
		Render("v" + m.version)

	leftHeader := lipgloss.JoinHorizontal(lipgloss.Center, logo, verPill)

	themePill := lipgloss.NewStyle().
		Foreground(pal.Highlight).
		Bold(true).
		Render("🎨 " + pal.Name) + lipgloss.NewStyle().Foreground(pal.Muted).Render(" [t]")

	var userPill string
	if m.userName != "" {
		credTag := ""
		if m.token != nil {
			switch {
			case strings.HasPrefix(*m.token, "ghp_"):
				credTag = " [classic PAT]"
			case strings.HasPrefix(*m.token, "github_pat_"):
				credTag = " [fine-grained PAT]"
			case strings.HasPrefix(*m.token, "ghu_"), strings.HasPrefix(*m.token, "gho_"):
				credTag = " [device-flow]"
			case strings.HasPrefix(*m.token, "ghs_"):
				credTag = " [GitHub App]"
			default:
				credTag = " [custom token]"
			}
		}
		if isVeryNarrow {
			userPill = lipgloss.NewStyle().
				Bold(true).
				Foreground(pal.Success).
				Render("● @" + m.userName)
		} else {
			userPill = lipgloss.NewStyle().
				Bold(true).
				Foreground(pal.Success).
				Render("● @" + m.userName + credTag)
		}
	} else {
		userPill = lipgloss.NewStyle().
			Foreground(pal.Muted).
			Render("○ Guest [l]")
	}

	var authButton string
	if m.userName != "" {
		authButton = lipgloss.NewStyle().
			Bold(true).
			Background(pal.Danger).
			Foreground(pal.Background).
			Padding(0, 1).
			Render(authButtonLabel(true))
	} else {
		authButton = lipgloss.NewStyle().
			Bold(true).
			Background(pal.Primary).
			Foreground(pal.Background).
			Padding(0, 1).
			Render(authButtonLabel(false))
	}

	rightHeader := lipgloss.JoinHorizontal(
		lipgloss.Center,
		themePill,
		lipgloss.NewStyle().Foreground(pal.Border).Render("  │  "),
		userPill,
	)

	headerGap := termWidth - lipgloss.Width(leftHeader) - lipgloss.Width(rightHeader) - 2
	if headerGap < 1 {
		headerGap = 1
	}
	headerLine := lipgloss.JoinHorizontal(lipgloss.Center, leftHeader, strings.Repeat(" ", headerGap), rightHeader)

	// 2. Navigation Tabs (responsive: short labels when narrow)
	tabLocal := formatTab("1", "Local", "📁", m.viewMode == ViewLocal && !m.explorerMode, pal, isNarrow)
	tabGH := formatTab("2", "GitHub", "🐙", m.viewMode == ViewGitHub && !m.explorerMode, pal, isNarrow)
	tabSys := formatTab("3", "System", "📈", m.viewMode == ViewSystem, pal, isNarrow)
	tabPlug := formatTab("4", "Plugins", "🔌", m.viewMode == ViewPlugins, pal, isNarrow)

	tabsList := []string{tabLocal, tabGH, tabSys, tabPlug}
	if m.explorerMode && m.activeExplorer != nil {
		expLabel := "EXPLORER"
		if !isNarrow {
			expLabel = "EXPLORER: " + m.activeExplorer.RelativeCurrentPath()
		}
		expTab := lipgloss.NewStyle().
			Bold(true).
			Background(pal.Highlight).
			Foreground(pal.Background).
			Padding(0, 1).
			MarginRight(1).
			Render(fmt.Sprintf(" 📂 %s ", expLabel))
		tabsList = append(tabsList, expTab)
	}
	tabsLeft := lipgloss.JoinHorizontal(lipgloss.Top, tabsList...)
	navGap := termWidth - lipgloss.Width(tabsLeft) - lipgloss.Width(authButton) - 2
	if navGap < 1 {
		navGap = 1
	}
	tabsLine := lipgloss.JoinHorizontal(lipgloss.Top, tabsLeft, strings.Repeat(" ", navGap), authButton)

	// 3. Subtitle / Shortcut Keycaps Bar (responsive: collapse at narrow widths)
	var keyItems []string
	if m.explorerMode {
		keyItems = m.explorerKeyItems(pal, isNarrow)
	} else if m.viewMode == ViewGitHub {
		keyItems = m.githubKeyItems(pal, isNarrow)
	} else if m.viewMode == ViewPlugins {
		keyItems = m.pluginsKeyItems(pal, isNarrow)
	} else if m.viewMode == ViewSystem {
		keyItems = m.systemKeyItems(pal, isNarrow)
	} else {
		keyItems = m.localKeyItems(pal, isNarrow)
	}

	bullet := lipgloss.NewStyle().Foreground(pal.Border).Render("  •  ")
	subtitleLine := " " + fitKeyItems(keyItems, bullet, termWidth-2, pal)

	// Thin horizontal divider
	dividerLine := lipgloss.NewStyle().Foreground(pal.Border).Render(strings.Repeat("─", termWidth))

	// 4. Main Body Height Management
	mainHeight := termHeight - 8
	if mainHeight < 14 {
		mainHeight = 14
	}

	var mainBody string
	// Generic confirm/prompt modals take precedence over legacy modals.
	if modalBody, isAdvanced := m.advancedModalHook(pal); isAdvanced {
		mainBody = modalBody
	} else if m.authModalOpen {
		mainBody = m.renderAuthModal(pal)
	} else if m.openFolderModalOpen {
		mainBody = m.renderOpenFolderModal(pal)
	} else if m.commitModalOpen {
		mainBody = m.renderCommitModal(pal)
	} else if m.publishModalOpen {
		mainBody = m.renderPublishModal(pal)
	} else if m.explorerMode && m.activeExplorer != nil {
		mainBody = m.renderExplorerView(pal, termWidth, mainHeight)
	} else {
		switch m.viewMode {
		case ViewLocal, ViewGitHub:
			mainBody = m.renderRepoView(pal, termWidth, mainHeight)
		case ViewSystem:
			mainBody = m.renderSystemView(pal, termWidth, mainHeight)
		case ViewPlugins:
			mainBody = m.renderPluginsView(pal, termWidth, mainHeight)
		}
	}
	// Advanced overlays (stash/branches/reflog/blame/issues/sync) replace the
	// main body; the hunk-mode bar rides above the diff pane.
	mainBody = m.advancedViewHook(pal, termWidth, mainHeight, mainBody)

	// 5. Status Bar with Indicator Icon, context and clock.
	statusIcon := statusIconFor(m.statusLevel, pal)
	statusPrefix := fmt.Sprintf("%sStatus: ", statusIcon)

	// Right-hand context: active branch (Local tab) and wall clock. Refreshes
	// because View() re-runs on every systemTickMsg (every 1.5 s).
	var ctxParts []string
	if m.viewMode == ViewLocal && m.selected >= 0 && m.selected < len(m.repos) {
		if br := m.repos[m.selected].Branch; br != "" && br != "…" && br != "unknown" {
			ctxParts = append(ctxParts, lipgloss.NewStyle().
				Foreground(pal.Highlight).Bold(true).Render("⎇ "+br))
		}
	}
	ctxParts = append(ctxParts, lipgloss.NewStyle().
		Foreground(pal.Muted).Render(time.Now().Format("15:04:05")))
	ctxSep := lipgloss.NewStyle().Foreground(pal.Border).Render("  │  ")
	statusRight := strings.Join(ctxParts, ctxSep)

	// Fit within the status bar's inner width (padding 1 each side).
	inner := termWidth - 2
	if inner < 20 {
		inner = 20
	}
	rightW := lipgloss.Width(statusRight)
	msgRoom := inner - lipgloss.Width(statusPrefix) - rightW - 1
	if msgRoom < 10 {
		// Not enough room for context: drop it, then keep a message stub.
		statusRight = ""
		rightW = 0
		msgRoom = inner - lipgloss.Width(statusPrefix) - 1
		if msgRoom < 10 {
			msgRoom = 10
		}
	}
	left := statusPrefix + trunc(m.message, msgRoom)
	gap := inner - lipgloss.Width(left) - rightW
	if gap < 1 {
		gap = 1
	}

	statusBar := lipgloss.NewStyle().
		Background(pal.Background).
		Foreground(pal.Foreground).
		Padding(0, 1).
		Render(left + strings.Repeat(" ", gap) + statusRight)

	// 6. Bottom Footer
	// The authenticated user already appears in the header; keep the footer
	// for shortcuts instead of repeating the identity pill.
	footerLeft := lipgloss.NewStyle().
		Foreground(pal.Muted).
		Render("Tab: views  •  t: theme  •  q: quit")

	footerCenter := lipgloss.NewStyle().
		Foreground(pal.Muted).
		Render("🔒 Zero-TCP Named Pipe IPC  •  🛡️ Windows Job Object Guard")

	footerRight := lipgloss.NewStyle().
		Foreground(pal.Muted).
		Render(fmt.Sprintf("v%s • UTF-8 VTP", m.version))

	footerMid := lipgloss.NewStyle().Foreground(pal.Border).Render("  │  ")
	footerCombinedLeft := lipgloss.JoinHorizontal(lipgloss.Center, footerLeft, footerMid, footerCenter)
	footerGap := termWidth - lipgloss.Width(footerCombinedLeft) - lipgloss.Width(footerRight) - 2
	if footerGap < 1 {
		footerGap = 1
	}
	footer := lipgloss.JoinHorizontal(lipgloss.Center, footerCombinedLeft, strings.Repeat(" ", footerGap), footerRight)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		headerLine,
		tabsLine,
		subtitleLine,
		dividerLine,
		mainBody,
		statusBar,
		footer,
	)

	return lipgloss.NewStyle().Width(termWidth).Height(termHeight).Render(content)
}

func formatTab(num, name, icon string, active bool, pal theme.Palette, isNarrow bool) string {
	labelName := name
	if isNarrow {
		labelName = ""
	}
	if active {
		label := fmt.Sprintf(" %s %s %s ",
			lipgloss.NewStyle().Bold(true).Foreground(pal.Highlight).Render(num),
			icon,
			labelName,
		)
		return lipgloss.NewStyle().
			Bold(true).
			Background(pal.Secondary).
			Foreground(pal.Foreground).
			Padding(0, 1).
			MarginRight(1).
			Render(label)
	}
	label := fmt.Sprintf(" %s %s %s ",
		lipgloss.NewStyle().Foreground(pal.Muted).Render(num),
		icon,
		labelName,
	)
	return lipgloss.NewStyle().
		Foreground(pal.Muted).
		Padding(0, 1).
		MarginRight(1).
		Render(label)
}

func renderKeyBadge(key, label string, pal theme.Palette) string {
	k := lipgloss.NewStyle().Bold(true).Foreground(pal.Highlight).Render("[" + key + "]")
	l := lipgloss.NewStyle().Foreground(pal.Foreground).Render(" " + label)
	return k + l
}

func (m DashboardModel) localKeyItems(pal theme.Palette, narrow bool) []string {
	if narrow {
		return []string{
			renderKeyBadge("n", "Create", pal),
			renderKeyBadge("a", "Stage", pal),
			renderKeyBadge("c", "Commit", pal),
			renderKeyBadge("P", "Push", pal),
			renderKeyBadge("F", "Pull", pal),
			renderKeyBadge("d", "Diff", pal),
			renderKeyBadge("g", "Log", pal),
			renderKeyBadge("f", "Files", pal),
			renderKeyBadge("o", "Open", pal),
			renderKeyBadge("l", "Login", pal),
			renderKeyBadge("t", "Theme", pal),
			renderKeyBadge("q", "Quit", pal),
		}
	}
	return []string{
		renderKeyBadge("n", "Create & Push", pal),
		renderKeyBadge("b", "Browse GUI", pal),
		renderKeyBadge("o", "Open Folder", pal),
		renderKeyBadge("i", "Init Git", pal),
		renderKeyBadge("a", "Stage All", pal),
		renderKeyBadge("c", "Commit", pal),
		renderKeyBadge("P", "Push", pal),
		renderKeyBadge("F", "Pull", pal),
		renderKeyBadge("d", "Diff", pal),
		renderKeyBadge("g", "Log", pal),
		renderKeyBadge("f / Enter", "Files", pal),
		renderKeyBadge("e", "GUI Explorer", pal),
		renderKeyBadge("p", "Terminal", pal),
		renderKeyBadge("l", "Login", pal),
		renderKeyBadge("u", "Logout", pal),
		renderKeyBadge("t", "Theme", pal),
		renderKeyBadge("q", "Quit", pal),
	}
}

func (m DashboardModel) githubKeyItems(pal theme.Palette, narrow bool) []string {
	if narrow {
		return []string{
			renderKeyBadge("Tab", "Tabs", pal),
			renderKeyBadge("l", "Login", pal),
			renderKeyBadge("r", "Refresh", pal),
			renderKeyBadge("Enter", "Details", pal),
			renderKeyBadge("t", "Theme", pal),
			renderKeyBadge("q", "Quit", pal),
		}
	}
	return []string{
		renderKeyBadge("Tab", "Switch Tab", pal),
		renderKeyBadge("l", "Login / PAT", pal),
		renderKeyBadge("u", "Logout", pal),
		renderKeyBadge("r", "Refresh", pal),
		renderKeyBadge("Enter", "Details", pal),
		renderKeyBadge("t", "Theme", pal),
		renderKeyBadge("q", "Quit", pal),
	}
}

func (m DashboardModel) pluginsKeyItems(pal theme.Palette, narrow bool) []string {
	if narrow {
		return []string{
			renderKeyBadge("Tab", "Tabs", pal),
			renderKeyBadge("s", "Start", pal),
			renderKeyBadge("x", "Stop", pal),
			renderKeyBadge("r", "Reload", pal),
			renderKeyBadge("j/k", "Select", pal),
			renderKeyBadge("t", "Theme", pal),
			renderKeyBadge("q", "Quit", pal),
		}
	}
	return []string{
		renderKeyBadge("Tab", "Switch Tab", pal),
		renderKeyBadge("s", "Start Plugin", pal),
		renderKeyBadge("x", "Stop", pal),
		renderKeyBadge("r", "Reload / Render", pal),
		renderKeyBadge("j/k", "Select", pal),
		renderKeyBadge("t", "Theme", pal),
		renderKeyBadge("q", "Quit", pal),
	}
}

func (m DashboardModel) systemKeyItems(pal theme.Palette, narrow bool) []string {
	if narrow {
		return []string{
			renderKeyBadge("Tab", "Tabs", pal),
			renderKeyBadge("r", "Refresh", pal),
			renderKeyBadge("t", "Theme", pal),
			renderKeyBadge("q", "Quit", pal),
		}
	}
	return []string{
		renderKeyBadge("Tab", "Switch Tab", pal),
		renderKeyBadge("r", "Refresh Metrics", pal),
		renderKeyBadge("t", "Theme", pal),
		renderKeyBadge("w", "Add to WT", pal),
		renderKeyBadge("q", "Quit", pal),
	}
}

func (m DashboardModel) explorerKeyItems(pal theme.Palette, narrow bool) []string {
	if narrow {
		return []string{
			renderKeyBadge("Enter", "Open", pal),
			renderKeyBadge("←/→", "Nav", pal),
			renderKeyBadge("j/k", "Move", pal),
			renderKeyBadge("e", "Explorer", pal),
			renderKeyBadge("p", "Terminal", pal),
			renderKeyBadge("v", "VS Code", pal),
			renderKeyBadge("r", "Refresh", pal),
			renderKeyBadge("t", "Theme", pal),
			renderKeyBadge("Esc", "Back", pal),
		}
	}
	return []string{
		renderKeyBadge("Enter / →", "Open", pal),
		renderKeyBadge("Backspace / ←", "Up", pal),
		renderKeyBadge("j / k", "Move", pal),
		renderKeyBadge("e", "GUI Explorer", pal),
		renderKeyBadge("p", "Terminal", pal),
		renderKeyBadge("v", "VS Code", pal),
		renderKeyBadge("r", "Refresh", pal),
		renderKeyBadge("t", "Theme", pal),
		renderKeyBadge("Esc / b", "Back to Repos", pal),
	}
}

func (m DashboardModel) renderAuthModal(pal theme.Palette) string {
	modalWidth := 68
	var b strings.Builder

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).Render(" 🔐 GitHub Authentication\n\n"))
	b.WriteString("  Authenticate to view private repositories, create repos, and push code.\n")
	if m.token != nil && strings.TrimSpace(*m.token) != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render(
			fmt.Sprintf("  Currently stored: %s (as @%s). Pasting a new token below replaces it.\n", describeTokenType(*m.token), m.userName)))
	}
	b.WriteString("\n")

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render("  Option 1: Personal Access Token (PAT) [Required for Creating Repos]\n"))
	b.WriteString("  Create token at: https://github.com/settings/tokens (Scope: repo, read:org)\n")
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Highlight).Bold(true).Render("  👉 Press [Ctrl+O] to open the browser with pre-configured scopes!\n\n"))
	b.WriteString(fmt.Sprintf("  Token: %s\n\n", m.tokenInput.View()))

	if m.deviceCode != nil {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Warning).Render("  Option 2: Device Code Authorization Active!\n"))
		b.WriteString(fmt.Sprintf("    1. Open URL:  %s\n", lipgloss.NewStyle().Bold(true).Foreground(pal.Highlight).Render(m.deviceCode.VerificationURL)))
		b.WriteString(fmt.Sprintf("    2. Enter Code: %s\n", lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).Render(m.deviceCode.UserCode)))
		b.WriteString(fmt.Sprintf("    3. Waiting for authorization... %s\n\n", m.spinner.View()))
	} else {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render("  Option 2: GitHub Device Flow (Read-Only / Basic)\n"))
		b.WriteString("  Press [Ctrl+D] to request a browser device verification code.\n")
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render("  (Note: GitHub Device Flow tokens cannot create repositories on user accounts)\n\n"))
	}

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render("  Option 3: Environment Variables (most reliable paste path)\n"))
	b.WriteString("  In PowerShell run:  $env:GITHUB_TOKEN=\"<paste PAT here>\"\n")
	b.WriteString("  Then press [Ctrl+E] to check and import it.\n\n")

	if m.authError != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Danger).Bold(true).Render(fmt.Sprintf("  ✖ Error: %s\n\n", m.authError)))
	}

	b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render("  Controls: [Enter] Submit PAT | [Ctrl+O] Browser | [Ctrl+D] Device Flow | [Ctrl+E] Env Token | [Ctrl+X] Logout | [Esc] Cancel"))
	if m.pendingPublish && m.token != nil {
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render(" ([Esc] continues publish with current token)"))
	}
	b.WriteString("\n")

	return lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(pal.Primary).
		Padding(1, 2).
		Width(modalWidth).
		Render(b.String())
}

func (m DashboardModel) renderOpenFolderModal(pal theme.Palette) string {
	modalWidth := 72
	var b strings.Builder

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).Render(" 📂 Open Local Folder\n\n"))
	b.WriteString("  Enter the directory path to open in the dashboard.\n")
	b.WriteString("  • If it is a Git repository, all branches, worktree status, diffs, and actions will load.\n")
	b.WriteString("  • If it is NOT a Git repo, you can initialize it with [i] (git init) or explore files.\n\n")

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render("  Folder Path:\n"))
	b.WriteString(fmt.Sprintf("  %s\n\n", m.folderInput.View()))

	b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render("  Examples:\n"))
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Foreground).Render("    • .                     (current working directory)\n"))
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Foreground).Render("    • Z:\\CodeBase\\TUI       (absolute path)\n"))
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Foreground).Render("    • ..\\another-project    (relative path)\n\n"))

	b.WriteString(lipgloss.NewStyle().Foreground(pal.Highlight).Bold(true).Render("  ⚡ Prefer GUI? Press [Tab] or [Ctrl+B] to launch Windows GUI File Explorer Dialog!\n\n"))
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render("  Controls: [Tab / Ctrl+B] Browse GUI | [Enter] Open Typed Path | [Esc] Cancel\n"))

	return lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(pal.Primary).
		Padding(1, 2).
		Width(modalWidth).
		Render(b.String())
}

func (m DashboardModel) renderCommitModal(pal theme.Palette) string {
	modalWidth := 72
	var b strings.Builder

	repoName := ""
	if m.selected >= 0 && m.selected < len(m.repos) {
		repoName = m.repos[m.selected].Name
	}

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).Render(fmt.Sprintf(" 💾 Commit Changes — %s\n\n", repoName)))
	b.WriteString("  All modified and untracked files will be automatically staged (git add -A)\n")
	b.WriteString("  before creating the commit.\n\n")

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render("  Commit Message:\n"))
	b.WriteString(fmt.Sprintf("  %s\n\n", m.commitInput.View()))

	if m.detailedGitStatus != nil && len(m.detailedGitStatus.Files) > 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Warning).Bold(true).Render(fmt.Sprintf("  Changes to be committed (%d file(s)):\n", len(m.detailedGitStatus.Files))))
		limit := 4
		for idx, f := range m.detailedGitStatus.Files {
			if idx >= limit {
				b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render(fmt.Sprintf("    ... and %d more file(s)\n", len(m.detailedGitStatus.Files)-limit)))
				break
			}
			codeColor := pal.Success
			if strings.Contains(f.Status, "?") {
				codeColor = pal.Highlight
			} else if strings.Contains(f.Status, "M") {
				codeColor = pal.Warning
			}
			b.WriteString(fmt.Sprintf("    [%s] %s\n", lipgloss.NewStyle().Foreground(codeColor).Bold(true).Render(f.Status), f.Path))
		}
		b.WriteString("\n")
	}

	b.WriteString(lipgloss.NewStyle().Foreground(pal.Highlight).Render("  Controls: [Enter] Create Commit | [Esc] Cancel\n"))

	return lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(pal.Primary).
		Padding(1, 2).
		Width(modalWidth).
		Render(b.String())
}

func (m DashboardModel) renderPublishModal(pal theme.Palette) string {
	modalWidth := 74
	var b strings.Builder

	repoPath := ""
	if m.selected >= 0 && m.selected < len(m.repos) {
		repoPath = m.repos[m.selected].Path
	}

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).Render(" 🚀 Create New GitHub Repository & Push\n\n"))
	b.WriteString(fmt.Sprintf("  Local Folder: %s\n\n",
		lipgloss.NewStyle().Foreground(pal.Highlight).Bold(true).Render(truncPath(repoPath, 50))))

	if m.token != nil && strings.HasPrefix(*m.token, "ghu_") {
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Warning).Bold(true).Render(
			"  ⚠️  Notice: Active session uses GitHub Device Flow (ghu_...).\n" +
				"      If creation fails with a scope error, press [Ctrl+L] to enter a PAT with 'repo' scope.\n\n",
		))
	} else if m.token == nil || *m.token == "" {
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Danger).Bold(true).Render(
			"  ⚠️  Notice: Not authenticated with GitHub.\n" +
				"      Press [Ctrl+L] to provide a Personal Access Token (PAT) with 'repo' scope.\n\n",
		))
	}

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render("  Repository Name on GitHub:\n"))
	b.WriteString(fmt.Sprintf("  %s\n\n", m.publishNameInput.View()))

	var visBadge string
	if m.publishPrivate {
		visBadge = lipgloss.NewStyle().Bold(true).Background(pal.Warning).Foreground(pal.Background).Padding(0, 1).Render("🔒 Private") +
			lipgloss.NewStyle().Foreground(pal.Muted).Render("  (Only you can see this repository)")
	} else {
		visBadge = lipgloss.NewStyle().Bold(true).Background(pal.Success).Foreground(pal.Background).Padding(0, 1).Render("🌐 Public") +
			lipgloss.NewStyle().Foreground(pal.Muted).Render("  (Anyone on GitHub can see this repository)")
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render("  Visibility: ") + visBadge + "\n")
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Highlight).Render("  Press [Tab] to toggle Public / Private\n\n"))

	targetAccount := "@" + m.userName
	if m.userName == "" {
		targetAccount = "Guest (Login with [Ctrl+L])"
	}
	b.WriteString(fmt.Sprintf("  Target GitHub Account: %s\n\n", lipgloss.NewStyle().Bold(true).Foreground(pal.Accent).Render(targetAccount)))

	b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render("  Automated Actions upon [Enter]:\n"))
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Foreground).Render("    1. Initialize Git repository and set branch to 'main'\n"))
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Foreground).Render("    2. Stage all files (git add -A) and create initial commit\n"))
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Foreground).Render("    3. Create remote repository via GitHub REST API\n"))
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Foreground).Render("    4. Connect remote origin & push code (git push -u origin main)\n\n"))

	b.WriteString(lipgloss.NewStyle().Foreground(pal.Highlight).Render("  Controls: [Enter] Create & Push | [Tab] Toggle Visibility | [Ctrl+L] Switch Token | [Esc] Cancel\n"))

	return lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(pal.Primary).
		Padding(1, 2).
		Width(modalWidth).
		Render(b.String())
}

func (m DashboardModel) renderRepoView(pal theme.Palette, totalWidth, totalHeight int) string {
	isNarrow := totalWidth < 100
	leftWidth := (totalWidth * 42) / 100
	if leftWidth < 30 {
		leftWidth = 30
	}
	if leftWidth > 52 {
		leftWidth = 52
	}
	rightWidth := totalWidth - leftWidth - 4
	if rightWidth < 34 {
		rightWidth = 34
	}

	// Left: Repo list
	var repoList strings.Builder
	repoTitle := "📦 Local Repositories"
	if m.viewMode == ViewGitHub {
		repoTitle = "🐙 GitHub Repositories"
	}
	if isNarrow {
		repoTitle = "📦 Repos"
		if m.viewMode == ViewGitHub {
			repoTitle = "🐙 GitHub"
		}
	}
	repoList.WriteString(lipgloss.NewStyle().
		Bold(true).
		Foreground(pal.Primary).
		Render(fmt.Sprintf(" %s (%d)\n", repoTitle, len(m.repos))))

	if isNarrow {
		repoList.WriteString(lipgloss.NewStyle().
			Foreground(pal.Muted).
			Render(fmt.Sprintf("   %-12s %-7s %s\n", "NAME", "BRANCH", "STATUS")))
	} else {
		repoList.WriteString(lipgloss.NewStyle().
			Foreground(pal.Muted).
			Render(fmt.Sprintf("   %-18s %-9s %s\n", "NAME", "BRANCH", "STATUS")))
	}
	repoList.WriteString(lipgloss.NewStyle().
		Foreground(pal.Border).
		Render("  " + strings.Repeat("─", leftWidth-6) + "\n"))

	maxDisplay := totalHeight - 5
	if maxDisplay < 4 {
		maxDisplay = 4
	}

	if m.scanning {
		repoList.WriteString("\n  " + m.spinner.View() + " Scanning repositories...\n")
	} else if len(m.repos) == 0 {
		if m.viewMode == ViewGitHub && m.token == nil {
			repoList.WriteString("\n  Not authenticated with GitHub.\n\n  Press [l] to enter a Personal Access Token (PAT).\n")
		} else {
			repoList.WriteString("\n  No repositories found.\n  Press [r] to scan or refresh.\n")
		}
	} else {
		startIdx := 0
		if m.selected >= maxDisplay {
			startIdx = m.selected - maxDisplay + 1
		}
		endIdx := startIdx + maxDisplay
		if endIdx > len(m.repos) {
			endIdx = len(m.repos)
		}

		for i := startIdx; i < endIdx; i++ {
			repo := m.repos[i]
			var statusBadge string
			switch repo.Status {
			case StatusClean:
				statusBadge = lipgloss.NewStyle().Foreground(pal.Success).Render("● Clean")
			case StatusModified, StatusHasChanges:
				statusBadge = lipgloss.NewStyle().Foreground(pal.Warning).Render("▲ Changed")
			case StatusLoading:
				statusBadge = lipgloss.NewStyle().Foreground(pal.Accent).Render("◌ Loading")
			case StatusError:
				statusBadge = lipgloss.NewStyle().Foreground(pal.Danger).Render("✖ Error")
			default:
				statusBadge = lipgloss.NewStyle().Foreground(pal.Muted).Render(string(repo.Status))
			}

			branchWidth := 9
			if isNarrow {
				branchWidth = 7
			}
			nameColWidth := leftWidth - branchWidth - 14
			if nameColWidth < 10 {
				nameColWidth = 10
			}
			rName := trunc(repo.Name, nameColWidth)
			rBranch := trunc(repo.Branch, branchWidth)
			if rBranch == "" {
				rBranch = "-"
			}

			if i == m.selected {
				rowText := fmt.Sprintf(" ▸ %-*s %-*s %s", nameColWidth, rName, branchWidth, rBranch, statusBadge)
				repoList.WriteString(lipgloss.NewStyle().
					Background(pal.Secondary).
					Foreground(pal.Foreground).
					Bold(true).
					Render(rowText) + "\n")
			} else {
				rowText := fmt.Sprintf("   %-*s %-*s %s\n", nameColWidth, rName, branchWidth, rBranch, statusBadge)
				repoList.WriteString(rowText)
			}
		}

		if len(m.repos) > maxDisplay {
			repoList.WriteString(lipgloss.NewStyle().
				Foreground(pal.Muted).
				Render(fmt.Sprintf("\n  Showing %d-%d of %d repos", startIdx+1, endIdx, len(m.repos))))
		}
	}

	leftStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Border).
		Width(leftWidth).
		Height(totalHeight).
		Padding(0, 1).
		Render(repoList.String())

	detailStr := m.viewDetail(pal, rightWidth, totalHeight)
	return lipgloss.JoinHorizontal(lipgloss.Top, leftStyle, detailStr)
}

func (m DashboardModel) renderGitDiffView(pal theme.Palette, width, height int) string {
	var b strings.Builder
	repoName := ""
	if m.selected >= 0 && m.selected < len(m.repos) {
		repoName = m.repos[m.selected].Name
	}

	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(pal.Primary).
		Render(fmt.Sprintf("📜 Git Diff (HEAD) — %s", repoName))

	// In hunk mode, show only the active hunk so n/p visibly swap the pane.
	// The hunk bodies from GitSplitHunks already carry the file header, so each
	// renders as a standalone patch. (Previously the full diff was always shown
	// and hunk mode was a counter with no visible target.)
	body := m.gitDiffText
	if m.hunkMode && m.overlay != nil && m.hunkCursor < len(m.overlay.hunks) {
		body = m.overlay.hunks[m.hunkCursor].Body
	}

	// Line-change summary so size is obvious before scrolling. Counts skip
	// +++/--- file headers (they'd otherwise inflate both counters).
	adds, dels := 0, 0
	for _, l := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
		case strings.HasPrefix(l, "+"):
			adds++
		case strings.HasPrefix(l, "-"):
			dels++
		}
	}
	summary := ""
	if adds+dels > 0 {
		summary = "  " +
			lipgloss.NewStyle().Foreground(pal.Success).Bold(true).Render(fmt.Sprintf("+%d", adds)) +
			lipgloss.NewStyle().Foreground(pal.Muted).Render(" / ") +
			lipgloss.NewStyle().Foreground(pal.Danger).Bold(true).Render(fmt.Sprintf("−%d", dels))
	}
	b.WriteString(header + summary + "  " + lipgloss.NewStyle().Foreground(pal.Highlight).Bold(true).Render("[d / Esc] Back\n\n"))

	if strings.TrimSpace(body) == "" {
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Success).Render("  ✔ Working tree is clean. No unstaged or staged changes relative to HEAD.\n\n"))
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render("  Press [c] to commit changes or [Esc] to exit diff view.\n"))
	} else {
		lines := strings.Split(body, "\n")
		maxLines := height - 6
		if maxLines < 6 {
			maxLines = 6
		}
		dispCount := 0
		for _, line := range lines {
			if dispCount >= maxLines {
				b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render(fmt.Sprintf("\n  ... and %d more diff lines", len(lines)-maxLines)))
				break
			}
			dispCount++
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
				b.WriteString(lipgloss.NewStyle().Foreground(pal.Success).Render(line) + "\n")
			} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
				b.WriteString(lipgloss.NewStyle().Foreground(pal.Danger).Render(line) + "\n")
			} else if strings.HasPrefix(line, "@@") {
				b.WriteString(lipgloss.NewStyle().Foreground(pal.Highlight).Bold(true).Render(line) + "\n")
			} else if strings.HasPrefix(line, "diff --git") {
				b.WriteString(lipgloss.NewStyle().Foreground(pal.Primary).Bold(true).Render(line) + "\n")
			} else {
				b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render(line) + "\n")
			}
		}
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Highlight).
		Padding(0, 1).
		Width(width).
		Height(height).
		Render(b.String())
}

func (m DashboardModel) renderGitLogView(pal theme.Palette, width, height int) string {
	var b strings.Builder
	repoName := ""
	if m.selected >= 0 && m.selected < len(m.repos) {
		repoName = m.repos[m.selected].Name
	}

	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(pal.Primary).
		Render(fmt.Sprintf("📜 Git Commit History — %s", repoName))
	b.WriteString(header + "  " + lipgloss.NewStyle().Foreground(pal.Highlight).Bold(true).Render("[g / Esc] Back · [j/k] select · [Enter] diff · [R] checkout · [Shift+R] hard reset\n\n"))

	if len(m.gitLogItems) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render("  No commits found in this repository branch.\n\n"))
	} else {
		// Two display lines per commit; scroll so the highlighted commit
		// (which [Enter] acts on) is always visible.
		maxItems := (height - 6) / 2
		if maxItems < 3 {
			maxItems = 3
		}
		startIdx := 0
		if m.gitLogCursor >= maxItems {
			startIdx = m.gitLogCursor - maxItems + 1
		}
		endIdx := startIdx + maxItems
		if endIdx > len(m.gitLogItems) {
			endIdx = len(m.gitLogItems)
		}
		for i := startIdx; i < endIdx; i++ {
			item := m.gitLogItems[i]
			marker := "  "
			subjStyle := lipgloss.NewStyle().Foreground(pal.Foreground)
			if i == m.gitLogCursor {
				marker = "▸ "
				subjStyle = lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
			}
			hashBadge := lipgloss.NewStyle().Bold(true).Foreground(pal.Highlight).Render(item.Hash)
			dateBadge := lipgloss.NewStyle().Foreground(pal.Muted).Render(item.Date)
			authorBadge := lipgloss.NewStyle().Foreground(pal.Secondary).Render("👤 " + item.Author)
			subj := subjStyle.Render(trunc(item.Subject, width-10))

			b.WriteString(fmt.Sprintf("%s%s  %s  %s\n", marker, hashBadge, dateBadge, authorBadge))
			b.WriteString(fmt.Sprintf("%s  %s\n\n", marker, subj))
		}
		if endIdx < len(m.gitLogItems) {
			b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render(fmt.Sprintf("  ... %d-%d of %d commits · j/k to scroll\n", startIdx+1, endIdx, len(m.gitLogItems))))
		}
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Highlight).
		Padding(0, 1).
		Width(width).
		Height(height).
		Render(b.String())
}

func (m DashboardModel) viewDetail(pal theme.Palette, detailWidth, totalHeight int) string {
	if m.selected < 0 || m.selected >= len(m.repos) {
		hint := "\n  Select a repository from the list to view details.\n\n  Press [j/k] or [↑/↓] to navigate.\n"
		if m.viewMode == ViewGitHub && m.token == nil {
			hint = "\n  Press [l] to Login with your GitHub Personal Access Token (PAT)\n"
		}
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(pal.Border).
			Padding(0, 1).
			Width(detailWidth).
			Height(totalHeight).
			Render(hint)
	}

	if m.gitDiffActive {
		return m.renderGitDiffView(pal, detailWidth, totalHeight)
	}
	if m.gitLogActive {
		return m.renderGitLogView(pal, detailWidth, totalHeight)
	}

	repo := m.repos[m.selected]
	var b strings.Builder

	innerW := detailWidth - 6
	if innerW < 30 {
		innerW = 30
	}

	// Local directory check
	isGitRepo := true
	if repo.LocalOnly {
		isGitRepo = git.IsGitRepository(repo.Path)
	}

	if !isGitRepo {
		// Non-git folder card
		title := lipgloss.NewStyle().
			Bold(true).
			Foreground(pal.Primary).
			Render("📁 " + repo.Name)
		badge := lipgloss.NewStyle().
			Bold(true).
			Background(pal.Warning).
			Foreground(pal.Background).
			Padding(0, 1).
			Render("Non-Git Folder")
		b.WriteString(title + "  " + badge + "\n\n")

		boxW := innerW
		if boxW < 40 {
			boxW = 40
		}
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" ┌─ Folder Information " + strings.Repeat("─", max(boxW-22, 2)) + "┐\n"))
		b.WriteString(fmt.Sprintf(" │  %-10s : %s\n", "Path", lipgloss.NewStyle().Foreground(pal.Foreground).Render(truncPath(repo.Path, boxW-16))))
		statusText := "Regular Local Directory (No Git tracking)"
		if boxW < 50 {
			statusText = "Local Directory (No Git)"
		}
		b.WriteString(fmt.Sprintf(" │  %-10s : %s\n", "Status", lipgloss.NewStyle().Foreground(pal.Warning).Bold(true).Render(statusText)))
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" └" + strings.Repeat("─", boxW) + "┘\n\n"))

		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Highlight).Render(" ⚡ Actions Available:\n"))
		btnPublish := lipgloss.NewStyle().Bold(true).Background(pal.Primary).Foreground(pal.Background).Padding(0, 1).Render("[n] Create & Push to GitHub")
		btnInit := lipgloss.NewStyle().Bold(true).Background(pal.Secondary).Foreground(pal.Foreground).Padding(0, 1).Render("[i] Git Init")
		if boxW < 60 {
			b.WriteString("   " + btnPublish + "\n")
			b.WriteString("   " + btnInit + "\n\n")
		} else {
			b.WriteString("   " + btnPublish + "  " + btnInit + "\n\n")
		}

		btnBrowse := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[b] Browse GUI")
		btnOpen := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[o] Open Folder")
		btnFiles := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[f / Enter] Browse Files")
		btnExp := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[e] GUI Explorer")
		btnTerm := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[p] Terminal")
		if boxW < 60 {
			b.WriteString("   " + btnBrowse + "\n")
			b.WriteString("   " + btnOpen + "\n\n")
			b.WriteString("   " + btnFiles + "\n")
			b.WriteString("   " + btnExp + "\n")
			b.WriteString("   " + btnTerm + "\n")
		} else {
			b.WriteString("   " + btnBrowse + "  " + btnOpen + "\n\n")
			b.WriteString("   " + btnFiles + "  " + btnExp + "  " + btnTerm + "\n")
		}

		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(pal.Border).
			Padding(0, 1).
			Width(detailWidth).
			Height(totalHeight).
			Render(b.String())
	}

	// Title Card
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(pal.Primary).
		Render("📦 " + repo.Name)

	if repo.Language != "" {
		langPill := lipgloss.NewStyle().
			Bold(true).
			Background(pal.Border).
			Foreground(pal.Highlight).
			Padding(0, 1).
			Render(repo.Language)
		title += " " + langPill
	}
	b.WriteString(title + "\n\n")

	if repo.HasError {
		b.WriteString(lipgloss.NewStyle().
			Foreground(pal.Danger).
			Render(fmt.Sprintf("  ✖ Error: %s\n\n", repo.ErrorMsg)))
	}

	// Information Box
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" ┌─ Repository Information " + strings.Repeat("─", max(innerW-26, 2)) + "┐\n"))
	b.WriteString(fmt.Sprintf(" │  %-10s : %s\n", "Path", lipgloss.NewStyle().Foreground(pal.Foreground).Render(truncPath(repo.Path, innerW-16))))
	b.WriteString(fmt.Sprintf(" │  %-10s : %s\n", "Branch", lipgloss.NewStyle().Foreground(pal.Highlight).Render("⎇ "+repo.Branch)))
	if repo.Remote != "" {
		b.WriteString(fmt.Sprintf(" │  %-10s : %s\n", "Remote", lipgloss.NewStyle().Foreground(pal.Muted).Render(truncPath(repo.Remote, innerW-16))))
	}
	if !repo.LocalOnly {
		b.WriteString(fmt.Sprintf(" │  %-10s : ★ %-5d  ⑂ %-5d\n", "Community", repo.Stars, repo.Forks))
	} else if m.detailedGitStatus != nil && m.detailedGitStatus.Upstream != "" {
		b.WriteString(fmt.Sprintf(" │  %-10s : %s\n", "Upstream", lipgloss.NewStyle().Foreground(pal.Accent).Render(m.detailedGitStatus.Upstream)))
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" └" + strings.Repeat("─", innerW) + "┘\n\n"))

	// Git Worktree & Sync Box
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" ┌─ Git Worktree & Sync " + strings.Repeat("─", max(innerW-23, 2)) + "┐\n"))
	if m.detailedGitStatus != nil {
		syncStr := "✔ Up to date with remote"
		syncColor := pal.Success
		if m.detailedGitStatus.Ahead > 0 || m.detailedGitStatus.Behind > 0 {
			syncStr = fmt.Sprintf("↑ %d ahead  •  ↓ %d behind", m.detailedGitStatus.Ahead, m.detailedGitStatus.Behind)
			syncColor = pal.Warning
		} else if m.detailedGitStatus.Upstream == "" {
			syncStr = "Local branch (no remote tracking)"
			syncColor = pal.Muted
		}
		b.WriteString(fmt.Sprintf(" │  Sync   : %s\n", lipgloss.NewStyle().Foreground(syncColor).Bold(true).Render(syncStr)))

		stagedPill := lipgloss.NewStyle().Foreground(pal.Success).Bold(true).Render(fmt.Sprintf("● %d staged", m.detailedGitStatus.StagedCount))
		unstagedPill := lipgloss.NewStyle().Foreground(pal.Warning).Bold(true).Render(fmt.Sprintf("▲ %d modified", m.detailedGitStatus.UnstagedCount))
		untrackedPill := lipgloss.NewStyle().Foreground(pal.Highlight).Bold(true).Render(fmt.Sprintf("? %d untracked", m.detailedGitStatus.UntrackedCount))
		b.WriteString(fmt.Sprintf(" │  Status : %s  │  %s  │  %s\n", stagedPill, unstagedPill, untrackedPill))
	} else {
		var statusColor lipgloss.Color = pal.Success
		if repo.Status == StatusModified || repo.Status == StatusHasChanges {
			statusColor = pal.Warning
		} else if repo.HasError {
			statusColor = pal.Danger
		}
		b.WriteString(fmt.Sprintf(" │  Status : %s\n", lipgloss.NewStyle().Foreground(statusColor).Bold(true).Render(string(repo.Status))))
	}

	if repo.LastMessage != "" {
		b.WriteString(fmt.Sprintf(" │  Commit : %s\n", lipgloss.NewStyle().Foreground(pal.Foreground).Render("\""+trunc(repo.LastMessage, innerW-14)+"\"")))
	}
	if repo.LastAuthor != "" {
		b.WriteString(fmt.Sprintf(" │  Author : %s\n", lipgloss.NewStyle().Foreground(pal.Muted).Render("👤 "+repo.LastAuthor)))
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" └" + strings.Repeat("─", innerW) + "┘\n\n"))

	// Changed-files list with staging cursor (Local repos only). S/U stage
	// and unstage the highlighted row; D discards it (with confirmation).
	if repo.LocalOnly && m.detailedGitStatus != nil && len(m.detailedGitStatus.Files) > 0 {
		files := m.detailedGitStatus.Files
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" ┌─ Changed Files " + strings.Repeat("─", max(innerW-16, 2)) + "┐\n"))

		// Window the file list so the cursor row is always visible. The cursor
		// wraps at both ends (see advanced_update.go), so without this scroll the
		// highlighted row could sit off-screen once a repo had more changed files
		// than the pane height.
		const fileWindow = 6
		start := 0
		if m.fileCursor >= fileWindow {
			start = m.fileCursor - fileWindow + 1
		}
		if maxStart := len(files) - fileWindow; start > maxStart {
			start = maxStart
		}
		if start < 0 {
			start = 0
		}
		end := start + fileWindow
		if end > len(files) {
			end = len(files)
		}

		for idx := start; idx < end; idx++ {
			f := files[idx]
			cursor := "  "
			rowStyle := lipgloss.NewStyle().Foreground(pal.Foreground)
			if idx == m.fileCursor {
				cursor = "▸ "
				rowStyle = lipgloss.NewStyle().Foreground(pal.Foreground).Bold(true)
			}
			statusColor := pal.Primary
			switch {
			case strings.Contains(f.Status, "?"):
				statusColor = pal.Highlight
			case f.Staged:
				statusColor = pal.Success
			default:
				statusColor = pal.Warning
			}
			badge := lipgloss.NewStyle().Foreground(statusColor).Bold(true).Render(f.Status)
			path := truncPath(f.Path, innerW-14)
			b.WriteString(fmt.Sprintf(" │ %s%s %s\n", cursor, badge, rowStyle.Render(path)))
		}

		if len(files) > fileWindow {
			b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).
				Render(fmt.Sprintf(" │  [%d-%d/%d]  ←/→ move\n", start+1, end, len(files))))
		}
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).
			Render(" │  [S] stage · [U] unstage · [D] discard · [h] hunks · [Ctrl+Y] history · [Ctrl+B] blame · [Ctrl+U] unstage all\n"))
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" └" + strings.Repeat("─", innerW) + "┘\n\n"))
	}

	// PRs section if GitHub
	if !repo.LocalOnly {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" ┌─ Pull Requests " + strings.Repeat("─", max(innerW-17, 2)) + "┐\n"))
		if !repo.OpenPRsLoaded {
			b.WriteString(" │  Loading pull requests...\n")
		} else if len(repo.OpenPRList) == 0 {
			b.WriteString(" │  No open pull requests\n")
		} else {
			limit := 4
			for idx, pr := range repo.OpenPRList {
				if idx >= limit {
					b.WriteString(fmt.Sprintf(" │  ... and %d more PR(s)\n", len(repo.OpenPRList)-limit))
					break
				}
				b.WriteString(fmt.Sprintf(" │  #%-3d %s\n", pr.Number, trunc(pr.Title, innerW-12)))
			}
		}
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" └" + strings.Repeat("─", innerW) + "┘\n\n"))
	}

	// Action buttons (responsive layout)
	if repo.LocalOnly {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Highlight).Render(" ⚡ Git Actions:\n"))

		btnStage := lipgloss.NewStyle().Bold(true).Background(pal.Secondary).Foreground(pal.Foreground).Padding(0, 1).Render("[a] Stage All")
		btnCommit := lipgloss.NewStyle().Bold(true).Background(pal.Secondary).Foreground(pal.Foreground).Padding(0, 1).Render("[c] Commit")
		btnPublish := lipgloss.NewStyle().Bold(true).Background(pal.Highlight).Foreground(pal.Background).Padding(0, 1).Render("[n] Create & Push Repo")
		btnPush := lipgloss.NewStyle().Bold(true).Background(pal.Primary).Foreground(pal.Background).Padding(0, 1).Render("[P] Push")
		btnPull := lipgloss.NewStyle().Bold(true).Background(pal.Primary).Foreground(pal.Background).Padding(0, 1).Render("[F] Pull")
		btnDiff := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[d] Diff")
		btnLog := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[g] Log")
		btnBrowse := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[b] Browse GUI")
		btnOpen := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[o] Open Folder")
		btnExplore := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[f / Enter] Files")
		btnExp := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[e] GUI Explorer")
		btnTerm := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[p] Terminal")

		if innerW < 60 {
			// Very narrow: stack all buttons vertically
			b.WriteString("   " + btnStage + "\n")
			b.WriteString("   " + btnCommit + "\n")
			b.WriteString("   " + btnPublish + "\n\n")
			b.WriteString("   " + btnPush + "\n")
			b.WriteString("   " + btnPull + "\n")
			b.WriteString("   " + btnDiff + "\n")
			b.WriteString("   " + btnLog + "\n\n")
			b.WriteString("   " + btnBrowse + "\n")
			b.WriteString("   " + btnOpen + "\n")
			b.WriteString("   " + btnExplore + "\n")
			b.WriteString("   " + btnExp + "\n")
			b.WriteString("   " + btnTerm + "\n")
		} else if innerW < 90 {
			// Narrow: group in 2-3 columns
			b.WriteString("   " + btnStage + "  " + btnCommit + "  " + btnPublish + "\n\n")
			b.WriteString("   " + btnPush + "  " + btnPull + "  " + btnDiff + "  " + btnLog + "\n\n")
			b.WriteString("   " + btnBrowse + "  " + btnOpen + "  " + btnExplore + "  " + btnExp + "  " + btnTerm + "\n")
		} else {
			// Wide: original 3-row layout
			b.WriteString("   " + btnStage + "  " + btnCommit + "  " + btnPublish + "\n\n")
			b.WriteString("   " + btnPush + "  " + btnPull + "  " + btnDiff + "  " + btnLog + "\n\n")
			b.WriteString("   " + btnBrowse + "  " + btnOpen + "  " + btnExplore + "  " + btnExp + "  " + btnTerm + "\n")
		}
	}
	// GitHub (non-local) repos have no on-disk path, so file/terminal actions
	// are intentionally omitted rather than rendered as silent no-ops.

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Border).
		Padding(0, 1).
		Width(detailWidth).
		Height(totalHeight).
		Render(b.String())
}

func (m DashboardModel) renderExplorerView(pal theme.Palette, totalWidth, totalHeight int) string {
	exp := m.activeExplorer
	if exp == nil {
		return "No active explorer"
	}

	leftColWidth := (totalWidth * 42) / 100
	if leftColWidth < 36 {
		leftColWidth = 36
	}
	if leftColWidth > 52 {
		leftColWidth = 52
	}
	rightColWidth := totalWidth - leftColWidth - 4
	if rightColWidth < 40 {
		rightColWidth = 40
	}

	maxListRows := totalHeight - 5
	if maxListRows < 6 {
		maxListRows = 6
	}

	// 1. Left Column: Directory & File Listing
	var left strings.Builder
	currentRel := exp.RelativeCurrentPath()
	left.WriteString(lipgloss.NewStyle().
		Bold(true).
		Foreground(pal.Primary).
		Render(fmt.Sprintf(" 📂 %s", currentRel)) + " " +
		lipgloss.NewStyle().Foreground(pal.Muted).Render(fmt.Sprintf("(%d items)\n", len(exp.Entries))))

	left.WriteString(lipgloss.NewStyle().
		Foreground(pal.Muted).
		Render(fmt.Sprintf("   %-22s %-5s %7s\n", "NAME", "GIT", "SIZE")))
	left.WriteString(lipgloss.NewStyle().
		Foreground(pal.Border).
		Render("  " + strings.Repeat("─", leftColWidth-6) + "\n"))

	if len(exp.Entries) == 0 {
		left.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render("\n   (Empty directory)\n"))
	} else {
		startIdx := 0
		if exp.Selected >= maxListRows {
			startIdx = exp.Selected - maxListRows + 1
		}
		endIdx := startIdx + maxListRows
		if endIdx > len(exp.Entries) {
			endIdx = len(exp.Entries)
		}

		for i := startIdx; i < endIdx; i++ {
			entry := exp.Entries[i]

			icon := "📄 "
			if entry.IsDir {
				icon = "📁 "
			} else {
				switch entry.Ext {
				case ".go":
					icon = "🔷 "
				case ".md", ".txt":
					icon = "📝 "
				case ".json", ".yaml", ".yml", ".toml":
					icon = "⚙️  "
				case ".py":
					icon = "🐍 "
				case ".bat", ".ps1", ".cmd", ".sh":
					icon = "⚡ "
				case ".exe", ".dll":
					icon = "📦 "
				case ".png", ".jpg", ".ico", ".svg":
					icon = "🖼️  "
				}
			}

			gitBadge := "   "
			gitColor := pal.Muted
			if entry.GitStatus != "" {
				switch entry.GitStatus {
				case "M":
					gitBadge = "[M]"
					gitColor = pal.Warning
				case "?":
					gitBadge = "[?]"
					gitColor = pal.Accent
				case "A":
					gitBadge = "[A]"
					gitColor = pal.Success
				case "D":
					gitBadge = "[D]"
					gitColor = pal.Danger
				default:
					gitBadge = fmt.Sprintf("[%s]", entry.GitStatus)
					gitColor = pal.Primary
				}
			}

			sizeStr := "[DIR]"
			if !entry.IsDir {
				sizeStr = explorer.HumanSize(uint64(entry.Size))
			}

			nameWidth := leftColWidth - 22
			if nameWidth < 10 {
				nameWidth = 10
			}
			displayName := trunc(entry.Name, nameWidth)

			if i == exp.Selected {
				rowText := fmt.Sprintf(" ▸ %s%-*s %s %7s", icon, nameWidth, displayName, gitBadge, sizeStr)
				left.WriteString(lipgloss.NewStyle().
					Background(pal.Secondary).
					Foreground(pal.Foreground).
					Bold(true).
					Render(rowText) + "\n")
			} else {
				left.WriteString(fmt.Sprintf("   %s%-*s ", icon, nameWidth, displayName))
				left.WriteString(lipgloss.NewStyle().Foreground(gitColor).Render(fmt.Sprintf("%-3s ", gitBadge)))
				left.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render(fmt.Sprintf("%7s\n", sizeStr)))
			}
		}

		if len(exp.Entries) > maxListRows {
			left.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).
				Render(fmt.Sprintf("\n  Showing %d-%d of %d items", startIdx+1, endIdx, len(exp.Entries))))
		}
	}

	leftStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Border).
		Width(leftColWidth).
		Height(totalHeight).
		Padding(0, 1).
		Render(left.String())

	// 2. Right Column: File Preview / Directory Details
	var right strings.Builder
	innerW := rightColWidth - 6
	if innerW < 30 {
		innerW = 30
	}

	if exp.Selected >= 0 && exp.Selected < len(exp.Entries) {
		selectedEntry := exp.Entries[exp.Selected]

		if selectedEntry.IsDir {
			right.WriteString(lipgloss.NewStyle().
				Bold(true).
				Foreground(pal.Primary).
				Render(fmt.Sprintf(" 📁 %s\n\n", selectedEntry.Name)))

			right.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" ┌─ Directory Details " + strings.Repeat("─", max(innerW-21, 2)) + "┐\n"))
			right.WriteString(fmt.Sprintf(" │  Full Path : %s\n", lipgloss.NewStyle().Foreground(pal.Foreground).Render(truncPath(selectedEntry.Path, innerW-14))))
			right.WriteString(fmt.Sprintf(" │  Relative  : %s\n", lipgloss.NewStyle().Foreground(pal.Highlight).Render(truncPath(selectedEntry.RelPath, innerW-14))))
			right.WriteString(fmt.Sprintf(" │  Modified  : %s\n", selectedEntry.ModTime.Format("2006-01-02 15:04:05")))
			right.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render(" └" + strings.Repeat("─", innerW) + "┘\n\n"))

			right.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Highlight).Render(" ⚡ Folder Navigation & Actions:\n"))
			btnOpen := lipgloss.NewStyle().Bold(true).Background(pal.Secondary).Foreground(pal.Foreground).Padding(0, 1).Render("[Enter / →] Open")
			btnUp := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[Backspace / ←] Up")
			btnExp := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[e] Explorer")
			btnTerm := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[p] Terminal")
			btnCode := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[v] VS Code")
			btnBack := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[Esc] Back")

			right.WriteString("   " + btnOpen + "  " + btnUp + "\n")
			right.WriteString("   " + btnExp + "  " + btnTerm + "  " + btnCode + "  " + btnBack + "\n\n")

			if selectedEntry.GitStatus != "" {
				right.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Warning).
					Render(fmt.Sprintf("  ▲ Git Status: Folder has uncommitted changes [%s]\n", selectedEntry.GitStatus)))
			}
		} else {
			// File preview
			right.WriteString(lipgloss.NewStyle().
				Bold(true).
				Foreground(pal.Primary).
				Render(fmt.Sprintf(" 📄 %s", selectedEntry.Name)))

			infoLine := fmt.Sprintf("  %s • %s",
				explorer.HumanSize(uint64(selectedEntry.Size)),
				selectedEntry.ModTime.Format("2006-01-02 15:04:05"))
			if selectedEntry.GitStatus != "" {
				infoLine += fmt.Sprintf(" • Git: [%s]", selectedEntry.GitStatus)
			}
			right.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render(infoLine + "\n\n"))

			btnExp := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[e] Explorer")
			btnTerm := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[p] Terminal")
			btnCode := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[v] VS Code")
			btnBack := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[Esc] Back")
			right.WriteString("  " + btnExp + "  " + btnTerm + "  " + btnCode + "  " + btnBack + "\n\n")

			right.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render("  Source Preview:\n"))

			if exp.PreviewError != "" {
				right.WriteString(lipgloss.NewStyle().Foreground(pal.Danger).Render(fmt.Sprintf("    ✖ Error: %s\n", exp.PreviewError)))
			} else if exp.PreviewText != "" {
				previewLines := strings.Split(exp.PreviewText, "\n")
				maxPreviewLines := totalHeight - 9
				if maxPreviewLines < 5 {
					maxPreviewLines = 5
				}
				if len(previewLines) > maxPreviewLines {
					previewLines = previewLines[:maxPreviewLines]
					previewLines = append(previewLines, lipgloss.NewStyle().Foreground(pal.Muted).Render("    ... (truncated) ..."))
				}
				for _, line := range previewLines {
					right.WriteString("  " + line + "\n")
				}
			} else {
				right.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render("    (Empty or binary file)\n"))
			}
		}
	} else {
		right.WriteString("\n  Select a file or directory to preview.\n")
	}

	rightStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Border).
		Width(rightColWidth).
		Height(totalHeight).
		Padding(0, 1).
		Render(right.String())

	return lipgloss.JoinHorizontal(lipgloss.Top, leftStyle, rightStyle)
}

func (m DashboardModel) renderSystemView(pal theme.Palette, totalWidth, totalHeight int) string {
	snap := m.sysSnapshot
	panelWidth := (totalWidth / 2) - 2
	if panelWidth < 38 {
		panelWidth = 38
	}

	// Left Box: CPU, Memory, Disk
	var left strings.Builder
	innerW := panelWidth - 6
	if innerW < 30 {
		innerW = 30
	}

	if snap.Simulated {
		left.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Warning).
			Render(" ⚠ Simulated metrics — real collectors are Windows-only\n\n"))
	}

	left.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).Render(" ⚡ CPU Performance\n"))
	barW := max(innerW-22, 12)
	cpuBar := renderProgressBar(snap.CPUPercent, barW, pal.Primary, pal.Border)
	left.WriteString(fmt.Sprintf("   Load : %5.1f%%  %s\n", snap.CPUPercent, cpuBar))
	spark := system.FormatSparkline(snap.CPUHistory, barW)
	left.WriteString(fmt.Sprintf("   Trend: %s\n\n", lipgloss.NewStyle().Foreground(pal.Accent).Bold(true).Render(spark)))

	left.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).Render(" 🧠 Memory (RAM)\n"))
	memBar := renderProgressBar(snap.Memory.UsedPercent, barW, pal.Secondary, pal.Border)
	left.WriteString(fmt.Sprintf("   Usage: %5.1f%%  %s\n", snap.Memory.UsedPercent, memBar))
	left.WriteString(fmt.Sprintf("   Pool : %.1f GB / %.1f GB  ", snap.Memory.UsedGB(), snap.Memory.TotalGB()))
	left.WriteString(lipgloss.NewStyle().Foreground(pal.Success).Bold(true).Render(fmt.Sprintf("(%.1f GB Free)\n\n", snap.Memory.FreeGB())))

	left.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).Render(" 💾 Storage Disks\n"))
	for _, d := range snap.Disks {
		dBar := renderProgressBar(d.UsedPercent, max(barW-6, 10), pal.Highlight, pal.Border)
		left.WriteString(fmt.Sprintf("   [%s] %5.1f%% %s %.0fGB/%.0fGB\n",
			d.DriveLetter, d.UsedPercent, dBar, d.UsedGB(), d.TotalGB()))
	}
	if len(snap.Disks) == 0 {
		left.WriteString("   No fixed disk partitions detected.\n")
	}

	leftStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Border).
		Width(panelWidth).
		Height(totalHeight).
		Padding(0, 1).
		Render(left.String())

	// Right Box: Process Table
	var right strings.Builder
	right.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).
		Render(fmt.Sprintf(" 📋 Active Processes (%d total • %d threads)\n", snap.ProcessCount, snap.ThreadCount)))

	right.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).
		Render(fmt.Sprintf("   %-7s %-7s %-8s %s\n", "PID", "PPID", "THREADS", "EXECUTABLE")))
	right.WriteString(lipgloss.NewStyle().Foreground(pal.Border).
		Render("  " + strings.Repeat("─", panelWidth-6) + "\n"))

	maxProcs := totalHeight - 5
	if maxProcs < 5 {
		maxProcs = 5
	}
	for idx, p := range snap.TopProcesses {
		if idx >= maxProcs {
			break
		}
		pName := trunc(p.Name, panelWidth-28)
		right.WriteString(fmt.Sprintf("   %-7d %-7d %-8d %s\n", p.PID, p.PPID, p.ThreadCount, pName))
	}

	rightStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Border).
		Width(panelWidth).
		Height(totalHeight).
		Padding(0, 1).
		Render(right.String())

	return lipgloss.JoinHorizontal(lipgloss.Top, leftStyle, rightStyle)
}

func (m DashboardModel) renderPluginsView(pal theme.Palette, totalWidth, totalHeight int) string {
	listWidth := (totalWidth / 2) - 2
	if listWidth < 38 {
		listWidth = 38
	}

	// Left Box: Plugin List
	var left strings.Builder
	left.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).
		Render(fmt.Sprintf(" 🔌 Plugins (%d)\n", len(m.plugins))))

	left.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).
		Render(fmt.Sprintf("   %-18s %-10s %s\n", "NAME", "STATUS", "PID")))
	left.WriteString(lipgloss.NewStyle().Foreground(pal.Border).
		Render("  " + strings.Repeat("─", listWidth-6) + "\n"))

	if len(m.plugins) == 0 {
		left.WriteString("\n  No plugins found.\n  Place .exe, .py, .js, .bat, or .cmd in ./plugins or %AppData%\\Dashboard\\plugins\n")
	} else {
		for i, p := range m.plugins {
			statusPill := lipgloss.NewStyle().Foreground(pal.Muted).Render("○ Stopped")
			switch p.Status {
			case plugin.StatusRunning:
				statusPill = lipgloss.NewStyle().Foreground(pal.Success).Bold(true).Render("● Running")
			case plugin.StatusStarting:
				statusPill = lipgloss.NewStyle().Foreground(pal.Warning).Render("◌ Starting")
			case plugin.StatusError:
				statusPill = lipgloss.NewStyle().Foreground(pal.Danger).Render("✖ Error")
			}

			pName := trunc(p.Name, 18)
			pidStr := "-"
			if p.PID > 0 {
				pidStr = fmt.Sprintf("%d", p.PID)
			}

			if i == m.selectedPlugin {
				rowText := fmt.Sprintf(" ▸ %-18s %-10s %s", pName, statusPill, pidStr)
				left.WriteString(lipgloss.NewStyle().
					Background(pal.Secondary).
					Foreground(pal.Foreground).
					Bold(true).
					Render(rowText) + "\n")
			} else {
				left.WriteString(fmt.Sprintf("   %-18s %-10s %s\n", pName, statusPill, pidStr))
			}
		}
	}

	leftStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Border).
		Width(listWidth).
		Height(totalHeight).
		Padding(0, 1).
		Render(left.String())

	// Right Box: Plugin Inspector
	var right strings.Builder
	innerW := listWidth - 6
	if innerW < 30 {
		innerW = 30
	}

	if len(m.plugins) > 0 && m.selectedPlugin < len(m.plugins) {
		p := m.plugins[m.selectedPlugin]
		right.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).
			Render(fmt.Sprintf(" 🔌 %s\n\n", p.Name)))

		right.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).
			Render(" ┌─ Runtime Diagnostics " + strings.Repeat("─", max(innerW-23, 2)) + "┐\n"))
		right.WriteString(fmt.Sprintf(" │  Runtime : %s\n", p.Extension))
		right.WriteString(fmt.Sprintf(" │  Path    : %s\n", truncPath(p.Path, innerW-14)))
		if p.PipePath != "" {
			right.WriteString(fmt.Sprintf(" │  Pipe    : %s\n", trunc(p.PipePath, innerW-14)))
		}
		right.WriteString(fmt.Sprintf(" │  Status  : %s\n", p.Status))
		if p.ErrorMsg != "" {
			right.WriteString(fmt.Sprintf(" │  Error   : %s\n", lipgloss.NewStyle().Foreground(pal.Danger).Render(trunc(p.ErrorMsg, innerW-14))))
		}
		right.WriteString(fmt.Sprintf(" │  Updated : %s\n", p.LastUpdated.Format("15:04:05")))
		right.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).
			Render(" └" + strings.Repeat("─", innerW) + "┘\n\n"))

		btnStart := lipgloss.NewStyle().Bold(true).Background(pal.Secondary).Foreground(pal.Foreground).Padding(0, 1).Render("[s] Start")
		btnStop := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[x] Stop")
		btnReload := lipgloss.NewStyle().Bold(true).Background(pal.Border).Foreground(pal.Foreground).Padding(0, 1).Render("[r] Reload")
		right.WriteString("  " + btnStart + "  " + btnStop + "  " + btnReload + "\n\n")

		right.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Highlight).
			Render("  Rendered Plugin Output:\n"))
		if p.LastRender != "" {
			right.WriteString("  " + p.LastRender + "\n")
		} else {
			right.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).
				Render("  (Plugin idle or waiting for data. Press [s] to launch)\n"))
		}
	} else {
		right.WriteString("\n  Select a plugin from the list to view diagnostics.\n")
	}

	rightStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Border).
		Width(listWidth).
		Height(totalHeight).
		Padding(0, 1).
		Render(right.String())

	return lipgloss.JoinHorizontal(lipgloss.Top, leftStyle, rightStyle)
}

// fitKeyItems joins keycap hints with the given bullet separator, dropping
// hints from the end (with a trailing ellipsis) when the rendered line would
// exceed maxWidth. Keeps the shortcut bar from wrapping on narrower
// terminals, where the isNarrow branch alone still overflows.
func fitKeyItems(items []string, bullet string, maxWidth int, pal theme.Palette) string {
	if maxWidth <= 0 || len(items) == 0 {
		return ""
	}
	bulletW := lipgloss.Width(bullet)
	ellipsis := lipgloss.NewStyle().Foreground(pal.Muted).Render("  …")
	ellipsisW := lipgloss.Width(ellipsis)

	var b strings.Builder
	used := 0
	for i, it := range items {
		w := lipgloss.Width(it)
		add := w
		if i > 0 {
			add += bulletW
		}
		// Reserve room for the ellipsis so the last kept hint always fits.
		if i > 0 && used+add+ellipsisW > maxWidth {
			b.WriteString(ellipsis)
			break
		}
		if i > 0 {
			b.WriteString(bullet)
			used += bulletW
		}
		b.WriteString(it)
		used += w
	}
	return b.String()
}
