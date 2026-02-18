// New test file; license header intentionally omitted per project guidelines.

//go:build !windows

package input

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"msrl.dev/git-appraise/repository"
)

func TestFromFileRegular(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "msg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("hello from file"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	got, err := FromFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello from file" {
		t.Errorf("FromFile = %q, want %q", got, "hello from file")
	}
}

func TestFromFileNonexistent(t *testing.T) {
	_, err := FromFile("/nonexistent/path/file.txt")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

// TestFromFileStdinPipe tests reading from stdin via pipe.
// Not safe for t.Parallel() since it replaces os.Stdin.
func TestFromFileStdinPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()

	go func() {
		defer w.Close()
		if _, err := w.WriteString("piped input\n"); err != nil {
			return
		}
	}()

	got, err := FromFile("-")
	if err != nil {
		t.Fatal(err)
	}
	if got != "piped input\n" {
		t.Errorf("FromFile('-') = %q, want %q", got, "piped input\n")
	}
}

// editorRepo provides a custom editor command and data directory.
type editorRepo struct {
	repository.Repo
	editor  string
	dataDir string
}

func (r editorRepo) GetCoreEditor() (string, error) { return r.editor, nil }
func (r editorRepo) GetDataDir() (string, error)    { return r.dataDir, nil }

type errEditorRepo struct{ repository.Repo }

func (r errEditorRepo) GetCoreEditor() (string, error) {
	return "", fmt.Errorf("no editor configured")
}

type errDataDirRepo struct{ repository.Repo }

func (r errDataDirRepo) GetDataDir() (string, error) {
	return "", fmt.Errorf("no data dir")
}

func writeScript(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLaunchEditor(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "test-editor.sh",
		"#!/bin/sh\necho 'editor output' > \"$1\"\n")

	repo := editorRepo{
		Repo:    repository.NewMockRepoForTest(),
		editor:  script,
		dataDir: dir,
	}

	got, err := LaunchEditor(repo, "COMMENT_EDITMSG")
	if err != nil {
		t.Fatal(err)
	}
	if got != "editor output\n" {
		t.Errorf("LaunchEditor = %q, want %q", got, "editor output\n")
	}
}

func TestLaunchEditorShellFallback(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "test-editor.sh",
		"#!/bin/sh\necho 'shell editor' > \"$1\"\n")

	// "sh <script>" is not a valid direct executable path, so
	// startInlineCommand fails and the function falls back to running
	// through bash/sh. Use %q so paths with spaces are handled correctly.
	repo := editorRepo{
		Repo:    repository.NewMockRepoForTest(),
		editor:  fmt.Sprintf("sh %q", script),
		dataDir: dir,
	}

	got, err := LaunchEditor(repo, "COMMENT_EDITMSG")
	if err != nil {
		t.Fatal(err)
	}
	if got != "shell editor\n" {
		t.Errorf("LaunchEditor = %q, want %q", got, "shell editor\n")
	}
}

func TestLaunchEditorGetCoreEditorError(t *testing.T) {
	repo := errEditorRepo{repository.NewMockRepoForTest()}
	_, err := LaunchEditor(repo, "COMMENT_EDITMSG")
	if err == nil {
		t.Error("expected error for bad editor config")
	}
}

func TestLaunchEditorGetDataDirError(t *testing.T) {
	repo := errDataDirRepo{repository.NewMockRepoForTest()}
	_, err := LaunchEditor(repo, "COMMENT_EDITMSG")
	if err == nil {
		t.Error("expected error for bad data dir")
	}
}

func TestLaunchEditorBadCommand(t *testing.T) {
	dir := t.TempDir()
	repo := editorRepo{
		Repo:    repository.NewMockRepoForTest(),
		editor:  "/nonexistent/editor/binary",
		dataDir: dir,
	}

	_, err := LaunchEditor(repo, "COMMENT_EDITMSG")
	if err == nil {
		t.Error("expected error for bad editor command")
	}
}

func TestLaunchEditorExitError(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "bad-editor.sh", "#!/bin/sh\nexit 1\n")

	repo := editorRepo{
		Repo:    repository.NewMockRepoForTest(),
		editor:  script,
		dataDir: dir,
	}

	_, err := LaunchEditor(repo, "COMMENT_EDITMSG")
	if err == nil {
		t.Error("expected error for editor exit failure")
	}
}

func TestLaunchEditorNoOutputFile(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "noop-editor.sh", "#!/bin/sh\nexit 0\n")

	repo := editorRepo{
		Repo:    repository.NewMockRepoForTest(),
		editor:  script,
		dataDir: dir,
	}

	_, err := LaunchEditor(repo, "COMMENT_EDITMSG")
	if err == nil {
		t.Error("expected error when editor doesn't create file")
	}
}

func TestStartInlineCommand(t *testing.T) {
	cmd, err := startInlineCommand("true")
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestStartInlineCommandBadBinary(t *testing.T) {
	_, err := startInlineCommand("/nonexistent/binary")
	if err == nil {
		t.Error("expected error for nonexistent binary")
	}
}
