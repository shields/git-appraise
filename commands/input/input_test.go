// New test file; license header intentionally omitted per project guidelines.

//go:build !windows

package input

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"os/exec"

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
	cmd, err := startInlineCommandImpl("true")
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestStartInlineCommandBadBinary(t *testing.T) {
	_, err := startInlineCommandImpl("/nonexistent/binary")
	if err == nil {
		t.Error("expected error for nonexistent binary")
	}
}

// --- LaunchEditor sh fallback and total failure ---

func TestLaunchEditorShFallback(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "test-editor.sh",
		"#!/bin/sh\necho 'sh fallback' > \"$1\"\n")

	callCount := 0
	orig := startInlineCommand
	defer func() { startInlineCommand = orig }()
	startInlineCommand = func(command string, args ...string) (*exec.Cmd, error) {
		callCount++
		switch callCount {
		case 1:
			// Direct editor invocation: fail
			return nil, fmt.Errorf("direct exec failed")
		case 2:
			// bash fallback: fail
			return nil, fmt.Errorf("bash not found")
		default:
			// sh fallback: succeed
			return startInlineCommandImpl(command, args...)
		}
	}

	repo := editorRepo{
		Repo:    repository.NewMockRepoForTest(),
		editor:  fmt.Sprintf("sh %q", script),
		dataDir: dir,
	}

	got, err := LaunchEditor(repo, "COMMENT_EDITMSG")
	if err != nil {
		t.Fatal(err)
	}
	if got != "sh fallback\n" {
		t.Errorf("LaunchEditor = %q, want %q", got, "sh fallback\n")
	}
}

func TestLaunchEditorAllFallbacksFail(t *testing.T) {
	dir := t.TempDir()

	orig := startInlineCommand
	defer func() { startInlineCommand = orig }()
	startInlineCommand = func(command string, args ...string) (*exec.Cmd, error) {
		return nil, fmt.Errorf("all commands fail")
	}

	repo := editorRepo{
		Repo:    repository.NewMockRepoForTest(),
		editor:  "/nonexistent/editor",
		dataDir: dir,
	}

	_, err := LaunchEditor(repo, "COMMENT_EDITMSG")
	if err == nil {
		t.Error("expected error when all fallbacks fail")
	}
}

// --- FromFile stdin Stat error ---

func TestFromFileStdinStatError(t *testing.T) {
	// Create a pipe and close the read end, then replace os.Stdin with it
	r, _, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	r.Close() // Close it so Stat fails

	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()

	_, err = FromFile("-")
	if err == nil {
		t.Error("expected error from FromFile with closed stdin")
	}
}

// --- FromFile stdin ReadAll error ---

func TestFromFileStdinReadError(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	w.Close()

	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()

	origReadAll := readAllStdin
	defer func() { readAllStdin = origReadAll }()
	readAllStdin = func() ([]byte, error) {
		return nil, fmt.Errorf("simulated read error")
	}

	_, err = FromFile("-")
	if err == nil {
		t.Error("expected error from FromFile with failing ReadAll")
	}
}

// --- FromFile TTY interactive path ---

func TestFromFileStdinTTY(t *testing.T) {
	// /dev/null is a character device on Linux that returns EOF immediately.
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Skip("cannot open /dev/null: " + err.Error())
	}
	defer devNull.Close()

	stat, err := devNull.Stat()
	if err != nil {
		t.Skip("cannot stat /dev/null")
	}
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		t.Skip("/dev/null is not a character device on this platform")
	}

	old := os.Stdin
	os.Stdin = devNull
	defer func() { os.Stdin = old }()

	oldStdout := os.Stdout
	devNull2, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull2.Close()
	os.Stdout = devNull2
	defer func() { os.Stdout = oldStdout }()

	got, err := FromFile("-")
	if err != nil {
		t.Fatalf("FromFile('-') error: %v", err)
	}
	if got != "" {
		t.Errorf("FromFile('-') = %q, want empty string", got)
	}
}

func TestFromFileStdinTTYWithData(t *testing.T) {
	// /dev/null is a character device, so Stat reports ModeCharDevice,
	// triggering the TTY/scanner path in FromFile.
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Skip("cannot open /dev/null: " + err.Error())
	}
	defer devNull.Close()

	stat, err := devNull.Stat()
	if err != nil {
		t.Skip("cannot stat /dev/null")
	}
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		t.Skip("/dev/null is not a character device on this platform")
	}

	old := os.Stdin
	os.Stdin = devNull
	defer func() { os.Stdin = old }()

	oldStdout := os.Stdout
	devNull2, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull2.Close()
	os.Stdout = devNull2
	defer func() { os.Stdout = oldStdout }()

	// Override newTTYScanner to provide a scanner backed by a pipe with data,
	// so the scanner loop body (lines 106-109) is exercised.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	origScanner := newTTYScanner
	defer func() { newTTYScanner = origScanner }()
	newTTYScanner = func() *bufio.Scanner {
		return bufio.NewScanner(r)
	}

	go func() {
		defer w.Close()
		w.WriteString("hello from tty\nsecond line\n")
	}()

	got, err := FromFile("-")
	if err != nil {
		t.Fatalf("FromFile('-') error: %v", err)
	}
	if got != "hello from tty\nsecond line\n" {
		t.Errorf("FromFile('-') = %q, want %q", got, "hello from tty\nsecond line\n")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errors.New("simulated scan error")
}

func TestFromFileStdinTTYScannerError(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Skip("cannot open /dev/null: " + err.Error())
	}
	defer devNull.Close()
	stat, err := devNull.Stat()
	if err != nil {
		t.Skip("cannot stat /dev/null")
	}
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		t.Skip("/dev/null is not a character device on this platform")
	}

	old := os.Stdin
	os.Stdin = devNull
	defer func() { os.Stdin = old }()

	oldStdout := os.Stdout
	devNull2, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull2.Close()
	os.Stdout = devNull2
	defer func() { os.Stdout = oldStdout }()

	origScanner := newTTYScanner
	defer func() { newTTYScanner = origScanner }()
	newTTYScanner = func() *bufio.Scanner {
		// Reader that returns an error on first read, simulating a scan failure
		r := io.MultiReader(strings.NewReader("partial\n"), errReader{})
		return bufio.NewScanner(r)
	}

	_, err = FromFile("-")
	if err == nil {
		t.Fatal("expected error from scanner")
	}
}
