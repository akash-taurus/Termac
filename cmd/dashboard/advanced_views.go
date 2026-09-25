package main

// Rendering for the advanced git features: overlay panes (stash, branches,
// reflog, file history, blame, issues, conflicts, sync results), the generic
// confirm/prompt modals, and hunk-mode diff chrome.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"tui/pkg/theme"
)

// renderOverlay renders the active overlay pane, if any.
func (m DashboardModel) renderOverlay(pal theme.Palette, width, height int) string {
	o := m.overlay
	if o == nil {
		return ""
	}
	boxW := width - 10
	if boxW < 60 {
		boxW = 60
	}
	if boxW > 110 {
		boxW = 110
	}
	boxH := height - 6
	if boxH < 8 {
		boxH = 8
	}

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).
		Render(fmt.Sprintf(" %s\n", o.title)))
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Border).
		Render(strings.Repeat("─", boxW-2) + "\n"))

	visible := boxH - 4
	if visible < 3 {
		visible = 3
	}
	total := len(o.lines)
	start := 0
	if o.cursor >= visible {
		start = o.cursor - visible + 1
	}
	end := start + visible
	if end > total {
		end = total
	}

	for i := start; i < end; i++ {
		line := trunc(o.lines[i], boxW-6)
		if i == o.cursor {
			b.WriteString(lipgloss.NewStyle().Background(pal.Secondary).
				Foreground(pal.Foreground).Bold(true).Render(line) + "\n")
		} else {
			b.WriteString(lipgloss.NewStyle().Foreground(pal.Foreground).Render(line) + "\n")
		}
	}
	if total == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).Render("  (empty)\n"))
	}
	if total > visible {
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).
			Render(fmt.Sprintf("  [%d-%d/%d] j/k to move\n", start+1, end, total)))
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Highlight).
		Padding(0, 1).
		Width(boxW).
		Height(boxH).
		Render(b.String())
}

// renderConfirmModal renders the generic confirmation modal.
func (m DashboardModel) renderConfirmModal(pal theme.Palette) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Danger).
		Render(" ⚠️  Confirm\n\n"))
	b.WriteString(fmt.Sprintf("  %s\n\n", m.confirm.title))
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Highlight).Bold(true).
		Render("  [Y] Yes, do it   [N] / [Esc] Cancel\n"))

	return lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(pal.Danger).
		Padding(1, 2).
		Width(64).
		Render(b.String())
}

// renderPromptModal renders the generic input modal.
func (m DashboardModel) renderPromptModal(pal theme.Palette) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).
		Render(" " + m.prompt.title + "\n\n"))
	b.WriteString(fmt.Sprintf("  %s\n\n", m.promptInput.View()))
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Muted).
		Render("  [Enter] Confirm   [Esc] Cancel\n"))

	return lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(pal.Primary).
		Padding(1, 2).
		Width(68).
		Render(b.String())
}

// renderSyncOverlay shows per-repo results of the last batch sync.
func (m DashboardModel) renderSyncOverlay(pal theme.Palette, width, height int) string {
	boxW := width - 12
	if boxW < 60 {
		boxW = 60
	}
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(pal.Primary).
		Render(" 🔄 Batch Sync Results (pull --ff-only)\n"))
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Border).
		Render(strings.Repeat("─", boxW-2) + "\n"))

	for _, r := range m.syncResults {
		icon, color := "✔", pal.Success
		switch {
		case r.Err != nil:
			icon, color = "✖", pal.Danger
		case r.Skipped:
			icon, color = "○", pal.Muted
		}
		name := trunc(r.Name, 30)
		detail := r.Output
		if r.Err != nil {
			detail = firstLine(r.Err.Error())
		}
		if detail == "" || detail == "Already up to date." {
			detail = "up to date"
		}
		row := fmt.Sprintf(" %s %-30s %s", icon, name, trunc(detail, boxW-42))
		b.WriteString(lipgloss.NewStyle().Foreground(color).Render(row) + "\n")
	}
	b.WriteString("\n" + lipgloss.NewStyle().Foreground(pal.Muted).
		Render("  [Esc] Close"))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Primary).
		Padding(0, 1).
		Width(boxW).
		Height(height - 4).
		Render(b.String())
}

// renderHunkBar renders the hunk-mode indicator above the diff pane.
func (m DashboardModel) renderHunkBar(pal theme.Palette, width int) string {
	if !m.hunkMode || m.overlay == nil {
		return ""
	}
	total := len(m.overlay.hunks)
	pos := m.hunkCursor + 1
	if pos > total {
		pos = total
	}
	hint := lipgloss.NewStyle().Foreground(pal.Highlight).Bold(true).
		Render(fmt.Sprintf(" ⚡ HUNK MODE %d/%d — [n/p] select · [s] stage hunk · [Esc] exit", pos, total))
	pad := width - lipgloss.Width(hint) - 1
	if pad > 0 {
		return hint + strings.Repeat(" ", pad)
	}
	return hint
}

// advancedViewHook wraps the main body render to overlay advanced panes.
// Called from View() around the normal mainBody.
func (m DashboardModel) advancedViewHook(pal theme.Palette, width, height int, mainBody string) string {
	// Full-screen overlays win over everything.
	if m.overlay != nil && m.overlay.kind != overlayNone {
		return m.renderOverlay(pal, width, height)
	}
	if m.syncOverlay {
		return m.renderSyncOverlay(pal, width, height)
	}
	// Hunk bar rides above the diff pane.
	if m.hunkMode {
		return m.renderHunkBar(pal, width) + "\n" + mainBody
	}
	return mainBody
}

// advancedModalHook returns modal chrome when a confirm/prompt modal is
// open; View() checks this before the legacy modal chain.
func (m DashboardModel) advancedModalHook(pal theme.Palette) (string, bool) {
	if m.confirm != nil {
		return m.renderConfirmModal(pal), true
	}
	if m.prompt != nil {
		return m.renderPromptModal(pal), true
	}
	return "", false
}
