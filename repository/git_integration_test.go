package repository

import (
	exec "golang.org/x/sys/execabs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func setupTestRepo(t *testing.T) *GitRepo {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "file.txt")
	gitRun(t, dir, "commit", "-m", "initial commit")

	repo, err := NewGitRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func addCommit(t *testing.T, repo *GitRepo, filename, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo.Path, filename), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo.Path, "add", filename)
	gitRun(t, repo.Path, "commit", "-m", message)
}

func TestGitRepoNewGitRepo(t *testing.T) {
	repo := setupTestRepo(t)
	if repo.GetPath() == "" {
		t.Fatal("expected non-empty path")
	}
}

func TestGitRepoNewGitRepoInvalid(t *testing.T) {
	dir := t.TempDir()
	_, err := NewGitRepo(dir)
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}
}

func TestGitRepoGetPath(t *testing.T) {
	repo := setupTestRepo(t)
	if repo.GetPath() == "" {
		t.Fatal("expected non-empty path")
	}
}

func TestGitRepoGetDataDir(t *testing.T) {
	repo := setupTestRepo(t)
	dir, err := repo.GetDataDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(dir, ".git") {
		t.Fatalf("expected .git dir, got %q", dir)
	}
}

func TestGitRepoGetRepoStateHash(t *testing.T) {
	repo := setupTestRepo(t)
	hash, err := repo.GetRepoStateHash()
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("expected non-empty state hash")
	}
}

func TestGitRepoGetUserEmail(t *testing.T) {
	repo := setupTestRepo(t)
	email, err := repo.GetUserEmail()
	if err != nil {
		t.Fatal(err)
	}
	if email != "test@example.com" {
		t.Fatalf("unexpected email: %q", email)
	}
}

func TestGitRepoGetSubmitStrategy(t *testing.T) {
	repo := setupTestRepo(t)
	strategy, err := repo.GetSubmitStrategy()
	if err != nil {
		t.Fatal(err)
	}
	if strategy != "" {
		t.Fatalf("unexpected strategy: %q", strategy)
	}
}

func TestGitRepoHasUncommittedChanges(t *testing.T) {
	repo := setupTestRepo(t)
	has, err := repo.HasUncommittedChanges()
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("expected no uncommitted changes")
	}
	if err := os.WriteFile(filepath.Join(repo.Path, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	has, err = repo.HasUncommittedChanges()
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected uncommitted changes")
	}
}

func TestGitRepoHasRef(t *testing.T) {
	repo := setupTestRepo(t)
	has, err := repo.HasRef("refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected main ref to exist")
	}
	has, err = repo.HasRef("refs/heads/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("expected nonexistent ref to not exist")
	}
}

func TestGitRepoVerifyCommit(t *testing.T) {
	repo := setupTestRepo(t)
	hash, _ := repo.GetCommitHash("HEAD")
	if err := repo.VerifyCommit(hash); err != nil {
		t.Fatal(err)
	}
	if err := repo.VerifyCommit("0000000000000000000000000000000000000000"); err == nil {
		t.Fatal("expected error for nonexistent commit")
	}
}

func TestGitRepoVerifyGitRef(t *testing.T) {
	repo := setupTestRepo(t)
	if err := repo.VerifyGitRef("refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	if err := repo.VerifyGitRef("refs/heads/nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent ref")
	}
}

func TestGitRepoGetHeadRef(t *testing.T) {
	repo := setupTestRepo(t)
	head, err := repo.GetHeadRef()
	if err != nil {
		t.Fatal(err)
	}
	if head != "refs/heads/main" {
		t.Fatalf("unexpected head: %q", head)
	}
}

func TestGitRepoGetCommitHash(t *testing.T) {
	repo := setupTestRepo(t)
	hash, err := repo.GetCommitHash("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != 40 {
		t.Fatalf("unexpected hash length: %d (%q)", len(hash), hash)
	}
}

func TestGitRepoGetCommitMessage(t *testing.T) {
	repo := setupTestRepo(t)
	msg, err := repo.GetCommitMessage("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "initial commit") {
		t.Fatalf("unexpected message: %q", msg)
	}
}

func TestGitRepoGetCommitTime(t *testing.T) {
	repo := setupTestRepo(t)
	time, err := repo.GetCommitTime("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if time == "" {
		t.Fatal("expected non-empty time")
	}
}

func TestGitRepoGetLastParent(t *testing.T) {
	repo := setupTestRepo(t)
	parent, err := repo.GetLastParent("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if parent != "" {
		t.Fatalf("expected empty parent for initial commit, got %q", parent)
	}
}

func TestGitRepoGetCommitDetails(t *testing.T) {
	repo := setupTestRepo(t)
	details, err := repo.GetCommitDetails("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if details.Author != "Test" {
		t.Fatalf("unexpected author: %q", details.Author)
	}
	if details.AuthorEmail != "test@example.com" {
		t.Fatalf("unexpected email: %q", details.AuthorEmail)
	}
	if details.Summary != "initial commit" {
		t.Fatalf("unexpected summary: %q", details.Summary)
	}
}

func TestGitRepoIsAncestor(t *testing.T) {
	repo := setupTestRepo(t)
	firstHash, _ := repo.GetCommitHash("HEAD")
	addCommit(t, repo, "file.txt", "updated\n", "second commit")
	secondHash, _ := repo.GetCommitHash("HEAD")

	is, err := repo.IsAncestor(firstHash, secondHash)
	if err != nil {
		t.Fatal(err)
	}
	if !is {
		t.Fatal("first should be ancestor of second")
	}
	is, err = repo.IsAncestor(secondHash, firstHash)
	if err != nil {
		t.Fatal(err)
	}
	if is {
		t.Fatal("second should not be ancestor of first")
	}
}

func TestGitRepoMergeBase(t *testing.T) {
	repo := setupTestRepo(t)
	firstHash, _ := repo.GetCommitHash("HEAD")
	gitRun(t, repo.Path, "checkout", "-b", "feature")
	addCommit(t, repo, "feature.txt", "feature\n", "feature commit")
	featureHash, _ := repo.GetCommitHash("HEAD")

	base, err := repo.MergeBase(firstHash, featureHash)
	if err != nil {
		t.Fatal(err)
	}
	if base != firstHash {
		t.Fatalf("expected merge base %q, got %q", firstHash, base)
	}
}

func TestGitRepoDiff(t *testing.T) {
	repo := setupTestRepo(t)
	firstHash, _ := repo.GetCommitHash("HEAD")
	addCommit(t, repo, "file.txt", "changed\n", "second")
	secondHash, _ := repo.GetCommitHash("HEAD")

	diff, err := repo.Diff(firstHash, secondHash)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "changed") {
		t.Fatalf("expected diff to contain 'changed', got %q", diff)
	}
}

func TestGitRepoDiff1(t *testing.T) {
	repo := setupTestRepo(t)
	addCommit(t, repo, "file.txt", "changed\n", "second")
	secondHash, _ := repo.GetCommitHash("HEAD")

	diff, err := repo.Diff1(secondHash)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "changed") {
		t.Fatalf("expected diff to contain 'changed', got %q", diff)
	}
}

func TestGitRepoParsedDiff(t *testing.T) {
	repo := setupTestRepo(t)
	firstHash, _ := repo.GetCommitHash("HEAD")
	addCommit(t, repo, "file.txt", "changed\n", "second")
	secondHash, _ := repo.GetCommitHash("HEAD")

	fileDiffs, err := repo.ParsedDiff(firstHash, secondHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(fileDiffs) != 1 {
		t.Fatalf("expected 1 file diff, got %d", len(fileDiffs))
	}
}

func TestGitRepoShow(t *testing.T) {
	repo := setupTestRepo(t)
	content, err := repo.Show("HEAD", "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "initial") {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestGitRepoListCommits(t *testing.T) {
	repo := setupTestRepo(t)
	commits := repo.ListCommits("HEAD")
	if len(commits) == 0 {
		t.Fatal("expected at least one commit")
	}
}

func TestGitRepoListCommitsBetween(t *testing.T) {
	repo := setupTestRepo(t)
	firstHash, _ := repo.GetCommitHash("HEAD")
	addCommit(t, repo, "file.txt", "changed\n", "second")
	secondHash, _ := repo.GetCommitHash("HEAD")

	commits, err := repo.ListCommitsBetween(firstHash, secondHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected 1 commit between, got %d", len(commits))
	}
	if commits[0] != secondHash {
		t.Fatalf("expected %q, got %q", secondHash, commits[0])
	}
}

func TestGitRepoStoreBlob(t *testing.T) {
	repo := setupTestRepo(t)
	hash, err := repo.StoreBlob("blob content")
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != 40 {
		t.Fatalf("unexpected hash length: %d", len(hash))
	}
	has, _ := repo.HasObject(hash)
	if !has {
		t.Fatal("stored blob should exist")
	}
}

func TestGitRepoStoreAndReadTree(t *testing.T) {
	repo := setupTestRepo(t)
	contents := map[string]TreeChild{
		"hello.txt": NewBlob("hello world"),
	}
	hash, err := repo.StoreTree(contents)
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != 40 {
		t.Fatalf("unexpected hash length: %d", len(hash))
	}
	tree, err := repo.ReadTree(hash)
	if err != nil {
		t.Fatal(err)
	}
	tc := tree.Contents()
	if len(tc) != 1 {
		t.Fatalf("expected 1 child, got %d", len(tc))
	}
	if _, ok := tc["hello.txt"]; !ok {
		t.Fatal("expected hello.txt in tree")
	}
}

func TestGitRepoCreateCommit(t *testing.T) {
	repo := setupTestRepo(t)
	parentHash, _ := repo.GetCommitHash("HEAD")
	treeHash, _ := repo.runGitCommand("rev-parse", "HEAD^{tree}")

	details := &CommitDetails{
		Author:         "Test Author",
		AuthorEmail:    "test@example.com",
		AuthorTime:     "1000000000 +0000",
		Committer:      "Test Committer",
		CommitterEmail: "test@example.com",
		Time:           "1000000000 +0000",
		Tree:           treeHash,
		Summary:        "test commit via CreateCommit",
		Parents:        []string{parentHash},
	}
	hash, err := repo.CreateCommit(details)
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != 40 {
		t.Fatalf("unexpected hash length: %d", len(hash))
	}
	if err := repo.VerifyCommit(hash); err != nil {
		t.Fatal(err)
	}
}

func TestGitRepoCreateCommitWithTree(t *testing.T) {
	repo := setupTestRepo(t)
	parentHash, _ := repo.GetCommitHash("HEAD")
	tree := NewTree(map[string]TreeChild{
		"test.txt": NewBlob("test content"),
	})
	details := &CommitDetails{
		Author:         "Test",
		AuthorEmail:    "test@example.com",
		AuthorTime:     "1000000000 +0000",
		Committer:      "Test",
		CommitterEmail: "test@example.com",
		Time:           "1000000000 +0000",
		Summary:        "commit with tree",
		Parents:        []string{parentHash},
	}
	hash, err := repo.CreateCommitWithTree(details, tree)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.VerifyCommit(hash); err != nil {
		t.Fatal(err)
	}
}

func TestGitRepoSetRef(t *testing.T) {
	repo := setupTestRepo(t)
	headHash, _ := repo.GetCommitHash("HEAD")

	if err := repo.SetRef("refs/test/myref", headHash, ""); err != nil {
		t.Fatal(err)
	}
	has, _ := repo.HasRef("refs/test/myref")
	if !has {
		t.Fatal("expected new ref to exist")
	}
}

func TestGitRepoNotes(t *testing.T) {
	repo := setupTestRepo(t)
	headHash, _ := repo.GetCommitHash("HEAD")
	notesRef := "refs/notes/test"

	notes := repo.GetNotes(notesRef, headHash)
	if len(notes) != 0 {
		t.Fatalf("expected no notes initially, got %d", len(notes))
	}

	if err := repo.AppendNote(notesRef, headHash, Note("test note")); err != nil {
		t.Fatal(err)
	}
	notes = repo.GetNotes(notesRef, headHash)
	if len(notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(notes))
	}
	if string(notes[0]) != "test note" {
		t.Fatalf("unexpected note: %q", string(notes[0]))
	}
}

func TestGitRepoListNotedRevisions(t *testing.T) {
	repo := setupTestRepo(t)
	headHash, _ := repo.GetCommitHash("HEAD")
	notesRef := "refs/notes/test"

	if err := repo.AppendNote(notesRef, headHash, Note("note")); err != nil {
		t.Fatal(err)
	}
	revisions := repo.ListNotedRevisions(notesRef)
	if len(revisions) != 1 {
		t.Fatalf("expected 1 noted revision, got %d", len(revisions))
	}
	if revisions[0] != headHash {
		t.Fatalf("expected %q, got %q", headHash, revisions[0])
	}
}

func TestGitRepoGetAllNotes(t *testing.T) {
	repo := setupTestRepo(t)
	headHash, _ := repo.GetCommitHash("HEAD")
	notesRef := "refs/notes/test"

	if err := repo.AppendNote(notesRef, headHash, Note("note content")); err != nil {
		t.Fatal(err)
	}
	allNotes, err := repo.GetAllNotes(notesRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(allNotes) != 1 {
		t.Fatalf("expected 1 commit with notes, got %d", len(allNotes))
	}
	notes := allNotes[headHash]
	if len(notes) == 0 {
		t.Fatal("expected at least one note")
	}
	found := false
	for _, n := range notes {
		if strings.Contains(string(n), "note content") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected to find 'note content' in notes: %v", notes)
	}
}

func TestGitRepoRemotes(t *testing.T) {
	repo := setupTestRepo(t)
	remotes, err := repo.Remotes()
	if err != nil {
		t.Fatal(err)
	}
	if len(remotes) != 0 {
		t.Fatalf("expected no remotes for fresh repo, got %v", remotes)
	}
}

func TestGitRepoHasObject(t *testing.T) {
	repo := setupTestRepo(t)
	headHash, _ := repo.GetCommitHash("HEAD")
	has, err := repo.HasObject(headHash)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected HEAD commit to exist")
	}
	has, err = repo.HasObject("0000000000000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("expected nonexistent object to not exist")
	}
}

func TestGitRepoResolveRefCommit(t *testing.T) {
	repo := setupTestRepo(t)
	hash, err := repo.ResolveRefCommit("refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	expectedHash, _ := repo.GetCommitHash("HEAD")
	if hash != expectedHash {
		t.Fatalf("expected %q, got %q", expectedHash, hash)
	}
}

func TestGitRepoSwitchToRef(t *testing.T) {
	repo := setupTestRepo(t)
	gitRun(t, repo.Path, "branch", "test-branch")

	if err := repo.SwitchToRef("refs/heads/test-branch"); err != nil {
		t.Fatal(err)
	}
	head, _ := repo.GetHeadRef()
	if head != "refs/heads/test-branch" {
		t.Fatalf("expected refs/heads/test-branch, got %q", head)
	}
}

func TestParsedDiffEmpty(t *testing.T) {
	fileDiffs, err := parsedDiff("")
	if err != nil {
		t.Fatal(err)
	}
	if len(fileDiffs) != 0 {
		t.Fatalf("expected 0 file diffs, got %d", len(fileDiffs))
	}
}

func TestGetRemoteNotesRef(t *testing.T) {
	ref := getRemoteNotesRef("origin", "refs/notes/devtools/reviews")
	expected := "refs/notes/remotes/origin/devtools/reviews"
	if ref != expected {
		t.Fatalf("expected %q, got %q", expected, ref)
	}
}

func TestGetLocalNotesRef(t *testing.T) {
	ref := getLocalNotesRef("origin", "refs/notes/remotes/origin/devtools/reviews")
	expected := "refs/notes/devtools/reviews"
	if ref != expected {
		t.Fatalf("expected %q, got %q", expected, ref)
	}
}

func TestGetRemoteDevtoolsRef(t *testing.T) {
	ref := getRemoteDevtoolsRef("origin", "refs/devtools/archives/reviews")
	expected := "refs/remoteDevtools/origin/archives/reviews"
	if ref != expected {
		t.Fatalf("expected %q, got %q", expected, ref)
	}
}

func TestGetLocalDevtoolsRef(t *testing.T) {
	ref := getLocalDevtoolsRef("origin", "refs/remoteDevtools/origin/archives/reviews")
	expected := "refs/devtools/archives/reviews"
	if ref != expected {
		t.Fatalf("expected %q, got %q", expected, ref)
	}
}
