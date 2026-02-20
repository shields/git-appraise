//go:build !windows

package main

import (
	"bytes"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

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

// --- Repos Discover tests ---

func TestReposDiscover(t *testing.T) {
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

func TestReposDiscoverNonGitDir(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(dir+"/subdir", 0755)
	os.WriteFile(dir+"/afile.txt", []byte("hello\n"), 0644)

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
	if len(loaded) != 0 {
		t.Errorf("expected no repos, got %d", len(loaded))
	}
}

func TestReposDiscoverEmptyGitRepo(t *testing.T) {
	// An empty git repo (no commits) is still a valid git repo.
	// GetRepoStateHash succeeds (empty ref list → valid hash)
	// and Update completes, so the repo is discovered.
	dir := t.TempDir()
	emptyRepo := dir + "/empty-repo"
	os.Mkdir(emptyRepo, 0755)
	cmd := exec.Command("git", "init", emptyRepo)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

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
	if len(loaded) != 1 {
		t.Errorf("expected 1 repo (empty repo is still valid), got %d", len(loaded))
	}
}

func TestReposDiscoverWalkError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: root ignores file permissions")
	}
	dir := t.TempDir()
	noPerms := dir + "/noaccess"
	os.Mkdir(noPerms, 0755)
	os.WriteFile(noPerms+"/file.txt", []byte("data\n"), 0644)
	os.Chmod(noPerms, 0000)
	defer os.Chmod(noPerms, 0755)

	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)

	var repos Repos
	m := make(reposMap)
	repos.Store(&m)
	if err := repos.Discover(); err == nil {
		t.Error("expected error from Discover with inaccessible directory")
	}
}

func TestReposDiscoverGetwdError(t *testing.T) {
	old, _ := os.Getwd()
	tmpDir := t.TempDir()
	subDir := tmpDir + "/vanishing"
	os.Mkdir(subDir, 0755)
	os.Chdir(subDir)
	os.RemoveAll(subDir)
	defer os.Chdir(old)

	var repos Repos
	m := make(reposMap)
	repos.Store(&m)
	err := repos.Discover()
	if err == nil {
		t.Error("expected error when Getwd fails")
	}
}

func TestReposDiscoverUpdateError(t *testing.T) {
	dir := t.TempDir()
	repoDir := dir + "/broken-repo"
	cmd := exec.Command("git", "init", repoDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}
	// Corrupt packed-refs so GetRepoStateHash → References() fails.
	os.WriteFile(repoDir+"/.git/packed-refs", []byte("corrupt\x00data\n"), 0644)

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
	if len(loaded) != 0 {
		t.Errorf("expected 0 repos (corrupt repo skipped), got %d", len(loaded))
	}
}

// --- HTTP handler tests ---

func setupRepos(t *testing.T) *Repos {
	t.Helper()
	repo := repository.NewMockRepoForTest()
	repoDetails := web.NewRepoDetails(repo)
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

func TestServeBranchTemplateFound(t *testing.T) {
	repos := setupRepos(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test-repo/branch.html?branch=0", nil)
	req.SetPathValue("repo", "test-repo")
	repos.ServeBranchTemplate(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
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

func TestServeReviewTemplateInvalidReview(t *testing.T) {
	repos := setupRepos(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test-repo/review.html?review=abcdef1234", nil)
	req.SetPathValue("repo", "test-repo")
	repos.ServeReviewTemplate(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 for invalid review", rec.Code)
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

func TestServeReposTemplateError(t *testing.T) {
	m := reposMap{"bad": nil}
	var repos Repos
	repos.Store(&m)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/repos.html", nil)
	repos.ServeReposTemplate(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
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

// --- webServe and main tests ---

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return p
}

// TestMainAndWebServe calls main() directly (in a goroutine since it blocks)
// to cover both main() and webServe().
func TestMainAndWebServe(t *testing.T) {
	dir := setupTestGitRepo(t)
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	p := freePort(t)

	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"git-appraise-web", "-port", fmt.Sprintf("%d", p)}

	// Reset global mux and flags so main() can re-register.
	// The goroutine running main() is intentionally leaked; the server
	// shuts down when the test binary exits.
	origMux := http.DefaultServeMux
	origFlags := flag.CommandLine
	origPort := port
	defer func() {
		http.DefaultServeMux = origMux
		flag.CommandLine = origFlags
		port = origPort
	}()
	http.DefaultServeMux = http.NewServeMux()
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	port = flag.Uint("port", 0, "Web server port.")

	go main()

	addr := fmt.Sprintf("http://127.0.0.1:%d", p)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(addr + "/_ah/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Verify the health endpoint
	resp, err := http.Get(addr + "/_ah/health")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health status = %d, want 200", resp.StatusCode)
	}

	// Test redirect from /
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err = client.Get(addr + "/")
	if err != nil {
		t.Fatalf("redirect check failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Errorf("redirect status = %d, want 307", resp.StatusCode)
	}

	// Send SIGUSR1 to trigger the signal handler path
	proc, _ := os.FindProcess(os.Getpid())
	proc.Signal(syscall.SIGUSR1)
	time.Sleep(200 * time.Millisecond)
}

// TestWebServeListenError covers the error path in webServe when
// ListenAndServe fails (e.g., port already in use).
func TestWebServeListenError(t *testing.T) {
	dir := setupTestGitRepo(t)
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// Bind a port so webServe will fail with "address already in use"
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	boundPort := l.Addr().(*net.TCPAddr).Port

	// Reset the default mux to avoid duplicate registration panic
	origMux := http.DefaultServeMux
	origPort := *port
	defer func() {
		http.DefaultServeMux = origMux
		*port = origPort
	}()
	http.DefaultServeMux = http.NewServeMux()
	*port = uint(boundPort)

	// Capture stdout to verify error message
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// webServe should return quickly because ListenAndServe fails
	done := make(chan struct{})
	go func() {
		webServe()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("webServe did not return after listen error")
	}

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
	if !strings.Contains(buf.String(), "Error:") {
		t.Errorf("expected error output, got %q", buf.String())
	}
}
