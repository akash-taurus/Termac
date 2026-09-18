package shell

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WindowsTerminalSettings represents minimal settings.json structure
type WindowsTerminalSettings struct {
	Profiles struct {
		List []map[string]interface{} `json:"list"`
	} `json:"profiles"`
	Other map[string]interface{} `json:"-"`
}

// FindWindowsTerminalSettings locates the active settings.json for Windows Terminal
func FindWindowsTerminalSettings() (string, error) {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return "", fmt.Errorf("LOCALAPPDATA environment variable not set")
	}

	// Packaged version path
	packagedPath := filepath.Join(localAppData, "Packages",
		"Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState", "settings.json")
	if _, err := os.Stat(packagedPath); err == nil {
		return packagedPath, nil
	}

	// Unpackaged / preview version path
	previewPath := filepath.Join(localAppData, "Microsoft", "Windows Terminal", "settings.json")
	if _, err := os.Stat(previewPath); err == nil {
		return previewPath, nil
	}

	return "", fmt.Errorf("Windows Terminal settings.json not found")
}

// RegisterTerminalProfile adds Terminal Dashboard profile to Windows Terminal
func RegisterTerminalProfile(exePath, iconPath string) error {
	settingsPath, err := FindWindowsTerminalSettings()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("failed to read settings.json: %w", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("failed to parse settings.json: %w", err)
	}

	profilesRaw, ok := raw["profiles"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid profiles object in settings.json")
	}

	list, ok := profilesRaw["list"].([]interface{})
	if !ok {
		list = []interface{}{}
	}

	const profileGuid = "{d6497b1a-5477-4c0a-b344-9b0bcbe2c4a2}"

	// Check if already registered
	for _, p := range list {
		if pMap, ok := p.(map[string]interface{}); ok {
			if pMap["guid"] == profileGuid || pMap["name"] == "Terminal Dashboard" {
				return nil // Already registered
			}
		}
	}

	newProfile := map[string]interface{}{
		"guid":              profileGuid,
		"name":              "Terminal Dashboard",
		"commandline":       exePath,
		"icon":              iconPath,
		"startingDirectory": "%USERPROFILE%",
		"colorScheme":       "Campbell",
		"hidden":            false,
	}

	profilesRaw["list"] = append(list, newProfile)
	raw["profiles"] = profilesRaw

	updated, err := json.MarshalIndent(raw, "", "    ")
	if err != nil {
		return err
	}

	return os.WriteFile(settingsPath, updated, 0644)
}
