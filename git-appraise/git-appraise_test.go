package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"msrl.dev/git-appraise/commands"
)

func setupTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"git", "init", dir},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	if err := os.WriteFile(dir+"/README.md", []byte("test repo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "-C", dir, "add", "."},
		{"git", "-C", dir, "commit", "-m", "initial"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	return dir
}

func TestPrintUsage(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf, "git-appraise")
	out := buf.String()
	if !strings.Contains(out, "git-appraise") {
		t.Errorf("expected 'git-appraise' in usage, got %q", out)
	}
	if !strings.Contains(out, "list") {
		t.Errorf("expected 'list' subcommand in usage, got %q", out)
	}
}

func TestPrintHelpNoSubcommand(t *testing.T) {
	var buf bytes.Buffer
	printHelp(&buf, []string{"git-appraise", "help"})
	if !strings.Contains(buf.String(), "Usage:") {
		t.Errorf("expected usage output, got %q", buf.String())
	}
}

func TestPrintHelpValidSubcommand(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	printHelp(w, []string{"git-appraise", "help", "list"})
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
	if !strings.Contains(buf.String(), "list") {
		t.Errorf("expected 'list' in help output, got %q", buf.String())
	}
}

func TestPrintHelpUnknownSubcommand(t *testing.T) {
	var buf bytes.Buffer
	printHelp(&buf, []string{"git-appraise", "help", "nonexistent"})
	if !strings.Contains(buf.String(), "Unknown command") {
		t.Errorf("expected 'Unknown command' in output, got %q", buf.String())
	}
}

func TestRunNotGitRepo(t *testing.T) {
	var buf bytes.Buffer
	err := run(&buf, []string{"git-appraise", "list"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "git repo") {
		t.Errorf("expected 'git repo' error, got %v", err)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	dir := setupTestGitRepo(t)
	var buf bytes.Buffer
	err := run(&buf, []string{"git-appraise", "nonexistent"}, dir)
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("expected 'unknown command' error, got %v", err)
	}
	if !strings.Contains(buf.String(), "Usage:") {
		t.Errorf("expected usage in output, got %q", buf.String())
	}
}

func TestRunListDefault(t *testing.T) {
	dir := setupTestGitRepo(t)
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := run(w, []string{"git-appraise"}, dir)
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunListExplicit(t *testing.T) {
	dir := setupTestGitRepo(t)
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := run(w, []string{"git-appraise", "list"}, dir)
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunListNotInCommandMap(t *testing.T) {
	dir := setupTestGitRepo(t)
	saved := commands.CommandMap["list"]
	delete(commands.CommandMap, "list")
	defer func() { commands.CommandMap["list"] = saved }()

	var buf bytes.Buffer
	err := run(&buf, []string{"git-appraise"}, dir)
	if err == nil || !strings.Contains(err.Error(), "unable to list reviews") {
		t.Errorf("expected 'unable to list reviews' error, got %v", err)
	}
}

func TestMainHelpPath(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	os.Args = []string{"git-appraise", "help"}
	main()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
	if !strings.Contains(buf.String(), "Usage:") {
		t.Errorf("expected Usage in output, got %q", buf.String())
	}
}

func TestMainHelpWithSubcommand(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	os.Args = []string{"git-appraise", "help", "list"}
	main()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
	if !strings.Contains(buf.String(), "list") {
		t.Errorf("expected 'list' in output, got %q", buf.String())
	}
}

func TestMainSuccessfulRun(t *testing.T) {
	dir := setupTestGitRepo(t)
	origArgs := os.Args
	origDir, _ := os.Getwd()
	defer func() {
		os.Args = origArgs
		os.Chdir(origDir)
	}()

	os.Chdir(dir)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	os.Args = []string{"git-appraise"}
	main()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
}

// TestMainGetwdError tests the os.Exit(1) path via subprocess.
func TestMainGetwdError(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainGetwdErrorHelper$")
	cmd.Env = append(os.Environ(), "TEST_GETWD_ERROR=1")
	cmd.Dir = t.TempDir()
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected non-zero exit code")
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitCode() != 1 {
		t.Errorf("expected exit code 1, got %d", exitErr.ExitCode())
	}
	if !strings.Contains(string(out), "Unable to get the current working directory") {
		t.Errorf("expected Getwd error message, got %q", string(out))
	}
}

func TestMainGetwdErrorHelper(t *testing.T) {
	if os.Getenv("TEST_GETWD_ERROR") != "1" {
		return
	}
	getwdFn = func() (string, error) {
		return "", errors.New("simulated getwd failure")
	}
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"git-appraise"}
	main()
}

// TestMainRunErrorSubprocess tests the os.Exit(1) path via subprocess.
func TestMainRunErrorSubprocess(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainRunErrorHelper$")
	cmd.Env = append(os.Environ(), "TEST_MAIN_RUN_ERROR=1")
	cmd.Dir = t.TempDir()
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit code")
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitCode() != 1 {
		t.Errorf("expected exit code 1, got %d", exitErr.ExitCode())
	}
}

func TestMainRunErrorHelper(t *testing.T) {
	if os.Getenv("TEST_MAIN_RUN_ERROR") != "1" {
		return
	}
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"git-appraise", "list"}
	main()
}
