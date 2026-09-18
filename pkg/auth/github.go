package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

const (
	githubAuthURL  = "https://github.com/login/oauth/authorize"
	githubTokenURL = "https://github.com/login/oauth/access_token"
	configDir      = "Dashboard"
	tokenFilename  = "github_token.json"
	clientIDFile   = "github_client_id"
)

// githubAPIBaseURL is a var (not const) so tests can redirect API calls to
// an httptest server.
var githubAPIBaseURL = "https://api.github.com"

// Token represents a stored GitHub OAuth token or PAT
type Token struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry"`
}

// IsExpired returns true if the token has expired (0 expiry means permanent PAT)
func (t *Token) IsExpired() bool {
	if t.Expiry.IsZero() {
		return false
	}
	return time.Now().After(t.Expiry)
}

// Config stores GitHub authentication configuration
type Config struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
}

// GetConfigPath returns the path to GitHub auth config file
func GetConfigPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve config directory: %w", err)
	}
	return filepath.Join(base, configDir, "github_config.json"), nil
}

// GetTokenPath returns the path to stored token file
func GetTokenPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve config directory: %w", err)
	}
	return filepath.Join(base, configDir, tokenFilename), nil
}

// SaveToken stores the OAuth token to disk
func SaveToken(token *Token) error {
	tokenPath, err := GetTokenPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(tokenPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}
	return os.WriteFile(tokenPath, data, 0600)
}

// ErrRateLimited signals 403/429 where validity is unknown (do not discard token).
var ErrRateLimited = errors.New("github rate limited or forbidden; validity unknown")

// ErrInvalidToken signals a definitive 401 rejection.
var ErrInvalidToken = errors.New("github token invalid or revoked")

// LoadToken reads the stored OAuth token from disk
func LoadToken() (*Token, error) {
	// 1. Try file
	tokenPath, err := GetTokenPath()
	if err == nil {
		data, err := os.ReadFile(tokenPath)
		if err == nil {
			var token Token
			if err := json.Unmarshal(data, &token); err == nil && strings.TrimSpace(token.AccessToken) != "" {
				token.AccessToken = strings.TrimSpace(token.AccessToken)
				return &token, nil
			}
		}
	}

	// 2. Try environment variables (read-only: never persist without explicit consent).
	for _, envVar := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if envToken := strings.TrimSpace(os.Getenv(envVar)); envToken != "" {
			return &Token{
				AccessToken: envToken,
				TokenType:   "Bearer",
			}, nil
		}
	}

	return nil, fmt.Errorf("token not found: run login or provide GITHUB_TOKEN")
}

// ImportEnvToken explicitly persists GITHUB_TOKEN/GH_TOKEN to disk.
func ImportEnvToken() (*Token, error) {
	for _, envVar := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if envToken := strings.TrimSpace(os.Getenv(envVar)); envToken != "" {
			tok := &Token{AccessToken: envToken, TokenType: "Bearer"}
			if err := SaveToken(tok); err != nil {
				return nil, err
			}
			return tok, nil
		}
	}
	return nil, fmt.Errorf("no token in environment")
}

// DeleteToken removes the stored OAuth token
func DeleteToken() error {
	tokenPath, err := GetTokenPath()
	if err != nil {
		return err
	}
	if err := os.Remove(tokenPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}

// LoadConfig reads the GitHub OAuth client configuration
func LoadConfig() (*Config, error) {
	configPath, err := GetConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("config not found: %w", err)
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	return &config, nil
}

// SaveConfig stores the GitHub OAuth client configuration
func SaveConfig(config *Config) error {
	configPath, err := GetConfigPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	return os.WriteFile(configPath, data, 0600)
}

// OAuthTransport returns an HTTP transport that automatically adds Authorization
func OAuthTransport(token *Token) *oauth2.Transport {
	return &oauth2.Transport{
		Source: oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken: token.AccessToken,
			TokenType:   token.TokenType,
			Expiry:      token.Expiry,
		}),
	}
}

// Client returns an authenticated HTTP client for GitHub API requests
func Client(token *Token) *http.Client {
	transport := OAuthTransport(token)
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
}

// ValidateToken checks if a token is valid by querying the GitHub /user API.
// 401 => (false, nil) invalid; 403/429 => (false, ErrRateLimited) unknown, keep token.
func ValidateToken(token *Token) (bool, error) {
	if token == nil || strings.TrimSpace(token.AccessToken) == "" {
		return false, fmt.Errorf("token is empty")
	}
	if token.IsExpired() {
		return false, fmt.Errorf("token has expired")
	}

	req, err := http.NewRequest("GET", githubAPIBaseURL+"/user", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token.AccessToken))
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "TerminalDashboard")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4*1024))

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusUnauthorized:
		return false, nil
	case http.StatusForbidden, 429:
		return false, ErrRateLimited
	default:
		return false, fmt.Errorf("github validation HTTP %d", resp.StatusCode)
	}
}

// minTokenLengths gives a floor for recognised prefixes. GitHub treats
// tokens as opaque and can change exact lengths, so these are minimums only
// — the API is the source of truth, never an exact-length check.
var minTokenLengths = map[string]int{
	"ghp_":        20,
	"github_pat_": 40,
	"gho_":        20,
	"ghu_":        20,
	"ghs_":        20,
	"ghr_":        20,
}

// cleanTokenInput trims surrounding whitespace and quotes that terminals,
// browsers, or password managers often add on paste.
func cleanTokenInput(s string) string {
	s = strings.TrimSpace(s)
	// Strip one layer of surrounding single/double/back quotes.
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '`' && s[len(s)-1] == '`') {
			s = strings.TrimSpace(s[1 : len(s)-1])
		}
	}
	return s
}

// ValidateTokenFormat rejects only clearly broken input (empty, whitespace,
// too short, invalid characters). Exact lengths are NOT checked: GitHub
// treats tokens as opaque and lengths can change. Unknown formats are
// accepted and left for the API to judge.
func ValidateTokenFormat(rawToken string) error {
	rawToken = cleanTokenInput(rawToken)
	if rawToken == "" {
		return fmt.Errorf("token cannot be empty")
	}
	// Embedded whitespace means a broken paste (line wrap, extra words).
	if strings.ContainsAny(rawToken, " \t\n\r") {
		return fmt.Errorf("token contains whitespace — paste the full token with GitHub's copy button")
	}
	for prefix, minLen := range minTokenLengths {
		if strings.HasPrefix(rawToken, prefix) {
			if len(rawToken) < minLen {
				return fmt.Errorf("token too short (%d characters for %q prefix). Paste the full token from GitHub", len(rawToken), prefix)
			}
			return nil
		}
	}
	// Unknown prefix: require a sane minimum so obvious typos fail fast,
	// but leave exact validation to the API.
	if len(rawToken) < 20 {
		return fmt.Errorf("token too short (%d characters). Paste the full token from GitHub", len(rawToken))
	}
	for _, r := range rawToken {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return fmt.Errorf("token contains invalid character %q. Paste the full token from GitHub", r)
	}
	return nil
}

// SaveTokenString validates a raw token string (PAT or OAuth token) and saves it to disk.
// Format checks are a fast-fail for clearly broken input only; the GitHub
// API is the source of truth for validity.
func SaveTokenString(rawToken string) (*Token, string, error) {
	rawToken = cleanTokenInput(rawToken)
	if err := ValidateTokenFormat(rawToken); err != nil {
		return nil, "", err
	}

	req, err := http.NewRequest("GET", githubAPIBaseURL+"/user", nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+rawToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "TerminalDashboard")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("connection to GitHub failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, "", fmt.Errorf("GitHub rejected the token (HTTP 401 Bad credentials) — check the token is complete, not expired or deleted, and belongs to your account")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("GitHub authentication failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var user struct {
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	_ = json.Unmarshal(body, &user)
	if strings.TrimSpace(user.Login) == "" {
		return nil, "", fmt.Errorf("GitHub authentication succeeded but user login was empty")
	}

	tok := &Token{
		AccessToken: rawToken,
		TokenType:   "Bearer",
	}

	if err := SaveToken(tok); err != nil {
		return nil, user.Login, fmt.Errorf("failed to save token to disk: %w", err)
	}

	return tok, user.Login, nil
}

// GetTokenScopes returns the OAuth scopes granted to a classic Personal
// Access Token, read from the X-OAuth-Scopes response header of GET /user.
// present=false when the header is absent: fine-grained PATs and OAuth-app
// (ghu_/gho_) tokens do not expose scopes this way, so absence must NOT be
// treated as "no scopes" — callers should attempt the operation and let the
// API decide.
func GetTokenScopes(token *Token) (scopes []string, present bool, err error) {
	if token == nil || strings.TrimSpace(token.AccessToken) == "" {
		return nil, false, fmt.Errorf("token is empty")
	}
	if token.IsExpired() {
		return nil, false, fmt.Errorf("token has expired")
	}

	req, err := http.NewRequest("GET", githubAPIBaseURL+"/user", nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "TerminalDashboard")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token.AccessToken))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4*1024))

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, false, fmt.Errorf("GitHub token is invalid or revoked (HTTP 401)")
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == 429 {
		return nil, false, ErrRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("GitHub authentication check failed (HTTP %d)", resp.StatusCode)
	}

	raw := resp.Header.Values("X-OAuth-Scopes")
	if len(raw) == 0 {
		return nil, false, nil
	}
	for _, s := range strings.Split(strings.Join(raw, ","), ",") {
		if s = strings.TrimSpace(s); s != "" {
			scopes = append(scopes, s)
		}
	}
	return scopes, true, nil
}

// HasRepoCreateScope reports whether granted classic-PAT scopes allow
// creating a repository: private repos need the "repo" scope, public repos
// additionally accept "public_repo".
func HasRepoCreateScope(scopes []string, isPrivate bool) bool {
	set := make(map[string]struct{}, len(scopes))
	for _, s := range scopes {
		set[strings.ToLower(strings.TrimSpace(s))] = struct{}{}
	}
	if _, ok := set["repo"]; ok {
		return true
	}
	if !isPrivate {
		if _, ok := set["public_repo"]; ok {
			return true
		}
	}
	return false
}

const DefaultClientID = "Iv1.b507a08c87ecfe98"

// DeviceCodeResponse represents GitHub device authorization response
type DeviceCodeResponse struct {
	ClientID        string `json:"client_id"`
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// RequestDeviceCode initiates the GitHub OAuth device code grant
func RequestDeviceCode(clientID string) (*DeviceCodeResponse, error) {
	if clientID == "" {
		clientID = DefaultClientID
	}

	formData := url.Values{
		"client_id": {clientID},
		"scope":     {"repo,read:org,user"},
	}

	req, err := http.NewRequest("POST", "https://github.com/login/device/code", strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device authorization request failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result DeviceCodeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.ExpiresIn == 0 {
		result.ExpiresIn = 900
	}
	result.ClientID = clientID
	return &result, nil
}

// PollForDeviceToken polls GitHub's token endpoint for user approval.
// ctx cancels polling (e.g. user pressed ESC); without cancellation the loop
// previously ran until ExpiresIn (15min) leaking a goroutine.
func PollForDeviceToken(clientID string, code *DeviceCodeResponse) (*Token, error) {
	return PollForDeviceTokenWithContext(context.Background(), clientID, code)
}

// PollForDeviceTokenWithContext is the cancellable variant.
func PollForDeviceTokenWithContext(ctx context.Context, clientID string, code *DeviceCodeResponse) (*Token, error) {
	if clientID == "" && code != nil && code.ClientID != "" {
		clientID = code.ClientID
	}
	if clientID == "" {
		clientID = DefaultClientID
	}

	if code == nil || code.DeviceCode == "" {
		return nil, fmt.Errorf("invalid device code response")
	}

	interval := time.Duration(code.Interval) * time.Second
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	expiresIn := code.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 900
	}

	timeout := time.After(time.Duration(expiresIn) * time.Second)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("device authorization cancelled: %w", ctx.Err())
		case <-timeout:
			return nil, fmt.Errorf("device authorization code expired; please try again")
		case <-ticker.C:
			formData := url.Values{
				"client_id":   {clientID},
				"device_code": {code.DeviceCode},
				"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			}

			req, err := http.NewRequest("POST", githubTokenURL, strings.NewReader(formData.Encode()))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Accept", "application/json")
			req.Header.Set("User-Agent", "TerminalDashboard")

			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				// Respect cancellation while backing off.
				select {
				case <-ctx.Done():
					return nil, fmt.Errorf("device authorization cancelled: %w", ctx.Err())
				case <-time.After(2 * time.Second):
				}
				continue
			}

			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()

			var pollResp struct {
				AccessToken string `json:"access_token"`
				TokenType   string `json:"token_type"`
				Scope       string `json:"scope"`
				Error       string `json:"error"`
				ErrorDesc   string `json:"error_description"`
				Interval    int    `json:"interval"`
				Message     string `json:"message"`
			}
			_ = json.Unmarshal(body, &pollResp)

			if pollResp.AccessToken != "" {
				tok := &Token{
					AccessToken: pollResp.AccessToken,
					TokenType:   pollResp.TokenType,
				}
				if err := SaveToken(tok); err != nil {
					return nil, fmt.Errorf("failed to save token: %w", err)
				}
				return tok, nil
			}

			if pollResp.Error == "authorization_pending" {
				continue
			} else if pollResp.Error == "slow_down" {
				if pollResp.Interval > 0 {
					ticker.Reset(time.Duration(pollResp.Interval) * time.Second)
				} else {
					ticker.Reset(interval + 5*time.Second)
				}
				continue
			} else if pollResp.Error == "expired_token" {
				return nil, fmt.Errorf("device authorization expired. Please press [d] to request a new code")
			} else if pollResp.Error == "access_denied" {
				return nil, fmt.Errorf("authorization was denied on GitHub")
			} else if pollResp.Error != "" {
				return nil, fmt.Errorf("OAuth error: %s (%s)", pollResp.Error, pollResp.ErrorDesc)
			} else if pollResp.Message != "" {
				return nil, fmt.Errorf("GitHub error: %s", pollResp.Message)
			}
		}
	}
}

// NewOAuthConfig creates an OAuth2 config
func NewOAuthConfig(clientID, clientSecret string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     github.Endpoint,
		RedirectURL:  "http://localhost:8085/callback",
		Scopes:       []string{"repo", "user", "read:org", "workflow"},
	}
}
