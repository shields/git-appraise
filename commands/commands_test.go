package commands

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"msrl.dev/git-appraise/commands/web"
	"msrl.dev/git-appraise/repository"
	"msrl.dev/git-appraise/review"
	"msrl.dev/git-appraise/review/comment"
)

// captureStdout redirects os.Stdout to capture printed output.
// Not safe for parallel use; none of these tests call t.Parallel().
// If f() panics, the deferred w.Close() unblocks the reader goroutine
// and the deferred os.Stdout restore runs afterward.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { os.Stdout = old }()
	os.Stdout = w
	defer w.Close()
	outCh := make(chan string)
	go func() {
		out, _ := io.ReadAll(r)
		outCh <- string(out)
	}()
	f()
	w.Close()
	return <-outCh
}

// --- Flag reset helpers ---
// Each reset restores the global flag variables for a command's FlagSet
// to their default values. Called via defer at the top of each test.

func resetListFlags() {
	*listAll = false
	*listJSONOutput = false
}

func resetShowFlags() {
	*showDetached = false
	*showJSONOutput = false
	*showDiffOutput = false
	*showDiffOptions = ""
	*showInlineOutput = false
}

func resetCommentFlags() {
	*commentMessage = ""
	*commentMessageFile = ""
	*commentParent = ""
	*commentFile = ""
	*commentDetached = false
	*commentLgtm = false
	*commentNmw = false
	*commentDate = ""
	commentLocation = comment.Range{}
}

func resetAcceptFlags() {
	*acceptMessage = ""
	*acceptMessageFile = ""
	*acceptDate = ""
}

func resetRejectFlags() {
	*rejectMessage = ""
	*rejectMessageFile = ""
}

func resetAbandonFlags() {
	*abandonMessage = ""
	*abandonMessageFile = ""
}

func resetRequestFlags() {
	*requestMessage = ""
	*requestMessageFile = ""
	*requestReviewers = ""
	*requestSource = "HEAD"
	*requestTarget = "refs/heads/master"
	*requestQuiet = false
	*requestAllowUncommitted = false
	*requestDate = ""
}

func resetSubmitFlags() {
	*submitMerge = false
	*submitRebase = false
	*submitFastForward = false
	*submitTBR = false
	*submitArchive = true
}

func resetRebaseFlags() {
	*rebaseArchive = true
}

func resetWebFlags() {
	*port = 0
	*outputDir = ""
}

// --- Wrapper repo types for testing specific code paths ---

type errEditorRepo struct {
	repository.Repo
}

func (r errEditorRepo) GetCoreEditor() (string, error) {
	return "", fmt.Errorf("no editor configured")
}

type uncommittedRepo struct {
	repository.Repo
}

func (r uncommittedRepo) HasUncommittedChanges() (bool, error) {
	return true, nil
}

type errUncommittedRepo struct {
	repository.Repo
}

func (r errUncommittedRepo) HasUncommittedChanges() (bool, error) {
	return false, fmt.Errorf("git status failed")
}

type strategyRepo struct {
	repository.Repo
	strategy string
}

func (r strategyRepo) GetSubmitStrategy() (string, error) {
	return r.strategy, nil
}

type errStrategyRepo struct {
	repository.Repo
}

func (r errStrategyRepo) GetSubmitStrategy() (string, error) {
	return "", fmt.Errorf("no strategy configured")
}

type errPushRepo struct {
	repository.Repo
}

func (r errPushRepo) PushNotesAndArchive(remote, notesRefPattern, archiveRefPattern string) error {
	return fmt.Errorf("push failed")
}

type nilCommitsRepo struct {
	repository.Repo
}

func (r nilCommitsRepo) ListCommitsBetween(from, to string) ([]string, error) {
	return nil, nil
}

type errVerifyRefRepo struct {
	repository.Repo
}

func (r errVerifyRefRepo) VerifyGitRef(ref string) error {
	if ref == repository.TestTargetRef {
		return fmt.Errorf("bad ref")
	}
	return r.Repo.VerifyGitRef(ref)
}

type errUserEmailRepo struct {
	repository.Repo
}

func (r errUserEmailRepo) GetUserEmail() (string, error) {
	return "", fmt.Errorf("no email configured")
}

type errMergeBaseRepo struct {
	repository.Repo
}

func (r errMergeBaseRepo) MergeBase(a, b string) (string, error) {
	return "", fmt.Errorf("merge-base failed")
}

type errCommitsBetweenRepo struct {
	repository.Repo
}

func (r errCommitsBetweenRepo) ListCommitsBetween(from, to string) ([]string, error) {
	return nil, fmt.Errorf("list commits failed")
}

func (r errCommitsBetweenRepo) MergeBase(a, b string) (string, error) {
	return r.Repo.MergeBase(a, b)
}

type errHeadRefRepo struct {
	repository.Repo
}

func (r errHeadRefRepo) GetHeadRef() (string, error) {
	return "", fmt.Errorf("head ref failed")
}

func (r errHeadRefRepo) HasUncommittedChanges() (bool, error) {
	return false, nil
}

// --- Helpers ---

func writeTestMessageFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "msg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

// setupAcceptedReview creates a fresh mock repo with the review at G accepted
// and the target ref pointing to E (ancestor of I) so submit can succeed.
func setupAcceptedReview(t *testing.T) repository.Repo {
	t.Helper()
	resetAcceptFlags()
	defer resetAcceptFlags()
	repo := repository.NewMockRepoForTest()
	if err := acceptReview(repo, []string{"-m", "LGTM", repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetRef(repository.TestTargetRef, repository.TestCommitE, repository.TestCommitJ); err != nil {
		t.Fatal(err)
	}
	return repo
}

// --- FormatDate tests ---

func TestFormatDateNil(t *testing.T) {
	if got := FormatDate(nil); got != "" {
		t.Errorf("FormatDate(nil) = %q, want %q", got, "")
	}
}

func TestFormatDateValid(t *testing.T) {
	now := time.Unix(1000000000, 0)
	got := FormatDate(&now)
	if got != "1000000000" {
		t.Errorf("FormatDate = %q, want %q", got, "1000000000")
	}
}

// --- GetDate additional tests ---

func TestGetDateEmpty(t *testing.T) {
	t.Setenv("GIT_AUTHOR_DATE", "")
	t.Setenv("GIT_COMMITTER_DATE", "")
	date, err := GetDate("")
	if err != nil {
		t.Fatal(err)
	}
	if date != nil {
		t.Error("expected nil date for empty inputs")
	}
}

func TestGetDateBadTimezoneOffset(t *testing.T) {
	_, err := GetDate("1488452400 INVALID")
	if err == nil {
		t.Error("expected error for bad timezone offset")
	}
}

func TestGetDateRFC3339(t *testing.T) {
	_, err := GetDate("2017-03-02T15:00:00Z")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

// --- Command.Run test ---

func TestCommandRun(t *testing.T) {
	called := false
	cmd := &Command{
		RunMethod: func(repo repository.Repo, args []string) error {
			called = true
			return nil
		},
	}
	if err := cmd.Run(repository.NewMockRepoForTest(), nil); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("RunMethod was not called")
	}
}

// --- CommandMap test ---

func TestCommandMapEntries(t *testing.T) {
	expected := []string{"abandon", "accept", "comment", "list", "pull", "push", "rebase", "reject", "request", "show", "submit", "web"}
	for _, name := range expected {
		if _, ok := CommandMap[name]; !ok {
			t.Errorf("CommandMap missing %q", name)
		}
	}
}

// --- list tests ---

func TestListReviewsDefault(t *testing.T) {
	defer resetListFlags()
	repo := repository.NewMockRepoForTest()
	out := captureStdout(t, func() {
		if err := listReviews(repo, nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "open reviews") {
		t.Errorf("expected 'open reviews' in output, got %q", out)
	}
}

func TestListReviewsAll(t *testing.T) {
	defer resetListFlags()
	repo := repository.NewMockRepoForTest()
	out := captureStdout(t, func() {
		if err := listReviews(repo, []string{"-a"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "reviews") {
		t.Errorf("expected review list output, got %q", out)
	}
}

func TestListReviewsJSON(t *testing.T) {
	defer resetListFlags()
	repo := repository.NewMockRepoForTest()
	out := captureStdout(t, func() {
		if err := listReviews(repo, []string{"-json"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "[") {
		t.Errorf("expected JSON array output, got %q", out)
	}
}

func TestListReviewsAllJSON(t *testing.T) {
	defer resetListFlags()
	repo := repository.NewMockRepoForTest()
	out := captureStdout(t, func() {
		if err := listReviews(repo, []string{"-a", "-json"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "[") {
		t.Errorf("expected JSON array output, got %q", out)
	}
}

// --- push/pull tests ---

func TestPushDefault(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	if err := push(repo, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPushExplicitRemote(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	if err := push(repo, []string{"upstream"}); err != nil {
		t.Fatal(err)
	}
}

func TestPushTooManyArgs(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	if err := push(repo, []string{"a", "b"}); err == nil {
		t.Error("expected error for too many args")
	}
}

func TestPushError(t *testing.T) {
	repo := errPushRepo{repository.NewMockRepoForTest()}
	if err := push(repo, nil); err == nil {
		t.Error("expected error from push")
	}
}

func TestPullDefault(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	if err := pull(repo, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPullExplicitRemote(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	if err := pull(repo, []string{"upstream"}); err != nil {
		t.Fatal(err)
	}
}

func TestPullTooManyArgs(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	if err := pull(repo, []string{"a", "b"}); err == nil {
		t.Error("expected error for too many args")
	}
}

// --- accept tests ---

func TestAcceptReview(t *testing.T) {
	defer resetAcceptFlags()
	repo := repository.NewMockRepoForTest()
	captureStdout(t, func() {
		if err := acceptReview(repo, []string{"-m", "LGTM", repository.TestCommitG}); err != nil {
			t.Fatal(err)
		}
	})
	r, err := review.Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	if r.Resolved == nil || !*r.Resolved {
		t.Error("expected review to be resolved after accept")
	}
}

func TestAcceptReviewTooManyArgs(t *testing.T) {
	defer resetAcceptFlags()
	repo := repository.NewMockRepoForTest()
	if err := acceptReview(repo, []string{"-m", "LGTM", "a", "b"}); err == nil {
		t.Error("expected error for too many args")
	}
}

func TestAcceptReviewNoMatch(t *testing.T) {
	defer resetAcceptFlags()
	repo := repository.NewMockRepoForTest()
	if err := acceptReview(repo, []string{"-m", "LGTM", "nonexistent"}); err == nil {
		t.Error("expected error for nonexistent review")
	}
}

func TestAcceptReviewNoMessage(t *testing.T) {
	resetAcceptFlags()
	defer resetAcceptFlags()
	repo := repository.NewMockRepoForTest()
	if err := acceptReview(repo, []string{repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

func TestAcceptReviewWithMessageFile(t *testing.T) {
	resetAcceptFlags()
	defer resetAcceptFlags()
	repo := repository.NewMockRepoForTest()
	f := writeTestMessageFile(t, "accept from file")
	if err := acceptReview(repo, []string{"-F", f, repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

func TestAcceptReviewBadMessageFile(t *testing.T) {
	resetAcceptFlags()
	defer resetAcceptFlags()
	repo := repository.NewMockRepoForTest()
	if err := acceptReview(repo, []string{"-F", "/nonexistent/file.txt", repository.TestCommitG}); err == nil {
		t.Error("expected error for bad message file")
	}
}

// --- reject tests ---

func TestRejectReview(t *testing.T) {
	defer resetRejectFlags()
	repo := repository.NewMockRepoForTest()
	if err := rejectReview(repo, []string{"-m", "NMW", repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
	r, err := review.Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	if r.Resolved == nil || *r.Resolved {
		t.Error("expected review to be rejected (resolved=false)")
	}
}

func TestRejectReviewTooManyArgs(t *testing.T) {
	defer resetRejectFlags()
	repo := repository.NewMockRepoForTest()
	if err := rejectReview(repo, []string{"-m", "NMW", "a", "b"}); err == nil {
		t.Error("expected error for too many args")
	}
}

func TestRejectReviewNoMatch(t *testing.T) {
	defer resetRejectFlags()
	repo := repository.NewMockRepoForTest()
	if err := rejectReview(repo, []string{"-m", "NMW", "nonexistent"}); err == nil {
		t.Error("expected error for nonexistent review")
	}
}

func TestRejectAbandonedReview(t *testing.T) {
	resetAbandonFlags()
	defer resetAbandonFlags()
	resetRejectFlags()
	defer resetRejectFlags()
	repo := repository.NewMockRepoForTest()
	if err := abandonReview(repo, []string{"-m", "abandoning", repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
	err := rejectReview(repo, []string{"-m", "NMW", repository.TestCommitG})
	if err == nil || !strings.Contains(err.Error(), "abandoned") {
		t.Errorf("expected 'abandoned' error, got %v", err)
	}
}

func TestRejectReviewWithMessageFile(t *testing.T) {
	resetRejectFlags()
	defer resetRejectFlags()
	repo := repository.NewMockRepoForTest()
	f := writeTestMessageFile(t, "reject from file")
	if err := rejectReview(repo, []string{"-F", f, repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

func TestRejectReviewBadMessageFile(t *testing.T) {
	resetRejectFlags()
	defer resetRejectFlags()
	repo := repository.NewMockRepoForTest()
	if err := rejectReview(repo, []string{"-F", "/nonexistent/file.txt", repository.TestCommitG}); err == nil {
		t.Error("expected error for bad message file")
	}
}

func TestRejectReviewEditorError(t *testing.T) {
	resetRejectFlags()
	defer resetRejectFlags()
	repo := errEditorRepo{repository.NewMockRepoForTest()}
	if err := rejectReview(repo, []string{repository.TestCommitG}); err == nil {
		t.Error("expected error when editor fails")
	}
}

// --- abandon tests ---

func TestAbandonReview(t *testing.T) {
	defer resetAbandonFlags()
	repo := repository.NewMockRepoForTest()
	if err := abandonReview(repo, []string{"-m", "abandoning", repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
	r, err := review.Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	if r.Request.TargetRef != "" {
		t.Errorf("expected empty target ref after abandon, got %q", r.Request.TargetRef)
	}
}

func TestAbandonReviewTooManyArgs(t *testing.T) {
	defer resetAbandonFlags()
	repo := repository.NewMockRepoForTest()
	if err := abandonReview(repo, []string{"-m", "abandoning", "a", "b"}); err == nil {
		t.Error("expected error for too many args")
	}
}

func TestAbandonReviewNoMatch(t *testing.T) {
	defer resetAbandonFlags()
	repo := repository.NewMockRepoForTest()
	if err := abandonReview(repo, []string{"-m", "abandoning", "nonexistent"}); err == nil {
		t.Error("expected error for nonexistent review")
	}
}

func TestAbandonReviewWithMessageFile(t *testing.T) {
	resetAbandonFlags()
	defer resetAbandonFlags()
	repo := repository.NewMockRepoForTest()
	f := writeTestMessageFile(t, "abandon from file")
	if err := abandonReview(repo, []string{"-F", f, repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

func TestAbandonReviewBadMessageFile(t *testing.T) {
	resetAbandonFlags()
	defer resetAbandonFlags()
	repo := repository.NewMockRepoForTest()
	if err := abandonReview(repo, []string{"-F", "/nonexistent/file.txt", repository.TestCommitG}); err == nil {
		t.Error("expected error for bad message file")
	}
}

func TestAbandonReviewEditorError(t *testing.T) {
	resetAbandonFlags()
	defer resetAbandonFlags()
	repo := errEditorRepo{repository.NewMockRepoForTest()}
	if err := abandonReview(repo, []string{repository.TestCommitG}); err == nil {
		t.Error("expected error when editor fails")
	}
}

// --- comment tests ---

func TestCommentHashExists(t *testing.T) {
	threads := []review.CommentThread{
		{Hash: "abc"},
		{Hash: "def", Children: []review.CommentThread{{Hash: "ghi"}}},
	}
	if !commentHashExists("abc", threads) {
		t.Error("expected to find hash 'abc'")
	}
	if !commentHashExists("ghi", threads) {
		t.Error("expected to find nested hash 'ghi'")
	}
	if commentHashExists("xyz", threads) {
		t.Error("should not find hash 'xyz'")
	}
}

func TestCommentHashExistsEmpty(t *testing.T) {
	if commentHashExists("any", nil) {
		t.Error("should not find hash in nil threads")
	}
}

func TestCommentOnReview(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessage = "great work"
	if err := commentOnReview(repo, []string{repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

func TestCommentOnReviewTooManyArgs(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessage = "msg"
	if err := commentOnReview(repo, []string{"a", "b"}); err == nil {
		t.Error("expected error for too many args")
	}
}

func TestCommentOnReviewNoMatch(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessage = "msg"
	if err := commentOnReview(repo, []string{"nonexistent"}); err == nil {
		t.Error("expected error for nonexistent review")
	}
}

func TestCommentOnReviewWithLGTM(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessage = "looks good"
	*commentLgtm = true
	if err := commentOnReview(repo, []string{repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

func TestCommentOnReviewWithNMW(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessage = "needs work"
	*commentNmw = true
	if err := commentOnReview(repo, []string{repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

func TestCommentOnReviewNoArgs(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessage = "comment on current"
	if err := commentOnReview(repo, nil); err == nil {
		t.Error("expected error for no matching review at HEAD")
	}
}

func TestCommentOnPath(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessage = "file comment"
	*commentFile = "foo.txt"
	if err := commentOnPath(repo, nil); err != nil {
		t.Fatal(err)
	}
}

func TestCommentOnPathNoFile(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessage = "msg"
	if err := commentOnPath(repo, nil); err == nil {
		t.Error("expected error when no file specified for detached comment")
	}
}

func TestCommentOnPathTooManyArgs(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentFile = "foo.txt"
	*commentMessage = "msg"
	if err := commentOnPath(repo, []string{"a", "b"}); err == nil {
		t.Error("expected error for too many args")
	}
}

func TestCommentOnPathWithRef(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessage = "file comment at ref"
	*commentFile = "foo.txt"
	if err := commentOnPath(repo, []string{repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

func TestCommentOnPathBadRef(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessage = "file comment"
	*commentFile = "foo.txt"
	if err := commentOnPath(repo, []string{"nonexistent_ref"}); err == nil {
		t.Error("expected error for bad ref")
	}
}

func TestValidateArgsLgtmAndNmw(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentLgtm = true
	*commentNmw = true
	if err := validateArgs(repo, nil, nil); err == nil {
		t.Error("expected error when both lgtm and nmw are set")
	}
}

func TestValidateArgsParentNotFound(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentParent = "nonexistent"
	*commentMessage = "msg"
	if err := validateArgs(repo, nil, nil); err == nil {
		t.Error("expected error for non-matching parent comment")
	}
}

func TestValidateArgsWithMessageFile(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	f := writeTestMessageFile(t, "comment from file")
	*commentMessageFile = f
	if err := validateArgs(repo, nil, nil); err != nil {
		t.Fatal(err)
	}
	if *commentMessage != "comment from file" {
		t.Errorf("expected message from file, got %q", *commentMessage)
	}
}

func TestValidateArgsBadMessageFile(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessageFile = "/nonexistent/path/file.txt"
	if err := validateArgs(repo, nil, nil); err == nil {
		t.Error("expected error for nonexistent message file")
	}
}

func TestValidateArgsEditorError(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := errEditorRepo{repository.NewMockRepoForTest()}
	if err := validateArgs(repo, nil, nil); err == nil {
		t.Error("expected error when editor fails")
	}
}

func TestBuildCommentFromFlagsBasic(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessage = "test comment"
	c, err := buildCommentFromFlags(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	if c.Description != "test comment" {
		t.Errorf("expected description 'test comment', got %q", c.Description)
	}
}

func TestBuildCommentFromFlagsWithFile(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessage = "file comment"
	*commentFile = "foo.txt"
	c, err := buildCommentFromFlags(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	if c.Location.Path != "foo.txt" {
		t.Errorf("expected path 'foo.txt', got %q", c.Location.Path)
	}
}

func TestBuildCommentFromFlagsWithDate(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessage = "dated comment"
	*commentDate = "1000000000 +0000"
	c, err := buildCommentFromFlags(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	if c.Timestamp != "1000000000" {
		t.Errorf("expected timestamp '1000000000', got %q", c.Timestamp)
	}
}

func TestBuildCommentFromFlagsBadDate(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessage = "test"
	*commentDate = "INVALID DATE"
	if _, err := buildCommentFromFlags(repo, repository.TestCommitG); err == nil {
		t.Error("expected error for bad date")
	}
}

func TestBuildCommentFromFlagsLGTM(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessage = "lgtm comment"
	*commentLgtm = true
	c, err := buildCommentFromFlags(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	if c.Resolved == nil || !*c.Resolved {
		t.Error("expected resolved=true for lgtm comment")
	}
}

func TestBuildCommentFromFlagsNMW(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessage = "needs work"
	*commentNmw = true
	c, err := buildCommentFromFlags(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	if c.Resolved == nil || *c.Resolved {
		t.Error("expected resolved=false for nmw comment")
	}
}

// --- show tests ---

func TestShowReviewDefault(t *testing.T) {
	resetShowFlags()
	defer resetShowFlags()
	repo := repository.NewMockRepoForTest()
	out := captureStdout(t, func() {
		if err := showReview(repo, []string{repository.TestCommitG}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, repository.TestCommitG[:1]) {
		t.Errorf("expected review revision in output, got %q", out)
	}
}

func TestShowReviewJSON(t *testing.T) {
	resetShowFlags()
	defer resetShowFlags()
	repo := repository.NewMockRepoForTest()
	*showJSONOutput = true
	out := captureStdout(t, func() {
		if err := showReview(repo, []string{repository.TestCommitG}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "revision") {
		t.Errorf("expected JSON output with 'revision', got %q", out)
	}
}

func TestShowReviewDiff(t *testing.T) {
	resetShowFlags()
	defer resetShowFlags()
	repo := repository.NewMockRepoForTest()
	*showDiffOutput = true
	captureStdout(t, func() {
		if err := showReview(repo, []string{repository.TestCommitG}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestShowReviewDiffWithOpts(t *testing.T) {
	resetShowFlags()
	defer resetShowFlags()
	repo := repository.NewMockRepoForTest()
	*showDiffOutput = true
	*showDiffOptions = "opt1,opt2"
	captureStdout(t, func() {
		if err := showReview(repo, []string{repository.TestCommitG}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestShowReviewInline(t *testing.T) {
	resetShowFlags()
	defer resetShowFlags()
	repo := repository.NewMockRepoForTest()
	*showInlineOutput = true
	out := captureStdout(t, func() {
		if err := showReview(repo, []string{repository.TestCommitG}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "commit:") {
		t.Errorf("expected inline output with 'commit:', got %q", out)
	}
}

func TestShowReviewDiffOptsWithoutDiff(t *testing.T) {
	resetShowFlags()
	defer resetShowFlags()
	repo := repository.NewMockRepoForTest()
	*showDiffOptions = "some-opt"
	if err := showReview(repo, []string{repository.TestCommitG}); err == nil {
		t.Error("expected error when --diff-opts used without --diff")
	}
}

func TestShowReviewTooManyArgs(t *testing.T) {
	resetShowFlags()
	defer resetShowFlags()
	repo := repository.NewMockRepoForTest()
	if err := showReview(repo, []string{"a", "b"}); err == nil {
		t.Error("expected error for too many args")
	}
}

func TestShowReviewNoMatch(t *testing.T) {
	resetShowFlags()
	defer resetShowFlags()
	repo := repository.NewMockRepoForTest()
	if err := showReview(repo, []string{"nonexistent"}); err == nil {
		t.Error("expected error for nonexistent review")
	}
}

func TestShowReviewNoArgs(t *testing.T) {
	resetShowFlags()
	defer resetShowFlags()
	repo := repository.NewMockRepoForTest()
	if err := showReview(repo, nil); err == nil {
		t.Error("expected error for no matching review at HEAD")
	}
}

func TestShowDetachedCommentsNoPath(t *testing.T) {
	resetShowFlags()
	defer resetShowFlags()
	repo := repository.NewMockRepoForTest()
	if err := showDetachedComments(repo, nil); err == nil {
		t.Error("expected error when no path specified")
	}
}

func TestShowDetachedCommentsTooManyPaths(t *testing.T) {
	resetShowFlags()
	defer resetShowFlags()
	repo := repository.NewMockRepoForTest()
	if err := showDetachedComments(repo, []string{"a", "b"}); err == nil {
		t.Error("expected error for too many paths")
	}
}

func TestShowDetachedCommentsDiffFlagConflict(t *testing.T) {
	resetShowFlags()
	defer resetShowFlags()
	repo := repository.NewMockRepoForTest()
	*showDiffOutput = true
	if err := showDetachedComments(repo, []string{"foo.txt"}); err == nil {
		t.Error("expected error when --diff combined with -d")
	}
}

func TestShowDetachedComments(t *testing.T) {
	resetShowFlags()
	defer resetShowFlags()
	repo := repository.NewMockRepoForTest()
	out := captureStdout(t, func() {
		if err := showDetachedComments(repo, []string{"foo.txt"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "comment threads") {
		t.Errorf("expected comment threads header, got %q", out)
	}
}

func TestShowDetachedCommentsJSON(t *testing.T) {
	resetShowFlags()
	defer resetShowFlags()
	repo := repository.NewMockRepoForTest()
	*showJSONOutput = true
	captureStdout(t, func() {
		if err := showDetachedComments(repo, []string{"foo.txt"}); err != nil {
			t.Fatal(err)
		}
	})
}

// --- request tests ---

func TestGetReviewCommitWithExplicitArg(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := repository.NewMockRepoForTest()
	r, err := buildRequestFromFlags("user@test.com")
	if err != nil {
		t.Fatal(err)
	}
	commit, base, err := getReviewCommit(repo, r, []string{repository.TestCommitG})
	if err != nil {
		t.Fatal(err)
	}
	if commit != repository.TestCommitG {
		t.Errorf("expected commit %q, got %q", repository.TestCommitG, commit)
	}
	if base == "" {
		t.Error("expected non-empty base commit")
	}
}

func TestGetReviewCommitTooManyArgs(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := repository.NewMockRepoForTest()
	r, _ := buildRequestFromFlags("user@test.com")
	if _, _, err := getReviewCommit(repo, r, []string{"a", "b"}); err == nil {
		t.Error("expected error for too many args")
	}
}

func TestGetReviewCommitDefault(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := repository.NewMockRepoForTest()
	*requestSource = repository.TestReviewRef
	*requestTarget = repository.TestTargetRef
	r, err := buildRequestFromFlags("user@test.com")
	if err != nil {
		t.Fatal(err)
	}
	commit, _, err := getReviewCommit(repo, r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if commit == "" {
		t.Error("expected non-empty review commit")
	}
}

func TestGetReviewCommitNilCommits(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := nilCommitsRepo{repository.NewMockRepoForTest()}
	*requestSource = repository.TestReviewRef
	*requestTarget = repository.TestTargetRef
	r, err := buildRequestFromFlags("user@test.com")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = getReviewCommit(repo, r, nil)
	if err == nil || !strings.Contains(err.Error(), "no commits") {
		t.Errorf("expected 'no commits' error, got %v", err)
	}
}

func TestRequestReview(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := repository.NewMockRepoForTest()
	out := captureStdout(t, func() {
		err := requestReview(repo, []string{
			"-m", "test review",
			"-source", repository.TestReviewRef,
			"-target", repository.TestTargetRef,
			"-allow-uncommitted",
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "Review requested") {
		t.Errorf("expected request summary, got %q", out)
	}
}

func TestRequestReviewQuiet(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := repository.NewMockRepoForTest()
	out := captureStdout(t, func() {
		err := requestReview(repo, []string{
			"-m", "quiet review",
			"-source", repository.TestReviewRef,
			"-target", repository.TestTargetRef,
			"-allow-uncommitted",
			"-quiet",
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(out, "Review requested") {
		t.Errorf("expected no output with -quiet, got %q", out)
	}
}

func TestRequestReviewUncommittedChanges(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := repository.NewMockRepoForTest()
	// Mock's HasUncommittedChanges returns false, so this succeeds.
	err := requestReview(repo, []string{
		"-m", "test review",
		"-source", repository.TestReviewRef,
		"-target", repository.TestTargetRef,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRequestReviewUncommittedChangesBlocked(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := uncommittedRepo{repository.NewMockRepoForTest()}
	err := requestReview(repo, []string{
		"-m", "test",
		"-source", repository.TestReviewRef,
		"-target", repository.TestTargetRef,
	})
	if err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Errorf("expected 'uncommitted' error, got %v", err)
	}
}

func TestRequestReviewUncommittedError(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := errUncommittedRepo{repository.NewMockRepoForTest()}
	err := requestReview(repo, []string{
		"-m", "test",
		"-source", repository.TestReviewRef,
		"-target", repository.TestTargetRef,
	})
	if err == nil {
		t.Error("expected error from HasUncommittedChanges")
	}
}

func TestRequestReviewWithReviewers(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := repository.NewMockRepoForTest()
	out := captureStdout(t, func() {
		err := requestReview(repo, []string{
			"-m", "test review",
			"-r", "alice,bob",
			"-source", repository.TestReviewRef,
			"-target", repository.TestTargetRef,
			"-allow-uncommitted",
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "Review requested") {
		t.Errorf("expected request summary, got %q", out)
	}
}

func TestRequestReviewWithMessageFile(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := repository.NewMockRepoForTest()
	f := writeTestMessageFile(t, "review from file")
	out := captureStdout(t, func() {
		err := requestReview(repo, []string{
			"-F", f,
			"-source", repository.TestReviewRef,
			"-target", repository.TestTargetRef,
			"-allow-uncommitted",
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "review from file") {
		t.Errorf("expected message from file in output, got %q", out)
	}
}

func TestRequestReviewNoDescription(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := repository.NewMockRepoForTest()
	out := captureStdout(t, func() {
		err := requestReview(repo, []string{
			"-source", repository.TestReviewRef,
			"-target", repository.TestTargetRef,
			"-allow-uncommitted",
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "Review requested") {
		t.Errorf("expected request summary, got %q", out)
	}
}

func TestRequestReviewHEADSource(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := repository.NewMockRepoForTest()
	out := captureStdout(t, func() {
		err := requestReview(repo, []string{
			"-m", "test",
			"-target", repository.TestTargetRef,
			"-allow-uncommitted",
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "Review requested") {
		t.Errorf("expected request summary, got %q", out)
	}
}

func TestRequestReviewBadTarget(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := repository.NewMockRepoForTest()
	err := requestReview(repo, []string{
		"-m", "test",
		"-source", repository.TestReviewRef,
		"-target", "refs/heads/nonexistent",
		"-allow-uncommitted",
	})
	if err == nil {
		t.Error("expected error for bad target ref")
	}
}

func TestRequestReviewBadSource(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := repository.NewMockRepoForTest()
	err := requestReview(repo, []string{
		"-m", "test",
		"-source", "refs/heads/nonexistent",
		"-target", repository.TestTargetRef,
		"-allow-uncommitted",
	})
	if err == nil {
		t.Error("expected error for bad source ref")
	}
}

func TestBuildRequestFromFlagsWithMessageFile(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	f := writeTestMessageFile(t, "request from file")
	*requestMessageFile = f
	r, err := buildRequestFromFlags("user@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if r.Description != "request from file" {
		t.Errorf("expected description from file, got %q", r.Description)
	}
}

func TestBuildRequestFromFlagsBadMessageFile(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	*requestMessageFile = "/nonexistent/file.txt"
	if _, err := buildRequestFromFlags("user@test.com"); err == nil {
		t.Error("expected error for bad message file")
	}
}

// --- rebase tests ---

func TestValidateRebaseRequestTooManyArgs(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	if _, err := validateRebaseRequest(repo, []string{"a", "b"}); err == nil {
		t.Error("expected error for too many args")
	}
}

func TestValidateRebaseRequestNoMatch(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	if _, err := validateRebaseRequest(repo, []string{"nonexistent"}); err == nil {
		t.Error("expected error for nonexistent review")
	}
}

func TestValidateRebaseRequest(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	r, err := validateRebaseRequest(repo, []string{repository.TestCommitG})
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Error("expected non-nil review")
	}
}

func TestRebaseSubmittedReview(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	_, err := validateRebaseRequest(repo, []string{repository.TestCommitB})
	if err == nil || !strings.Contains(err.Error(), "already been submitted") {
		t.Errorf("expected 'submitted' error, got %v", err)
	}
}

func TestRebaseAbandonedReview(t *testing.T) {
	resetAbandonFlags()
	defer resetAbandonFlags()
	repo := repository.NewMockRepoForTest()
	if err := abandonReview(repo, []string{"-m", "abandoning", repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
	_, err := validateRebaseRequest(repo, []string{repository.TestCommitG})
	if err == nil || !strings.Contains(err.Error(), "abandoned") {
		t.Errorf("expected 'abandoned' error, got %v", err)
	}
}

func TestValidateRebaseRequestBadTargetRef(t *testing.T) {
	resetRebaseFlags()
	defer resetRebaseFlags()
	repo := errVerifyRefRepo{repository.NewMockRepoForTest()}
	if _, err := validateRebaseRequest(repo, []string{repository.TestCommitG}); err == nil {
		t.Error("expected error for bad target ref")
	}
}

func TestRebaseReview(t *testing.T) {
	defer resetRebaseFlags()
	repo := repository.NewMockRepoForTest()
	if err := rebaseReview(repo, []string{repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

func TestRebaseReviewError(t *testing.T) {
	resetRebaseFlags()
	defer resetRebaseFlags()
	repo := repository.NewMockRepoForTest()
	if err := rebaseReview(repo, []string{"nonexistent"}); err == nil {
		t.Error("expected error for nonexistent review")
	}
}

// --- submit tests ---

func TestSubmitReviewNotAccepted(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := repository.NewMockRepoForTest()
	if err := submitReview(repo, []string{repository.TestCommitG}); err == nil {
		t.Error("expected error for unaccepted review")
	}
}

func TestSubmitReviewTooManyArgs(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := repository.NewMockRepoForTest()
	if err := submitReview(repo, []string{"a", "b"}); err == nil {
		t.Error("expected error for too many args")
	}
}

func TestSubmitReviewNoMatch(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := repository.NewMockRepoForTest()
	if err := submitReview(repo, []string{"nonexistent"}); err == nil {
		t.Error("expected error for nonexistent review")
	}
}

func TestSubmitReviewMergeAndRebase(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := repository.NewMockRepoForTest()
	if err := submitReview(repo, []string{"-merge", "-rebase", repository.TestCommitG}); err == nil {
		t.Error("expected error when both --merge and --rebase")
	}
}

func TestSubmitReviewTBRNonFastForward(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := repository.NewMockRepoForTest()
	err := submitReview(repo, []string{"-tbr", repository.TestCommitG})
	if err == nil || !strings.Contains(err.Error(), "non-fast-forward") {
		t.Errorf("expected non-fast-forward error, got %v", err)
	}
}

func TestSubmitReviewAlreadySubmitted(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := repository.NewMockRepoForTest()
	err := submitReview(repo, []string{repository.TestCommitB})
	if err == nil || !strings.Contains(err.Error(), "already been submitted") {
		t.Errorf("expected 'already submitted' error, got %v", err)
	}
}

func TestSubmitReviewFastForward(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := setupAcceptedReview(t)
	*submitFastForward = true
	if err := submitReview(repo, []string{repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

func TestSubmitReviewMerge(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := setupAcceptedReview(t)
	*submitMerge = true
	if err := submitReview(repo, []string{repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

func TestSubmitReviewRebase(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := setupAcceptedReview(t)
	*submitRebase = true
	if err := submitReview(repo, []string{repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

func TestSubmitReviewDefaultStrategy(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := setupAcceptedReview(t)
	if err := submitReview(repo, []string{repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

func TestSubmitReviewRebaseStrategy(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := setupAcceptedReview(t)
	repo = strategyRepo{repo, "rebase"}
	if err := submitReview(repo, []string{repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

func TestSubmitReviewFastForwardStrategy(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := setupAcceptedReview(t)
	repo = strategyRepo{repo, "fast-forward"}
	if err := submitReview(repo, []string{repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

func TestSubmitReviewStrategyError(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := setupAcceptedReview(t)
	repo = errStrategyRepo{repo}
	if err := submitReview(repo, []string{repository.TestCommitG}); err == nil {
		t.Error("expected error from GetSubmitStrategy")
	}
}

func TestSubmitReviewBadTargetRef(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := setupAcceptedReview(t)
	repo = errVerifyRefRepo{repo}
	if err := submitReview(repo, []string{repository.TestCommitG}); err == nil {
		t.Error("expected error for bad target ref")
	}
}

func TestSubmitReviewTBRFastForward(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := repository.NewMockRepoForTest()
	if err := repo.SetRef(repository.TestTargetRef, repository.TestCommitE, repository.TestCommitJ); err != nil {
		t.Fatal(err)
	}
	*submitTBR = true
	*submitFastForward = true
	if err := submitReview(repo, []string{repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

// --- web tests ---

func TestWebUsage(t *testing.T) {
	out := captureStdout(t, func() { usage("test-app") })
	if !strings.Contains(out, "test-app") {
		t.Errorf("expected arg0 in usage output, got %q", out)
	}
}

func TestWebCmdNoFlags(t *testing.T) {
	resetWebFlags()
	defer resetWebFlags()
	// webCmd.RunMethod (web.go:117-142) checks:
	//   if *port != 0 { webServe(...) }  // skipped when port==0
	//   if *outputDir == "" && *port == 0 { return error }
	// So webServe is never called.
	if *port != 0 {
		t.Fatal("precondition: *port must be 0")
	}
	repo := repository.NewMockRepoForTest()
	captureStdout(t, func() {
		if err := webCmd.RunMethod(repo, nil); err == nil {
			t.Error("expected error when neither -port nor -output specified")
		}
	})
}

func TestWebCmdWithOutput(t *testing.T) {
	resetWebFlags()
	defer resetWebFlags()
	repo := repository.NewMockRepoForTest()
	dir := t.TempDir()
	if err := webCmd.RunMethod(repo, []string{"-output", dir + "/web-out"}); err != nil {
		t.Fatal(err)
	}
}

func TestWebGenerateStatic(t *testing.T) {
	resetWebFlags()
	defer resetWebFlags()
	repo := repository.NewMockRepoForTest()
	dir := t.TempDir()
	*outputDir = dir + "/static"
	repoDetails, err := web.NewRepoDetails(repo)
	if err != nil {
		t.Fatal(err)
	}
	// webGenerateStatic calls repoDetails.Update() internally (web.go:26),
	// so no separate Update() call is needed here.
	if err := webGenerateStatic(repoDetails); err != nil {
		t.Fatal(err)
	}
}

func TestWebGenerateStaticExistingDir(t *testing.T) {
	resetWebFlags()
	defer resetWebFlags()
	repo := repository.NewMockRepoForTest()
	dir := t.TempDir()
	outPath := dir + "/static"
	if err := os.Mkdir(outPath, 0755); err != nil {
		t.Fatal(err)
	}
	*outputDir = outPath
	repoDetails, err := web.NewRepoDetails(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := webGenerateStatic(repoDetails); err != nil {
		t.Fatal(err)
	}
}

func TestWebGenerateStaticBadDir(t *testing.T) {
	resetWebFlags()
	defer resetWebFlags()
	repo := repository.NewMockRepoForTest()
	*outputDir = "/nonexistent/path/static"
	repoDetails, err := web.NewRepoDetails(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := webGenerateStatic(repoDetails); err == nil {
		t.Error("expected error for bad output dir")
	}
}

func TestWebSetupHandlers(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	repoDetails, err := web.NewRepoDetails(repo)
	if err != nil {
		t.Fatal(err)
	}
	// Update() is called explicitly here because webSetupHandlers does not
	// call it internally (unlike webGenerateStatic which does).
	if err := repoDetails.Update(); err != nil {
		t.Fatal(err)
	}
	mux := webSetupHandlers(repoDetails)
	if mux == nil {
		t.Fatal("expected non-nil mux")
	}

	req := httptest.NewRequest(http.MethodGet, "/_ah/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("health check returned %d, want 200", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("health check returned %q, want 'ok'", rec.Body.String())
	}

	// ServePaths.Css() returns a non-empty path like "style.css?...".
	var paths web.ServePaths
	cssPath, _, _ := strings.Cut(paths.Css(), "?")
	if cssPath == "" {
		t.Fatal("expected non-empty CSS path from ServePaths")
	}
	req = httptest.NewRequest(http.MethodGet, "/"+cssPath, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("stylesheet returned %d, want 200", rec.Code)
	}

	repoPath, _, _ := strings.Cut(paths.Repo(), "?")
	req = httptest.NewRequest(http.MethodGet, "/"+repoPath, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("repo endpoint returned %d, want 200", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code < 200 || rec.Code >= 400 {
		t.Errorf("entry point returned %d", rec.Code)
	}
}

// --- Usage function tests ---
// Each command's Usage closure is defined as a struct literal. Calling
// it directly covers those lines.

func TestAbandonUsage(t *testing.T) {
	out := captureStdout(t, func() { abandonCmd.Usage("test-app") })
	if !strings.Contains(out, "abandon") {
		t.Errorf("expected 'abandon' in usage output, got %q", out)
	}
}

func TestAcceptUsage(t *testing.T) {
	out := captureStdout(t, func() { acceptCmd.Usage("test-app") })
	if !strings.Contains(out, "accept") {
		t.Errorf("expected 'accept' in usage output, got %q", out)
	}
}

func TestCommentUsage(t *testing.T) {
	out := captureStdout(t, func() { commentCmd.Usage("test-app") })
	if !strings.Contains(out, "comment") {
		t.Errorf("expected 'comment' in usage output, got %q", out)
	}
}

func TestListUsage(t *testing.T) {
	out := captureStdout(t, func() { listCmd.Usage("test-app") })
	if !strings.Contains(out, "list") {
		t.Errorf("expected 'list' in usage output, got %q", out)
	}
}

func TestRebaseUsage(t *testing.T) {
	out := captureStdout(t, func() { rebaseCmd.Usage("test-app") })
	if !strings.Contains(out, "rebase") {
		t.Errorf("expected 'rebase' in usage output, got %q", out)
	}
}

func TestRejectUsage(t *testing.T) {
	out := captureStdout(t, func() { rejectCmd.Usage("test-app") })
	if !strings.Contains(out, "reject") {
		t.Errorf("expected 'reject' in usage output, got %q", out)
	}
}

func TestRequestUsage(t *testing.T) {
	out := captureStdout(t, func() { requestCmd.Usage("test-app") })
	if !strings.Contains(out, "request") {
		t.Errorf("expected 'request' in usage output, got %q", out)
	}
}

func TestShowUsage(t *testing.T) {
	out := captureStdout(t, func() { showCmd.Usage("test-app") })
	if !strings.Contains(out, "show") {
		t.Errorf("expected 'show' in usage output, got %q", out)
	}
}

func TestSubmitUsage(t *testing.T) {
	out := captureStdout(t, func() { submitCmd.Usage("test-app") })
	if !strings.Contains(out, "submit") {
		t.Errorf("expected 'submit' in usage output, got %q", out)
	}
}

// --- No-args tests (trigger GetCurrent → nil review path) ---
// The mock's HEAD doesn't correspond to a review, so GetCurrent
// returns nil. This covers the else{GetCurrent} and r==nil branches.

func TestAbandonNoArgs(t *testing.T) {
	resetAbandonFlags()
	defer resetAbandonFlags()
	repo := repository.NewMockRepoForTest()
	*abandonMessage = "msg"
	err := abandonReview(repo, nil)
	if err == nil || !strings.Contains(err.Error(), "no matching review") {
		t.Errorf("expected 'no matching review' error, got %v", err)
	}
}

func TestAcceptNoArgs(t *testing.T) {
	resetAcceptFlags()
	defer resetAcceptFlags()
	repo := repository.NewMockRepoForTest()
	*acceptMessage = "LGTM"
	err := acceptReview(repo, nil)
	if err == nil || !strings.Contains(err.Error(), "no matching review") {
		t.Errorf("expected 'no matching review' error, got %v", err)
	}
}

func TestRejectNoArgs(t *testing.T) {
	resetRejectFlags()
	defer resetRejectFlags()
	repo := repository.NewMockRepoForTest()
	*rejectMessage = "NMW"
	err := rejectReview(repo, nil)
	if err == nil || !strings.Contains(err.Error(), "no matching review") {
		t.Errorf("expected 'no matching review' error, got %v", err)
	}
}

func TestSubmitNoArgs(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := repository.NewMockRepoForTest()
	err := submitReview(repo, nil)
	if err == nil || !strings.Contains(err.Error(), "no matching review") {
		t.Errorf("expected 'no matching review' error, got %v", err)
	}
}

func TestRebaseNoArgs(t *testing.T) {
	resetRebaseFlags()
	defer resetRebaseFlags()
	repo := repository.NewMockRepoForTest()
	_, err := validateRebaseRequest(repo, nil)
	if err == nil || !strings.Contains(err.Error(), "no matching review") {
		t.Errorf("expected 'no matching review' error, got %v", err)
	}
}

func TestCommentOnReviewNoArgsGetCurrent(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessage = "msg"
	err := commentOnReview(repo, nil)
	if err == nil || !strings.Contains(err.Error(), "no matching review") {
		t.Errorf("expected 'no matching review' error, got %v", err)
	}
}

// --- RunMethod wrapper tests ---
// Calling through RunMethod covers the closure and flag parsing.

func TestAbandonCmdRunMethod(t *testing.T) {
	resetAbandonFlags()
	defer resetAbandonFlags()
	repo := repository.NewMockRepoForTest()
	if err := abandonCmd.RunMethod(repo, []string{"-m", "abandoning", repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

func TestAcceptCmdRunMethod(t *testing.T) {
	resetAcceptFlags()
	defer resetAcceptFlags()
	repo := repository.NewMockRepoForTest()
	if err := acceptCmd.RunMethod(repo, []string{"-m", "LGTM", repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

func TestRejectCmdRunMethod(t *testing.T) {
	resetRejectFlags()
	defer resetRejectFlags()
	repo := repository.NewMockRepoForTest()
	if err := rejectCmd.RunMethod(repo, []string{"-m", "NMW", repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

func TestCommentCmdRunMethod(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	if err := commentCmd.RunMethod(repo, []string{"-m", "test", repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

func TestCommentCmdRunMethodDetached(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	if err := commentCmd.RunMethod(repo, []string{"-d", "-f", "foo.txt", "-m", "detached"}); err != nil {
		t.Fatal(err)
	}
}

func TestListCmdRunMethod(t *testing.T) {
	resetListFlags()
	defer resetListFlags()
	repo := repository.NewMockRepoForTest()
	captureStdout(t, func() {
		if err := listCmd.RunMethod(repo, nil); err != nil {
			t.Fatal(err)
		}
	})
}

func TestShowCmdRunMethod(t *testing.T) {
	resetShowFlags()
	defer resetShowFlags()
	repo := repository.NewMockRepoForTest()
	captureStdout(t, func() {
		if err := showCmd.RunMethod(repo, []string{repository.TestCommitG}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestShowCmdRunMethodDetached(t *testing.T) {
	resetShowFlags()
	defer resetShowFlags()
	repo := repository.NewMockRepoForTest()
	captureStdout(t, func() {
		if err := showCmd.RunMethod(repo, []string{"-d", "foo.txt"}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestRequestCmdRunMethod(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := repository.NewMockRepoForTest()
	captureStdout(t, func() {
		err := requestCmd.RunMethod(repo, []string{
			"-m", "test review",
			"-source", repository.TestReviewRef,
			"-target", repository.TestTargetRef,
			"-allow-uncommitted",
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestRebaseCmdRunMethod(t *testing.T) {
	resetRebaseFlags()
	defer resetRebaseFlags()
	repo := repository.NewMockRepoForTest()
	if err := rebaseCmd.RunMethod(repo, []string{repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

func TestSubmitCmdRunMethod(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := setupAcceptedReview(t)
	if err := submitCmd.RunMethod(repo, []string{repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

// --- Additional coverage tests ---

func TestBuildRequestFromFlagsBadDate(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	*requestDate = "INVALID DATE"
	if _, err := buildRequestFromFlags("user@test.com"); err == nil {
		t.Error("expected error for bad date")
	}
}

func TestShowReviewInlineWithDiffOpts(t *testing.T) {
	resetShowFlags()
	defer resetShowFlags()
	repo := repository.NewMockRepoForTest()
	*showInlineOutput = true
	*showDiffOptions = "opt1,opt2"
	captureStdout(t, func() {
		if err := showReview(repo, []string{repository.TestCommitG}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestShowDiffOptsErrorMessage(t *testing.T) {
	resetShowFlags()
	defer resetShowFlags()
	repo := repository.NewMockRepoForTest()
	*showDiffOptions = "some-opt"
	err := showReview(repo, []string{repository.TestCommitG})
	if err == nil || !strings.Contains(err.Error(), "--inline") {
		t.Errorf("expected error mentioning --inline, got %v", err)
	}
}

// --- Error propagation tests using wrapper repos ---

func TestAbandonUserEmailError(t *testing.T) {
	resetAbandonFlags()
	defer resetAbandonFlags()
	repo := errUserEmailRepo{repository.NewMockRepoForTest()}
	*abandonMessage = "msg"
	err := abandonReview(repo, []string{repository.TestCommitG})
	if err == nil || !strings.Contains(err.Error(), "no email") {
		t.Errorf("expected 'no email' error, got %v", err)
	}
}

func TestAcceptUserEmailError(t *testing.T) {
	resetAcceptFlags()
	defer resetAcceptFlags()
	repo := errUserEmailRepo{repository.NewMockRepoForTest()}
	*acceptMessage = "LGTM"
	err := acceptReview(repo, []string{repository.TestCommitG})
	if err == nil || !strings.Contains(err.Error(), "no email") {
		t.Errorf("expected 'no email' error, got %v", err)
	}
}

func TestRejectUserEmailError(t *testing.T) {
	resetRejectFlags()
	defer resetRejectFlags()
	repo := errUserEmailRepo{repository.NewMockRepoForTest()}
	*rejectMessage = "NMW"
	err := rejectReview(repo, []string{repository.TestCommitG})
	if err == nil || !strings.Contains(err.Error(), "no email") {
		t.Errorf("expected 'no email' error, got %v", err)
	}
}

func TestBuildCommentFromFlagsUserEmailError(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := errUserEmailRepo{repository.NewMockRepoForTest()}
	*commentMessage = "test"
	_, err := buildCommentFromFlags(repo, repository.TestCommitG)
	if err == nil || !strings.Contains(err.Error(), "no email") {
		t.Errorf("expected 'no email' error, got %v", err)
	}
}

func TestRequestReviewUserEmailError(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := errUserEmailRepo{repository.NewMockRepoForTest()}
	err := requestReview(repo, []string{
		"-m", "test",
		"-source", repository.TestReviewRef,
		"-target", repository.TestTargetRef,
		"-allow-uncommitted",
	})
	if err == nil || !strings.Contains(err.Error(), "no email") {
		t.Errorf("expected 'no email' error, got %v", err)
	}
}

func TestCommentOnReviewValidateArgsError(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessage = "msg"
	*commentLgtm = true
	*commentNmw = true
	err := commentOnReview(repo, []string{repository.TestCommitG})
	if err == nil || !strings.Contains(err.Error(), "-lgtm and -nmw") {
		t.Errorf("expected lgtm/nmw conflict error, got %v", err)
	}
}

func TestCommentOnPathValidateArgsError(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessage = "msg"
	*commentFile = "foo.txt"
	*commentLgtm = true
	*commentNmw = true
	err := commentOnPath(repo, nil)
	if err == nil || !strings.Contains(err.Error(), "-lgtm and -nmw") {
		t.Errorf("expected lgtm/nmw conflict error, got %v", err)
	}
}

func TestGetReviewCommitMergeBaseErrorWithArg(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := errMergeBaseRepo{repository.NewMockRepoForTest()}
	r, _ := buildRequestFromFlags("user@test.com")
	_, _, err := getReviewCommit(repo, r, []string{repository.TestCommitG})
	if err == nil || !strings.Contains(err.Error(), "merge-base") {
		t.Errorf("expected merge-base error, got %v", err)
	}
}

func TestGetReviewCommitMergeBaseErrorNoArgs(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	*requestSource = repository.TestReviewRef
	*requestTarget = repository.TestTargetRef
	repo := errMergeBaseRepo{repository.NewMockRepoForTest()}
	r, _ := buildRequestFromFlags("user@test.com")
	_, _, err := getReviewCommit(repo, r, nil)
	if err == nil || !strings.Contains(err.Error(), "merge-base") {
		t.Errorf("expected merge-base error, got %v", err)
	}
}

func TestGetReviewCommitListCommitsError(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	*requestSource = repository.TestReviewRef
	*requestTarget = repository.TestTargetRef
	repo := errCommitsBetweenRepo{repository.NewMockRepoForTest()}
	r, _ := buildRequestFromFlags("user@test.com")
	_, _, err := getReviewCommit(repo, r, nil)
	if err == nil || !strings.Contains(err.Error(), "list commits") {
		t.Errorf("expected list commits error, got %v", err)
	}
}

func TestRequestReviewHeadRefError(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := errHeadRefRepo{repository.NewMockRepoForTest()}
	err := requestReview(repo, []string{
		"-m", "test",
		"-target", repository.TestTargetRef,
		"-allow-uncommitted",
	})
	if err == nil || !strings.Contains(err.Error(), "head ref") {
		t.Errorf("expected head ref error, got %v", err)
	}
}
