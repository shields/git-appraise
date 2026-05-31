//go:build !windows

package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"msrl.dev/git-appraise/commands/web"
	"msrl.dev/git-appraise/repository"
)

// gitEnv is the process environment with all GIT_* variables removed. Git
// exports GIT_DIR, GIT_INDEX_FILE, GIT_PREFIX, etc. to hooks it invokes; when
// these tests run inside a pre-commit hook (e.g. via lefthook) those variables
// leak into the child `git` processes below and make them operate on the outer
// repository instead of the per-test temp dir. Scrubbing them keeps every
// command scoped to its intended -C directory.
func gitEnv() []string {
	env := os.Environ()
	cleaned := env[:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, "GIT_") {
			cleaned = append(cleaned, kv)
		}
	}
	return cleaned
}

func runGit(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = gitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func setupTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	initTestGitRepo(t, dir)
	return dir
}

// initTestGitRepo initializes a git repository with a single commit in dir.
func initTestGitRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, "init", dir)
	runGit(t, "-C", dir, "config", "user.email", "test@test.com")
	runGit(t, "-C", dir, "config", "user.name", "Test")
	if err := os.WriteFile(dir+"/README.md", []byte("# Test Repo\nSome body.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, "-C", dir, "add", ".")
	runGit(t, "-C", dir, "commit", "-m", "initial")
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

// TestReposDiscoverPathMatchesKey guards against regressing the bug where the
// discovered RepoDetails.Path was the repo's absolute filesystem root while the
// map was keyed by the cwd-relative name. That mismatch both leaked the
// server's directory layout in the repos.html links and produced links that
// could not route back to the map entry.
func TestReposDiscoverPathMatchesKey(t *testing.T) {
	// Discover from a dedicated parent directory holding a single repo in a
	// named subdirectory, so the repo is found as a child whose cwd-relative
	// name (the map key) is a single path segment.
	parent := t.TempDir()
	name := "myrepo"
	repoDir := filepath.Join(parent, name)
	if err := os.Mkdir(repoDir, 0755); err != nil {
		t.Fatal(err)
	}
	initTestGitRepo(t, repoDir)

	old, _ := os.Getwd()
	if err := os.Chdir(parent); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)

	var repos Repos
	repos.Store(new(reposMap))
	if err := repos.Discover(); err != nil {
		t.Fatal(err)
	}
	loaded := repos.Load()
	details, ok := loaded[name]
	if !ok {
		t.Fatalf("expected repo keyed by relative name %q, got map %v", name, loaded)
	}
	if details.Path != name {
		t.Errorf("RepoDetails.Path = %q, want %q (must equal the map key)", details.Path, name)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/repos.html", nil)
	repos.ServeReposTemplate(rec, req)
	body := rec.Body.String()
	if strings.Contains(body, parent) {
		t.Errorf("repos.html leaks absolute server path %q in:\n%s", parent, body)
	}
	if !strings.Contains(body, `href="`+name+`/repo.html"`) {
		t.Errorf("expected relative repo link in repos.html, got:\n%s", body)
	}
}

// TestReposDiscoverCwdIsRepo covers running the server from inside a repo: the
// cwd-relative path is ".", which HTTP clients normalize away, so the entry
// must instead be keyed by the directory's base name to stay reachable.
func TestReposDiscoverCwdIsRepo(t *testing.T) {
	repoDir := setupTestGitRepo(t)
	name := filepath.Base(repoDir)

	old, _ := os.Getwd()
	if err := os.Chdir(repoDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)

	var repos Repos
	repos.Store(new(reposMap))
	if err := repos.Discover(); err != nil {
		t.Fatal(err)
	}
	loaded := repos.Load()
	if _, ok := loaded["."]; ok {
		t.Errorf(`repo keyed by ".", which is not addressable; got map %v`, loaded)
	}
	details, ok := loaded[name]
	if !ok {
		t.Fatalf("expected repo keyed by base name %q, got map %v", name, loaded)
	}
	if details.Path != name {
		t.Errorf("RepoDetails.Path = %q, want %q", details.Path, name)
	}
}

func TestReposDiscoverNestedRepoSkipped(t *testing.T) {
	// A repo nested more than one level below cwd cannot be addressed by the
	// single-segment URL scheme, so Discover must skip it rather than list an
	// unreachable entry.
	parent := t.TempDir()
	sub := filepath.Join(parent, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	repoDir := filepath.Join(sub, "deep")
	if err := os.Mkdir(repoDir, 0755); err != nil {
		t.Fatal(err)
	}
	initTestGitRepo(t, repoDir)

	old, _ := os.Getwd()
	if err := os.Chdir(parent); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)

	var repos Repos
	repos.Store(new(reposMap))
	if err := repos.Discover(); err != nil {
		t.Fatal(err)
	}
	if loaded := repos.Load(); len(loaded) != 0 {
		t.Errorf("expected the nested repo to be skipped, got map %v", loaded)
	}
}

func TestDiscoverAndLogError(t *testing.T) {
	// A discovery failure must be logged rather than panicking or being
	// silently swallowed.
	origGetwd := getwdFn
	defer func() { getwdFn = origGetwd }()
	getwdFn = func() (string, error) {
		return "", errors.New("simulated getwd failure")
	}
	var repos Repos
	repos.Store(new(reposMap))
	discoverAndLog(&repos) // must not panic
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
	runGit(t, "init", emptyRepo)

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
	origGetwd := getwdFn
	defer func() { getwdFn = origGetwd }()
	getwdFn = func() (string, error) {
		return "", errors.New("simulated getwd failure")
	}

	var repos Repos
	m := make(reposMap)
	repos.Store(&m)
	if err := repos.Discover(); err == nil {
		t.Error("expected error when Getwd fails")
	}
}

func TestReposDiscoverUpdateError(t *testing.T) {
	dir := t.TempDir()
	repoDir := dir + "/broken-repo"
	runGit(t, "init", repoDir)
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

	// Bind a port so webServe will fail with "address already in use".
	// Use the wildcard address — binding on 127.0.0.1:N does not conflict
	// with webServe's 0.0.0.0:N listen on Darwin.
	l, err := net.Listen("tcp", ":0")
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
