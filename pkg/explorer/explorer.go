package explorer

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"tui/pkg/git"
)

// FileEntry represents a file or directory in the explorer
type FileEntry struct {
	Name      string
	Path      string
	RelPath   string
	IsDir     bool
	Size      int64
	ModTime   time.Time
	GitStatus string // "M", "A", "?", "D", or ""
	Ext       string
}

// Explorer handles directory navigation, file previews, and external launcher actions
type Explorer struct {
	RepoRoot     string
	CurrentDir   string
	Entries      []FileEntry
	Selected     int
	PreviewPath  string
	PreviewText  string
	PreviewError string
	GitStatuses  map[string]string
}

// NewExplorer initializes an explorer pointing to a repository root directory
func NewExplorer(repoRoot string) (*Explorer, error) {
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, err
	}

	exp := &Explorer{
		RepoRoot:    absRoot,
		CurrentDir:  absRoot,
		GitStatuses: make(map[string]string),
	}

	if err := exp.Refresh(); err != nil {
		return nil, err
	}

	return exp, nil
}

// Refresh re-reads git status and directory contents
func (e *Explorer) Refresh() error {
	// Re-query git status for file badges
	if statusMap, err := git.GetFileStatusMap(e.RepoRoot); err == nil {
		e.GitStatuses = statusMap
	}

	return e.Load(e.CurrentDir)
}

// ensureInsideRepo rejects paths escaping RepoRoot (traversal, abs, symlink).
func (e *Explorer) ensureInsideRepo(dirPath string) error {
	clean := filepath.Clean(dirPath)
	rel, err := filepath.Rel(e.RepoRoot, clean)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes repository root")
	}
	if eval, err := filepath.EvalSymlinks(clean); err == nil {
		if rel2, err := filepath.Rel(e.RepoRoot, eval); err != nil || rel2 == ".." || strings.HasPrefix(rel2, ".."+string(filepath.Separator)) {
			return fmt.Errorf("symlink escapes repository root")
		}
	}
	return nil
}

// Load reads the contents of the given directory
func (e *Explorer) Load(dirPath string) error {
	if err := e.ensureInsideRepo(dirPath); err != nil {
		return err
	}
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	e.CurrentDir = dirPath
	var list []FileEntry

	for _, entry := range entries {
		// Skip .git directory internals to keep view clean
		if entry.Name() == ".git" {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		fullPath := filepath.Join(dirPath, entry.Name())
		relPath, _ := filepath.Rel(e.RepoRoot, fullPath)
		relSlash := filepath.ToSlash(relPath)

		gitStatus := ""
		if entry.IsDir() {
			// Check if any file inside this directory has modified git status
			prefix := relSlash + "/"
			for p, s := range e.GitStatuses {
				if strings.HasPrefix(p, prefix) {
					gitStatus = s
					break
				}
			}
		} else {
			gitStatus = e.GitStatuses[relSlash]
		}

		list = append(list, FileEntry{
			Name:      entry.Name(),
			Path:      fullPath,
			RelPath:   relSlash,
			IsDir:     entry.IsDir(),
			Size:      info.Size(),
			ModTime:   info.ModTime(),
			GitStatus: gitStatus,
			Ext:       strings.ToLower(filepath.Ext(entry.Name())),
		})
	}

	// Sort directories first, then alphabetically
	sort.Slice(list, func(i, j int) bool {
		if list[i].IsDir != list[j].IsDir {
			return list[i].IsDir // Directories first
		}
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})

	e.Entries = list
	if e.Selected >= len(list) {
		e.Selected = 0
	}
	if e.Selected < 0 && len(list) > 0 {
		e.Selected = 0
	}

	// Automatically preview first file if available
	if len(e.Entries) > 0 && !e.Entries[e.Selected].IsDir {
		_ = e.LoadPreview(e.Entries[e.Selected].Path, 150)
	} else {
		e.PreviewPath = ""
		e.PreviewText = ""
	}

	return nil
}

// GoUp navigates to the parent directory if still within RepoRoot
func (e *Explorer) GoUp() bool {
	if e.CurrentDir == e.RepoRoot {
		return false // Already at repository root
	}

	parent := filepath.Dir(e.CurrentDir)
	// Guard against navigating above RepoRoot
	rel, err := filepath.Rel(e.RepoRoot, parent)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}

	if err := e.Load(parent); err != nil {
		return false
	}
	return true
}

// OpenSelected handles Enter key: enters folder or previews file
func (e *Explorer) OpenSelected() (isDir bool, err error) {
	if len(e.Entries) == 0 || e.Selected < 0 || e.Selected >= len(e.Entries) {
		return false, nil
	}

	entry := e.Entries[e.Selected]
	if entry.IsDir {
		err := e.Load(entry.Path)
		return true, err
	}

	err = e.LoadPreview(entry.Path, 150)
	return false, err
}

// LoadPreview reads up to maxLines of a text file for in-TUI inspection
func (e *Explorer) LoadPreview(filePath string, maxLines int) error {
	if err := e.ensureInsideRepo(filepath.Dir(filePath)); err != nil {
		e.PreviewError = err.Error()
		return err
	}
	if maxLines <= 0 {
		maxLines = 150
	}
	if maxLines > 1000 {
		maxLines = 1000
	}
	file, err := os.Open(filePath)
	if err != nil {
		e.PreviewError = err.Error()
		return err
	}
	defer file.Close()

	stat, _ := file.Stat()
	if stat != nil && stat.Size() > 5*1024*1024 {
		e.PreviewPath = filePath
		e.PreviewText = fmt.Sprintf("[File too large for live preview: %s]", HumanSize(uint64(stat.Size())))
		e.PreviewError = ""
		return nil
	}

	var lines []string
	scanner := bufio.NewScanner(file)
	// Allow long minified lines (default 64KB aborts with false binary error).
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lineCount := 0
	totalBytes := 0

	for scanner.Scan() {
		lineCount++
		if lineCount > maxLines {
			lines = append(lines, fmt.Sprintf("... (truncated: displaying first %d lines) ...", maxLines))
			break
		}
		text := scanner.Text()
		// NUL byte => binary.
		if strings.IndexByte(text, 0) >= 0 {
			e.PreviewPath = filePath
			e.PreviewText = "[Binary file or unrecognized encoding]"
			e.PreviewError = ""
			return nil
		}
		totalBytes += len(text)
		if totalBytes > 2*1024*1024 {
			lines = append(lines, "... (truncated: preview size limit) ...")
			break
		}
		// Format line with line number
		lines = append(lines, fmt.Sprintf("%4d │ %s", lineCount, text))
	}

	if err := scanner.Err(); err != nil {
		// Likely binary file
		e.PreviewPath = filePath
		e.PreviewText = "[Binary file or unrecognized encoding]"
		e.PreviewError = ""
		return nil
	}

	e.PreviewPath = filePath
	e.PreviewText = strings.Join(lines, "\n")
	e.PreviewError = ""
	return nil
}

// OpenInFileExplorer launches Windows Explorer focused on the current directory or file
func (e *Explorer) OpenInFileExplorer() error {
	target := e.CurrentDir
	if len(e.Entries) > 0 && e.Selected >= 0 && e.Selected < len(e.Entries) {
		target = e.Entries[e.Selected].Path
	}

	fi, err := os.Stat(target)
	if err == nil && !fi.IsDir() {
		// When target is a file, open Windows Explorer with that file selected
		cmd := exec.Command("explorer.exe", "/select,"+target)
		return cmd.Start()
	}

	cmd := exec.Command("explorer.exe", target)
	return cmd.Start()
}

// OpenInTerminal opens a new PowerShell or Windows Terminal window at CurrentDir
func (e *Explorer) OpenInTerminal() error {
	dir := e.CurrentDir

	// Try Windows Terminal (wt.exe) first
	if _, err := exec.LookPath("wt.exe"); err == nil {
		cmd := exec.Command("wt.exe", "-d", dir)
		return cmd.Start()
	}

	// Fallback to powershell.exe without cmd.exe wrapper to avoid injection.
	// Single-quote escape for PowerShell: ' -> ''.
	safe := strings.ReplaceAll(dir, "'", "''")
	cmd := exec.Command("powershell.exe", "-NoExit", "-Command", fmt.Sprintf("Set-Location -LiteralPath '%s'", safe))
	return cmd.Start()
}

// OpenInVSCode launches VS Code for the selected file or directory
func (e *Explorer) OpenInVSCode() error {
	target := e.CurrentDir
	if len(e.Entries) > 0 && e.Selected >= 0 && e.Selected < len(e.Entries) {
		target = e.Entries[e.Selected].Path
	}

	if _, err := exec.LookPath("code"); err != nil {
		return fmt.Errorf("VS Code (code) not found in PATH: %w", err)
	}
	cmd := exec.Command("code", target)
	return cmd.Start()
}

// validateSingleName rejects traversal, separators, absolute and reserved names.
func validateSingleName(name string) error {
	if name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	if filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return fmt.Errorf("invalid name %q", name)
	}
	if filepath.Clean(name) != name {
		return fmt.Errorf("invalid name %q", name)
	}
	// Windows reserved device names.
	upper := strings.ToUpper(name)
	if i := strings.IndexByte(upper, '.'); i >= 0 {
		upper = upper[:i]
	}
	switch upper {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return fmt.Errorf("reserved name %q", name)
	}
	return nil
}

// CreateDirectory creates a sub-directory in CurrentDir
func (e *Explorer) CreateDirectory(name string) error {
	name = strings.TrimSpace(name)
	if err := validateSingleName(name); err != nil {
		return err
	}

	target := filepath.Join(e.CurrentDir, name)
	if err := e.ensureInsideRepo(target); err != nil {
		return err
	}
	if err := os.Mkdir(target, 0755); err != nil {
		return err
	}

	return e.Refresh()
}

// CreateFile creates a new file in CurrentDir
func (e *Explorer) CreateFile(name string) error {
	name = strings.TrimSpace(name)
	if err := validateSingleName(name); err != nil {
		return err
	}

	target := filepath.Join(e.CurrentDir, name)
	if err := e.ensureInsideRepo(target); err != nil {
		return err
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_ = f.Close()

	return e.Refresh()
}

// DeleteSelected deletes the selected file or folder
func (e *Explorer) DeleteSelected() error {
	if len(e.Entries) == 0 || e.Selected < 0 || e.Selected >= len(e.Entries) {
		return fmt.Errorf("no entry selected")
	}

	target := e.Entries[e.Selected].Path
	if err := e.ensureInsideRepo(target); err != nil {
		return fmt.Errorf("refusing to delete outside repository: %w", err)
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}

	return e.Refresh()
}

// RelativeCurrentPath returns the path relative to RepoRoot
func (e *Explorer) RelativeCurrentPath() string {
	rel, err := filepath.Rel(e.RepoRoot, e.CurrentDir)
	if err != nil || rel == "." {
		return "/"
	}
	return "/" + filepath.ToSlash(rel)
}

// HumanSize converts bytes to a readable format
func HumanSize(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
		if exp >= 5 {
			break
		}
	}
	units := "KMGTPE"
	if exp >= len(units) {
		exp = len(units) - 1
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), units[exp])
}
