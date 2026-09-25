package main

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Pasting (or typing) a token containing shortcut letters c/d must land
// verbatim in the Token field and must not trigger env-check/device-flow.
// Regression test: the auth modal previously intercepted c/C/d/D on every
// keypress, mangling pastes like ghp_Cd07... into truncated tokens.
func TestAuthModalPasteKeepsShortcutLetters(t *testing.T) {
	ti := textinput.New()
	ti.Focus()
	m := DashboardModel{
		authModalOpen: true,
		tokenInput:    ti,
		viewMode:      ViewLocal,
		selected:      -1,
	}
	pasted := "ghp_Cd07J1tK2xYzDc9"
	for _, r := range pasted {
		model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = model.(DashboardModel)
		if m.authError != "" {
			t.Fatalf("key %q set authError: %s", r, m.authError)
		}
		if m.authPolling || m.deviceCode != nil {
			t.Fatalf("key %q triggered device flow", r)
		}
		if m.message == "Requesting GitHub device authorization code..." {
			t.Fatalf("key %q triggered device flow request", r)
		}
	}
	if got := m.tokenInput.Value(); got != pasted {
		t.Fatalf("paste mangled: got %q want %q", got, pasted)
	}
}

// A bracketed paste arrives as a single flagged message and must go
// straight to the input.
func TestAuthModalBracketedPaste(t *testing.T) {
	ti := textinput.New()
	ti.Focus()
	m := DashboardModel{
		authModalOpen: true,
		tokenInput:    ti,
		viewMode:      ViewLocal,
		selected:      -1,
	}
	pasted := "ghp_Cd07J1tK2xYzDc9"
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true})
	m = model.(DashboardModel)
	if got := m.tokenInput.Value(); got != pasted {
		t.Fatalf("bracketed paste mangled: got %q want %q", got, pasted)
	}
	if m.authPolling || m.deviceCode != nil {
		t.Fatal("bracketed paste triggered device flow")
	}
}

// Auth-modal actions are Ctrl chords so every printable character reaches the
// token field. A plain 'd' must be typed, never treated as device flow.
func TestAuthModalActionsRequireCtrl(t *testing.T) {
	ti := textinput.New()
	ti.Focus()
	m := DashboardModel{
		authModalOpen: true,
		tokenInput:    ti,
		viewMode:      ViewLocal,
		selected:      -1,
	}
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = model.(DashboardModel)
	if m.tokenInput.Value() != "d" {
		t.Fatalf("plain 'd' should reach the input, got %q", m.tokenInput.Value())
	}
	if m.message == "Requesting GitHub device authorization code..." {
		t.Fatal("plain 'd' triggered device flow")
	}

	// Ctrl+D on an empty field triggers device flow.
	ti2 := textinput.New()
	ti2.Focus()
	m2 := DashboardModel{authModalOpen: true, tokenInput: ti2, viewMode: ViewLocal, selected: -1}
	model, _ = m2.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m2 = model.(DashboardModel)
	if m2.message != "Requesting GitHub device authorization code..." {
		t.Fatalf("Ctrl+D did not trigger device flow, message=%q", m2.message)
	}
}
