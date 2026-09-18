package theme

import (
	"github.com/charmbracelet/lipgloss"
)

// Palette defines the colors used throughout the dashboard TUI
type Palette struct {
	Name       string
	Primary    lipgloss.Color
	Secondary  lipgloss.Color
	Accent     lipgloss.Color
	Background lipgloss.Color
	Foreground lipgloss.Color
	Muted      lipgloss.Color
	Success    lipgloss.Color
	Warning    lipgloss.Color
	Danger     lipgloss.Color
	Border     lipgloss.Color
	Highlight  lipgloss.Color
}

var (
	// Default Cyan theme
	Default = Palette{
		Name:       "Cyan Cyber",
		Primary:    lipgloss.Color("#00e5ff"), // Vibrant Cyan
		Secondary:  lipgloss.Color("#7c3aed"), // Neon Violet
		Accent:     lipgloss.Color("#f43f5e"), // Electric Rose
		Background: lipgloss.Color("#0b0f19"), // Deep Obsidian
		Foreground: lipgloss.Color("#f8fafc"), // Bright Slate
		Muted:      lipgloss.Color("#64748b"), // Slate Gray
		Success:    lipgloss.Color("#10b981"), // Emerald Green
		Warning:    lipgloss.Color("#f59e0b"), // Vivid Amber
		Danger:     lipgloss.Color("#ef4444"), // Crimson Red
		Border:     lipgloss.Color("#1e293b"), // Slate Border
		Highlight:  lipgloss.Color("#38bdf8"), // Sky Blue
	}

	// Catppuccin Mocha
	Catppuccin = Palette{
		Name:       "Catppuccin Mocha",
		Primary:    lipgloss.Color("#89b4fa"), // Blue
		Secondary:  lipgloss.Color("#b4befe"), // Lavender
		Accent:     lipgloss.Color("#cba6f7"), // Mauve
		Background: lipgloss.Color("#1e1e2e"), // Base
		Foreground: lipgloss.Color("#cdd6f4"), // Text
		Muted:      lipgloss.Color("#6c7086"), // Overlay0
		Success:    lipgloss.Color("#a6e3a1"), // Green
		Warning:    lipgloss.Color("#f9e2af"), // Yellow
		Danger:     lipgloss.Color("#f38ba8"), // Red
		Border:     lipgloss.Color("#45475a"), // Surface1
		Highlight:  lipgloss.Color("#f5c2e7"), // Pink
	}

	// Nord
	Nord = Palette{
		Name:       "Nord Frost",
		Primary:    lipgloss.Color("#88c0d0"), // Frost Cyan
		Secondary:  lipgloss.Color("#81a1c1"), // Frost Blue
		Accent:     lipgloss.Color("#b48ead"), // Aurora Purple
		Background: lipgloss.Color("#2e3440"), // Polar Night
		Foreground: lipgloss.Color("#eceff4"), // Snow Storm
		Muted:      lipgloss.Color("#4c566a"), // Polar Night bright
		Success:    lipgloss.Color("#a3be8c"), // Aurora Green
		Warning:    lipgloss.Color("#ebcb8b"), // Aurora Yellow
		Danger:     lipgloss.Color("#bf616a"), // Aurora Red
		Border:     lipgloss.Color("#3b4252"), // Polar Night lighter
		Highlight:  lipgloss.Color("#8fbcbb"), // Frost Mint
	}

	// Tokyo Night
	TokyoNight = Palette{
		Name:       "Tokyo Night",
		Primary:    lipgloss.Color("#7aa2f7"), // Blue
		Secondary:  lipgloss.Color("#7dcfff"), // Cyan
		Accent:     lipgloss.Color("#bb9af7"), // Purple
		Background: lipgloss.Color("#1a1b26"), // Storm BG
		Foreground: lipgloss.Color("#c0caf5"), // Text
		Muted:      lipgloss.Color("#565f89"), // Comment
		Success:    lipgloss.Color("#9ece6a"), // Green
		Warning:    lipgloss.Color("#e0af68"), // Orange
		Danger:     lipgloss.Color("#f7768e"), // Red
		Border:     lipgloss.Color("#292e42"), // Surface
		Highlight:  lipgloss.Color("#2ac3de"), // Teal
	}

	// Gruvbox Dark
	Gruvbox = Palette{
		Name:       "Gruvbox Dark",
		Primary:    lipgloss.Color("#fabd2f"), // Yellow
		Secondary:  lipgloss.Color("#83a598"), // Blue
		Accent:     lipgloss.Color("#d3869b"), // Purple
		Background: lipgloss.Color("#282828"), // Dark BG
		Foreground: lipgloss.Color("#ebdbb2"), // Light FG
		Muted:      lipgloss.Color("#928374"), // Gray
		Success:    lipgloss.Color("#b8bb26"), // Green
		Warning:    lipgloss.Color("#fe8019"), // Orange
		Danger:     lipgloss.Color("#fb4934"), // Red
		Border:     lipgloss.Color("#504945"), // Dark gray border
		Highlight:  lipgloss.Color("#8ec07c"), // Aqua
	}

	AvailableThemes = []Palette{
		Default,
		Catppuccin,
		Nord,
		TokyoNight,
		Gruvbox,
	}
)

// GetThemeByIndex retrieves theme cyclically
func GetThemeByIndex(index int) Palette {
	if len(AvailableThemes) == 0 {
		return Default
	}
	idx := index % len(AvailableThemes)
	if idx < 0 {
		idx += len(AvailableThemes)
	}
	return AvailableThemes[idx]
}
