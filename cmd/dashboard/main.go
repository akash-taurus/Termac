package main

// CLI entry point: flag handling (-token/-logout/-version), terminal and
// config-directory initialization, signal handling, and program start.
// Also hosts the small shared render helpers (trunc/truncPath/progress bar).

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tui/pkg/auth"
	"tui/pkg/config"
	"tui/pkg/term"
)

func trunc(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(r[:maxLen])
	}
	return string(r[:maxLen-2]) + ".."
}

func truncPath(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	if maxLen <= 6 {
		return string(r[:maxLen])
	}
	return "..." + string(r[len(r)-(maxLen-3):])
}

func renderProgressBar(percent float64, width int, filledColor, emptyColor lipgloss.Color) string {
	if width <= 0 {
		width = 10
	}
	filledCount := int((percent / 100.0) * float64(width))
	if filledCount < 0 {
		filledCount = 0
	}
	if filledCount > width {
		filledCount = width
	}
	emptyCount := width - filledCount

	filledStr := strings.Repeat("█", filledCount)
	emptyStr := strings.Repeat("░", emptyCount)

	return lipgloss.NewStyle().Foreground(filledColor).Render(filledStr) +
		lipgloss.NewStyle().Foreground(emptyColor).Render(emptyStr)
}

func main() {
	// Command-line flag parsing
	tokenFlag := flag.String("token", "", "Authenticate using a GitHub Personal Access Token (PAT)")
	logoutFlag := flag.Bool("logout", false, "Clear saved GitHub authentication token")
	versionFlag := flag.Bool("version", false, "Print dashboard version and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("Terminal Dashboard v%s (Windows Native)\n", version)
		return
	}

	if *logoutFlag {
		if err := auth.DeleteToken(); err != nil {
			fmt.Fprintf(os.Stderr, "Logout error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Successfully logged out from GitHub. Stored token deleted.")
		return
	}

	if *tokenFlag != "" {
		tok, user, err := auth.SaveTokenString(*tokenFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "GitHub authentication failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Successfully authenticated as @%s! Token saved to %s.\n", user, filepath.Join(os.Getenv("APPDATA"), "Dashboard", "github_token.json"))
		_ = tok
		return
	}

	// Initialize Windows terminal features
	if err := term.EnableWindowsVirtualTerminal(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}
	if err := term.EnableMouseInput(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}

	// Initialize config directory
	if _, err := config.EnsureDirectory(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}

	// Start the TUI
	p := tea.NewProgram(initialModel(), tea.WithAltScreen(), tea.WithMouseAllMotion())
	defer p.ReleaseTerminal()

	// Handle SIGINT/SIGTERM for cleanup
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		_ = term.DisableMouseInput()
		p.Kill()
	}()

	// Run the program
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}
