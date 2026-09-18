package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	apiBaseURL = "https://api.github.com"
	userAgent  = "TerminalDashboard/1.0.0"
)

// GitHubClient handles GitHub API interactions
type GitHubClient struct {
	Client *http.Client
}

// NewClient creates a new GitHub API client
func NewClient(token *string) *GitHubClient {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	if token != nil && *token != "" {
		client.Transport = &oauthTransport{
			token: *token,
		}
	}

	return &GitHubClient{Client: client}
}

// oauthTransport adds the Authorization header to requests
type oauthTransport struct {
	token string
}

func (t *oauthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", userAgent)
	return http.DefaultTransport.RoundTrip(req)
}

// RepoOwner represents repository owner details from GitHub API
type RepoOwner struct {
	Login     string `json:"login"`
	ID        int64  `json:"id"`
	AvatarURL string `json:"avatar_url"`
}

// Repo represents a GitHub repository
type Repo struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	FullName    string    `json:"full_name"`
	Owner       RepoOwner `json:"owner"`
	Description string    `json:"description"`
	URL         string    `json:"html_url"`
	CloneURL    string    `json:"clone_url"`
	Branch      string    `json:"default_branch"`
	Stars       int       `json:"stargazers_count"`
	Forks       int       `json:"forks_count"`
	Language    string    `json:"language"`
	Private     bool      `json:"private"`
	UpdatedAt   string    `json:"updated_at"`
}

// Branch represents a GitHub branch
type Branch struct {
	Name       string    `json:"name"`
	CommitSHA  string    `json:"sha"`
	CommitDate time.Time `json:"commit_date"`
}

// Commit represents a GitHub commit
type Commit struct {
	SHA     string    `json:"sha"`
	Message string    `json:"message"`
	Author  string    `json:"author"`
	Date    time.Time `json:"date"`
}

type rawCommitItem struct {
	SHA    string `json:"sha"`
	Commit struct {
		Author struct {
			Name string    `json:"name"`
			Date time.Time `json:"date"`
		} `json:"author"`
		Message string `json:"message"`
	} `json:"commit"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
}

// PRUser represents GitHub PR creator
type PRUser struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

// PullRequest represents a GitHub pull request
type PullRequest struct {
	ID     int    `json:"id"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	User   PRUser `json:"user"`
	URL    string `json:"html_url"`
}

// User represents a GitHub user
type User struct {
	Login    string `json:"login"`
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Avatar   string `json:"avatar_url"`
	Company  string `json:"company"`
	Blog     string `json:"blog"`
	Location string `json:"location"`
}

// GetAuthenticatedUser fetches the authenticated user info
func (c *GitHubClient) GetAuthenticatedUser() (*User, error) {
	body, err := c.get(apiBaseURL + "/user")
	if err != nil {
		return nil, err
	}

	var user User
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// GetRepositories lists repositories for the authenticated user
func (c *GitHubClient) GetRepositories(page, perPage int) ([]Repo, error) {
	url := fmt.Sprintf("%s/user/repos?page=%d&per_page=%d&sort=updated&type=all",
		apiBaseURL, page, perPage)
	body, err := c.get(url)
	if err != nil {
		return nil, err
	}

	var repos []Repo
	if err := json.Unmarshal(body, &repos); err != nil {
		return nil, err
	}
	return repos, nil
}

// GetRepository gets a single repository by full name
func (c *GitHubClient) GetRepository(owner, repo string) (*Repo, error) {
	url := fmt.Sprintf("%s/repos/%s/%s", apiBaseURL, url.PathEscape(owner), url.PathEscape(repo))
	body, err := c.get(url)
	if err != nil {
		return nil, err
	}

	var r Repo
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// GetBranches lists branches for a repository
func (c *GitHubClient) GetBranches(owner, repo string) ([]Branch, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/branches", apiBaseURL, url.PathEscape(owner), url.PathEscape(repo))
	body, err := c.get(url)
	if err != nil {
		return nil, err
	}

	var branches []Branch
	if err := json.Unmarshal(body, &branches); err != nil {
		return nil, err
	}
	return branches, nil
}

// GetCommits lists commits for a repository
func (c *GitHubClient) GetCommits(owner, repo string, page int) ([]Commit, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/commits?page=%d&per_page=10", apiBaseURL, url.PathEscape(owner), url.PathEscape(repo), page)
	body, err := c.get(url)
	if err != nil {
		return nil, err
	}

	var commits []Commit
	if err := json.Unmarshal(body, &commits); err != nil {
		return nil, err
	}
	return commits, nil
}

// GetPullRequests lists pull requests for a repository
func (c *GitHubClient) GetPullRequests(owner, repo string) ([]PullRequest, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls?state=open&per_page=20", apiBaseURL, url.PathEscape(owner), url.PathEscape(repo))
	body, err := c.get(url)
	if err != nil {
		return nil, err
	}

	var prs []PullRequest
	if err := json.Unmarshal(body, &prs); err != nil {
		return nil, err
	}
	return prs, nil
}

// GetRepositoryPRs gets pull requests for a specific repository
func (c *GitHubClient) GetRepositoryPRs(owner, repo string) ([]PullRequest, error) {
	return c.GetPullRequests(owner, repo)
}

// SearchRepositories searches GitHub repositories
func (c *GitHubClient) SearchRepositories(query string, page int) ([]Repo, error) {
	url := fmt.Sprintf("%s/search/repositories?q=%s&page=%d&per_page=20&sort=stars", apiBaseURL, url.QueryEscape(query), page)
	body, err := c.get(url)
	if err != nil {
		return nil, err
	}

	var result struct {
		Items []Repo `json:"items"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

// get performs a GET request and returns the response body
func (c *GitHubClient) get(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 404 {
		// 404 may mean not-found OR no permission; surface body for context.
		if len(body) > 0 {
			return nil, fmt.Errorf("resource not found: %s", truncateBody(body))
		}
		return nil, fmt.Errorf("resource not found")
	}
	if resp.StatusCode == 401 {
		return nil, fmt.Errorf("unauthorized - token may be expired")
	}
	if resp.StatusCode == 403 {
		return nil, fmt.Errorf("forbidden - rate limit exceeded or insufficient permissions")
	}
	if resp.StatusCode >= 400 {
		if len(body) > 0 {
			return nil, fmt.Errorf("API error: status %d: %s", resp.StatusCode, truncateBody(body))
		}
		return nil, fmt.Errorf("API error: status %d", resp.StatusCode)
	}

	return body, nil
}

func truncateBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 500 {
		return s[:500] + "..."
	}
	return s
}

// GetBranchProtection checks if a branch has protection enabled
func (c *GitHubClient) GetBranchProtection(owner, repo, branch string) (bool, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/branches/%s/protection", apiBaseURL, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(branch))
	_, err := c.get(url)
	if err != nil {
		if strings.Contains(err.Error(), "resource not found") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// GetRepoCommit gets a specific commit
func (c *GitHubClient) GetRepoCommit(owner, repo, sha string) (*Commit, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/commits/%s", apiBaseURL, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(sha))
	body, err := c.get(url)
	if err != nil {
		return nil, err
	}

	var raw rawCommitItem
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	author := raw.Author.Login
	if author == "" {
		author = raw.Commit.Author.Name
	}
	return &Commit{
		SHA:     raw.SHA,
		Message: raw.Commit.Message,
		Author:  author,
		Date:    raw.Commit.Author.Date,
	}, nil
}

// GetRepoCommits gets commits for a repository with pagination
func (c *GitHubClient) GetRepoCommits(owner, repo string, page int) ([]Commit, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/commits?page=%d&per_page=10", apiBaseURL, url.PathEscape(owner), url.PathEscape(repo), page)
	body, err := c.get(url)
	if err != nil {
		return nil, err
	}

	var raw []rawCommitItem
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	commits := make([]Commit, len(raw))
	for i, r := range raw {
		author := r.Author.Login
		if author == "" {
			author = r.Commit.Author.Name
		}
		commits[i] = Commit{
			SHA:     r.SHA,
			Message: r.Commit.Message,
			Author:  author,
			Date:    r.Commit.Author.Date,
		}
	}
	return commits, nil
}

// GetRepoStats gets repository statistics
func (c *GitHubClient) GetRepoStats(owner, repo string) (map[string]string, error) {
	stats := make(map[string]string)

	repoData, err := c.GetRepository(owner, repo)
	if err != nil {
		return stats, err
	}

	stats["stars"] = strconv.Itoa(repoData.Stars)
	stats["forks"] = strconv.Itoa(repoData.Forks)
	stats["language"] = repoData.Language
	stats["default_branch"] = repoData.Branch
	stats["updated_at"] = repoData.UpdatedAt
	stats["private"] = strconv.FormatBool(repoData.Private)

	return stats, nil
}

// CreateRepoRequest represents parameters for creating a new GitHub repository
type CreateRepoRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Private     bool   `json:"private"`
	AutoInit    bool   `json:"auto_init"`
}

// CreateRepository creates a new repository on GitHub for the authenticated user
func (c *GitHubClient) CreateRepository(req CreateRepoRequest) (*Repo, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("repository name cannot be empty")
	}
	if len(req.Name) > 100 {
		return nil, fmt.Errorf("repository name too long")
	}
	for _, r := range req.Name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return nil, fmt.Errorf("invalid repository name %q", req.Name)
	}
	url := apiBaseURL + "/user/repos"
	body, err := c.post(url, req)
	if err != nil {
		return nil, err
	}

	var r Repo
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// post performs a POST request with JSON body and returns the response body
func (c *GitHubClient) post(url string, payload interface{}) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		var ghErr struct {
			Message string `json:"message"`
			Errors  []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(body, &ghErr); err == nil && ghErr.Message != "" {
			if strings.Contains(strings.ToLower(ghErr.Message), "resource not accessible by integration") {
				return nil, fmt.Errorf("GitHub token lacks 'repo' creation scope (Device Flow tokens cannot create repositories). Please provide a Personal Access Token (PAT) with 'repo' scope (press [l])")
			}
			if len(ghErr.Errors) > 0 && ghErr.Errors[0].Message != "" {
				return nil, fmt.Errorf("%s: %s", ghErr.Message, ghErr.Errors[0].Message)
			}
			// 403/404 on creation almost always means insufficient token
			// scope (GitHub may answer 404 to avoid leaking repo existence).
			if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
				return nil, fmt.Errorf("%s (HTTP %d — the token likely lacks the 'repo' scope; press [l] to log in with a classic Personal Access Token that has it)", ghErr.Message, resp.StatusCode)
			}
			return nil, fmt.Errorf("%s", ghErr.Message)
		}
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("GitHub API error (%d) creating repository — the token likely lacks the 'repo' scope (press [l] to log in with a classic Personal Access Token that has it)", resp.StatusCode)
		}
		return nil, fmt.Errorf("GitHub API error (%d): %s", resp.StatusCode, string(body))
	}

	return body, nil
}
