package auth

import (
	"encoding/json"
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
	githubAuthURL    = "https://github.com/login/oauth/authorize"
	githubTokenURL   = "https://github.com/login/oauth/access_token"
	githubAPIBaseURL = "https://api.github.com"
	configDir        = "Dashboard"
	tokenFilename    = "github_token.json"
	clientIDFile     = "github_client_id"
)

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

// LoadToken reads the stored OAuth token from disk
func LoadToken() (*Token, error) {
	// 1. Try file
	tokenPath, err := GetTokenPath()
	if err == nil {
		data, err := os.ReadFile(tokenPath)
		if err == nil {
			var token Token
			if err := json.Unmarshal(data, &token); err == nil && token.AccessToken != "" {
				return &token, nil
			}
		}
	}

	// 2. Try environment variables
	for _, envVar := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if envToken := strings.TrimSpace(os.Getenv(envVar)); envToken != "" {
			tok := &Token{
				AccessToken: envToken,
				TokenType:   "Bearer",
			}
			_ = SaveToken(tok)
			return tok, nil
		}
	}

	return nil, fmt.Errorf("token not found: run login or provide GITHUB_TOKEN")
}

// DeleteToken removes the stored OAuth token
func DeleteToken() error {
	tokenPath, err := GetTokenPath()
	if err != nil {
		return err
	}
	return os.Remove(tokenPath)
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

// ValidateToken checks if a token is valid by querying the GitHub /user API
func ValidateToken(token *Token) (bool, error) {
	if token == nil || token.AccessToken == "" {
		return false, fmt.Errorf("token is empty")
	}
	if token.IsExpired() {
		return false, fmt.Errorf("token has expired")
	}

	req, err := http.NewRequest("GET", githubAPIBaseURL+"/user", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "TerminalDashboard")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

// SaveTokenString validates a raw token string (PAT or OAuth token) and saves it to disk
func SaveTokenString(rawToken string) (*Token, string, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, "", fmt.Errorf("token cannot be empty")
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

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("GitHub authentication failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var user struct {
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	_ = json.Unmarshal(body, &user)

	tok := &Token{
		AccessToken: rawToken,
		TokenType:   "Bearer",
	}

	if err := SaveToken(tok); err != nil {
		return nil, user.Login, fmt.Errorf("failed to save token to disk: %w", err)
	}

	return tok, user.Login, nil
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

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device authorization request failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result DeviceCodeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	result.ClientID = clientID
	return &result, nil
}

// PollForDeviceToken polls GitHub's token endpoint for user approval
func PollForDeviceToken(clientID string, code *DeviceCodeResponse) (*Token, error) {
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

	timeout := time.After(time.Duration(code.ExpiresIn) * time.Second)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
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
				continue
			}

			body, _ := io.ReadAll(resp.Body)
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