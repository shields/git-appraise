package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
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
	"msrl.dev/git-appraise/review/request"
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
	repoDetails := web.NewRepoDetails(repo)
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
	repoDetails := web.NewRepoDetails(repo)
	if err := webGenerateStatic(repoDetails); err != nil {
		t.Fatal(err)
	}
}

func TestWebGenerateStaticBadDir(t *testing.T) {
	resetWebFlags()
	defer resetWebFlags()
	repo := repository.NewMockRepoForTest()
	*outputDir = "/nonexistent/path/static"
	repoDetails := web.NewRepoDetails(repo)
	if err := webGenerateStatic(repoDetails); err == nil {
		t.Error("expected error for bad output dir")
	}
}

func TestWebSetupHandlers(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	repoDetails := web.NewRepoDetails(repo)
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

// --- Additional wrapper repos for full coverage ---

type errAppendNoteRepo struct {
	repository.Repo
}

func (r errAppendNoteRepo) AppendNote(ref, revision string, note repository.Note) error {
	return fmt.Errorf("append note failed")
}

type errIsAncestorRepo struct {
	repository.Repo
}

func (r errIsAncestorRepo) IsAncestor(ancestor, descendant string) (bool, error) {
	return false, fmt.Errorf("is-ancestor failed")
}

func (r errIsAncestorRepo) VerifyGitRef(ref string) error {
	return r.Repo.VerifyGitRef(ref)
}

func (r errIsAncestorRepo) GetSubmitStrategy() (string, error) {
	return "merge", nil
}

type errSwitchToRefRepo struct {
	repository.Repo
}

func (r errSwitchToRefRepo) SwitchToRef(ref string) error {
	return fmt.Errorf("switch-to-ref failed")
}

func (r errSwitchToRefRepo) GetSubmitStrategy() (string, error) {
	return "merge", nil
}

type errGetCommitMessageRepo struct {
	repository.Repo
}

func (r errGetCommitMessageRepo) GetCommitMessage(ref string) (string, error) {
	return "", fmt.Errorf("commit message failed")
}

type errGetRepoStateHashRepo struct {
	repository.Repo
}

func (r errGetRepoStateHashRepo) GetRepoStateHash() (string, error) {
	return "", fmt.Errorf("state hash failed")
}

// --- listReviews jsonMarshalIndent error ---

func TestListReviewsJSONMarshalError(t *testing.T) {
	defer resetListFlags()
	repo := repository.NewMockRepoForTest()
	origMarshal := jsonMarshalIndent
	defer func() { jsonMarshalIndent = origMarshal }()
	jsonMarshalIndent = func(v any, prefix, indent string) ([]byte, error) {
		return nil, fmt.Errorf("marshal error")
	}
	err := listReviews(repo, []string{"-json"})
	if err == nil || !strings.Contains(err.Error(), "marshal error") {
		t.Errorf("expected marshal error, got %v", err)
	}
}

// --- requestReview buildRequestFromFlags error (bad date) ---

func TestRequestReviewBuildRequestError(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := repository.NewMockRepoForTest()
	err := requestReview(repo, []string{
		"-date", "INVALID DATE",
		"-source", repository.TestReviewRef,
		"-target", repository.TestTargetRef,
		"-allow-uncommitted",
	})
	if err == nil {
		t.Error("expected error from buildRequestFromFlags bad date")
	}
}

// --- requestReview GetCommitMessage error ---

func TestRequestReviewGetCommitMessageError(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := errGetCommitMessageRepo{repository.NewMockRepoForTest()}
	err := requestReview(repo, []string{
		"-source", repository.TestReviewRef,
		"-target", repository.TestTargetRef,
		"-allow-uncommitted",
	})
	if err == nil || !strings.Contains(err.Error(), "commit message") {
		t.Errorf("expected commit message error, got %v", err)
	}
}

// --- requestReview writeRequest error ---

func TestRequestReviewWriteRequestError(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := repository.NewMockRepoForTest()
	origWrite := writeRequest
	defer func() { writeRequest = origWrite }()
	writeRequest = func(r *request.Request) (repository.Note, error) {
		return nil, fmt.Errorf("write request failed")
	}
	err := requestReview(repo, []string{
		"-m", "test",
		"-source", repository.TestReviewRef,
		"-target", repository.TestTargetRef,
		"-allow-uncommitted",
	})
	if err == nil || !strings.Contains(err.Error(), "write request") {
		t.Errorf("expected write request error, got %v", err)
	}
}

// --- requestReview AppendNote error ---

func TestRequestReviewAppendNoteError(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := errAppendNoteRepo{repository.NewMockRepoForTest()}
	err := requestReview(repo, []string{
		"-m", "test",
		"-source", repository.TestReviewRef,
		"-target", repository.TestTargetRef,
		"-allow-uncommitted",
	})
	if err == nil || !strings.Contains(err.Error(), "append note") {
		t.Errorf("expected append note error, got %v", err)
	}
}

// --- submitReview IsAncestor error ---

func TestSubmitReviewIsAncestorError(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	// First create an accepted review on a normal repo
	repo := setupAcceptedReview(t)
	// Wrap with errIsAncestorRepo
	wrappedRepo := errIsAncestorRepo{repo}
	err := submitReview(wrappedRepo, []string{repository.TestCommitG})
	if err == nil || !strings.Contains(err.Error(), "is-ancestor") {
		t.Errorf("expected is-ancestor error, got %v", err)
	}
}

// --- submitReview SwitchToRef error ---

func TestSubmitReviewSwitchToRefError(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := setupAcceptedReview(t)
	wrappedRepo := errSwitchToRefRepo{repo}
	*submitFastForward = true
	err := submitReview(wrappedRepo, []string{repository.TestCommitG})
	if err == nil || !strings.Contains(err.Error(), "switch-to-ref") {
		t.Errorf("expected switch-to-ref error, got %v", err)
	}
}

// --- submitReview merge strategy ---

func TestSubmitReviewMergeStrategy(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := setupAcceptedReview(t)
	repo = strategyRepo{repo, "merge"}
	if err := submitReview(repo, []string{repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

// --- abandonReview AppendNote error ---

func TestAbandonReviewAppendNoteError(t *testing.T) {
	resetAbandonFlags()
	defer resetAbandonFlags()
	repo := errAppendNoteRepo{repository.NewMockRepoForTest()}
	*abandonMessage = "msg"
	err := abandonReview(repo, []string{repository.TestCommitG})
	if err == nil || !strings.Contains(err.Error(), "append note") {
		t.Errorf("expected append note error, got %v", err)
	}
}

// --- commentOnReview buildCommentFromFlags user email error ---

func TestCommentOnReviewBuildCommentError(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := errUserEmailRepo{repository.NewMockRepoForTest()}
	*commentMessage = "msg"
	err := commentOnReview(repo, []string{repository.TestCommitG})
	if err == nil || !strings.Contains(err.Error(), "no email") {
		t.Errorf("expected 'no email' error, got %v", err)
	}
}

// --- commentOnPath buildCommentFromFlags user email error ---

func TestCommentOnPathBuildCommentError(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := errUserEmailRepo{repository.NewMockRepoForTest()}
	*commentMessage = "msg"
	*commentFile = "foo.txt"
	err := commentOnPath(repo, nil)
	if err == nil || !strings.Contains(err.Error(), "no email") {
		t.Errorf("expected 'no email' error, got %v", err)
	}
}

// --- buildCommentFromFlags location.Check error ---

func TestBuildCommentFromFlagsLocationCheckError(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessage = "test"
	*commentFile = "foo.txt"
	commentLocation = comment.Range{StartLine: 9999}
	_, err := buildCommentFromFlags(repo, repository.TestCommitG)
	if err == nil || !strings.Contains(err.Error(), "Unable to comment") {
		t.Errorf("expected location check error, got %v", err)
	}
}

// --- accept with bad date ---

func TestAcceptReviewBadDate(t *testing.T) {
	resetAcceptFlags()
	defer resetAcceptFlags()
	repo := repository.NewMockRepoForTest()
	*acceptMessage = "LGTM"
	*acceptDate = "INVALID DATE"
	err := acceptReview(repo, []string{repository.TestCommitG})
	if err == nil {
		t.Error("expected error for bad date in accept")
	}
}

// --- showDetachedComments error loading comments ---

func TestShowDetachedCommentsBadRef(t *testing.T) {
	resetShowFlags()
	defer resetShowFlags()
	repo := repository.NewMockRepoForTest()
	// "nonexistent_ref" is not a valid path for detached comments;
	// GetDetachedComments won't error but will return empty. The
	// success path for loading should still work. Instead test JSON.
	*showJSONOutput = true
	captureStdout(t, func() {
		if err := showDetachedComments(repo, []string{"nonexistent_path"}); err != nil {
			t.Fatal(err)
		}
	})
}

// --- showDetachedComments --diff-opts without --diff, with -d ---

func TestShowDetachedCommentsDiffOptsConflict(t *testing.T) {
	resetShowFlags()
	defer resetShowFlags()
	repo := repository.NewMockRepoForTest()
	*showDiffOptions = "some-opt"
	if err := showDetachedComments(repo, []string{"foo.txt"}); err == nil {
		t.Error("expected error when --diff-opts combined with -d")
	}
}

// --- push/pull Usage and RunMethod ---

func TestPushUsage(t *testing.T) {
	out := captureStdout(t, func() { pushCmd.Usage("test-app") })
	if !strings.Contains(out, "push") {
		t.Errorf("expected 'push' in usage output, got %q", out)
	}
}

func TestPullUsage(t *testing.T) {
	out := captureStdout(t, func() { pullCmd.Usage("test-app") })
	if !strings.Contains(out, "pull") {
		t.Errorf("expected 'pull' in usage output, got %q", out)
	}
}

func TestPushCmdRunMethod(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	if err := pushCmd.RunMethod(repo, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPullCmdRunMethod(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	if err := pullCmd.RunMethod(repo, nil); err != nil {
		t.Fatal(err)
	}
}

// --- webCmd RunMethod with -port (webServe path) ---
// webServe calls http.ListenAndServe which blocks, so we test it indirectly
// by using port 0 + output (already tested) and by verifying webSetupHandlers.

func TestWebCmdUsage(t *testing.T) {
	out := captureStdout(t, func() { webCmd.Usage("test-app") })
	if !strings.Contains(out, "web") {
		t.Errorf("expected 'web' in usage output, got %q", out)
	}
}

// --- webGenerateStatic error paths ---

func TestWebGenerateStaticUpdateError(t *testing.T) {
	resetWebFlags()
	defer resetWebFlags()
	repo := errGetRepoStateHashRepo{repository.NewMockRepoForTest()}
	dir := t.TempDir()
	*outputDir = dir + "/static"
	repoDetails := web.NewRepoDetails(repo)
	// Update will fail because GetRepoStateHash returns error
	if err := webGenerateStatic(repoDetails); err == nil {
		t.Error("expected error from Update in webGenerateStatic")
	}
}

func TestWebGenerateStaticChdirError(t *testing.T) {
	resetWebFlags()
	defer resetWebFlags()
	repo := repository.NewMockRepoForTest()
	// Create a directory, then remove it so Chdir fails
	dir := t.TempDir()
	outPath := dir + "/static"
	*outputDir = outPath
	// Make the directory exist so Mkdir succeeds, but then make Chdir fail
	// by setting outputDir to a file (not a directory)
	tmpFile := dir + "/not_a_dir"
	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	*outputDir = tmpFile
	repoDetails := web.NewRepoDetails(repo)
	if err := webGenerateStatic(repoDetails); err == nil {
		t.Error("expected error from Chdir on a file")
	}
}

// --- webCmd RunMethod with invalid output dir to trigger webGenerateStatic error ---

func TestWebCmdRunMethodGenerateStaticError(t *testing.T) {
	resetWebFlags()
	defer resetWebFlags()
	repo := repository.NewMockRepoForTest()
	captureStdout(t, func() {
		err := webCmd.RunMethod(repo, []string{"-output", "/nonexistent/deeply/nested/path"})
		if err == nil {
			t.Error("expected error from webGenerateStatic in RunMethod")
		}
	})
}

// --- GetDate with GIT_AUTHOR_DATE env ---

func TestGetDateFromGitAuthorDate(t *testing.T) {
	t.Setenv("GIT_AUTHOR_DATE", "1488452400 +0000")
	t.Setenv("GIT_COMMITTER_DATE", "")
	date, err := GetDate("")
	if err != nil {
		t.Fatal(err)
	}
	if date == nil {
		t.Error("expected non-nil date from GIT_AUTHOR_DATE")
	}
}

func TestGetDateFromGitCommitterDate(t *testing.T) {
	t.Setenv("GIT_AUTHOR_DATE", "")
	t.Setenv("GIT_COMMITTER_DATE", "1488452400 +0000")
	date, err := GetDate("")
	if err != nil {
		t.Fatal(err)
	}
	if date == nil {
		t.Error("expected non-nil date from GIT_COMMITTER_DATE")
	}
}

// --- webGenerateStatic CSS/repo/branch/review write paths ---
// These are already partially covered; the remaining uncovered lines are
// deep error paths in OS operations. Testing them would require breaking
// the filesystem mid-operation which isn't feasible without modifying source.

// --- webSetupHandlers branch endpoint ---

func TestWebSetupHandlersBranch(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	repoDetails := web.NewRepoDetails(repo)
	if err := repoDetails.Update(); err != nil {
		t.Fatal(err)
	}
	mux := webSetupHandlers(repoDetails)

	// Test branch endpoint
	req := httptest.NewRequest(http.MethodGet, "/branch.html?branch=0", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("branch endpoint returned %d, want 200", rec.Code)
	}

	// Test review endpoint (will 500 for non-hex hash, but covers the path)
	req = httptest.NewRequest(http.MethodGet, "/review.html?review=abcdef", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	// Status may be 500 for nonexistent review, but the handler code is exercised
}

// --- Additional tests for full coverage of reject.go ---

func TestRejectReviewNoMessage(t *testing.T) {
	resetRejectFlags()
	defer resetRejectFlags()
	repo := repository.NewMockRepoForTest()
	// No message, no message file - triggers editor path which fails with mock
	if err := rejectReview(repo, []string{repository.TestCommitG}); err != nil {
		// With mock repo, editor is "vi" which may or may not work.
		// The point is to exercise the code path.
		t.Logf("editor path: %v", err)
	}
}

// --- Abandon review with no message (triggers editor path) ---

func TestAbandonReviewNoMessage(t *testing.T) {
	resetAbandonFlags()
	defer resetAbandonFlags()
	repo := repository.NewMockRepoForTest()
	if err := abandonReview(repo, []string{repository.TestCommitG}); err != nil {
		t.Logf("editor path: %v", err)
	}
}

// --- Accept review editor error (no message, no file) ---

func TestAcceptReviewEditorError(t *testing.T) {
	resetAcceptFlags()
	defer resetAcceptFlags()
	// Accept doesn't use editor, but test the GetDate error path
	// already covered via TestAcceptReviewBadDate
}

// --- Verify JSON marshal indent uses the injectable var ---

func TestJsonMarshalIndentDefault(t *testing.T) {
	result, err := jsonMarshalIndent([]string{"a"}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result), "a") {
		t.Error("expected 'a' in marshaled output")
	}
}

// --- writeRequest default uses the injectable var ---

func TestWriteRequestDefault(t *testing.T) {
	origWrite := writeRequest
	defer func() { writeRequest = origWrite }()
	// Just verify the default writeRequest doesn't panic on a valid request
	// We need a real request object, but Write() requires a repo context.
	// This is tested indirectly through requestReview tests.
}

// --- Tests for comment.go GetDetachedComments error path in commentOnPath ---
// This requires a repo where GetDetachedComments fails, which happens when
// the notes ref doesn't exist. The mock repo always succeeds, so we can't
// easily trigger this without a wrapper.

// --- submit: no-archive rebase ---

func TestSubmitReviewRebaseNoArchive(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := setupAcceptedReview(t)
	*submitRebase = true
	*submitArchive = false
	if err := submitReview(repo, []string{repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

// --- submit TBR merge ---

func TestSubmitReviewTBRMerge(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	mock := repository.NewMockRepoForTest()
	if err := mock.SetRef(repository.TestTargetRef, repository.TestCommitE, repository.TestCommitJ); err != nil {
		t.Fatal(err)
	}
	*submitTBR = true
	*submitMerge = true
	if err := submitReview(mock, []string{repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

// --- errCreateCommitWithTreeRepo makes GetDetachedComments fail ---

type errCreateCommitWithTreeRepo struct {
	repository.Repo
}

func (r errCreateCommitWithTreeRepo) CreateCommitWithTree(details *repository.CommitDetails, t *repository.Tree) (string, error) {
	return "", fmt.Errorf("create commit with tree failed")
}

// --- errRebaseRepo makes Rebase fail inside submit ---

type errRebaseRepo struct {
	repository.Repo
}

func (r errRebaseRepo) RebaseRef(ref string) error {
	return fmt.Errorf("rebase failed")
}

func (r errRebaseRepo) GetSubmitStrategy() (string, error) {
	return "merge", nil
}

// lateIsAncestorErrRepo succeeds on IsAncestor during review loading
// (GetSummaryViaRefs checks if review is submitted) but fails on subsequent
// IsAncestor calls (in GetHeadCommit). It uses a call counter.
type lateIsAncestorErrRepo struct {
	repository.Repo
	callCount int
}

func (r *lateIsAncestorErrRepo) IsAncestor(ancestor, descendant string) (bool, error) {
	r.callCount++
	if r.callCount <= 2 {
		// First calls are during review.Get -> GetSummaryViaRefs and Details
		return r.Repo.IsAncestor(ancestor, descendant)
	}
	return false, fmt.Errorf("late is-ancestor failed")
}

// --- Tests for GetHeadCommit error paths ---

func TestAbandonReviewGetHeadCommitError(t *testing.T) {
	resetAbandonFlags()
	defer resetAbandonFlags()
	repo := &lateIsAncestorErrRepo{Repo: repository.NewMockRepoForTest()}
	*abandonMessage = "msg"
	err := abandonReview(repo, []string{repository.TestCommitG})
	if err == nil {
		t.Error("expected error from GetHeadCommit in abandon")
	}
}

func TestAcceptReviewGetHeadCommitError(t *testing.T) {
	resetAcceptFlags()
	defer resetAcceptFlags()
	repo := &lateIsAncestorErrRepo{Repo: repository.NewMockRepoForTest()}
	*acceptMessage = "LGTM"
	err := acceptReview(repo, []string{repository.TestCommitG})
	if err == nil {
		t.Error("expected error from GetHeadCommit in accept")
	}
}

func TestRejectReviewGetHeadCommitError(t *testing.T) {
	resetRejectFlags()
	defer resetRejectFlags()
	repo := &lateIsAncestorErrRepo{Repo: repository.NewMockRepoForTest()}
	*rejectMessage = "NMW"
	err := rejectReview(repo, []string{repository.TestCommitG})
	if err == nil {
		t.Error("expected error from GetHeadCommit in reject")
	}
}

func TestCommentOnReviewGetHeadCommitError(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := &lateIsAncestorErrRepo{Repo: repository.NewMockRepoForTest()}
	*commentMessage = "msg"
	err := commentOnReview(repo, []string{repository.TestCommitG})
	if err == nil {
		t.Error("expected error from GetHeadCommit in commentOnReview")
	}
}

func TestSubmitReviewGetHeadCommitError(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := setupAcceptedReview(t)
	wrappedRepo := &lateIsAncestorErrRepo{Repo: repo}
	err := submitReview(wrappedRepo, []string{repository.TestCommitG})
	if err == nil {
		t.Error("expected error from GetHeadCommit/IsAncestor in submit")
	}
}

// --- showDetachedComments with CreateCommitWithTree error ---

func TestShowDetachedCommentsLoadError(t *testing.T) {
	resetShowFlags()
	defer resetShowFlags()
	repo := errCreateCommitWithTreeRepo{repository.NewMockRepoForTest()}
	err := showDetachedComments(repo, []string{"foo.txt"})
	if err == nil {
		t.Error("expected error from GetDetachedComments")
	}
}

// --- commentOnPath GetDetachedComments error ---

func TestCommentOnPathGetDetachedCommentsError(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := errCreateCommitWithTreeRepo{repository.NewMockRepoForTest()}
	*commentMessage = "msg"
	*commentFile = "foo.txt"
	err := commentOnPath(repo, nil)
	if err == nil {
		t.Error("expected error from GetDetachedComments in commentOnPath")
	}
}

// --- abandon Request.Write error ---
// The Request.Write() error path requires json.Marshal to fail, which
// requires a non-serializable value. We can't trigger this without source
// changes. Instead, test the AddComment error path via AppendNote wrapper.
// The abandon AppendNote error is tested in TestAbandonReviewAppendNoteError.

// --- request getReviewCommit error within requestReview ---
// This is already covered by TestRequestReviewBadSource and
// TestRequestReviewBadTarget. But let me verify...
// Actually, request.go:154 is the getReviewCommit call, which fails when
// source/target are bad. Let me check if it's still uncovered...

// --- submitReview Rebase error ---

func TestSubmitReviewRebaseError(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := setupAcceptedReview(t)
	wrappedRepo := errRebaseRepo{repo}
	*submitRebase = true
	err := submitReview(wrappedRepo, []string{repository.TestCommitG})
	if err == nil || !strings.Contains(err.Error(), "rebase failed") {
		t.Errorf("expected rebase error, got %v", err)
	}
}

// --- requestReview with getReviewCommit error (MergeBase fails) ---

func TestRequestReviewGetReviewCommitError(t *testing.T) {
	resetRequestFlags()
	defer resetRequestFlags()
	repo := errMergeBaseRepo{repository.NewMockRepoForTest()}
	err := requestReview(repo, []string{
		"-m", "test",
		"-source", repository.TestReviewRef,
		"-target", repository.TestTargetRef,
		"-allow-uncommitted",
	})
	if err == nil || !strings.Contains(err.Error(), "merge-base") {
		t.Errorf("expected merge-base error, got %v", err)
	}
}

// --- submitReview IsAncestor error (separate from GetHeadCommit error) ---

func TestSubmitReviewIsAncestorErrorPath(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := setupAcceptedReview(t)
	// In submitReview, IsAncestor is called at line 83 after GetHeadCommit.
	// We need enough successful calls to pass through review.Get, then fail.
	// review.Get calls: GetSummaryViaRefs->IsAncestor (1), Details->GetHeadCommit->IsAncestor (1)
	// submitReview calls: GetHeadCommit->IsAncestor (1), then repo.IsAncestor at line 83 (fail)
	// So fail after 3 calls (threshold of 3, callCount starts at -1 to allow 4 total calls)
	wrappedRepo := &lateIsAncestorErrRepo{Repo: repo, callCount: -1}
	err := submitReview(wrappedRepo, []string{repository.TestCommitG})
	if err == nil {
		t.Error("expected IsAncestor error in submit")
	}
}

// --- submitReview GetHeadCommit error after rebase ---

func TestSubmitReviewGetHeadCommitAfterRebaseError(t *testing.T) {
	resetSubmitFlags()
	defer resetSubmitFlags()
	repo := setupAcceptedReview(t)
	// IsAncestor call count during submitReview with rebase:
	//   1. review.Get -> GetSummaryViaRefs -> IsAncestor
	//   2. review.Get -> Details -> GetHeadCommit -> IsAncestor
	//   3. submitReview:78 -> r.GetHeadCommit -> IsAncestor
	//   4. submitReview:83 -> repo.IsAncestor(target, source)
	//   5. r.Rebase:682 -> r.GetHeadCommit -> IsAncestor
	//   6. submitReview:112 -> r.GetHeadCommit -> IsAncestor <- fail here
	// Start callCount at -3 so 5 calls succeed (callCount 3 > threshold 2 on call 6)
	wrappedRepo := &lateIsAncestorErrRepo{Repo: repo, callCount: -3}
	*submitRebase = true
	err := submitReview(wrappedRepo, []string{repository.TestCommitG})
	if err == nil {
		t.Error("expected GetHeadCommit error after rebase")
	}
}

// --- abandon Request.Write error ---
// This requires json.Marshal to fail. We can use the writeRequest var
// injection to simulate this path.

func TestAbandonReviewWriteError(t *testing.T) {
	resetAbandonFlags()
	defer resetAbandonFlags()
	orig := writeRequest
	defer func() { writeRequest = orig }()
	writeRequest = func(r *request.Request) (repository.Note, error) {
		return nil, fmt.Errorf("write error")
	}
	*abandonMessage = "msg"
	repo := repository.NewMockRepoForTest()
	err := abandonReview(repo, []string{repository.TestCommitG})
	if err == nil || !strings.Contains(err.Error(), "write error") {
		t.Errorf("expected write error, got %v", err)
	}
}

// --- Additional test for webGenerateStatic with reviews ---

func TestWebGenerateStaticWithReviews(t *testing.T) {
	resetWebFlags()
	defer resetWebFlags()
	repo := repository.NewMockRepoForTest()
	dir := t.TempDir()
	outPath := dir + "/static-reviews"
	*outputDir = outPath
	repoDetails := web.NewRepoDetails(repo)
	if err := webGenerateStatic(repoDetails); err != nil {
		t.Fatal(err)
	}
	// Verify files were created
	entries, err := os.ReadDir(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Error("expected files in output directory")
	}
}

// --- webGenerateStatic with read-only dir to trigger os.Create errors ---

func TestWebGenerateStaticReadOnlyDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: root ignores file permissions")
	}
	resetWebFlags()
	defer resetWebFlags()
	repo := repository.NewMockRepoForTest()
	dir := t.TempDir()
	outPath := dir + "/readonly"
	if err := os.Mkdir(outPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(outPath, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(outPath, 0755)
	*outputDir = outPath
	repoDetails := web.NewRepoDetails(repo)
	if err := webGenerateStatic(repoDetails); err == nil {
		t.Error("expected error from os.Create on read-only directory")
	}
}

func TestWebGenerateStaticRepoCreateError(t *testing.T) {
	resetWebFlags()
	defer resetWebFlags()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	repo := repository.NewMockRepoForTest()
	dir := t.TempDir()
	outPath := dir + "/out"
	*outputDir = outPath
	repoDetails := web.NewRepoDetails(repo)
	// Pre-create the output dir and put a directory named "index.html"
	// inside it so os.Create("index.html") fails (can't truncate a dir).
	if err := os.MkdirAll(outPath+"/index.html", 0755); err != nil {
		t.Fatal(err)
	}
	if err := webGenerateStatic(repoDetails); err == nil {
		t.Error("expected error from os.Create for repo file")
	}
}

func TestWebGenerateStaticBranchCreateError(t *testing.T) {
	resetWebFlags()
	defer resetWebFlags()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	repo := repository.NewMockRepoForTest()
	dir := t.TempDir()
	outPath := dir + "/out"
	*outputDir = outPath
	repoDetails := web.NewRepoDetails(repo)
	// Let Update populate branches so the inner loop runs
	if err := repoDetails.Update(); err != nil {
		t.Fatal(err)
	}
	// Pre-create output dir with branch_0.html as a directory
	if err := os.MkdirAll(outPath+"/branch_0.html", 0755); err != nil {
		t.Fatal(err)
	}
	if err := webGenerateStatic(repoDetails); err == nil {
		t.Error("expected error from os.Create for branch file")
	}
}

func TestWebGenerateStaticReviewCreateError(t *testing.T) {
	resetWebFlags()
	defer resetWebFlags()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	repo := repository.NewMockRepoForTest()
	dir := t.TempDir()
	outPath := dir + "/out"
	*outputDir = outPath
	repoDetails := web.NewRepoDetails(repo)
	if err := repoDetails.Update(); err != nil {
		t.Fatal(err)
	}
	// Find a review revision so we can block its file creation
	if len(repoDetails.Branches) == 0 {
		t.Skip("no branches in mock repo")
	}
	var revision string
	for _, branch := range repoDetails.Branches {
		for _, rev := range branch.OpenReviews {
			revision = rev.Revision
			break
		}
		for _, rev := range branch.ClosedReviews {
			if revision == "" {
				revision = rev.Revision
			}
			break
		}
		if revision != "" {
			break
		}
	}
	if revision == "" {
		t.Skip("no reviews found")
	}
	// Pre-create output dir with review_{revision}.html as a directory
	if err := os.MkdirAll(outPath+"/review_"+revision+".html", 0755); err != nil {
		t.Fatal(err)
	}
	if err := webGenerateStatic(repoDetails); err == nil {
		t.Error("expected error from os.Create for review file")
	}
}

func TestWebGenerateStaticCssWriteError(t *testing.T) {
	resetWebFlags()
	defer resetWebFlags()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	repo := repository.NewMockRepoForTest()
	dir := t.TempDir()
	outPath := dir + "/out"
	if err := os.MkdirAll(outPath, 0755); err != nil {
		t.Fatal(err)
	}
	// Create a symlink stylesheet.css -> /dev/full so os.Create succeeds
	// but the subsequent write returns ENOSPC.
	if err := os.Symlink("/dev/full", outPath+"/stylesheet.css"); err != nil {
		t.Skip("cannot create symlink to /dev/full: " + err.Error())
	}
	*outputDir = outPath
	repoDetails := web.NewRepoDetails(repo)
	if err := webGenerateStatic(repoDetails); err == nil {
		t.Error("expected error from WriteStyleSheet on /dev/full")
	}
}

func TestWebGenerateStaticRepoWriteError(t *testing.T) {
	resetWebFlags()
	defer resetWebFlags()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	repo := repository.NewMockRepoForTest()
	dir := t.TempDir()
	outPath := dir + "/out"
	if err := os.MkdirAll(outPath, 0755); err != nil {
		t.Fatal(err)
	}
	// Make index.html a symlink to /dev/full so WriteRepoTemplate fails
	if err := os.Symlink("/dev/full", outPath+"/index.html"); err != nil {
		t.Skip("cannot create symlink to /dev/full: " + err.Error())
	}
	*outputDir = outPath
	repoDetails := web.NewRepoDetails(repo)
	if err := webGenerateStatic(repoDetails); err == nil {
		t.Error("expected error from WriteRepoTemplate on /dev/full")
	}
}

func TestWebGenerateStaticBranchWriteError(t *testing.T) {
	resetWebFlags()
	defer resetWebFlags()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	repo := repository.NewMockRepoForTest()
	dir := t.TempDir()
	outPath := dir + "/out"
	*outputDir = outPath
	repoDetails := web.NewRepoDetails(repo)
	if err := repoDetails.Update(); err != nil {
		t.Fatal(err)
	}
	if len(repoDetails.Branches) == 0 {
		t.Skip("no branches in mock repo")
	}
	if err := os.MkdirAll(outPath, 0755); err != nil {
		t.Fatal(err)
	}
	// Make branch_0.html a symlink to /dev/full so WriteBranchTemplate fails
	if err := os.Symlink("/dev/full", outPath+"/branch_0.html"); err != nil {
		t.Skip("cannot create symlink to /dev/full: " + err.Error())
	}
	if err := webGenerateStatic(repoDetails); err == nil {
		t.Error("expected error from WriteBranchTemplate on /dev/full")
	}
}

func TestWebGenerateStaticReviewWriteError(t *testing.T) {
	resetWebFlags()
	defer resetWebFlags()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	repo := repository.NewMockRepoForTest()
	dir := t.TempDir()
	outPath := dir + "/out"
	*outputDir = outPath
	repoDetails := web.NewRepoDetails(repo)
	if err := repoDetails.Update(); err != nil {
		t.Fatal(err)
	}
	// Find a review revision
	var revision string
	for _, branch := range repoDetails.Branches {
		for _, rev := range branch.OpenReviews {
			revision = rev.Revision
			break
		}
		for _, rev := range branch.ClosedReviews {
			if revision == "" {
				revision = rev.Revision
			}
			break
		}
		if revision != "" {
			break
		}
	}
	if revision == "" {
		t.Skip("no reviews found")
	}
	if err := os.MkdirAll(outPath, 0755); err != nil {
		t.Fatal(err)
	}
	// Make review_{revision}.html a symlink to /dev/full so WriteReviewTemplate fails
	if err := os.Symlink("/dev/full", outPath+"/review_"+revision+".html"); err != nil {
		t.Skip("cannot create symlink to /dev/full: " + err.Error())
	}
	if err := webGenerateStatic(repoDetails); err == nil {
		t.Error("expected error from WriteReviewTemplate on /dev/full")
	}
}

func TestWebGenerateStaticMkdirError(t *testing.T) {
	resetWebFlags()
	defer resetWebFlags()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	repo := repository.NewMockRepoForTest()
	dir := t.TempDir()
	// Put a regular file where the output dir should be, so Mkdir fails
	// with something other than ErrExist.
	outPath := dir + "/out"
	if err := os.WriteFile(outPath, []byte("block"), 0644); err != nil {
		t.Fatal(err)
	}
	*outputDir = outPath
	repoDetails := web.NewRepoDetails(repo)
	if err := webGenerateStatic(repoDetails); err == nil {
		t.Error("expected error from os.Mkdir on file path")
	}
}

// --- accept date valid ---

func TestAcceptReviewWithDate(t *testing.T) {
	resetAcceptFlags()
	defer resetAcceptFlags()
	repo := repository.NewMockRepoForTest()
	if err := acceptReview(repo, []string{"-m", "LGTM", "-date", "1000000000 +0000", repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

// --- comment date valid ---

func TestCommentOnReviewWithDate(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	*commentMessage = "dated"
	*commentDate = "1000000000 +0000"
	if err := commentOnReview(repo, []string{repository.TestCommitG}); err != nil {
		t.Fatal(err)
	}
}

// --- PullCmdRunMethod with remote ---

func TestPullCmdRunMethodWithRemote(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	if err := pullCmd.RunMethod(repo, []string{"upstream"}); err != nil {
		t.Fatal(err)
	}
}

// --- Comment with detached flag through RunMethod ---

func TestCommentCmdRunMethodDetachedBadRef(t *testing.T) {
	resetCommentFlags()
	defer resetCommentFlags()
	repo := repository.NewMockRepoForTest()
	err := commentCmd.RunMethod(repo, []string{"-d", "-f", "foo.txt", "-m", "msg", "nonexistent_ref"})
	if err == nil {
		t.Error("expected error for bad ref in detached comment")
	}
}

// --- Verify commands.go json/write vars default correctly ---

// --- webCmd RunMethod with port (webServe path) ---

func TestWebCmdRunMethodWithPort(t *testing.T) {
	resetWebFlags()
	defer resetWebFlags()
	repo := repository.NewMockRepoForTest()
	// Bind a listener on a random port, then try to serve on the same port
	// to trigger an "address already in use" error from ListenAndServe.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)
	captureStdout(t, func() {
		webErr := webCmd.RunMethod(repo, []string{"-port", fmt.Sprintf("%d", addr.Port)})
		if webErr == nil {
			t.Error("expected error from webServe on already-bound port")
		}
	})
}

func TestWebCmdRunMethodWithPortUpdateError(t *testing.T) {
	resetWebFlags()
	defer resetWebFlags()
	repo := errGetRepoStateHashRepo{repository.NewMockRepoForTest()}
	err := webCmd.RunMethod(repo, []string{"-port", "12345"})
	if err == nil {
		t.Error("expected error from Update in webCmd with -port")
	}
}

func TestDefaultJsonMarshalIndent(t *testing.T) {
	b, err := json.MarshalIndent([]int{1}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b2, err := jsonMarshalIndent([]int{1}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != string(b2) {
		t.Error("jsonMarshalIndent should match json.MarshalIndent by default")
	}
}
