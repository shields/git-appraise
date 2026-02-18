package main

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
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
	// subcommand.Usage writes to os.Stdout directly, so capture it.
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
