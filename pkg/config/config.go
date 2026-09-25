// Package config manages application configuration using platform-appropriate
// base directories (os.UserConfigDir on Windows, ~/.config on Unix).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const appName = "Dashboard"

// ConfigPath returns the absolute path to the dashboard configuration directory
// file. On Windows this resolves to %AppData%\Dashboard\config.yaml.
func ConfigPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve config directory: %w", err)
	}
	return filepath.Join(base, appName, "config.yaml"), nil
}

// PluginsPath returns the absolute path to the directory containing plugins.
// Plugins are stored in the user's config directory, e.g. %AppData%\Dashboard\plugins.
func PluginsPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve config directory: %w", err)
	}
	return filepath.Join(base, appName, "plugins"), nil
}

// EnsureDirectory creates the base directory for the application's config and
// plugin files. The directory is not marked as hidden on Windows; the OS
// convention is that %AppData% is already hidden.
func EnsureDirectory() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve config directory: %w", err)
	}
	path := filepath.Join(base, appName)
	if err := os.MkdirAll(path, 0755); err != nil {
		return "", fmt.Errorf("failed to create config directory %s: %w", path, err)
	}
	return path, nil
}

// EnsurePluginsPath creates the plugins directory.
func EnsurePluginsPath() (string, error) {
	path, err := PluginsPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return "", fmt.Errorf("failed to create plugins directory %s: %w", path, err)
	}
	return path, nil
}

// GetTokenPath returns the path to the GitHub OAuth token file
func GetTokenPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve config directory: %w", err)
	}
	return filepath.Join(base, appName, "github_token.json"), nil
}

// GetGitHubConfigPath returns the path to the GitHub OAuth client configuration
func GetGitHubConfigPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve config directory: %w", err)
	}
	return filepath.Join(base, appName, "github_config.json"), nil
}

// GetGitHubClientID returns the stored GitHub OAuth client ID
func GetGitHubClientID() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(base, appName, "github_client_id")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SaveGitHubClientID stores the GitHub OAuth client ID
func SaveGitHubClientID(id string) error {
	base, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(base, appName), 0700); err != nil {
		return err
	}
	path := filepath.Join(base, appName, "github_client_id")
	return os.WriteFile(path, []byte(id), 0600)
}

// GetLastLogin returns the timestamp of the last GitHub login
func GetLastLogin() (time.Time, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return time.Time{}, err
	}
	path := filepath.Join(base, appName, "last_login")
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, err
	}
	t, err := time.Parse(time.RFC3339, string(data))
	if err != nil {
		return time.Time{}, fmt.Errorf("corrupt last_login: %w", err)
	}
	return t, nil
}

// SaveLastLogin stores the current time as the last login timestamp
func SaveLastLogin() error {
	base, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(base, appName), 0700); err != nil {
		return err
	}
	path := filepath.Join(base, appName, "last_login")
	return os.WriteFile(path, []byte(time.Now().Format(time.RFC3339)), 0600)
}
