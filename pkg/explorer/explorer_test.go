package explorer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplorer_NavigationAndPreview(t *testing.T) {
	tempRoot, err := os.MkdirTemp("", "explorer_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempRoot)

	// Create test structure:
	//   file1.txt
	//   subdir/
	//     nested.go
	sub := filepath.Join(tempRoot, "subdir")
	_ = os.MkdirAll(sub, 0755)
	_ = os.WriteFile(filepath.Join(tempRoot, "file1.txt"), []byte("line1\nline2\nline3\n"), 0644)
	_ = os.WriteFile(filepath.Join(sub, "nested.go"), []byte("package main\n"), 0644)

	exp, err := NewExplorer(tempRoot)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}

	// Should list file1.txt and subdir
	if len(exp.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(exp.Entries))
	}

	// Subdir should be first due to directory sorting
	if !exp.Entries[0].IsDir || exp.Entries[0].Name != "subdir" {
		t.Errorf("expected first entry to be 'subdir', got %+v", exp.Entries[0])
	}

	// Enter subdir
	exp.Selected = 0
	isDir, err := exp.OpenSelected()
	if err != nil {
		t.Fatalf("OpenSelected failed: %v", err)
	}
	if !isDir {
		t.Fatalf("expected OpenSelected to return isDir=true")
	}

	if exp.RelativeCurrentPath() != "/subdir" {
		t.Errorf("expected relative path '/subdir', got %q", exp.RelativeCurrentPath())
	}

	// Go back up
	if !exp.GoUp() {
		t.Errorf("expected GoUp to succeed")
	}
	if exp.RelativeCurrentPath() != "/" {
		t.Errorf("expected relative path '/', got %q", exp.RelativeCurrentPath())
	}

	// Cannot go up beyond root
	if exp.GoUp() {
		t.Errorf("expected GoUp to fail at root")
	}

	// Test File Creation
	if err := exp.CreateFile("newfile.md"); err != nil {
		t.Fatalf("CreateFile failed: %v", err)
	}
	found := false
	for _, e := range exp.Entries {
		if e.Name == "newfile.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'newfile.md' to be found in entries")
	}

	// Test Preview
	if err := exp.LoadPreview(filepath.Join(tempRoot, "file1.txt"), 10); err != nil {
		t.Fatalf("LoadPreview failed: %v", err)
	}
	if !strings.Contains(exp.PreviewText, "line1") {
		t.Errorf("expected preview to contain 'line1', got %q", exp.PreviewText)
	}
}
