package shell

import (
	"encoding/json"
	"fmt"
)

// ScoopManifest represents a Scoop package manager manifest
type ScoopManifest struct {
	Version      string                 `json:"version"`
	Description  string                 `json:"description"`
	Homepage     string                 `json:"homepage"`
	License      string                 `json:"license"`
	Architecture map[string]interface{} `json:"architecture"`
	Bin          string                 `json:"bin"`
	Checkver     string                 `json:"checkver"`
	Autoupdate   map[string]interface{} `json:"autoupdate"`
}

// GenerateScoopManifest creates the JSON specification for Scoop
func GenerateScoopManifest(version, repoOwner, repoName, sha256Amd64, sha256Arm64 string) (string, error) {
	manifest := ScoopManifest{
		Version:     version,
		Description: "Terminal Dashboard for Everything - Native Windows TUI",
		Homepage:    fmt.Sprintf("https://github.com/%s/%s", repoOwner, repoName),
		License:     "MIT",
		Bin:         "dashboard.exe",
		Checkver:    "github",
		Architecture: map[string]interface{}{
			"64bit": map[string]string{
				"url":  fmt.Sprintf("https://github.com/%s/%s/releases/download/v%s/dashboard_%s_windows_amd64.zip", repoOwner, repoName, version, version),
				"hash": sha256Amd64,
			},
			"arm64": map[string]string{
				"url":  fmt.Sprintf("https://github.com/%s/%s/releases/download/v%s/dashboard_%s_windows_arm64.zip", repoOwner, repoName, version, version),
				"hash": sha256Arm64,
			},
		},
		Autoupdate: map[string]interface{}{
			"architecture": map[string]interface{}{
				"64bit": map[string]string{
					"url": fmt.Sprintf("https://github.com/%s/%s/releases/download/v$version/dashboard_$version_windows_amd64.zip", repoOwner, repoName),
				},
				"arm64": map[string]string{
					"url": fmt.Sprintf("https://github.com/%s/%s/releases/download/v$version/dashboard_$version_windows_arm64.zip", repoOwner, repoName),
				},
			},
		},
	}

	bytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// GenerateWingetManifest produces the Microsoft Winget YAML installer manifest
func GenerateWingetManifest(version, repoOwner, repoName, sha256Installer string) string {
	return fmt.Sprintf(`PackageIdentifier: DashboardTeam.TerminalDashboard
PackageVersion: %s
PackageType: installer
Installers:
  - Architecture: x64
    InstallerType: inno
    InstallerUrl: https://github.com/%s/%s/releases/download/v%s/dashboard_setup_%s_x64.exe
    InstallerSha256: %s
ManifestType: installer
ManifestVersion: 1.6.0
`, version, repoOwner, repoName, version, version, sha256Installer)
}
