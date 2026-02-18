package output

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"msrl.dev/git-appraise/repository"
	"msrl.dev/git-appraise/review"
	"msrl.dev/git-appraise/review/analyses"
	"msrl.dev/git-appraise/review/ci"
	"msrl.dev/git-appraise/review/comment"
	"msrl.dev/git-appraise/review/request"
)

// captureStdout redirects os.Stdout to capture printed output.
// Not safe for parallel use; none of these tests call t.Parallel().
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

func boolPtr(b bool) *bool { return &b }

func testMockRepo() repository.Repo {
	return repository.NewMockRepoForTest()
}

// errShowRepo wraps a Repo and makes Show return an error.
type errShowRepo struct {
	repository.Repo
}

func (r *errShowRepo) Show(string, string) (string, error) {
	return "", errors.New("show error")
}

// customDiffRepo wraps a Repo and overrides ParsedDiff1 to return
// fragments with all three line operation types and multiple fragments.
type customDiffRepo struct {
	repository.Repo
}

func (r *customDiffRepo) ParsedDiff1(string, ...string) ([]repository.FileDiff, error) {
	return []repository.FileDiff{
		{
			OldName: "old.txt",
			NewName: "new.txt",
			Fragments: []repository.DiffFragment{
				{
					OldPosition: 1,
					OldLines:    2,
					NewPosition: 1,
					NewLines:    3,
					Lines: []repository.DiffLine{
						{Op: repository.OpContext, Line: "context line"},
						{Op: repository.OpDelete, Line: "deleted line"},
						{Op: repository.OpAdd, Line: "added line"},
						{Op: repository.OpAdd, Line: "another added"},
					},
				},
				{
					OldPosition: 10,
					OldLines:    1,
					NewPosition: 11,
					NewLines:    1,
					Lines: []repository.DiffLine{
						{Op: repository.OpContext, Line: "far away"},
					},
				},
			},
		},
	}, nil
}

// errDiffRepo wraps a Repo and makes Diff1 return an error.
type errDiffRepo struct {
	repository.Repo
}

func (r *errDiffRepo) Diff1(_ string, _ ...string) (string, error) {
	return "", errors.New("diff error")
}

// errGetCommitHashRepo makes GetCommitHash return an error.
type errGetCommitHashRepo struct {
	repository.Repo
}

func (r *errGetCommitHashRepo) GetCommitHash(string) (string, error) {
	return "", errors.New("hash error")
}

// errGetCommitDetailsRepo makes GetCommitDetails return an error.
type errGetCommitDetailsRepo struct {
	repository.Repo
}

func (r *errGetCommitDetailsRepo) GetCommitDetails(string) (*repository.CommitDetails, error) {
	return nil, errors.New("details error")
}

// errGetCommitMessageRepo makes GetCommitMessage return an error.
type errGetCommitMessageRepo struct {
	repository.Repo
}

func (r *errGetCommitMessageRepo) GetCommitMessage(string) (string, error) {
	return "", errors.New("message error")
}

// errParsedDiffRepo makes ParsedDiff1 return an error.
type errParsedDiffRepo struct {
	repository.Repo
}

func (r *errParsedDiffRepo) ParsedDiff1(string, ...string) ([]repository.FileDiff, error) {
	return nil, errors.New("diff1 error")
}

// --- getStatusString tests ---

func TestGetStatusStringPending(t *testing.T) {
	s := &review.Summary{}
	if got := getStatusString(s); got != "pending" {
		t.Errorf("got %q, want %q", got, "pending")
	}
}

func TestGetStatusStringTBR(t *testing.T) {
	s := &review.Summary{Submitted: true}
	if got := getStatusString(s); got != "tbr" {
		t.Errorf("got %q, want %q", got, "tbr")
	}
}

func TestGetStatusStringSubmitted(t *testing.T) {
	s := &review.Summary{Resolved: boolPtr(true), Submitted: true}
	if got := getStatusString(s); got != "submitted" {
		t.Errorf("got %q, want %q", got, "submitted")
	}
}

func TestGetStatusStringAccepted(t *testing.T) {
	s := &review.Summary{Resolved: boolPtr(true)}
	if got := getStatusString(s); got != "accepted" {
		t.Errorf("got %q, want %q", got, "accepted")
	}
}

func TestGetStatusStringDanger(t *testing.T) {
	s := &review.Summary{Resolved: boolPtr(false), Submitted: true}
	if got := getStatusString(s); got != "danger" {
		t.Errorf("got %q, want %q", got, "danger")
	}
}

func TestGetStatusStringAbandon(t *testing.T) {
	s := &review.Summary{
		Resolved: boolPtr(false),
		Request:  request.Request{TargetRef: ""},
	}
	if got := getStatusString(s); got != "abandon" {
		t.Errorf("got %q, want %q", got, "abandon")
	}
}

func TestGetStatusStringRejected(t *testing.T) {
	s := &review.Summary{
		Resolved: boolPtr(false),
		Request:  request.Request{TargetRef: "refs/heads/master"},
	}
	if got := getStatusString(s); got != "rejected" {
		t.Errorf("got %q, want %q", got, "rejected")
	}
}

// --- reformatTimestamp tests ---

func TestReformatTimestamp(t *testing.T) {
	got := reformatTimestamp("1000000000")
	if !strings.Contains(got, "2001") {
		t.Errorf("expected year 2001 in formatted timestamp, got %q", got)
	}
}

func TestReformatTimestampInvalid(t *testing.T) {
	if got := reformatTimestamp("not-a-number"); got != "not-a-number" {
		t.Errorf("expected passthrough for invalid timestamp, got %q", got)
	}
}

// --- PrintSummary / PrintSummaries tests ---

func TestPrintSummary(t *testing.T) {
	s := &review.Summary{
		Revision: "abc123def456",
		Request:  request.Request{Description: "test change"},
	}
	out := captureStdout(t, func() { PrintSummary(s) })
	if !strings.Contains(out, "pending") {
		t.Errorf("expected status in output, got %q", out)
	}
	if !strings.Contains(out, "abc123def456"[:12]) {
		t.Errorf("expected truncated revision in output, got %q", out)
	}
}

func TestPrintSummariesAll(t *testing.T) {
	summaries := []review.Summary{
		{Revision: "aaa", Request: request.Request{Description: "first"}},
		{Revision: "bbb", Request: request.Request{Description: "second"}},
	}
	out := captureStdout(t, func() { PrintSummaries(summaries, true) })
	if !strings.Contains(out, "Loaded 2 reviews") {
		t.Errorf("expected review count, got %q", out)
	}
}

func TestPrintSummariesOpen(t *testing.T) {
	summaries := []review.Summary{
		{Revision: "aaa", Request: request.Request{Description: "first"}},
	}
	out := captureStdout(t, func() { PrintSummaries(summaries, false) })
	if !strings.Contains(out, "Loaded 1 open reviews") {
		t.Errorf("expected open review count, got %q", out)
	}
}

// --- showSubThread tests ---

func TestShowSubThreadLGTM(t *testing.T) {
	thread := review.CommentThread{
		Hash: "abc123",
		Comment: comment.Comment{
			Timestamp:   "1000000000",
			Author:      "tester",
			Description: "looks good",
		},
		Resolved: boolPtr(true),
	}
	out := captureStdout(t, func() {
		if err := showSubThread(testMockRepo(), thread, ""); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "lgtm") {
		t.Errorf("expected 'lgtm' status, got %q", out)
	}
}

func TestShowSubThreadNeedsWork(t *testing.T) {
	thread := review.CommentThread{
		Hash: "def456",
		Comment: comment.Comment{
			Timestamp:   "1000000000",
			Author:      "reviewer",
			Description: "needs changes",
		},
		Resolved: boolPtr(false),
	}
	out := captureStdout(t, func() {
		if err := showSubThread(testMockRepo(), thread, ""); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "needs work") {
		t.Errorf("expected 'needs work' status, got %q", out)
	}
}

func TestShowSubThreadFYI(t *testing.T) {
	thread := review.CommentThread{
		Hash: "fyi123",
		Comment: comment.Comment{
			Timestamp:   "1000000000",
			Author:      "observer",
			Description: "just a note",
		},
	}
	out := captureStdout(t, func() {
		if err := showSubThread(testMockRepo(), thread, ""); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "fyi") {
		t.Errorf("expected 'fyi' status, got %q", out)
	}
}

func TestShowSubThreadWithChildren(t *testing.T) {
	thread := review.CommentThread{
		Hash: "parent",
		Comment: comment.Comment{
			Timestamp:   "1000000000",
			Author:      "author1",
			Description: "parent comment",
		},
		Children: []review.CommentThread{
			{
				Hash: "child",
				Comment: comment.Comment{
					Timestamp:   "1000000001",
					Author:      "author2",
					Description: "reply comment",
				},
			},
		},
	}
	out := captureStdout(t, func() {
		if err := showSubThread(testMockRepo(), thread, ""); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "parent comment") {
		t.Errorf("expected parent description, got %q", out)
	}
	if !strings.Contains(out, "reply comment") {
		t.Errorf("expected child description, got %q", out)
	}
}

// --- showThread tests ---

func TestShowThreadWithLocation(t *testing.T) {
	thread := review.CommentThread{
		Hash: "loc123",
		Comment: comment.Comment{
			Timestamp:   "1000000000",
			Author:      "reviewer",
			Description: "inline note",
			Location: &comment.Location{
				Commit: repository.TestCommitE,
				Path:   "foo.txt",
				Range: &comment.Range{
					StartLine: 1,
				},
			},
		},
	}
	out := captureStdout(t, func() {
		if err := showThread(testMockRepo(), thread, ""); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "foo.txt") {
		t.Errorf("expected file path in output, got %q", out)
	}
}

func TestShowThreadWithLocationRange(t *testing.T) {
	// Multi-line mock Show returns "commit:path" which is 1 line, so use
	// StartLine=1 and EndLine=1 but set them explicitly to exercise the
	// endLine != startLine code path.  Actually endLine > len(lines)
	// won't print, so test with start=end to exercise lastLine==firstLine.
	thread := review.CommentThread{
		Hash: "range1",
		Comment: comment.Comment{
			Timestamp:   "1000000000",
			Author:      "reviewer",
			Description: "range note",
			Location: &comment.Location{
				Commit: repository.TestCommitE,
				Path:   "foo.txt",
				Range: &comment.Range{
					StartLine: 1,
					EndLine:   1,
				},
			},
		},
	}
	out := captureStdout(t, func() {
		if err := showThread(testMockRepo(), thread, "  "); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "foo.txt") {
		t.Errorf("expected file path in output, got %q", out)
	}
}

func TestShowThreadNoLocation(t *testing.T) {
	thread := review.CommentThread{
		Hash: "noloc",
		Comment: comment.Comment{
			Timestamp:   "1000000000",
			Author:      "reviewer",
			Description: "no location",
		},
	}
	out := captureStdout(t, func() {
		if err := showThread(testMockRepo(), thread, ""); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "no location") {
		t.Errorf("expected description in output, got %q", out)
	}
}

func TestShowThreadShowError(t *testing.T) {
	repo := &errShowRepo{testMockRepo()}
	thread := review.CommentThread{
		Hash: "err1",
		Comment: comment.Comment{
			Timestamp:   "1000000000",
			Author:      "reviewer",
			Description: "will fail",
			Location: &comment.Location{
				Commit: repository.TestCommitE,
				Path:   "foo.txt",
				Range:  &comment.Range{StartLine: 1},
			},
		},
	}
	captureStdout(t, func() {
		err := showThread(repo, thread, "")
		if err == nil {
			t.Fatal("expected error from Show")
		}
	})
}

func TestShowThreadLocationOutOfRange(t *testing.T) {
	// Mock Show returns "commit:path" which is 1 line. StartLine=5 > 1 line.
	thread := review.CommentThread{
		Hash: "oor1",
		Comment: comment.Comment{
			Timestamp:   "1000000000",
			Author:      "reviewer",
			Description: "out of range",
			Location: &comment.Location{
				Commit: repository.TestCommitE,
				Path:   "foo.txt",
				Range:  &comment.Range{StartLine: 5},
			},
		},
	}
	captureStdout(t, func() {
		err := showThread(testMockRepo(), thread, "")
		if err == nil {
			t.Fatal("expected error from Check (line out of range)")
		}
	})
}

// --- printAnalyses ---

func TestPrintAnalyses(t *testing.T) {
	r := &review.Review{
		Summary: &review.Summary{
			Repo:     testMockRepo(),
			Revision: repository.TestCommitG,
			Request: request.Request{
				ReviewRef: repository.TestReviewRef,
				TargetRef: repository.TestTargetRef,
			},
		},
	}
	out := captureStdout(t, func() { printAnalyses(r) })
	if !strings.Contains(out, "analyses") {
		t.Errorf("expected 'analyses' in output, got %q", out)
	}
}

// --- PrintComments ---

func TestPrintComments(t *testing.T) {
	threads := []review.CommentThread{
		{
			Hash: "c1",
			Comment: comment.Comment{
				Timestamp:   "1000000000",
				Author:      "tester",
				Description: "thread one",
			},
		},
		{
			Hash: "c2",
			Comment: comment.Comment{
				Timestamp:   "1000000001",
				Author:      "tester",
				Description: "thread two",
			},
		},
	}
	out := captureStdout(t, func() {
		if err := PrintComments(testMockRepo(), threads); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "Loaded 2 comment threads") {
		t.Errorf("expected comment count header, got %q", out)
	}
}

func TestPrintCommentsEmpty(t *testing.T) {
	out := captureStdout(t, func() {
		if err := PrintComments(testMockRepo(), nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "Loaded 0 comment threads") {
		t.Errorf("expected zero count, got %q", out)
	}
}

func TestPrintCommentsWithError(t *testing.T) {
	repo := &errShowRepo{testMockRepo()}
	threads := []review.CommentThread{
		{
			Hash: "err",
			Comment: comment.Comment{
				Timestamp:   "1000000000",
				Author:      "tester",
				Description: "will fail",
				Location: &comment.Location{
					Commit: "X",
					Path:   "f.txt",
					Range:  &comment.Range{StartLine: 1},
				},
			},
		},
	}
	captureStdout(t, func() {
		err := PrintComments(repo, threads)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

// --- SeparateComments ---

func TestSeparateComments(t *testing.T) {
	threads := []review.CommentThread{
		{Comment: comment.Comment{Description: "commit comment"}},
		{
			Comment: comment.Comment{
				Description: "file comment",
				Location: &comment.Location{
					Path:  "foo.go",
					Range: &comment.Range{StartLine: 5},
				},
			},
		},
		{
			Comment: comment.Comment{
				Description: "whole file comment",
				Location:    &comment.Location{Path: "bar.go"},
			},
		},
	}
	commitThreads := make(map[uint32][]review.CommentThread)
	lineThreads := make(map[string]map[uint32][]review.CommentThread)
	SeparateComments(threads, commitThreads, lineThreads)

	if len(commitThreads[0]) != 1 {
		t.Errorf("expected 1 commit thread at line 0, got %d", len(commitThreads[0]))
	}
	if len(lineThreads["foo.go"][5]) != 1 {
		t.Errorf("expected 1 line thread for foo.go:5, got %d", len(lineThreads["foo.go"][5]))
	}
	if len(lineThreads["bar.go"][0]) != 1 {
		t.Errorf("expected 1 whole-file thread for bar.go, got %d", len(lineThreads["bar.go"][0]))
	}
}

func TestSeparateCommentsEndLine(t *testing.T) {
	threads := []review.CommentThread{
		{
			Comment: comment.Comment{
				Description: "range comment",
				Location: &comment.Location{
					Path:  "file.go",
					Range: &comment.Range{StartLine: 3, EndLine: 7},
				},
			},
		},
	}
	commitThreads := make(map[uint32][]review.CommentThread)
	lineThreads := make(map[string]map[uint32][]review.CommentThread)
	SeparateComments(threads, commitThreads, lineThreads)
	if len(lineThreads["file.go"][7]) != 1 {
		t.Errorf("expected comment at end line 7, got %v", lineThreads["file.go"])
	}
}

// --- PrintDetails ---

func TestPrintDetails(t *testing.T) {
	r := &review.Review{
		Summary: &review.Summary{
			Repo:     testMockRepo(),
			Revision: repository.TestCommitG,
			Request: request.Request{
				ReviewRef:   repository.TestReviewRef,
				TargetRef:   repository.TestTargetRef,
				Requester:   "tester",
				Reviewers:   []string{"reviewer1"},
				Description: "test review",
			},
		},
	}
	out := captureStdout(t, func() {
		if err := PrintDetails(r); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "tester") {
		t.Errorf("expected requester in output, got %q", out)
	}
	if !strings.Contains(out, "reviewer1") {
		t.Errorf("expected reviewer in output, got %q", out)
	}
	if !strings.Contains(out, "build status") {
		t.Errorf("expected build status label, got %q", out)
	}
}

func TestPrintDetailsWithComments(t *testing.T) {
	r := &review.Review{
		Summary: &review.Summary{
			Repo:     testMockRepo(),
			Revision: repository.TestCommitG,
			Request: request.Request{
				ReviewRef: repository.TestReviewRef,
				TargetRef: repository.TestTargetRef,
			},
			Comments: []review.CommentThread{
				{
					Hash: "c1",
					Comment: comment.Comment{
						Timestamp:   "1000000000",
						Author:      "commenter",
						Description: "a comment",
					},
				},
			},
		},
	}
	out := captureStdout(t, func() {
		if err := PrintDetails(r); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "comments (1 threads)") {
		t.Errorf("expected comment summary, got %q", out)
	}
}

func TestPrintDetailsError(t *testing.T) {
	repo := &errShowRepo{testMockRepo()}
	r := &review.Review{
		Summary: &review.Summary{
			Repo:     repo,
			Revision: repository.TestCommitG,
			Request: request.Request{
				ReviewRef: repository.TestReviewRef,
				TargetRef: repository.TestTargetRef,
			},
			Comments: []review.CommentThread{
				{
					Hash: "err",
					Comment: comment.Comment{
						Timestamp:   "1000",
						Author:      "x",
						Description: "err",
						Location: &comment.Location{
							Commit: "X", Path: "f.txt",
							Range: &comment.Range{StartLine: 1},
						},
					},
				},
			},
		},
	}
	captureStdout(t, func() {
		if err := PrintDetails(r); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestPrintDetailsWithCIReports(t *testing.T) {
	r := &review.Review{
		Summary: &review.Summary{
			Repo:     testMockRepo(),
			Revision: repository.TestCommitG,
			Request: request.Request{
				ReviewRef: repository.TestReviewRef,
				TargetRef: repository.TestTargetRef,
			},
		},
		Reports: []ci.Report{
			{Timestamp: "1000", URL: "http://ci.example.com", Status: "success"},
		},
	}
	out := captureStdout(t, func() {
		if err := PrintDetails(r); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "build status") {
		t.Errorf("expected build status in output, got %q", out)
	}
}

func TestPrintDetailsWithAnalyses(t *testing.T) {
	r := &review.Review{
		Summary: &review.Summary{
			Repo:     testMockRepo(),
			Revision: repository.TestCommitG,
			Request: request.Request{
				ReviewRef: repository.TestReviewRef,
				TargetRef: repository.TestTargetRef,
			},
		},
		Analyses: []analyses.Report{
			{Timestamp: "1000", URL: "http://analyses.example.com", Status: "passed"},
		},
	}
	out := captureStdout(t, func() {
		if err := PrintDetails(r); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "analyses") {
		t.Errorf("expected analyses in output, got %q", out)
	}
}

// --- PrintJSON ---

func TestPrintJSON(t *testing.T) {
	r := &review.Review{
		Summary: &review.Summary{
			Repo:     testMockRepo(),
			Revision: "abc123",
			Request:  request.Request{Description: "json test"},
		},
	}
	out := captureStdout(t, func() {
		if err := PrintJSON(r); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "json test") {
		t.Errorf("expected description in JSON output, got %q", out)
	}
}

// --- PrintCommentsJSON ---

func TestPrintCommentsJSON(t *testing.T) {
	threads := []review.CommentThread{
		{
			Hash: "hash1",
			Comment: comment.Comment{
				Timestamp:   "1000",
				Author:      "author",
				Description: "json comment",
			},
		},
	}
	out := captureStdout(t, func() {
		if err := PrintCommentsJSON(threads); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "json comment") {
		t.Errorf("expected comment in JSON, got %q", out)
	}
}

// --- PrintDiff ---

func TestPrintDiff(t *testing.T) {
	r := &review.Review{
		Summary: &review.Summary{
			Repo:     testMockRepo(),
			Revision: repository.TestCommitG,
			Request: request.Request{
				ReviewRef: repository.TestReviewRef,
				TargetRef: repository.TestTargetRef,
			},
		},
	}
	out := captureStdout(t, func() {
		if err := PrintDiff(r); err != nil {
			t.Fatal(err)
		}
	})
	if out == "" {
		t.Error("expected non-empty diff output")
	}
}

func TestPrintDiffError(t *testing.T) {
	repo := &errDiffRepo{testMockRepo()}
	r := &review.Review{
		Summary: &review.Summary{
			Repo:     repo,
			Revision: repository.TestCommitG,
			Request: request.Request{
				ReviewRef: repository.TestReviewRef,
				TargetRef: repository.TestTargetRef,
			},
		},
	}
	captureStdout(t, func() {
		if err := PrintDiff(r); err == nil {
			t.Fatal("expected error")
		}
	})
}

// --- PrintInlineComments ---

func TestPrintInlineComments(t *testing.T) {
	r := &review.Review{
		Summary: &review.Summary{
			Repo:     testMockRepo(),
			Revision: repository.TestCommitG,
			Request: request.Request{
				ReviewRef: repository.TestReviewRef,
				TargetRef: repository.TestTargetRef,
			},
		},
	}
	out := captureStdout(t, func() {
		if err := PrintInlineComments(r); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "commit:") {
		t.Errorf("expected 'commit:' in output, got %q", out)
	}
}

func TestPrintInlineCommentsWithThreads(t *testing.T) {
	r := &review.Review{
		Summary: &review.Summary{
			Repo:     testMockRepo(),
			Revision: repository.TestCommitG,
			Request: request.Request{
				ReviewRef: repository.TestReviewRef,
				TargetRef: repository.TestTargetRef,
			},
			Comments: []review.CommentThread{
				{
					Hash: "inline1",
					Comment: comment.Comment{
						Timestamp:   "1000",
						Author:      "reviewer",
						Description: "commit level comment",
					},
				},
			},
		},
	}
	out := captureStdout(t, func() {
		if err := PrintInlineComments(r); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "commit level comment") {
		t.Errorf("expected inline comment in output, got %q", out)
	}
}

func TestPrintInlineCommentsAllOps(t *testing.T) {
	repo := &customDiffRepo{testMockRepo()}
	r := &review.Review{
		Summary: &review.Summary{
			Repo:     repo,
			Revision: repository.TestCommitG,
			Request: request.Request{
				ReviewRef: repository.TestReviewRef,
				TargetRef: repository.TestTargetRef,
			},
			Comments: []review.CommentThread{
				// Whole-file comment on "new.txt"
				{
					Hash: "wf1",
					Comment: comment.Comment{
						Timestamp:   "1000",
						Author:      "reviewer",
						Description: "whole file note",
						Location:    &comment.Location{Path: "new.txt"},
					},
				},
				// Line-level comment on "new.txt" at line 1 (context line)
				{
					Hash: "ln1",
					Comment: comment.Comment{
						Timestamp:   "1001",
						Author:      "reviewer",
						Description: "line one note",
						Location: &comment.Location{
							Path:  "new.txt",
							Range: &comment.Range{StartLine: 1},
						},
					},
				},
				// Line-level comment on "new.txt" at line 3 (added line)
				{
					Hash: "ln3",
					Comment: comment.Comment{
						Timestamp:   "1002",
						Author:      "reviewer",
						Description: "line three note",
						Location: &comment.Location{
							Path:  "new.txt",
							Range: &comment.Range{StartLine: 3},
						},
					},
				},
				// Commit message line comment at line 1
				{
					Hash: "cm1",
					Comment: comment.Comment{
						Timestamp:   "1003",
						Author:      "reviewer",
						Description: "commit msg line note",
						Location: &comment.Location{
							Range: &comment.Range{StartLine: 1},
						},
					},
				},
			},
		},
	}
	out := captureStdout(t, func() {
		if err := PrintInlineComments(r); err != nil {
			t.Fatal(err)
		}
	})
	// Verify context, delete, add operations are printed
	if !strings.Contains(out, "context line") {
		t.Errorf("expected context line in output, got %q", out)
	}
	if !strings.Contains(out, "deleted line") {
		t.Errorf("expected deleted line in output, got %q", out)
	}
	if !strings.Contains(out, "added line") {
		t.Errorf("expected added line in output, got %q", out)
	}
	// Verify "..." separator between fragments
	if !strings.Contains(out, "...") {
		t.Errorf("expected '...' fragment separator in output, got %q", out)
	}
	// Verify whole-file comment
	if !strings.Contains(out, "whole file note") {
		t.Errorf("expected whole file comment, got %q", out)
	}
	// Verify line-level comments
	if !strings.Contains(out, "line one note") {
		t.Errorf("expected line 1 comment, got %q", out)
	}
	if !strings.Contains(out, "line three note") {
		t.Errorf("expected line 3 comment, got %q", out)
	}
	// Verify commit message line comment
	if !strings.Contains(out, "commit msg line note") {
		t.Errorf("expected commit message line comment, got %q", out)
	}
}

func TestPrintInlineCommentsGetCommitHashError(t *testing.T) {
	repo := &errGetCommitHashRepo{testMockRepo()}
	r := &review.Review{
		Summary: &review.Summary{
			Repo:     repo,
			Revision: repository.TestCommitG,
			Request:  request.Request{ReviewRef: repository.TestReviewRef, TargetRef: repository.TestTargetRef},
		},
	}
	captureStdout(t, func() {
		if err := PrintInlineComments(r); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestPrintInlineCommentsGetCommitDetailsError(t *testing.T) {
	repo := &errGetCommitDetailsRepo{testMockRepo()}
	r := &review.Review{
		Summary: &review.Summary{
			Repo:     repo,
			Revision: repository.TestCommitG,
			Request:  request.Request{ReviewRef: repository.TestReviewRef, TargetRef: repository.TestTargetRef},
		},
	}
	captureStdout(t, func() {
		if err := PrintInlineComments(r); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestPrintInlineCommentsGetCommitMessageError(t *testing.T) {
	repo := &errGetCommitMessageRepo{testMockRepo()}
	r := &review.Review{
		Summary: &review.Summary{
			Repo:     repo,
			Revision: repository.TestCommitG,
			Request:  request.Request{ReviewRef: repository.TestReviewRef, TargetRef: repository.TestTargetRef},
		},
	}
	captureStdout(t, func() {
		if err := PrintInlineComments(r); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestPrintInlineCommentsParsedDiff1Error(t *testing.T) {
	repo := &errParsedDiffRepo{testMockRepo()}
	r := &review.Review{
		Summary: &review.Summary{
			Repo:     repo,
			Revision: repository.TestCommitG,
			Request:  request.Request{ReviewRef: repository.TestReviewRef, TargetRef: repository.TestTargetRef},
		},
	}
	captureStdout(t, func() {
		if err := PrintInlineComments(r); err == nil {
			t.Fatal("expected error")
		}
	})
}

// --- Reflow edge cases ---

func TestReflowTripleNewlines(t *testing.T) {
	input := "first\n\n\nsecond"
	got := Reflow(input, "", 80)
	want := "first\n\nsecond"
	if got != want {
		t.Errorf("Reflow(%q, %q, 80) =\n%q\nwant\n%q", input, "", got, want)
	}
}

func TestReflowSpaceAfterParagraphBreak(t *testing.T) {
	input := "first\n\n second"
	got := Reflow(input, "", 80)
	want := "first\n\nsecond"
	if got != want {
		t.Errorf("Reflow(%q, %q, 80) =\n%q\nwant\n%q", input, "", got, want)
	}
}

func TestReflowNewlineAfterSpace(t *testing.T) {
	input := "hello \nworld"
	got := Reflow(input, "", 80)
	want := "hello world"
	if got != want {
		t.Errorf("Reflow(%q, %q, 80) =\n%q\nwant\n%q", input, "", got, want)
	}
}
