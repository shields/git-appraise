package repository

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestNoteHash(t *testing.T) {
	n := Note("test content")
	h1 := n.Hash()
	h2 := n.Hash()
	if h1 != h2 {
		t.Fatalf("hash not deterministic: %q vs %q", h1, h2)
	}
	if h1 == "" {
		t.Fatal("hash should not be empty")
	}
}

func TestNoteHashDifferent(t *testing.T) {
	n1 := Note("content A")
	n2 := Note("content B")
	if n1.Hash() == n2.Hash() {
		t.Fatal("different notes should have different hashes")
	}
}

func TestNewBlob(t *testing.T) {
	b := NewBlob("hello world")
	if b.Contents() != "hello world" {
		t.Fatalf("unexpected contents: %q", b.Contents())
	}
	if b.Type() != "blob" {
		t.Fatalf("unexpected type: %q", b.Type())
	}
}

func TestBlobStore(t *testing.T) {
	repo := NewMockRepoForTest()
	b := NewBlob("test blob")
	hash, err := b.Store(repo)
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	// Storing again should return cached hash.
	hash2, err := b.Store(repo)
	if err != nil {
		t.Fatal(err)
	}
	if hash != hash2 {
		t.Fatalf("expected same hash on second store: %q vs %q", hash, hash2)
	}
}

func TestNewTree(t *testing.T) {
	contents := map[string]TreeChild{
		"file.txt": NewBlob("content"),
	}
	tree := NewTree(contents)
	if tree.Type() != "tree" {
		t.Fatalf("unexpected type: %q", tree.Type())
	}
	tc := tree.Contents()
	if len(tc) != 1 {
		t.Fatalf("expected 1 child, got %d", len(tc))
	}
	if _, ok := tc["file.txt"]; !ok {
		t.Fatal("expected file.txt in tree contents")
	}
}

func TestTreeImmutable(t *testing.T) {
	contents := map[string]TreeChild{
		"file.txt": NewBlob("content"),
	}
	tree := NewTree(contents)
	// Modifying the original map should not affect the tree.
	contents["new.txt"] = NewBlob("new")
	tc := tree.Contents()
	if len(tc) != 1 {
		t.Fatalf("tree should be immutable, got %d children", len(tc))
	}
	// Modifying the returned contents should not affect the tree.
	tc["another.txt"] = NewBlob("another")
	if len(tree.Contents()) != 1 {
		t.Fatal("tree.Contents() mutation leaked back")
	}
}

func TestTreeStore(t *testing.T) {
	repo := NewMockRepoForTest()
	tree := NewTree(map[string]TreeChild{
		"a.txt": NewBlob("aaa"),
		"b.txt": NewBlob("bbb"),
	})
	hash, err := tree.Store(repo)
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	// Second store should return cached hash.
	hash2, err := tree.Store(repo)
	if err != nil {
		t.Fatal(err)
	}
	if hash != hash2 {
		t.Fatalf("expected same hash on second store: %q vs %q", hash, hash2)
	}
}

func TestTreeNested(t *testing.T) {
	repo := NewMockRepoForTest()
	inner := NewTree(map[string]TreeChild{
		"inner.txt": NewBlob("inner content"),
	})
	outer := NewTree(map[string]TreeChild{
		"dir": inner,
	})
	hash, err := outer.Store(repo)
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
}

func TestDiffOpString(t *testing.T) {
	tests := []struct {
		op       DiffOp
		expected string
	}{
		{OpContext, " "},
		{OpDelete, "-"},
		{OpAdd, "+"},
		{DiffOp(99), ""},
	}
	for _, tt := range tests {
		if got := tt.op.String(); got != tt.expected {
			t.Fatalf("DiffOp(%d).String() = %q, want %q", tt.op, got, tt.expected)
		}
	}
}

func TestMockRepoBasics(t *testing.T) {
	repo := NewMockRepoForTest()
	if repo.GetPath() != "~/mockRepo/" {
		t.Fatalf("unexpected path: %q", repo.GetPath())
	}
	dir, err := repo.GetDataDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != "~/mockRepo/.git" {
		t.Fatalf("unexpected data dir: %q", dir)
	}
	email, err := repo.GetUserEmail()
	if err != nil {
		t.Fatal(err)
	}
	if email != "user@example.com" {
		t.Fatalf("unexpected email: %q", email)
	}
	editor, err := repo.GetCoreEditor()
	if err != nil {
		t.Fatal(err)
	}
	if editor != "vi" {
		t.Fatalf("unexpected editor: %q", editor)
	}
	strategy, err := repo.GetSubmitStrategy()
	if err != nil {
		t.Fatal(err)
	}
	if strategy != "merge" {
		t.Fatalf("unexpected strategy: %q", strategy)
	}
	uncommitted, err := repo.HasUncommittedChanges()
	if err != nil {
		t.Fatal(err)
	}
	if uncommitted {
		t.Fatal("mock should have no uncommitted changes")
	}
}

func TestMockRepoStateHash(t *testing.T) {
	repo := NewMockRepoForTest()
	hash, err := repo.GetRepoStateHash()
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("expected non-empty state hash")
	}
}

func TestMockRepoHasRef(t *testing.T) {
	repo := NewMockRepoForTest()
	has, err := repo.HasRef(TestTargetRef)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected target ref to exist")
	}
	has, err = repo.HasRef("refs/heads/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("expected nonexistent ref to not exist")
	}
}

func TestMockRepoHasObject(t *testing.T) {
	repo := NewMockRepoForTest()
	has, err := repo.HasObject(TestCommitA)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected commit A to exist")
	}
	has, err = repo.HasObject("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("expected nonexistent hash to not exist")
	}
}

func TestMockRepoVerifyCommit(t *testing.T) {
	repo := NewMockRepoForTest()
	if err := repo.VerifyCommit(TestCommitA); err != nil {
		t.Fatal(err)
	}
	if err := repo.VerifyCommit("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent commit")
	}
}

func TestMockRepoVerifyGitRef(t *testing.T) {
	repo := NewMockRepoForTest()
	if err := repo.VerifyGitRef(TestTargetRef); err != nil {
		t.Fatal(err)
	}
	if err := repo.VerifyGitRef("refs/heads/nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent ref")
	}
}

func TestMockRepoGetHeadRef(t *testing.T) {
	repo := NewMockRepoForTest()
	head, err := repo.GetHeadRef()
	if err != nil {
		t.Fatal(err)
	}
	if head != TestTargetRef {
		t.Fatalf("unexpected head: %q", head)
	}
}

func TestMockRepoGetCommitHash(t *testing.T) {
	repo := NewMockRepoForTest()
	hash, err := repo.GetCommitHash(TestTargetRef)
	if err != nil {
		t.Fatal(err)
	}
	if hash != TestCommitJ {
		t.Fatalf("expected %q, got %q", TestCommitJ, hash)
	}
	hash, err = repo.GetCommitHash(TestCommitA)
	if err != nil {
		t.Fatal(err)
	}
	if hash != TestCommitA {
		t.Fatalf("expected %q, got %q", TestCommitA, hash)
	}
}

func TestMockRepoResolveRefCommit(t *testing.T) {
	repo := NewMockRepoForTest()
	hash, err := repo.ResolveRefCommit(TestTargetRef)
	if err != nil {
		t.Fatal(err)
	}
	if hash != TestCommitJ {
		t.Fatalf("expected %q, got %q", TestCommitJ, hash)
	}
}

func TestMockRepoGetCommitMessage(t *testing.T) {
	repo := NewMockRepoForTest()
	msg, err := repo.GetCommitMessage(TestCommitA)
	if err != nil {
		t.Fatal(err)
	}
	if msg != "First commit" {
		t.Fatalf("unexpected message: %q", msg)
	}
}

func TestMockRepoGetCommitTime(t *testing.T) {
	repo := NewMockRepoForTest()
	time, err := repo.GetCommitTime(TestCommitA)
	if err != nil {
		t.Fatal(err)
	}
	if time != "0" {
		t.Fatalf("unexpected time: %q", time)
	}
}

func TestMockRepoGetLastParent(t *testing.T) {
	repo := NewMockRepoForTest()
	parent, err := repo.GetLastParent(TestCommitD)
	if err != nil {
		t.Fatal(err)
	}
	if parent != TestCommitC {
		t.Fatalf("expected %q, got %q", TestCommitC, parent)
	}
	// Commit A has no parents.
	parent, err = repo.GetLastParent(TestCommitA)
	if err != nil {
		t.Fatal(err)
	}
	if parent != "" {
		t.Fatalf("expected empty parent for root commit, got %q", parent)
	}
}

func TestMockRepoGetCommitDetails(t *testing.T) {
	repo := NewMockRepoForTest()
	details, err := repo.GetCommitDetails(TestCommitB)
	if err != nil {
		t.Fatal(err)
	}
	if details.Author != "Test Author" {
		t.Fatalf("unexpected author: %q", details.Author)
	}
	if details.Summary != "Second commit" {
		t.Fatalf("unexpected summary: %q", details.Summary)
	}
	if len(details.Parents) != 1 || details.Parents[0] != TestCommitA {
		t.Fatalf("unexpected parents: %v", details.Parents)
	}
}

func TestMockRepoIsAncestor(t *testing.T) {
	repo := NewMockRepoForTest()
	is, err := repo.IsAncestor(TestCommitA, TestCommitD)
	if err != nil {
		t.Fatal(err)
	}
	if !is {
		t.Fatal("A should be ancestor of D")
	}
	is, err = repo.IsAncestor(TestCommitD, TestCommitA)
	if err != nil {
		t.Fatal(err)
	}
	if is {
		t.Fatal("D should not be ancestor of A")
	}
	// Self-ancestor check.
	is, err = repo.IsAncestor(TestCommitA, TestCommitA)
	if err != nil {
		t.Fatal(err)
	}
	if !is {
		t.Fatal("A should be ancestor of itself")
	}
}

func TestMockRepoMergeBase(t *testing.T) {
	repo := NewMockRepoForTest()
	base, err := repo.MergeBase(TestCommitB, TestCommitC)
	if err != nil {
		t.Fatal(err)
	}
	if base != TestCommitA {
		t.Fatalf("expected %q, got %q", TestCommitA, base)
	}
}

func TestMockRepoDiff(t *testing.T) {
	repo := NewMockRepoForTest()
	diff, err := repo.Diff(TestCommitA, TestCommitB)
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" {
		t.Fatal("expected non-empty diff")
	}
}

func TestMockRepoDiff1(t *testing.T) {
	repo := NewMockRepoForTest()
	diff, err := repo.Diff1(TestCommitB)
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" {
		t.Fatal("expected non-empty diff")
	}
}

func TestMockRepoParsedDiff(t *testing.T) {
	repo := NewMockRepoForTest()
	fileDiffs, err := repo.ParsedDiff(TestCommitA, TestCommitB)
	if err != nil {
		t.Fatal(err)
	}
	if len(fileDiffs) != 1 {
		t.Fatalf("expected 1 file diff, got %d", len(fileDiffs))
	}
	if fileDiffs[0].OldName != "foo" || fileDiffs[0].NewName != "bar" {
		t.Fatalf("unexpected file names: %q/%q", fileDiffs[0].OldName, fileDiffs[0].NewName)
	}
}

func TestMockRepoParsedDiff1(t *testing.T) {
	repo := NewMockRepoForTest()
	fileDiffs, err := repo.ParsedDiff1(TestCommitB)
	if err != nil {
		t.Fatal(err)
	}
	if len(fileDiffs) != 1 {
		t.Fatalf("expected 1 file diff, got %d", len(fileDiffs))
	}
}

func TestMockRepoShow(t *testing.T) {
	repo := NewMockRepoForTest()
	content, err := repo.Show(TestCommitA, "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if content != "A:file.txt" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestMockRepoSwitchToRef(t *testing.T) {
	repo := NewMockRepoForTest()
	if err := repo.SwitchToRef(TestReviewRef); err != nil {
		t.Fatal(err)
	}
	head, _ := repo.GetHeadRef()
	if head != TestReviewRef {
		t.Fatalf("expected head to be review ref, got %q", head)
	}
}

func TestMockRepoArchiveRef(t *testing.T) {
	repo := NewMockRepoForTest()
	archive := "refs/devtools/archives/test"
	if err := repo.ArchiveRef(TestCommitA, archive); err != nil {
		t.Fatal(err)
	}
	has, _ := repo.HasRef(archive)
	if !has {
		t.Fatal("expected archive ref to exist")
	}
	// Archive again to test the existing-archive path.
	if err := repo.ArchiveRef(TestCommitB, archive); err != nil {
		t.Fatal(err)
	}
}

func TestMockRepoMergeRefFastForward(t *testing.T) {
	repo := NewMockRepoForTest()
	if err := repo.SwitchToRef(TestTargetRef); err != nil {
		t.Fatal(err)
	}
	if err := repo.MergeRef(TestReviewRef, true); err != nil {
		t.Fatal(err)
	}
	hash, _ := repo.GetCommitHash(TestTargetRef)
	if hash != TestCommitI {
		t.Fatalf("expected target to advance to I, got %q", hash)
	}
}

func TestMockRepoMergeRefNonFastForward(t *testing.T) {
	repo := NewMockRepoForTest()
	if err := repo.SwitchToRef(TestTargetRef); err != nil {
		t.Fatal(err)
	}
	if err := repo.MergeRef(TestReviewRef, false, "merge message"); err != nil {
		t.Fatal(err)
	}
	// Should have created a new merge commit.
	hash, _ := repo.GetCommitHash(TestTargetRef)
	if hash == TestCommitJ || hash == TestCommitI {
		t.Fatal("expected a new merge commit")
	}
}

func TestMockRepoRebaseRef(t *testing.T) {
	repo := NewMockRepoForTest()
	if err := repo.SwitchToRef(TestReviewRef); err != nil {
		t.Fatal(err)
	}
	if err := repo.RebaseRef(TestTargetRef); err != nil {
		t.Fatal(err)
	}
	hash, _ := repo.GetCommitHash(TestReviewRef)
	if hash == TestCommitI {
		t.Fatal("expected review ref to change after rebase")
	}
}

func TestMockRepoListCommits(t *testing.T) {
	repo := NewMockRepoForTest()
	commits := repo.ListCommits(TestTargetRef)
	if len(commits) == 0 {
		t.Fatal("expected commits for target ref")
	}
	// First commit should be oldest (A).
	if commits[0] != TestCommitA {
		t.Fatalf("expected oldest commit A first, got %q", commits[0])
	}
}

func TestMockRepoListCommitsBetween(t *testing.T) {
	repo := NewMockRepoForTest()
	commits, err := repo.ListCommitsBetween(TestCommitA, TestCommitB)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) == 0 {
		t.Fatal("expected at least one commit between A and B")
	}
}

func TestMockRepoStoreAndReadTree(t *testing.T) {
	repo := NewMockRepoForTest()
	contents := map[string]TreeChild{
		"file.txt": NewBlob("hello"),
	}
	hash, err := repo.StoreTree(contents)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := repo.ReadTree(hash)
	if err != nil {
		t.Fatal(err)
	}
	tc := tree.Contents()
	if len(tc) != 1 {
		t.Fatalf("expected 1 child, got %d", len(tc))
	}
}

func TestMockRepoReadTreeNotFound(t *testing.T) {
	repo := NewMockRepoForTest()
	_, err := repo.ReadTree("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent tree")
	}
}

func TestMockRepoCreateCommit(t *testing.T) {
	repo := NewMockRepoForTest()
	details := &CommitDetails{
		Summary: "test commit",
		Time:    "999",
		Parents: []string{TestCommitA},
	}
	hash, err := repo.CreateCommit(details)
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if err := repo.VerifyCommit(hash); err != nil {
		t.Fatalf("created commit should be verifiable: %v", err)
	}
}

func TestMockRepoCreateCommitWithTree(t *testing.T) {
	repo := NewMockRepoForTest()
	tree := NewTree(map[string]TreeChild{
		"file.txt": NewBlob("content"),
	})
	details := &CommitDetails{
		Summary: "commit with tree",
		Time:    "999",
		Parents: []string{TestCommitA},
	}
	hash, err := repo.CreateCommitWithTree(details, tree)
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
}

func TestMockRepoSetRef(t *testing.T) {
	repo := NewMockRepoForTest()
	// Unconditional update (empty previousCommitHash).
	if err := repo.SetRef("refs/heads/test", TestCommitA, ""); err != nil {
		t.Fatal(err)
	}
	hash, _ := repo.GetCommitHash("refs/heads/test")
	if hash != TestCommitA {
		t.Fatalf("expected %q, got %q", TestCommitA, hash)
	}
	// CAS update.
	if err := repo.SetRef("refs/heads/test", TestCommitB, TestCommitA); err != nil {
		t.Fatal(err)
	}
	// CAS failure.
	if err := repo.SetRef("refs/heads/test", TestCommitC, TestCommitA); err == nil {
		t.Fatal("expected error for CAS mismatch")
	}
}

func TestMockRepoNotes(t *testing.T) {
	repo := NewMockRepoForTest()
	notes := repo.GetNotes(TestRequestsRef, TestCommitB)
	if len(notes) == 0 {
		t.Fatal("expected notes for commit B")
	}
	// Append a note.
	if err := repo.AppendNote(TestRequestsRef, TestCommitB, Note("new note")); err != nil {
		t.Fatal(err)
	}
	notes = repo.GetNotes(TestRequestsRef, TestCommitB)
	found := false
	for _, n := range notes {
		if string(n) == "new note" {
			found = true
		}
	}
	if !found {
		t.Fatal("appended note not found")
	}
}

func TestMockRepoGetAllNotes(t *testing.T) {
	repo := NewMockRepoForTest()
	allNotes, err := repo.GetAllNotes(TestRequestsRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(allNotes) == 0 {
		t.Fatal("expected notes")
	}
	if _, ok := allNotes[TestCommitB]; !ok {
		t.Fatal("expected notes for commit B")
	}
}

func TestMockRepoListNotedRevisions(t *testing.T) {
	repo := NewMockRepoForTest()
	revisions := repo.ListNotedRevisions(TestRequestsRef)
	if len(revisions) == 0 {
		t.Fatal("expected noted revisions")
	}
}

func TestMockRepoRemotes(t *testing.T) {
	repo := NewMockRepoForTest()
	remotes, err := repo.Remotes()
	if err != nil {
		t.Fatal(err)
	}
	if len(remotes) != 1 || remotes[0] != "origin" {
		t.Fatalf("unexpected remotes: %v", remotes)
	}
}

func TestMockRepoNoopOperations(t *testing.T) {
	repo := NewMockRepoForTest()
	if err := repo.Fetch("origin"); err != nil {
		t.Fatal(err)
	}
	if err := repo.PushNotes("origin", "refs/notes/*"); err != nil {
		t.Fatal(err)
	}
	if err := repo.PullNotes("origin", "refs/notes/*"); err != nil {
		t.Fatal(err)
	}
	if err := repo.PushNotesAndArchive("origin", "refs/notes/*", "refs/devtools/*"); err != nil {
		t.Fatal(err)
	}
	if err := repo.PullNotesAndArchive("origin", "refs/notes/*", "refs/devtools/*"); err != nil {
		t.Fatal(err)
	}
	if err := repo.MergeNotes("origin", "refs/notes/*"); err != nil {
		t.Fatal(err)
	}
	if err := repo.MergeArchives("origin", "refs/devtools/*"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FetchAndReturnNewReviewHashes("origin", "refs/notes/*"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Push("origin", "refs/heads/*"); err != nil {
		t.Fatal(err)
	}
}

func TestMockRepoResolveLocalRefHEAD(t *testing.T) {
	repo := NewMockRepoForTest().(*mockRepoForTest)
	hash, err := repo.GetCommitHash("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if hash != TestCommitJ {
		t.Fatalf("expected %q, got %q", TestCommitJ, hash)
	}
}

func TestMockRepoIsAncestorGetCommitError(t *testing.T) {
	repo := NewMockRepoForTest().(*mockRepoForTest)
	// Add a ref that points to a hash not in Commits, so resolveLocalRef
	// succeeds (it's in Refs) but getCommit on the resolved hash fails.
	repo.Refs["refs/heads/bad"] = "nonexistent_hash"
	_, err := repo.IsAncestor(TestCommitA, "refs/heads/bad")
	if err == nil {
		t.Fatal("expected error when getCommit fails for descendant")
	}
}

func TestMockRepoMergeRefNonFFHeadResolveError(t *testing.T) {
	repo := NewMockRepoForTest().(*mockRepoForTest)
	// Set Head to a value that can resolve ref (it's valid) but then
	// resolveLocalRef(r.Head) fails in non-ff path.
	repo.Head = "nonexistent_head_ref"
	err := repo.MergeRef(TestCommitA, false, "msg")
	if err == nil {
		t.Fatal("expected error when head cannot be resolved in non-ff merge")
	}
}

func TestMockRepoMergeRefNonFFGetCommitError(t *testing.T) {
	repo := NewMockRepoForTest().(*mockRepoForTest)
	// Create a ref whose hash resolves (it's in Refs) but whose resolved
	// hash is NOT in Commits, so getCommit fails with "unable to find commit".
	repo.Refs["refs/heads/bad"] = "hash_not_in_commits"
	repo.Head = TestTargetRef
	err := repo.MergeRef("refs/heads/bad", false, "msg")
	if err == nil {
		t.Fatal("expected error when getCommit fails in non-ff merge")
	}
}

func TestMockRepoRebaseRefWithRefNotInRefs(t *testing.T) {
	repo := NewMockRepoForTest().(*mockRepoForTest)
	// When ref is not in r.Refs, parentHash will be empty string.
	// Head must resolve properly for getCommit to work.
	repo.Head = TestReviewRef
	err := repo.RebaseRef("nonexistent_ref")
	if err != nil {
		t.Fatal(err)
	}
}

func TestMockRepoListCommitsBetweenNotBlocked(t *testing.T) {
	repo := NewMockRepoForTest()
	// Use from=C, to=D. D's ancestors include B and C.
	// IsAncestor(B, C) is false since B is not reachable from C.
	// So B should be included in the result (the !blocked path).
	commits, err := repo.ListCommitsBetween(TestCommitC, TestCommitD)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) == 0 {
		t.Fatal("expected commits between C and D")
	}
	// D and B should be in the result (B is not an ancestor of C)
	foundD := false
	foundB := false
	for _, c := range commits {
		if c == TestCommitD {
			foundD = true
		}
		if c == TestCommitB {
			foundB = true
		}
	}
	if !foundD {
		t.Fatal("expected D in result")
	}
	if !foundB {
		t.Fatal("expected B in result (not an ancestor of C)")
	}
}

// failingTreeChild is a TreeChild whose Store always returns an error.
type failingTreeChild struct{}

func (f *failingTreeChild) Type() string                 { return "blob" }
func (f *failingTreeChild) Store(_ Repo) (string, error) { return "", fmt.Errorf("store failed") }

func TestMockRepoStoreTreeChildStoreError(t *testing.T) {
	repo := NewMockRepoForTest()
	contents := map[string]TreeChild{
		"bad.txt": &failingTreeChild{},
	}
	_, err := repo.StoreTree(contents)
	if err == nil {
		t.Fatal("expected error when child.Store fails")
	}
}

func TestMockRepoCreateCommitWithTreeStoreError(t *testing.T) {
	repo := NewMockRepoForTest()
	tree := NewTree(map[string]TreeChild{
		"bad.txt": &failingTreeChild{},
	})
	details := &CommitDetails{
		Summary: "commit with bad tree",
		Time:    "999",
		Parents: []string{TestCommitA},
	}
	_, err := repo.CreateCommitWithTree(details, tree)
	if err == nil {
		t.Fatal("expected error when StoreTree fails")
	}
}

func TestMockRepoReadTreeNilTrees(t *testing.T) {
	repo := NewMockRepoForTest().(*mockRepoForTest)
	// Trees field is nil initially (never stored anything)
	repo.Trees = nil
	_, err := repo.ReadTree("anything")
	if err == nil {
		t.Fatal("expected error when Trees is nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got: %v", err)
	}
}

func TestMockRepoReadTreeExistingKeyNotFound(t *testing.T) {
	repo := NewMockRepoForTest().(*mockRepoForTest)
	// Initialize Trees but with a different key
	repo.Trees = make(map[string]map[string]TreeChild)
	repo.Trees["some_hash"] = map[string]TreeChild{}
	_, err := repo.ReadTree("nonexistent_hash")
	if err == nil {
		t.Fatal("expected error for missing tree hash")
	}
}

func TestMockRepoArchiveRefCreateCommitSuccess(t *testing.T) {
	repo := NewMockRepoForTest().(*mockRepoForTest)
	// Test archiving with archive already existing (both paths in ArchiveRef)
	archive := "refs/devtools/archives/test"
	if err := repo.ArchiveRef(TestTargetRef, archive); err != nil {
		t.Fatal(err)
	}
	// Archive again with a different ref
	if err := repo.ArchiveRef(TestReviewRef, archive); err != nil {
		t.Fatal(err)
	}
	has, _ := repo.HasRef(archive)
	if !has {
		t.Fatal("expected archive ref to exist")
	}
}

// errAfterReader returns data for the first N bytes, then returns an error.
type errAfterReader struct {
	data []byte
	pos  int
	err  error
}

func (r *errAfterReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, r.err
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	if r.pos >= len(r.data) {
		return n, r.err
	}
	return n, nil
}

func TestSplitBatchCheckOutputNameReadError(t *testing.T) {
	// Provide partial data followed by a non-EOF error on the name line read.
	r := &errAfterReader{
		data: []byte("abc"),
		err:  errors.New("injected read error"),
	}
	_, err := splitBatchCheckOutput(r)
	if err == nil {
		t.Fatal("expected error from splitBatchCheckOutput")
	}
	if !strings.Contains(err.Error(), "reading the next object name") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSplitBatchCheckOutputTypeNonEOFError(t *testing.T) {
	// Provide a valid name line (ending with space), then non-EOF error on the type line.
	r := &errAfterReader{
		data: []byte("abc123 "),
		err:  errors.New("injected type error"),
	}
	_, err := splitBatchCheckOutput(r)
	if err == nil {
		t.Fatal("expected error from splitBatchCheckOutput")
	}
	if !strings.Contains(err.Error(), "reading the next object type") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSplitBatchCatFileOutputNameReadError(t *testing.T) {
	// Provide partial data that fails during the name line read.
	r := &errAfterReader{
		data: []byte("abc"),
		err:  errors.New("injected name error"),
	}
	_, err := splitBatchCatFileOutput(r)
	if err == nil {
		t.Fatal("expected error from splitBatchCatFileOutput")
	}
	if !strings.Contains(err.Error(), "reading the next object name") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSplitBatchCatFileOutputSizeReadError(t *testing.T) {
	// Provide a valid name line (ending with newline), then error on size line.
	r := &errAfterReader{
		data: []byte("abc123\n"),
		err:  errors.New("injected size error"),
	}
	_, err := splitBatchCatFileOutput(r)
	if err == nil {
		t.Fatal("expected error from splitBatchCatFileOutput")
	}
	if !strings.Contains(err.Error(), "reading the next object size") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSplitBatchCatFileOutputContentReadError(t *testing.T) {
	// Provide name + size, then error during content read.
	r := &errAfterReader{
		data: []byte("abc123\n5\nhe"),
		err:  errors.New("injected content error"),
	}
	_, err := splitBatchCatFileOutput(r)
	if err == nil {
		t.Fatal("expected error during content read")
	}
}
