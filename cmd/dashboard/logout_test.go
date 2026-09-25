package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// The nav bar button is its last element, so its hit region must end exactly
// at the window's right edge on the nav bar row.
func TestAuthButtonHitBounds(t *testing.T) {
	m := DashboardModel{width: 80, userName: "me"}
	w := authButtonWidth(true)
	if !m.authButtonHit(80-w, navBarRow) {
		t.Fatal("button start column should hit")
	}
	if !m.authButtonHit(79, navBarRow) {
		t.Fatal("window right edge should hit")
	}
	if m.authButtonHit(80-w-1, navBarRow) {
		t.Fatal("one column left of the button must miss")
	}
	if m.authButtonHit(79, 0) {
		t.Fatal("the title bar (row 0) is not the nav bar and must miss")
	}
	if m.authButtonHit(79, navBarRow+1) {
		t.Fatal("only the nav bar row hosts the button")
	}
}

// A left-click on the Logout button must clear the whole GitHub session and
// schedule deletion of the stored token.
func TestNavBarLogoutButtonClickClearsSession(t *testing.T) {
	m := DashboardModel{
		width:          120,
		viewMode:       ViewLocal,
		selected:       -1,
		token:          strPtr("ghp_secret"),
		userName:       "octocat",
		githubRepos:    []RepoDetail{{Name: "o/r"}},
		githubSelected: 4,
		authNeedsPAT:   true,
		pendingPublish: true,
	}
	model, cmd := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 119, Y: navBarRow})
	got := model.(DashboardModel)
	if cmd == nil {
		t.Fatal("clicking Logout should return the token-deletion command")
	}
	if got.userName != "" || got.token != nil {
		t.Fatalf("session not cleared: user=%q token=%v", got.userName, got.token)
	}
	if got.githubRepos != nil || got.githubSelected != 0 || got.authNeedsPAT || got.pendingPublish {
		t.Fatalf("logout left stale state: %+v", got)
	}
}

// Clicks that miss the button (wrong column, or a motion event) are no-ops.
func TestNavBarButtonClickAwayIsIgnored(t *testing.T) {
	m := DashboardModel{width: 120, viewMode: ViewLocal, selected: -1, userName: "octocat", token: strPtr("ghp_x")}
	if _, cmd := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 5, Y: navBarRow}); cmd != nil {
		t.Fatal("click away from the button must be ignored")
	}
	if _, cmd := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 119, Y: 0}); cmd != nil {
		t.Fatal("a click on the title bar must be ignored")
	}
	model, cmd := m.Update(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonNone, X: 119, Y: navBarRow})
	if cmd != nil {
		t.Fatal("mouse motion must not trigger the button")
	}
	if model.(DashboardModel).userName != "octocat" {
		t.Fatal("motion should not change session state")
	}
}

// When logged out the same button reads "Login" and opens the auth modal.
func TestNavBarLoginButtonClickOpensModal(t *testing.T) {
	m := DashboardModel{width: 100, viewMode: ViewLocal, selected: -1, tokenInput: textinput.New()}
	model, cmd := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 99, Y: navBarRow})
	got := model.(DashboardModel)
	if !got.authModalOpen {
		t.Fatal("clicking Login when logged out should open the auth modal")
	}
	if cmd == nil {
		t.Fatal("opening the auth modal should focus the input")
	}
}

// The nav bar must actually render the button in both auth states.
func TestViewRendersAuthButton(t *testing.T) {
	loggedOut := DashboardModel{width: 120, height: 40, viewMode: ViewLocal, selected: -1, tokenInput: textinput.New()}
	if !strings.Contains(loggedOut.View(), authButtonLabel(false)) {
		t.Fatalf("nav bar missing the Login button: %q", authButtonLabel(false))
	}
	signedIn := loggedOut
	signedIn.userName = "octocat"
	signedIn.token = strPtr("ghp_x")
	if !strings.Contains(signedIn.View(), authButtonLabel(true)) {
		t.Fatalf("nav bar missing the Logout button: %q", authButtonLabel(true))
	}
}

// The [u] shortcut and the button share one implementation.
func TestLogoutKeyClearsSession(t *testing.T) {
	m := DashboardModel{viewMode: ViewLocal, selected: -1, token: strPtr("ghp_x"), userName: "octocat"}
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	got := model.(DashboardModel)
	if cmd == nil {
		t.Fatal("[u] should return the logout command")
	}
	if got.userName != "" || got.token != nil {
		t.Fatalf("[u] did not clear the session: user=%q token=%v", got.userName, got.token)
	}
}
