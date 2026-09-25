package main

// TUI flow for creating a GitHub pull request from the current branch of a
// local repository:
//
//	[ctrl+p] → preflight check (auth, remote, pushed branch, divergence)
//	         → title prompt → base prompt → CreatePullRequest (async)
//
// The preflight runs in a tea.Cmd (UI never blocks) and reports exactly
// what is missing: token, HTTPS remote, unpushed commits, or a branch that
// has diverged from its upstream.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"tui/pkg/git"
	"tui/pkg/github"
)

// ---------- Model fields ----------

// advancedModelFields additions for the PR-create flow live in
// prCreateModelFields (embedded by DashboardModel in model.go).
type prCreateModelFields struct {
	// prCreate carries the in-flight create-PR context while the user is
	// typing title/base. nil when the flow is not active.
	prCreate *prCreateContext
}

// prCreateContext holds everything needed between preflight and submission.
type prCreateContext struct {
	repoPath string
	branch   string // head branch (current)
	base     string // proposed default base
	title    string // set after the title prompt
}

// ---------- Message types ----------

type prPreflightMsg struct {
	RepoPath   string
	Branch     string
	Base       string
	Upstream   string
	Ahead      int
	Behind     int
	HasRemote  bool
	HasCommits bool
	RemoteURL  string
	Err        error
}

type prCreatedMsg struct {
	Number int
	URL    string
	Title  string
	Err    error
}

// ---------- Commands ----------

// prPreflightCmd validates everything a PR needs before asking the user for
// a title. Anything missing is reported through the msg fields.
func prPreflightCmd(repoPath string) tea.Cmd {
	return func() tea.Msg {
		msg := prPreflightMsg{RepoPath: repoPath}
		msg.Branch = git.GitBranchName(repoPath)
		if msg.Branch == "" || msg.Branch == "HEAD" {
			msg.Err = fmt.Errorf("detached HEAD — check out a branch first")
			return msg
		}

		remotes, err := git.GitListRemotes(repoPath)
		if err != nil || len(remotes) == 0 {
			msg.Err = fmt.Errorf("no remote configured — publish with [n] first")
			return msg
		}
		msg.HasRemote = true
		msg.RemoteURL = remotes[0].URL

		// The remote must look like a host/owner/repo URL for the API call.
		if _, _, ok := ownerRepoFromRemote(msg.RemoteURL); !ok {
			msg.Err = fmt.Errorf("cannot parse owner/repo from remote %s", msg.RemoteURL)
			return msg
		}
		msg.Base = githubDefaultBranch(repoPath)

		// Upstream + ahead/behind from the branch header parser.
		st, err := git.GitStatusDetailed(repoPath)
		if err == nil {
			msg.Upstream = st.Upstream
			msg.Ahead, msg.Behind = st.Ahead, st.Behind
		}

		// The remote must know the branch for a PR to be possible.
		out, err := git.RunGitCapture(repoPath, "ls-remote", "--heads", "origin", msg.Branch)
		hasCommits := err == nil && strings.TrimSpace(string(out)) != ""
		msg.HasCommits = hasCommits
		if !hasCommits {
			return msg // caller prompts to push
		}

		// Propose the default base: prefer the remote's default branch.
		return msg
	}
}

// prCreateCmd submits the PR.
func prCreateCmd(token *string, owner, repo, title, head, base, body string) tea.Cmd {
	return func() tea.Msg {
		if token == nil || *token == "" {
			return prCreatedMsg{Err: fmt.Errorf("GitHub login required — press [l]")}
		}
		client := github.NewClient(token)
		pr, err := client.CreatePullRequest(owner, repo, title, head, base, body)
		if err != nil {
			return prCreatedMsg{Title: title, Err: err}
		}
		return prCreatedMsg{Number: pr.Number, URL: pr.URL, Title: title}
	}
}

// ---------- helpers ----------

// ownerRepoFromRemote extracts owner/repo from an https or ssh remote URL.
func ownerRepoFromRemote(raw string) (owner, repo string, ok bool) {
	s := strings.TrimSpace(raw)
	s = strings.TrimSuffix(s, ".git")
	switch {
	case strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://"):
		rest := s[strings.Index(s, "://")+3:]
		parts := strings.SplitN(rest, "/", 3)
		if len(parts) == 3 {
			return parts[1], parts[2], true
		}
	case strings.HasPrefix(s, "git@"):
		rest := strings.TrimPrefix(s, "git@")
		rest = strings.Replace(rest, ":", "/", 1)
		parts := strings.SplitN(rest, "/", 3)
		if len(parts) == 3 {
			return parts[1], parts[2], true
		}
	}
	return "", "", false
}

// githubDefaultBranch reads the repository's default branch via a lightweight
// ls-remote against origin's HEAD symref (no API call, no token needed).
//
// repoPath is required: the query must run inside the repository whose remote
// is being inspected. Running it with an empty dir resolved "origin" of the
// dashboard process's own working directory instead, so the PR base was
// silently taken from an unrelated repository.
func githubDefaultBranch(repoPath string) string {
	// git ls-remote --symref origin HEAD resolves to
	// "ref: refs/heads/main\tHEAD" — parse the branch out.
	out, err := git.RunGitCapture(repoPath, "ls-remote", "--symref", "origin", "HEAD")
	if err != nil {
		return "main" // offline or bare: propose main
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasSuffix(strings.TrimSpace(line), "HEAD") && strings.HasPrefix(line, "ref:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return strings.TrimPrefix(fields[1], "refs/heads/")
			}
		}
	}
	return "main"
}

// prBodyFromCommits builds a PR body listing the commits between base and head.
func prBodyFromCommits(repoPath, base, head string) string {
	commits, err := git.RunGitCapture(repoPath, "log", "--reverse", "--pretty=format:- %s", base+".."+head)
	if err != nil || strings.TrimSpace(string(commits)) == "" {
		return ""
	}
	return "## Commits\n" + strings.TrimSpace(string(commits))
}

// pushForPRCmd pushes the current branch with the stored token so the PR
// has something to point at.
func pushForPRCmd(token *string, repoPath, branch string) tea.Cmd {
	return func() tea.Msg {
		if token == nil || *token == "" {
			return historyActionMsg{Action: "pr-push", Err: fmt.Errorf("GitHub login required — press [l]"), Repo: repoPath}
		}
		out, err := git.GitPushUpstreamAuth(repoPath, "origin", branch, *token)
		return historyActionMsg{Action: "pr-push", Output: out, Err: err, Repo: repoPath}
	}
}

// ---------- Update routing ----------

// handlePRCreateMsg processes the PR-create flow messages. Returns
// (cmd, handled).
func (m *DashboardModel) handlePRCreateMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {

	case prPreflightMsg:
		if msg.Err != nil {
			m.setStatusf(statusError, "PR preflight: %v", msg.Err)
			m.prCreate = nil
			return nil, true
		}

		// Unpushed branch: offer to push first, then the user retries.
		if !msg.HasCommits {
			m.setStatusf(statusWarn, "Branch %s has no upstream commits — press [P] to push, then [Ctrl+P] again", msg.Branch)
			m.prCreate = nil
			return nil, true
		}
		if msg.Behind > 0 {
			m.setStatusf(statusWarn, "Branch is %d behind upstream — pull first ([F]), then create the PR", msg.Behind)
			m.prCreate = nil
			return nil, true
		}

		// Remember the context and ask for a title.
		m.prCreate = &prCreateContext{
			repoPath: msg.RepoPath,
			branch:   msg.Branch,
			base:     msg.Base,
		}
		title := ""
		if m.selected >= 0 && m.selected < len(m.repos) {
			title = firstLine(m.repos[m.selected].LastMessage)
		}
		if title == "" {
			title = msg.Branch
		}
		return m.openPrompt(promptAction{
			kind:       "pr-title",
			title:      fmt.Sprintf("PR title (base: %s)", msg.Base),
			repoPath:   msg.RepoPath,
			defaultVal: title,
		}), true

	case prCreatedMsg:
		if msg.Err != nil {
			m.setStatusf(statusError, "PR failed: %v", msg.Err)
		} else {
			m.setStatusf(statusSuccess, "🎉 PR #%d created: %s", msg.Number, msg.URL)
		}
		m.prCreate = nil
		return nil, true
	}
	return nil, false
}

// handlePRCreatePrompt routes prompt kinds that belong to this flow.
// Returns (cmd, handled).
func (m *DashboardModel) handlePRCreatePrompt(kind, value string) (tea.Cmd, bool) {
	pc := m.prCreate
	if pc == nil {
		return nil, false
	}
	switch kind {
	case "pr-title":
		if strings.TrimSpace(value) == "" {
			m.setStatus(statusWarn, "PR title cannot be empty — flow cancelled")
			m.prCreate = nil
			return nil, true
		}
		pc.title = strings.TrimSpace(value)
		return m.openPrompt(promptAction{
			kind:       "pr-base",
			title:      fmt.Sprintf("Base branch (default: %s)", pc.base),
			repoPath:   pc.repoPath,
			defaultVal: pc.base,
		}), true

	case "pr-base":
		base := strings.TrimSpace(value)
		if base == "" {
			base = pc.base
		}
		if base == pc.branch {
			m.setStatusf(statusError, "Base cannot equal head (%s) — flow cancelled", base)
			m.prCreate = nil
			return nil, true
		}
		pc.base = base

		// Submit asynchronously with a commit-list body.
		full := m.activeRepoName()
		owner, repo, ok := splitRepoName(full)
		if !ok {
			// Remote may not match the GitHub full name; derive from remote URL.
			remotes, err := git.GitListRemotes(pc.repoPath)
			if err != nil || len(remotes) == 0 {
				m.setStatus(statusError, "Cannot determine owner/repo for PR")
				m.prCreate = nil
				return nil, true
			}
			owner, repo, ok = ownerRepoFromRemote(remotes[0].URL)
			if !ok {
				m.setStatusf(statusError, "Cannot parse owner/repo from %s", remotes[0].URL)
				m.prCreate = nil
				return nil, true
			}
		}
		body := prBodyFromCommits(pc.repoPath, base, pc.branch)
		m.setStatusf(statusLoading, "Creating PR %s → %s…", pc.branch, base)
		return prCreateCmd(m.token, owner, repo, pc.title, pc.branch, base, body), true
	}
	return nil, false
}

// prCreateKey is the entry point: Ctrl+P starts the preflight from the
// Local tab. Returns (cmd, handled).
func (m *DashboardModel) prCreateKey(key string) (tea.Cmd, bool) {
	if key != "ctrl+p" {
		return nil, false
	}
	repo := m.activeRepoPath()
	if repo == "" {
		m.setStatus(statusWarn, "Select a local repository first")
		return nil, true
	}
	if !git.IsGitRepository(repo) {
		m.setStatus(statusWarn, "Not a git repository")
		return nil, true
	}
	m.setStatusf(statusLoading, "Checking PR readiness for %s…", m.activeRepoName())
	return prPreflightCmd(repo), true
}
