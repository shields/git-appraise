package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"msrl.dev/git-appraise/commands/web"
	"msrl.dev/git-appraise/repository"
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
	if err := os.WriteFile(dir+"/README.md", []byte("# Test Repo\nSome body.\n"), 0644); err != nil {
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

// --- ServeMultiPaths tests ---

func TestServeMultiPaths(t *testing.T) {
	var p ServeMultiPaths
	if got := p.Css(); got != "/stylesheet.css" {
		t.Errorf("Css() = %q", got)
	}
	if got := p.Repo(); got != "repo.html" {
		t.Errorf("Repo() = %q", got)
	}
	if got := p.Branch(5); !strings.Contains(got, "branch=5") {
		t.Errorf("Branch(5) = %q", got)
	}
	if got := p.Review("abc"); !strings.Contains(got, "review=abc") {
		t.Errorf("Review('abc') = %q", got)
	}
}

// --- Repos Load/Store tests ---

func TestReposLoadStore(t *testing.T) {
	var repos Repos
	m := make(reposMap)
	repos.Store(&m)
	loaded := repos.Load()
	if loaded == nil {
		t.Error("expected non-nil repos map")
	}
	if len(loaded) != 0 {
		t.Errorf("expected empty map, got %d entries", len(loaded))
	}
}

// --- Repos Discover test ---

func TestReposDiscover(t *testing.T) {
	// Not parallel: Discover uses os.Getwd internally, so we must chdir.
	dir := setupTestGitRepo(t)
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)

	var repos Repos
	m := make(reposMap)
	repos.Store(&m)
	if err := repos.Discover(); err != nil {
		t.Fatal(err)
	}
	loaded := repos.Load()
	if len(loaded) == 0 {
		t.Error("expected at least one repo discovered")
	}
}

// --- HTTP handler tests ---

func setupRepos(t *testing.T) *Repos {
	t.Helper()
	repo := repository.NewMockRepoForTest()
	repoDetails, err := web.NewRepoDetails(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := repoDetails.Update(); err != nil {
		t.Fatal(err)
	}
	m := reposMap{"test-repo": repoDetails}
	var repos Repos
	repos.Store(&m)
	return &repos
}

func TestServeStyleSheetHTTP(t *testing.T) {
	repos := setupRepos(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stylesheet.css", nil)
	repos.ServeStyleSheet(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestServeRepoTemplateFound(t *testing.T) {
	repos := setupRepos(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test-repo/repo.html", nil)
	req.SetPathValue("repo", "test-repo")
	repos.ServeRepoTemplate(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestServeRepoTemplateNotFound(t *testing.T) {
	repos := setupRepos(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/missing/repo.html", nil)
	req.SetPathValue("repo", "missing")
	repos.ServeRepoTemplate(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestServeBranchTemplateNotFound(t *testing.T) {
	repos := setupRepos(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/missing/branch.html", nil)
	req.SetPathValue("repo", "missing")
	repos.ServeBranchTemplate(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestServeReviewTemplateNotFound(t *testing.T) {
	repos := setupRepos(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/missing/review.html", nil)
	req.SetPathValue("repo", "missing")
	repos.ServeReviewTemplate(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestServeReposTemplate(t *testing.T) {
	repos := setupRepos(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/repos.html", nil)
	repos.ServeReposTemplate(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

func TestServeEntryPointRedirect(t *testing.T) {
	repos := setupRepos(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	repos.ServeEntryPointRedirect(rec, req)
	if rec.Code != http.StatusTemporaryRedirect {
		t.Errorf("status = %d, want 307", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/repos.html" {
		t.Errorf("Location = %q, want '/repos.html'", loc)
	}
}

func TestServeReposTemplateEmpty(t *testing.T) {
	m := make(reposMap)
	var repos Repos
	repos.Store(&m)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/repos.html", nil)
	repos.ServeReposTemplate(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// --- WriteStyleSheet via buffer ---

func TestWriteStyleSheetViaBuffer(t *testing.T) {
	var buf bytes.Buffer
	if err := web.WriteStyleSheet(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty stylesheet")
	}
}
