package repository

import (
	exec "golang.org/x/sys/execabs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestRepoWithRemote(t *testing.T) (local *GitRepo, remoteDir string) {
	t.Helper()
	localDir := t.TempDir()
	remoteDir = t.TempDir()

	gitRun(t, remoteDir, "init", "--bare")
	gitRun(t, localDir, "init", "-b", "main")
	gitRun(t, localDir, "config", "user.email", "test@example.com")
	gitRun(t, localDir, "config", "user.name", "Test")
	gitRun(t, localDir, "remote", "add", "origin", remoteDir)

	if err := os.WriteFile(filepath.Join(localDir, "file.txt"), []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, localDir, "add", "file.txt")
	gitRun(t, localDir, "commit", "-m", "initial commit")
	gitRun(t, localDir, "push", "origin", "main")

	repo, err := NewGitRepo(localDir)
	if err != nil {
		t.Fatal(err)
	}
	return repo, remoteDir
}

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

func TestGitRepoGetCoreEditor(t *testing.T) {
	repo := setupTestRepo(t)
	// GIT_EDITOR env var takes highest precedence in `git var GIT_EDITOR`.
	t.Setenv("GIT_EDITOR", "nano")
	editor, err := repo.GetCoreEditor()
	if err != nil {
		t.Fatal(err)
	}
	if editor != "nano" {
		t.Fatalf("expected 'nano', got %q", editor)
	}
}

func TestGitRepoParsedDiff1(t *testing.T) {
	repo := setupTestRepo(t)
	addCommit(t, repo, "file.txt", "changed\n", "second")
	secondHash, _ := repo.GetCommitHash("HEAD")

	fileDiffs, err := repo.ParsedDiff1(secondHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(fileDiffs) != 1 {
		t.Fatalf("expected 1 file diff, got %d", len(fileDiffs))
	}
	if fileDiffs[0].NewName != "file.txt" {
		t.Fatalf("unexpected file name: %q", fileDiffs[0].NewName)
	}
}

func TestGitRepoResolveRefCommitRemoteFallback(t *testing.T) {
	repo, _ := setupTestRepoWithRemote(t)
	gitRun(t, repo.Path, "fetch", "origin")

	// Delete the local branch and test fallback to remote
	gitRun(t, repo.Path, "checkout", "-b", "other")
	gitRun(t, repo.Path, "branch", "-D", "main")

	hash, err := repo.ResolveRefCommit("refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash from remote fallback")
	}
}

func TestGitRepoResolveRefCommitUnknown(t *testing.T) {
	repo := setupTestRepo(t)
	_, err := repo.ResolveRefCommit("refs/tags/nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown non-branch ref")
	}
}

func TestGitRepoResolveRefCommitNoRemoteMatch(t *testing.T) {
	repo := setupTestRepo(t)
	_, err := repo.ResolveRefCommit("refs/heads/nonexistent")
	if err == nil {
		t.Fatal("expected error when no remote matches")
	}
}

func TestGitRepoGetRefHashes(t *testing.T) {
	repo := setupTestRepo(t)
	headHash, _ := repo.GetCommitHash("HEAD")
	notesRef := "refs/notes/test"
	if err := repo.AppendNote(notesRef, headHash, Note("test")); err != nil {
		t.Fatal(err)
	}

	refs, err := repo.getRefHashes("refs/notes/*")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) == 0 {
		t.Fatal("expected at least one ref")
	}
}

func TestGitRepoGetRefHashesInvalidPattern(t *testing.T) {
	repo := setupTestRepo(t)
	_, err := repo.getRefHashes("refs/notes/test")
	if err == nil {
		t.Fatal("expected error for pattern without /*")
	}
}

func TestGitRepoArchiveRef(t *testing.T) {
	repo := setupTestRepo(t)
	archive := "refs/devtools/archives/test"

	// First archive: creates the archive ref
	if err := repo.ArchiveRef("refs/heads/main", archive); err != nil {
		t.Fatal(err)
	}
	has, _ := repo.HasRef(archive)
	if !has {
		t.Fatal("expected archive ref to exist")
	}

	// Add a commit and archive again (tests existing archive path)
	addCommit(t, repo, "file.txt", "updated\n", "second")
	if err := repo.ArchiveRef("refs/heads/main", archive); err != nil {
		t.Fatal(err)
	}

	// Archive same ref again (tests already-archived/idempotent path)
	if err := repo.ArchiveRef("refs/heads/main", archive); err != nil {
		t.Fatal(err)
	}
}

func TestGitRepoMergeArchivesNoRemote(t *testing.T) {
	repo := setupTestRepo(t)
	// Remote archive doesn't exist; should be a no-op
	err := repo.mergeArchives("refs/devtools/archives/local", "refs/devtools/archives/remote")
	if err != nil {
		t.Fatal(err)
	}
}

func TestGitRepoMergeArchivesNoLocal(t *testing.T) {
	repo := setupTestRepo(t)
	// Create a remote archive but no local one
	if err := repo.ArchiveRef("refs/heads/main", "refs/devtools/archives/remote"); err != nil {
		t.Fatal(err)
	}

	err := repo.mergeArchives("refs/devtools/archives/local", "refs/devtools/archives/remote")
	if err != nil {
		t.Fatal(err)
	}
	has, _ := repo.HasRef("refs/devtools/archives/local")
	if !has {
		t.Fatal("expected local archive to be created")
	}
}

func TestGitRepoMergeArchivesFastForward(t *testing.T) {
	repo := setupTestRepo(t)
	// Build an archive chain in "remote": archive init, then archive second.
	if err := repo.ArchiveRef("refs/heads/main", "refs/devtools/archives/remote"); err != nil {
		t.Fatal(err)
	}
	// Save the first archive commit hash — this becomes "local".
	firstArchiveHash, _ := repo.GetCommitHash("refs/devtools/archives/remote")

	addCommit(t, repo, "file.txt", "updated\n", "second")
	if err := repo.ArchiveRef("refs/heads/main", "refs/devtools/archives/remote"); err != nil {
		t.Fatal(err)
	}
	remoteHash, _ := repo.GetCommitHash("refs/devtools/archives/remote")

	// Set "local" to the first archive commit (ancestor of remote).
	gitRun(t, repo.Path, "update-ref", "refs/devtools/archives/local", firstArchiveHash)

	if err := repo.mergeArchives("refs/devtools/archives/local", "refs/devtools/archives/remote"); err != nil {
		t.Fatal(err)
	}
	// After fast-forward, local should match remote.
	newLocal, _ := repo.GetCommitHash("refs/devtools/archives/local")
	if newLocal != remoteHash {
		t.Fatalf("expected local to fast-forward to %q, got %q", remoteHash, newLocal)
	}
}

func TestGitRepoMergeArchivesMergeCommit(t *testing.T) {
	repo := setupTestRepo(t)

	// Build two independent archive chains so neither is an ancestor of the other.
	// Local: archive the initial commit.
	if err := repo.ArchiveRef("refs/heads/main", "refs/devtools/archives/local"); err != nil {
		t.Fatal(err)
	}

	// Remote: archive a different commit into a separate ref so the archive
	// commit itself is not descended from local's archive commit.
	addCommit(t, repo, "file.txt", "updated\n", "second")
	if err := repo.ArchiveRef("refs/heads/main", "refs/devtools/archives/remote"); err != nil {
		t.Fatal(err)
	}

	localHash, _ := repo.GetCommitHash("refs/devtools/archives/local")
	remoteHash, _ := repo.GetCommitHash("refs/devtools/archives/remote")
	isAnc, _ := repo.IsAncestor(localHash, remoteHash)
	if isAnc {
		t.Fatal("precondition: local should NOT be ancestor of remote for merge test")
	}

	err := repo.mergeArchives("refs/devtools/archives/local", "refs/devtools/archives/remote")
	if err != nil {
		t.Fatal(err)
	}
	// After merge, local should point to a new merge commit.
	newLocal, _ := repo.GetCommitHash("refs/devtools/archives/local")
	if newLocal == localHash || newLocal == remoteHash {
		t.Fatal("expected a new merge commit, not either original")
	}
}

func TestGitRepoMergeRefFastForward(t *testing.T) {
	repo := setupTestRepo(t)
	gitRun(t, repo.Path, "checkout", "-b", "feature")
	addCommit(t, repo, "feature.txt", "feature\n", "feature commit")
	gitRun(t, repo.Path, "checkout", "main")

	if err := repo.MergeRef("feature", true); err != nil {
		t.Fatal(err)
	}
	head, _ := repo.GetHeadRef()
	if head != "refs/heads/main" {
		t.Fatalf("expected to stay on main, got %q", head)
	}
}

func TestGitRepoMergeRefNoFastForward(t *testing.T) {
	repo := setupTestRepo(t)
	gitRun(t, repo.Path, "checkout", "-b", "feature")
	addCommit(t, repo, "feature.txt", "feature\n", "feature commit")
	gitRun(t, repo.Path, "checkout", "main")
	addCommit(t, repo, "main.txt", "main\n", "main commit")

	if err := repo.MergeRef("feature", false); err != nil {
		t.Fatal(err)
	}
}

func TestGitRepoMergeRefWithMessages(t *testing.T) {
	repo := setupTestRepo(t)
	gitRun(t, repo.Path, "checkout", "-b", "feature")
	addCommit(t, repo, "feature.txt", "feature\n", "feature commit")
	gitRun(t, repo.Path, "checkout", "main")
	addCommit(t, repo, "main.txt", "main\n", "main commit")

	// Use git config to set the editor to a no-op, avoiding env portability issues.
	gitRun(t, repo.Path, "config", "core.editor", "true")
	if err := repo.MergeRef("feature", false, "Merge feature branch"); err != nil {
		t.Fatal(err)
	}
}

func TestGitRepoRebaseRef(t *testing.T) {
	repo := setupTestRepo(t)
	gitRun(t, repo.Path, "checkout", "-b", "feature")
	addCommit(t, repo, "feature.txt", "feature\n", "feature commit")
	gitRun(t, repo.Path, "checkout", "main")
	addCommit(t, repo, "main.txt", "main\n", "main commit")
	gitRun(t, repo.Path, "checkout", "feature")

	// Use git config to set editors to a no-op for the interactive rebase.
	gitRun(t, repo.Path, "config", "sequence.editor", "true")
	gitRun(t, repo.Path, "config", "core.editor", "true")
	if err := repo.RebaseRef("main"); err != nil {
		t.Fatalf("rebase failed: %v", err)
	}
}

func TestGitRepoFetch(t *testing.T) {
	repo, _ := setupTestRepoWithRemote(t)
	if err := repo.Fetch("origin"); err != nil {
		t.Fatal(err)
	}
}

func TestGitRepoPush(t *testing.T) {
	repo, _ := setupTestRepoWithRemote(t)
	addCommit(t, repo, "new.txt", "new\n", "new commit")
	if err := repo.Push("origin", "refs/heads/main:refs/heads/main"); err != nil {
		t.Fatal(err)
	}
}

func TestGitRepoPushNotes(t *testing.T) {
	repo, _ := setupTestRepoWithRemote(t)
	headHash, _ := repo.GetCommitHash("HEAD")
	notesRef := "refs/notes/devtools/reviews"
	if err := repo.AppendNote(notesRef, headHash, Note("test review")); err != nil {
		t.Fatal(err)
	}
	if err := repo.PushNotes("origin", notesRef); err != nil {
		t.Fatal(err)
	}
}

func TestGitRepoPushNotesAndArchive(t *testing.T) {
	repo, _ := setupTestRepoWithRemote(t)
	headHash, _ := repo.GetCommitHash("HEAD")
	notesRef := "refs/notes/devtools/reviews"
	archiveRef := "refs/devtools/archives/reviews"
	if err := repo.AppendNote(notesRef, headHash, Note("test review")); err != nil {
		t.Fatal(err)
	}
	if err := repo.ArchiveRef("refs/heads/main", archiveRef); err != nil {
		t.Fatal(err)
	}
	if err := repo.PushNotesAndArchive("origin", notesRef, archiveRef); err != nil {
		t.Fatal(err)
	}
}

func TestGitRepoPullNotes(t *testing.T) {
	repo, remoteDir := setupTestRepoWithRemote(t)
	headHash, _ := repo.GetCommitHash("HEAD")

	// Add notes directly to the remote bare repo
	gitRun(t, remoteDir, "notes", "--ref", "refs/notes/devtools/reviews", "add", "-m", "remote note", headHash)

	if err := repo.PullNotes("origin", "refs/notes/devtools/*"); err != nil {
		t.Fatal(err)
	}
	notes := repo.GetNotes("refs/notes/devtools/reviews", headHash)
	if len(notes) == 0 {
		t.Fatal("expected notes after pull")
	}
}

func TestGitRepoMergeNotes(t *testing.T) {
	repo, remoteDir := setupTestRepoWithRemote(t)
	headHash, _ := repo.GetCommitHash("HEAD")

	// Add notes to remote and fetch
	gitRun(t, remoteDir, "notes", "--ref", "refs/notes/devtools/reviews", "add", "-m", "remote note", headHash)
	gitRun(t, repo.Path, "fetch", "origin",
		"+refs/notes/devtools/*:refs/notes/remotes/origin/devtools/*")

	if err := repo.MergeNotes("origin", "refs/notes/devtools/*"); err != nil {
		t.Fatal(err)
	}
	notes := repo.GetNotes("refs/notes/devtools/reviews", headHash)
	if len(notes) == 0 {
		t.Fatal("expected notes after merge")
	}
}

func TestGitRepoMergeArchivesRemote(t *testing.T) {
	repo, _ := setupTestRepoWithRemote(t)

	// Create archive refs locally and push
	if err := repo.ArchiveRef("refs/heads/main", "refs/devtools/archives/reviews"); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo.Path, "push", "origin", "refs/devtools/archives/reviews")

	// Fetch into the remote devtools prefix
	gitRun(t, repo.Path, "fetch", "origin",
		"+refs/devtools/*:refs/remoteDevtools/origin/*")

	if err := repo.MergeArchives("origin", "refs/devtools/archives/*"); err != nil {
		t.Fatal(err)
	}
}

func TestGitRepoFetchAndReturnNewReviewHashes(t *testing.T) {
	repo, remoteDir := setupTestRepoWithRemote(t)
	headHash, _ := repo.GetCommitHash("HEAD")

	// Add review notes to the remote
	gitRun(t, remoteDir, "notes", "--ref", "refs/notes/devtools/reviews", "add", "-m", "review note", headHash)

	hashes, err := repo.FetchAndReturnNewReviewHashes("origin", "refs/notes/devtools/*", "refs/devtools/archives/reviews")
	if err != nil {
		t.Fatal(err)
	}
	if len(hashes) == 0 {
		t.Fatal("expected new review hashes")
	}
}

func TestGitRepoPullNotesAndArchive(t *testing.T) {
	repo, remoteDir := setupTestRepoWithRemote(t)
	headHash, _ := repo.GetCommitHash("HEAD")

	// Add notes and archive to remote
	gitRun(t, remoteDir, "notes", "--ref", "refs/notes/devtools/reviews", "add", "-m", "review note", headHash)

	if err := repo.PullNotesAndArchive("origin", "refs/notes/devtools/*", "refs/devtools/archives/*"); err != nil {
		t.Fatal(err)
	}
}

func TestGitRepoRemotesWithRemote(t *testing.T) {
	repo, _ := setupTestRepoWithRemote(t)
	remotes, err := repo.Remotes()
	if err != nil {
		t.Fatal(err)
	}
	if len(remotes) != 1 || remotes[0] != "origin" {
		t.Fatalf("expected [origin], got %v", remotes)
	}
}

func TestGitRepoListCommitsBetweenEmpty(t *testing.T) {
	repo := setupTestRepo(t)
	hash, _ := repo.GetCommitHash("HEAD")
	commits, err := repo.ListCommitsBetween(hash, hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 0 {
		t.Fatalf("expected no commits between same ref, got %v", commits)
	}
}

func TestGitRepoVerifyCommitNonCommit(t *testing.T) {
	repo := setupTestRepo(t)
	// Store a blob and try to verify it as a commit
	blobHash, err := repo.StoreBlob("not a commit")
	if err != nil {
		t.Fatal(err)
	}
	err = repo.VerifyCommit(blobHash)
	if err == nil {
		t.Fatal("expected error for non-commit object")
	}
	if !strings.Contains(err.Error(), "non-commit") {
		t.Fatalf("expected 'non-commit' in error, got %v", err)
	}
}

func TestGitRepoRunGitCommandWithEnvError(t *testing.T) {
	repo := setupTestRepo(t)
	// Run a git command that fails to exercise the error path in runGitCommandWithEnv
	_, err := repo.runGitCommandWithEnv(nil, "log", "--invalid-flag-that-does-not-exist")
	if err == nil {
		t.Fatal("expected error for invalid git command")
	}
}

func TestGitRepoStoreAndReadTreeNested(t *testing.T) {
	repo := setupTestRepo(t)
	innerTree := NewTree(map[string]TreeChild{
		"inner.txt": NewBlob("inner content"),
	})
	outerContents := map[string]TreeChild{
		"file.txt": NewBlob("file content"),
		"subdir":   innerTree,
	}
	outerHash, err := repo.StoreTree(outerContents)
	if err != nil {
		t.Fatal(err)
	}

	tree, err := repo.ReadTree(outerHash)
	if err != nil {
		t.Fatal(err)
	}
	tc := tree.Contents()
	if len(tc) != 2 {
		t.Fatalf("expected 2 entries in outer tree, got %d", len(tc))
	}
	subdir, ok := tc["subdir"]
	if !ok {
		t.Fatal("expected 'subdir' in tree contents")
	}
	if subdir.Type() != "tree" {
		t.Fatalf("expected subdir to be a tree, got %q", subdir.Type())
	}
}

func TestGitRepoFetchAndReturnNewReviewHashesInvalidDevtools(t *testing.T) {
	repo, _ := setupTestRepoWithRemote(t)
	_, err := repo.FetchAndReturnNewReviewHashes("origin", "refs/notes/devtools/reviews", "refs/invalid/pattern")
	if err == nil {
		t.Fatal("expected error for invalid devtools ref pattern")
	}
}

func TestGitRepoFetchAndReturnNewReviewHashesExisting(t *testing.T) {
	repo, remoteDir := setupTestRepoWithRemote(t)
	headHash, _ := repo.GetCommitHash("HEAD")

	// Add notes to remote, fetch once to establish baseline
	gitRun(t, remoteDir, "notes", "--ref", "refs/notes/devtools/reviews", "add", "-m", "note1", headHash)
	_, err := repo.FetchAndReturnNewReviewHashes("origin", "refs/notes/devtools/*", "refs/devtools/archives/reviews")
	if err != nil {
		t.Fatal(err)
	}

	// Add more notes and fetch again to test the diff path
	addCommit(t, repo, "new.txt", "new\n", "new commit")
	gitRun(t, repo.Path, "push", "origin", "main")
	newHash, _ := repo.GetCommitHash("HEAD")
	gitRun(t, remoteDir, "notes", "--ref", "refs/notes/devtools/reviews", "add", "-m", "note2", newHash)

	hashes, err := repo.FetchAndReturnNewReviewHashes("origin", "refs/notes/devtools/*", "refs/devtools/archives/reviews")
	if err != nil {
		t.Fatal(err)
	}
	if len(hashes) == 0 {
		t.Fatal("expected new review hashes on second fetch")
	}
}

// Mock repo additional error path tests

func TestMockRepoResolveRefCommitRemoteFallback(t *testing.T) {
	repo := NewMockRepoForTest().(*mockRepoForTest)
	// Add a remote ref to test the fallback
	repo.Refs["refs/remotes/origin/feature"] = TestCommitA
	hash, err := repo.ResolveRefCommit("refs/heads/feature")
	if err != nil {
		t.Fatal(err)
	}
	if hash != TestCommitA {
		t.Fatalf("expected %q, got %q", TestCommitA, hash)
	}
}

func TestMockRepoResolveRefCommitError(t *testing.T) {
	repo := NewMockRepoForTest()
	_, err := repo.ResolveRefCommit("refs/heads/nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent ref")
	}
}

func TestMockRepoGetCommitHashError(t *testing.T) {
	repo := NewMockRepoForTest()
	_, err := repo.GetCommitHash("refs/heads/nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent ref")
	}
}

func TestMockRepoGetCommitMessageError(t *testing.T) {
	repo := NewMockRepoForTest()
	_, err := repo.GetCommitMessage("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent commit")
	}
}

func TestMockRepoGetCommitTimeError(t *testing.T) {
	repo := NewMockRepoForTest()
	_, err := repo.GetCommitTime("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent commit")
	}
}

func TestMockRepoGetCommitDetailsError(t *testing.T) {
	repo := NewMockRepoForTest()
	_, err := repo.GetCommitDetails("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent commit")
	}
}

func TestMockRepoIsAncestorError(t *testing.T) {
	repo := NewMockRepoForTest()
	_, err := repo.IsAncestor("nonexistent", TestCommitA)
	if err == nil {
		t.Fatal("expected error for nonexistent ancestor")
	}
	_, err = repo.IsAncestor(TestCommitA, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent descendant")
	}
}

func TestMockRepoMergeBaseError(t *testing.T) {
	repo := NewMockRepoForTest()
	_, err := repo.MergeBase("nonexistent", TestCommitA)
	if err == nil {
		t.Fatal("expected error for nonexistent commit in MergeBase")
	}
}

func TestMockRepoMergeBaseNoCommonAncestor(t *testing.T) {
	repo := NewMockRepoForTest().(*mockRepoForTest)
	// Add a completely disconnected commit
	repo.Commits["Z"] = mockCommit{Message: "disconnected", Time: "99"}
	base, err := repo.MergeBase(TestCommitA, "Z")
	if err != nil {
		t.Fatal(err)
	}
	if base != "" {
		t.Fatalf("expected empty merge base, got %q", base)
	}
}

func TestMockRepoArchiveRefError(t *testing.T) {
	repo := NewMockRepoForTest()
	err := repo.ArchiveRef("nonexistent", "refs/devtools/archives/test")
	if err == nil {
		t.Fatal("expected error for nonexistent ref")
	}
}

func TestMockRepoMergeRefError(t *testing.T) {
	repo := NewMockRepoForTest()
	err := repo.MergeRef("nonexistent", true)
	if err == nil {
		t.Fatal("expected error for nonexistent ref in MergeRef")
	}
}

func TestMockRepoRebaseRefDetachedHead(t *testing.T) {
	repo := NewMockRepoForTest().(*mockRepoForTest)
	// Set head to a commit hash (not a branch)
	repo.Head = TestCommitI
	if err := repo.RebaseRef(TestTargetRef); err != nil {
		t.Fatal(err)
	}
	// Should be in detached head state (head is a hash, not a branch ref)
	if strings.HasPrefix(repo.Head, "refs/") {
		t.Fatalf("expected detached head, got %q", repo.Head)
	}
}

func TestMockRepoRebaseRefError(t *testing.T) {
	repo := NewMockRepoForTest().(*mockRepoForTest)
	repo.Head = "nonexistent"
	err := repo.RebaseRef(TestTargetRef)
	if err == nil {
		t.Fatal("expected error for nonexistent head in RebaseRef")
	}
}

func TestMockRepoListCommitsNonexistent(t *testing.T) {
	repo := NewMockRepoForTest()
	commits := repo.ListCommits("nonexistent")
	if commits != nil {
		t.Fatalf("expected nil for nonexistent ref, got %v", commits)
	}
}

func TestMockRepoListCommitsBetweenError(t *testing.T) {
	repo := NewMockRepoForTest()
	// IsAncestor fails when 'from' can't be resolved, triggering the error path.
	_, err := repo.ListCommitsBetween("nonexistent", TestCommitB)
	if err == nil {
		t.Fatal("expected error for nonexistent 'from' ref")
	}
}

func TestMockRepoHasObjectBlobAndTree(t *testing.T) {
	repo := NewMockRepoForTest()
	blobHash, _ := repo.StoreBlob("test content")
	has, err := repo.HasObject(blobHash)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected stored blob to be found")
	}

	treeHash, _ := repo.StoreTree(map[string]TreeChild{"f.txt": NewBlob("c")})
	has, err = repo.HasObject(treeHash)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected stored tree to be found")
	}
}

func TestMockRepoMergeRefNonFFError(t *testing.T) {
	repo := NewMockRepoForTest().(*mockRepoForTest)
	repo.Head = "refs/heads/master"
	err := repo.MergeRef("nonexistent", false, "msg")
	if err == nil {
		t.Fatal("expected error for nonexistent ref in non-ff merge")
	}
}
