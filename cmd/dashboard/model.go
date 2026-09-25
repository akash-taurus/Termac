package main

// Package layout note: this file holds the Bubble Tea model, its types, and
// the initial model construction. Update lives in update.go, tea.Cmd
// constructors and message types in commands.go, status/view-switch helpers
// in helpers.go, rendering in views.go, and the CLI entry point in main.go.

import (
	"context"
	"os"
	"path/filepath"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"tui/pkg/auth"
	"tui/pkg/config"
	"tui/pkg/explorer"
	"tui/pkg/git"
	"tui/pkg/github"
	"tui/pkg/plugin"
	"tui/pkg/system"
)

var version = "1.1.0"

// RepoStatus represents the git status of a repository
type RepoStatus string

const (
	StatusClean      RepoStatus = "Clean"
	StatusModified   RepoStatus = "Modified"
	StatusHasChanges RepoStatus = "Has Changes"
	StatusLoading    RepoStatus = "Loading..."
	StatusError      RepoStatus = "Error"
)

// RepoDetail represents a repository with full details
type RepoDetail struct {
	Name          string
	Path          string
	Remote        string
	Branch        string
	LastMessage   string
	LastAuthor    string
	Status        RepoStatus
	HasError      bool
	ErrorMsg      string
	Stars         int
	Forks         int
	Language      string
	OpenPRs       int
	OpenPRList    []github.PullRequest
	OpenPRsLoaded bool
	LocalOnly     bool
}

// ViewMode defines the current active dashboard tab
type ViewMode int

const (
	ViewLocal ViewMode = iota
	ViewGitHub
	ViewSystem
	ViewPlugins
)

// statusLevel is the explicit severity of the current status-bar message.
// It is set where the message is produced instead of being guessed from the
// message text, which misclassified messages (e.g. "Loaded 0 repositories").
type statusLevel int

const (
	statusInfo statusLevel = iota
	statusSuccess
	statusWarn
	statusError
	statusLoading
)

// DashboardModel is the main Bubble Tea model
type DashboardModel struct {
	width    int
	height   int
	viewMode ViewMode
	repos    []RepoDetail
	selected int
	// Per-view lists: repos/selected always mirror the ACTIVE view.
	// Switching tabs stashes the outgoing list and restores the incoming
	// one, so GitHub login/fetch never wipes the local list (and vice
	// versa). A nil slot means that view has never been loaded.
	localRepos     []RepoDetail
	localSelected  int
	githubRepos    []RepoDetail
	githubSelected int
	detailWidth    int
	spinner        spinner.Model
	message        string
	statusLevel    statusLevel
	version        string
	userName       string
	scanning       bool
	token          *string

	// Theme
	themeIndex int

	// GitHub Authentication Modal
	authModalOpen bool
	tokenInput    textinput.Model
	deviceCode    *auth.DeviceCodeResponse
	authPolling   bool
	authError     string
	// authCancel stops an in-flight device-flow poll when the user closes
	// the modal; without it the poll keeps hitting GitHub until expiry.
	authCancel context.CancelFunc
	// authNeedsPAT is set when the modal was opened because repo
	// creation/push needs a classic PAT: device-flow login must then be
	// refused (it can never satisfy the requirement and would loop).
	authNeedsPAT bool
	// pendingPublish remembers an n/P publish intent routed through the
	// auth modal, so [Esc] can resume it with the current token instead
	// of dropping the user's action.
	pendingPublish bool

	// System Performance Monitor
	sysCollector *system.Collector
	sysSnapshot  system.SystemSnapshot

	// Plugin Manager
	pluginManager  *plugin.Manager
	plugins        []*plugin.PluginInstance
	selectedPlugin int

	// In-Repo Directory & File Explorer
	explorerMode   bool
	activeExplorer *explorer.Explorer

	// Open Local Folder Modal
	openFolderModalOpen bool
	folderInput         textinput.Model

	// Git Commit Modal
	commitModalOpen bool
	commitInput     textinput.Model

	// Detailed Git State for active repo
	detailedGitStatus *git.DetailedGitStatus
	gitDiffActive     bool
	gitDiffText       string
	gitLogActive      bool
	gitLogItems       []git.CommitLogItem
	gitLogCursor      int    // highlighted row in the log pane
	logDiffSha        string // commit whose diff is currently shown
	logActionStep     int    // Enter-cycle position for reset options

	// Create & Push to GitHub Modal
	publishModalOpen bool
	publishNameInput textinput.Model
	publishPrivate   bool

	// Advanced git features (staging, stash, branches, overlays, sync…)
	advancedModelFields
}

func initialModel() DashboardModel {
	s := spinner.New()
	s.Spinner = spinner.Dot

	ti := textinput.New()
	ti.Placeholder = "Paste GitHub PAT (ghp_...) or token here"
	ti.CharLimit = 255
	ti.Width = 50
	ti.EchoMode = textinput.EchoNormal

	fInput := textinput.New()
	fInput.Placeholder = "Enter or paste directory path (e.g. Z:\\CodeBase\\TUI or .)"
	fInput.CharLimit = 250
	fInput.Width = 56
	fInput.EchoMode = textinput.EchoNormal

	cInput := textinput.New()
	cInput.Placeholder = "Enter commit message (e.g. feat: add git actions)"
	cInput.CharLimit = 160
	cInput.Width = 56
	cInput.EchoMode = textinput.EchoNormal

	pubInput := textinput.New()
	pubInput.Placeholder = "repository-name"
	pubInput.CharLimit = 100
	pubInput.Width = 40
	pubInput.EchoMode = textinput.EchoNormal

	pluginsDir, _ := config.PluginsPath()
	if _, err := os.Stat(pluginsDir); os.IsNotExist(err) {
		if _, localErr := os.Stat("plugins"); localErr == nil {
			pluginsDir = "plugins"
		}
	}

	collector := system.NewCollector(40)
	snap := collector.TakeSnapshot()

	mgr := plugin.NewManager(pluginsDir)
	discovered, _ := mgr.DiscoverPlugins()

	// Check if already authenticated. A stored token is only adopted
	// when it live-validates: a definitively rejected token (revoked,
	// expired) must not linger in the model, otherwise every
	// token-guarded action (push, publish) proceeds with dead
	// credentials and fails late with confusing errors. When validation
	// itself errors (offline), the token is kept so the UI does not flap;
	// each authenticated action re-validates before doing any work.
	var existingToken *string
	var existingUser string
	if tok, err := auth.LoadToken(); err == nil && tok.AccessToken != "" {
		if valid, verr := auth.ValidateToken(tok); verr == nil && valid {
			existingToken = &tok.AccessToken
			client := github.NewClient(existingToken)
			if u, err := client.GetAuthenticatedUser(); err == nil {
				existingUser = u.Login
			}
		}
	}

	// Always pre-load current working directory so user has it immediately.
	// The row is added cheaply (no git calls on the UI thread); its git
	// details are filled in asynchronously by cwdDetailCmd via repoDetailMsg.
	var initialRepos []RepoDetail
	if cwd, err := os.Getwd(); err == nil {
		absCwd, _ := filepath.Abs(cwd)
		initialRepos = append(initialRepos, RepoDetail{
			Name:      filepath.Base(absCwd),
			Path:      absCwd,
			Branch:    "…",
			Status:    StatusLoading,
			Remote:    "local",
			LocalOnly: true,
		})
	}

	selectedIdx := -1
	if len(initialRepos) > 0 {
		selectedIdx = 0
	}

	return DashboardModel{
		version:          version,
		spinner:          s,
		viewMode:         ViewLocal,
		repos:            initialRepos,
		selected:         selectedIdx,
		localRepos:       initialRepos,
		localSelected:    selectedIdx,
		githubSelected:   0,
		themeIndex:       0,
		token:            existingToken,
		userName:         existingUser,
		tokenInput:       ti,
		folderInput:      fInput,
		commitInput:      cInput,
		publishNameInput: pubInput,
		publishPrivate:   false,
		sysCollector:     collector,
		sysSnapshot:      snap,
		pluginManager:    mgr,
		plugins:          discovered,
		selectedPlugin:   0,
		message:          "Welcome to Terminal Dashboard! Press [o] to open any local folder, [f] to explore, [a/c/P/F] for Git actions.",
	}
}

func (m DashboardModel) Init() tea.Cmd {
	var boot []tea.Cmd
	// Load the working directory's git details off the UI thread: on a
	// large repository the status/log/diff calls previously delayed the
	// first paint by hundreds of milliseconds.
	if len(m.repos) > 0 {
		boot = append(boot, repoDetailCommand(context.Background(), m.repos[0]))
	}
	return tea.Batch(append(boot,
		m.spinner.Tick,
		systemTickCmd(),
		tea.EnableBracketedPaste,
	)...)
}
