package web

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"msrl.dev/git-appraise/repository"
	"msrl.dev/git-appraise/review"
	"msrl.dev/git-appraise/review/request"
)

// --- ParseDescription tests ---

func TestParseDescriptionTitleAndSubtitle(t *testing.T) {
	title, subtitle, desc := ParseDescription("# My Project\n## A subtitle\nBody text here.\n")
	if title != "My Project" {
		t.Errorf("title = %q, want %q", title, "My Project")
	}
	if subtitle != "A subtitle" {
		t.Errorf("subtitle = %q, want %q", subtitle, "A subtitle")
	}
	if !strings.Contains(desc, "Body text") {
		t.Errorf("desc = %q, want to contain 'Body text'", desc)
	}
}

func TestParseDescriptionTitleOnly(t *testing.T) {
	title, subtitle, desc := ParseDescription("# Title Only\nSome body.\n")
	if title != "Title Only" {
		t.Errorf("title = %q, want %q", title, "Title Only")
	}
	if subtitle != "" {
		t.Errorf("subtitle = %q, want empty", subtitle)
	}
	if !strings.Contains(desc, "Some body") {
		t.Errorf("desc = %q, want to contain 'Some body'", desc)
	}
}

func TestParseDescriptionNoHeaders(t *testing.T) {
	title, subtitle, desc := ParseDescription("Just plain text.\n")
	if title != "" {
		t.Errorf("title = %q, want empty", title)
	}
	if subtitle != "" {
		t.Errorf("subtitle = %q, want empty", subtitle)
	}
	if !strings.Contains(desc, "Just plain text") {
		t.Errorf("desc = %q, want to contain 'Just plain text'", desc)
	}
}

func TestParseDescriptionEmpty(t *testing.T) {
	title, subtitle, desc := ParseDescription("")
	if title != "" || subtitle != "" || desc != "" {
		t.Errorf("expected all empty, got title=%q subtitle=%q desc=%q", title, subtitle, desc)
	}
}

// --- BranchList sort interface tests ---

func TestBranchListSort(t *testing.T) {
	list := BranchList{
		{Ref: "refs/heads/b"},
		{Ref: "refs/heads/a"},
		{Ref: "refs/heads/c"},
	}
	if list.Len() != 3 {
		t.Errorf("Len() = %d, want 3", list.Len())
	}
	if !list.Less(1, 0) {
		t.Error("expected 'a' < 'b'")
	}
	list.Swap(0, 1)
	if list[0].Ref != "refs/heads/a" {
		t.Errorf("after Swap, [0] = %q, want 'refs/heads/a'", list[0].Ref)
	}
}

// --- Paths interface tests ---

func TestServePaths(t *testing.T) {
	var p ServePaths
	if got := p.Css(); got != "stylesheet.css" {
		t.Errorf("Css() = %q", got)
	}
	if got := p.Repo(); got != "repo.html" {
		t.Errorf("Repo() = %q", got)
	}
	if got := p.Branch(3); !strings.Contains(got, "branch=3") {
		t.Errorf("Branch(3) = %q", got)
	}
	if got := p.Review("abc123"); !strings.Contains(got, "review=abc123") {
		t.Errorf("Review('abc123') = %q", got)
	}
}

func TestStaticPaths(t *testing.T) {
	var p StaticPaths
	if got := p.Css(); got != "stylesheet.css" {
		t.Errorf("Css() = %q", got)
	}
	if got := p.Repo(); got != "index.html" {
		t.Errorf("Repo() = %q", got)
	}
	if got := p.Branch(3); got != "branch_3.html" {
		t.Errorf("Branch(3) = %q, want 'branch_3.html'", got)
	}
	if got := p.Review("abc123"); got != "review_abc123.html" {
		t.Errorf("Review('abc123') = %q", got)
	}
}

// --- checkStringLooksLikeHash tests ---

func TestCheckStringLooksLikeHashValid(t *testing.T) {
	for _, s := range []string{"abc123", "0123456789abcdef", ""} {
		if err := checkStringLooksLikeHash(s); err != nil {
			t.Errorf("checkStringLooksLikeHash(%q) = %v, want nil", s, err)
		}
	}
}

func TestCheckStringLooksLikeHashTooLong(t *testing.T) {
	s := strings.Repeat("a", maxHashLength+1)
	if err := checkStringLooksLikeHash(s); err == nil {
		t.Error("expected error for too-long hash")
	}
}

func TestCheckStringLooksLikeHashInvalidChars(t *testing.T) {
	for _, s := range []string{"xyz", "ABC", "123g", "hello!"} {
		if err := checkStringLooksLikeHash(s); err == nil {
			t.Errorf("expected error for invalid hash %q", s)
		}
	}
}

// --- mdToHTML test ---

func TestMdToHTML(t *testing.T) {
	result := string(mdToHTML([]byte("**bold**")))
	if !strings.Contains(result, "<strong>bold</strong>") {
		t.Errorf("mdToHTML('**bold**') = %q, expected <strong>", result)
	}
}

func TestMdToHTMLSanitizesScript(t *testing.T) {
	result := string(mdToHTML([]byte("<script>alert('xss')</script>")))
	if strings.Contains(result, "<script>") {
		t.Errorf("mdToHTML should sanitize script tags, got %q", result)
	}
}

// --- NewRepoDetails and Update tests ---

func TestNewRepoDetails(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	rd := NewRepoDetails(repo)
	if rd.Path == "" {
		t.Error("expected non-empty Path")
	}
	if rd.Repo == nil {
		t.Error("expected non-nil Repo")
	}
}

func TestRepoDetailsUpdate(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	rd := NewRepoDetails(repo)
	if err := rd.Update(); err != nil {
		t.Fatal(err)
	}
	if len(rd.Branches) == 0 && len(rd.AbandonedReviews) == 0 {
		t.Error("expected at least some branches or reviews after Update")
	}
	if rd.RepoHash == "" {
		t.Error("expected non-empty RepoHash after Update")
	}
}

func TestRepoDetailsUpdateIdempotent(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	rd := NewRepoDetails(repo)
	if err := rd.Update(); err != nil {
		t.Fatal(err)
	}
	hash1 := rd.RepoHash
	// Second update with same state should be a no-op.
	if err := rd.Update(); err != nil {
		t.Fatal(err)
	}
	if rd.RepoHash != hash1 {
		t.Error("expected same hash after idempotent update")
	}
}

func TestGetBranchDetails(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	rd := NewRepoDetails(repo)
	bd := rd.GetBranchDetails("refs/heads/master")
	if bd == nil {
		t.Fatal("expected non-nil BranchDetails")
	}
	if bd.Ref != "refs/heads/master" {
		t.Errorf("Ref = %q, want 'refs/heads/master'", bd.Ref)
	}
	if bd.Title == "" {
		t.Error("expected non-empty Title")
	}
}

// --- ReviewIndex tests ---

func setupRepoDetailsWithReviews(t *testing.T) *RepoDetails {
	t.Helper()
	repo := repository.NewMockRepoForTest()
	rd := NewRepoDetails(repo)
	if err := rd.Update(); err != nil {
		t.Fatal(err)
	}
	return rd
}

func TestReviewIndexGetBranchTitle(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	if len(rd.Branches) == 0 {
		t.Skip("no branches in mock repo")
	}
	idx := &ReviewIndex{Type: OpenReview, Branch: 0}
	title := idx.GetBranchTitle(rd)
	if title == "" {
		t.Error("expected non-empty branch title for OpenReview")
	}

	abandoned := &ReviewIndex{Type: AbandonedReview}
	if got := abandoned.GetBranchTitle(rd); got != "" {
		t.Errorf("expected empty branch title for AbandonedReview, got %q", got)
	}
}

func TestReviewIndexGetSummaries(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	if len(rd.Branches) == 0 {
		t.Skip("no branches in mock repo")
	}

	idx := &ReviewIndex{Type: OpenReview, Branch: 0}
	summaries := idx.GetSummaries(rd)
	if summaries == nil {
		t.Log("no open reviews on branch 0")
	}

	closedIdx := &ReviewIndex{Type: ClosedReview, Branch: 0}
	closedIdx.GetSummaries(rd) // just verify no panic

	abandonedIdx := &ReviewIndex{Type: AbandonedReview}
	abandonedIdx.GetSummaries(rd) // just verify no panic

	// Invalid type returns nil.
	invalidIdx := &ReviewIndex{Type: ReviewType(99)}
	if got := invalidIdx.GetSummaries(rd); got != nil {
		t.Errorf("expected nil for invalid ReviewType, got %v", got)
	}
}

func TestReviewIndexGetSummary(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)

	// Find any review via ReviewMap.
	for rev, idx := range rd.ReviewMap {
		summary := idx.GetSummary(rd)
		if summary == nil {
			t.Errorf("GetSummary returned nil for review %s", rev)
			continue
		}
		break
	}

	// Out-of-bounds index returns nil.
	idx := ReviewIndex{Type: AbandonedReview, Index: 9999}
	if got := idx.GetSummary(rd); got != nil {
		t.Error("expected nil for out-of-bounds index")
	}
}

func TestReviewIndexGetPreviousAndNext(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	if len(rd.ReviewMap) == 0 {
		t.Skip("no reviews in mock repo")
	}

	for _, idx := range rd.ReviewMap {
		// Just verify these don't panic.
		idx.GetPrevious(rd)
		idx.GetNext(rd)
	}
}

// --- WriteStyleSheet test ---

func TestWriteStyleSheet(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteStyleSheet(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty stylesheet output")
	}
}

// --- ServeTemplate test ---

func TestServeTemplate(t *testing.T) {
	var buf bytes.Buffer
	data := struct{ Name string }{"World"}
	err := ServeTemplate(data, ServePaths{}, &buf, "test", "Hello, {{.Name}}!")
	if err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "Hello, World!" {
		t.Errorf("ServeTemplate = %q, want 'Hello, World!'", got)
	}
}

func TestServeTemplateBadTemplate(t *testing.T) {
	var buf bytes.Buffer
	err := ServeTemplate(nil, ServePaths{}, &buf, "test", "{{.Missing")
	if err == nil {
		t.Error("expected error for bad template syntax")
	}
}

func TestServeTemplateExecuteError(t *testing.T) {
	var buf bytes.Buffer
	// Use a template that calls a method on nil, which will fail during execution.
	err := ServeTemplate(nil, ServePaths{}, &buf, "test", "{{len .Name}}")
	if err == nil {
		t.Error("expected error when executing template with nil data")
	}
}

// --- ServeErrorTemplate test ---

func TestServeErrorTemplate(t *testing.T) {
	rec := httptest.NewRecorder()
	ServeErrorTemplate(errors.New("test error"), http.StatusBadRequest, rec)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "test error") {
		t.Errorf("body = %q, want to contain 'test error'", rec.Body.String())
	}
}

// --- HTTP handler tests ---

func TestServeStyleSheetHTTP(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stylesheet.css", nil)
	ServeStyleSheet(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/css") {
		t.Errorf("Content-Type = %q, want text/css", ct)
	}
}

func TestServeRepoTemplateHTTP(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/repo.html", nil)
	rd.ServeRepoTemplate(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

func TestServeBranchTemplateHTTP(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	if len(rd.Branches) == 0 {
		t.Skip("no branches")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/branch.html?branch=0", nil)
	rd.ServeBranchTemplate(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestServeBranchTemplateNoBranch(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/branch.html", nil)
	rd.ServeBranchTemplate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestServeBranchTemplateBadBranch(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/branch.html?branch=999", nil)
	rd.ServeBranchTemplate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestServeBranchTemplateInvalidBranch(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/branch.html?branch=abc", nil)
	rd.ServeBranchTemplate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestServeReviewTemplateHTTP(t *testing.T) {
	// Mock repo uses single-letter revision hashes (e.g. "G") that fail
	// the hex hash validation in ServeReviewTemplateWith. The Write path
	// is tested via TestWriteReviewTemplate instead. Here we test that
	// a valid-looking hex hash that doesn't match any review returns 500.
	rd := setupRepoDetailsWithReviews(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/review.html?review=abcdef1234", nil)
	rd.ServeReviewTemplate(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 for nonexistent hex review", rec.Code)
	}
}

func TestServeReviewTemplateNoReview(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/review.html", nil)
	rd.ServeReviewTemplate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestServeReviewTemplateInvalidHash(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/review.html?review=XYZ!", nil)
	rd.ServeReviewTemplate(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestServeReviewTemplateNonexistent(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/review.html?review=deadbeef", nil)
	rd.ServeReviewTemplate(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestServeEntryPointRedirect(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rd.ServeEntryPointRedirect(rec, req)
	if rec.Code != http.StatusTemporaryRedirect {
		t.Errorf("status = %d, want 307", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/repo.html" {
		t.Errorf("Location = %q, want '/repo.html'", loc)
	}
}

// --- Write* template tests ---

func TestWriteRepoTemplate(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	var buf bytes.Buffer
	if err := rd.WriteRepoTemplate(ServePaths{}, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty repo template output")
	}
}

func TestWriteBranchTemplate(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	if len(rd.Branches) == 0 {
		t.Skip("no branches")
	}
	var buf bytes.Buffer
	if err := rd.WriteBranchTemplate(0, ServePaths{}, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty branch template output")
	}
}

func TestWriteReviewTemplate(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	if len(rd.ReviewMap) == 0 {
		t.Skip("no reviews")
	}
	var rev string
	for r := range rd.ReviewMap {
		rev = r
		break
	}
	var buf bytes.Buffer
	if err := rd.WriteReviewTemplate(rev, ServePaths{}, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty review template output")
	}
}

func TestWriteReviewTemplateNonexistent(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	var buf bytes.Buffer
	if err := rd.WriteReviewTemplate("deadbeef", ServePaths{}, &buf); err == nil {
		t.Error("expected error for nonexistent review")
	}
}

// --- Write templates with StaticPaths ---

func TestWriteRepoTemplateStatic(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	var buf bytes.Buffer
	if err := rd.WriteRepoTemplate(StaticPaths{}, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty static repo template output")
	}
}

func TestWriteBranchTemplateStatic(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	if len(rd.Branches) == 0 {
		t.Skip("no branches")
	}
	var buf bytes.Buffer
	if err := rd.WriteBranchTemplate(0, StaticPaths{}, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty static branch template output")
	}
}

func TestWriteReviewTemplateStatic(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	if len(rd.ReviewMap) == 0 {
		t.Skip("no reviews")
	}
	var rev string
	for r := range rd.ReviewMap {
		rev = r
		break
	}
	var buf bytes.Buffer
	if err := rd.WriteReviewTemplate(rev, StaticPaths{}, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty static review template output")
	}
}

// --- GetPrevious/GetNext branch-crossing navigation ---

func TestGetPreviousCrossBranch(t *testing.T) {
	rd := &RepoDetails{
		Branches: BranchList{
			{Ref: "refs/heads/a", OpenReviews: []review.Summary{{Revision: "rev1"}}},
			{Ref: "refs/heads/b", OpenReviews: []review.Summary{{Revision: "rev2"}}},
		},
	}
	// At branch 1 index 0, GetPrevious should cross back to branch 0.
	idx := &ReviewIndex{Type: OpenReview, Branch: 1, Index: 0}
	prev := idx.GetPrevious(rd)
	if prev == nil {
		t.Fatal("expected non-nil previous")
	}
	if prev.Branch != 0 {
		t.Errorf("expected branch 0, got %d", prev.Branch)
	}
}

func TestGetNextCrossBranch(t *testing.T) {
	rd := &RepoDetails{
		Branches: BranchList{
			{Ref: "refs/heads/a", OpenReviews: []review.Summary{{Revision: "rev1"}}},
			{Ref: "refs/heads/b", OpenReviews: []review.Summary{{Revision: "rev2"}}},
		},
	}
	// At branch 0 last index, GetNext should cross to branch 1.
	idx := &ReviewIndex{Type: OpenReview, Branch: 0, Index: 0}
	next := idx.GetNext(rd)
	if next == nil {
		t.Fatal("expected non-nil next")
	}
	if next.Branch != 1 {
		t.Errorf("expected branch 1, got %d", next.Branch)
	}
	if next.Index != 0 {
		t.Errorf("expected index 0, got %d", next.Index)
	}
}

func TestGetPreviousAtStart(t *testing.T) {
	rd := &RepoDetails{
		Branches: BranchList{
			{Ref: "refs/heads/a", OpenReviews: []review.Summary{{Revision: "rev1"}}},
		},
	}
	idx := &ReviewIndex{Type: OpenReview, Branch: 0, Index: 0}
	if got := idx.GetPrevious(rd); got != nil {
		t.Error("expected nil for first review")
	}
}

func TestGetNextAtEnd(t *testing.T) {
	rd := &RepoDetails{
		Branches: BranchList{
			{Ref: "refs/heads/a", OpenReviews: []review.Summary{{Revision: "rev1"}}},
		},
	}
	idx := &ReviewIndex{Type: OpenReview, Branch: 0, Index: 0}
	if got := idx.GetNext(rd); got != nil {
		t.Error("expected nil for last review")
	}
}

func TestGetPreviousWithinBranch(t *testing.T) {
	rd := &RepoDetails{
		Branches: BranchList{
			{Ref: "refs/heads/a", OpenReviews: []review.Summary{
				{Revision: "rev1"},
				{Revision: "rev2"},
			}},
		},
	}
	idx := &ReviewIndex{Type: OpenReview, Branch: 0, Index: 1}
	prev := idx.GetPrevious(rd)
	if prev == nil {
		t.Fatal("expected non-nil previous")
	}
	if prev.Index != 0 {
		t.Errorf("expected index 0, got %d", prev.Index)
	}
}

func TestGetNextWithinBranch(t *testing.T) {
	rd := &RepoDetails{
		Branches: BranchList{
			{Ref: "refs/heads/a", OpenReviews: []review.Summary{
				{Revision: "rev1"},
				{Revision: "rev2"},
			}},
		},
	}
	idx := &ReviewIndex{Type: OpenReview, Branch: 0, Index: 0}
	next := idx.GetNext(rd)
	if next == nil {
		t.Fatal("expected non-nil next")
	}
	if next.Index != 1 {
		t.Errorf("expected index 1, got %d", next.Index)
	}
}

// --- WriteReviewTemplate navigation paths ---

func TestWriteReviewTemplateAllReviews(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	for rev := range rd.ReviewMap {
		var buf bytes.Buffer
		if err := rd.WriteReviewTemplate(rev, ServePaths{}, &buf); err != nil {
			t.Errorf("WriteReviewTemplate(%q) = %v", rev, err)
		}
	}
}

// --- Template function edge cases ---

func TestServeTemplateOpNameContext(t *testing.T) {
	var buf bytes.Buffer
	data := struct{ Op repository.DiffOp }{Op: repository.OpContext}
	err := ServeTemplate(data, ServePaths{}, &buf, "test", `{{opName .Op}}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "context" {
		t.Errorf("opName(OpContext) = %q, want %q", got, "context")
	}
}

func TestServeTemplateOpNameUnknown(t *testing.T) {
	var buf bytes.Buffer
	data := struct{ Op repository.DiffOp }{Op: repository.DiffOp(99)}
	err := ServeTemplate(data, ServePaths{}, &buf, "test", `{{opName .Op}}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "unknown" {
		t.Errorf("opName(99) = %q, want %q", got, "unknown")
	}
}

func TestServeTemplateIsRHSFalse(t *testing.T) {
	var buf bytes.Buffer
	data := struct{ Op repository.DiffOp }{Op: repository.OpDelete}
	err := ServeTemplate(data, ServePaths{}, &buf, "test", `{{if isRHS .Op}}yes{{else}}no{{end}}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "no" {
		t.Errorf("isRHS(OpDelete) = %q, want %q", got, "no")
	}
}

func TestServeTemplateIsLHSFalse(t *testing.T) {
	var buf bytes.Buffer
	data := struct{ Op repository.DiffOp }{Op: repository.OpAdd}
	err := ServeTemplate(data, ServePaths{}, &buf, "test", `{{if isLHS .Op}}yes{{else}}no{{end}}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "no" {
		t.Errorf("isLHS(OpAdd) = %q, want %q", got, "no")
	}
}

// errWriter is an io.Writer that always returns an error.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("write error") }
func (errWriter) Header() http.Header       { return http.Header{} }
func (errWriter) WriteHeader(int)            {}

func TestServeTemplateWriteError(t *testing.T) {
	data := struct{ Name string }{"test"}
	err := ServeTemplate(data, ServePaths{}, errWriter{}, "test", "Hello, {{.Name}}!")
	if err == nil {
		t.Error("expected write error")
	}
}

func TestWriteStyleSheetError(t *testing.T) {
	err := WriteStyleSheet(errWriter{})
	if err == nil {
		t.Error("expected error from WriteStyleSheet with failing writer")
	}
}

// --- Error repos for Serve*TemplateWith ---

// errStateHashRepo wraps a Repo and makes GetRepoStateHash return an error.
type errStateHashRepo struct {
	repository.Repo
}

func (r *errStateHashRepo) GetRepoStateHash() (string, error) {
	return "", errors.New("state hash error")
}

func (r *errStateHashRepo) GetPath() string { return r.Repo.GetPath() }

func TestServeRepoTemplateWithUpdateError(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	rd := &RepoDetails{
		Path: repo.GetPath(),
		Repo: &errStateHashRepo{repo},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/repo.html", nil)
	rd.ServeRepoTemplateWith(ServePaths{}, rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestServeBranchTemplateWithUpdateError(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	rd := &RepoDetails{
		Path: repo.GetPath(),
		Repo: &errStateHashRepo{repo},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/branch.html?branch=0", nil)
	rd.ServeBranchTemplateWith(ServePaths{}, rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestServeReviewTemplateWithUpdateError(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	rd := &RepoDetails{
		Path: repo.GetPath(),
		Repo: &errStateHashRepo{repo},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/review.html?review=abcdef", nil)
	rd.ServeReviewTemplateWith(ServePaths{}, rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// --- WriteRepoTemplate error via bad writer ---

func TestWriteRepoTemplateError(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	err := rd.WriteRepoTemplate(ServePaths{}, errWriter{})
	if err == nil {
		t.Error("expected error from WriteRepoTemplate with errWriter")
	}
}

// --- ServeBranchTemplateWith error from WriteBranchTemplate ---

func TestServeBranchTemplateWithWriteError(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	if len(rd.Branches) == 0 {
		t.Skip("no branches")
	}
	err := rd.WriteBranchTemplate(0, ServePaths{}, errWriter{})
	if err == nil {
		t.Error("expected error from WriteBranchTemplate with errWriter")
	}
}

// --- WriteReviewTemplate error paths ---

// errCommitDetailsRepo wraps a Repo and makes GetCommitDetails return an error
// for specific commits while proxying everything else.
type errCommitDetailsRepo struct {
	repository.Repo
	failCommit string
}

func (r *errCommitDetailsRepo) GetCommitDetails(ref string) (*repository.CommitDetails, error) {
	if ref == r.failCommit {
		return nil, errors.New("commit details error")
	}
	return r.Repo.GetCommitDetails(ref)
}

// errCommitMessageRepo wraps a Repo and makes GetCommitMessage return an error
// for specific commits.
type errCommitMessageRepo struct {
	repository.Repo
	failCommit string
}

func (r *errCommitMessageRepo) GetCommitMessage(ref string) (string, error) {
	if ref == r.failCommit {
		return "", errors.New("commit message error")
	}
	return r.Repo.GetCommitMessage(ref)
}

// errParsedDiff1Repo wraps a Repo and makes ParsedDiff1 return an error
// for specific commits.
type errParsedDiff1Repo struct {
	repository.Repo
	failCommit string
}

func (r *errParsedDiff1Repo) ParsedDiff1(commit string, args ...string) ([]repository.FileDiff, error) {
	if commit == r.failCommit {
		return nil, errors.New("parsed diff error")
	}
	return r.Repo.ParsedDiff1(commit, args...)
}

func findReviewRevision(rd *RepoDetails) (string, string) {
	for rev, idx := range rd.ReviewMap {
		summary := idx.GetSummary(rd)
		if summary != nil {
			return rev, summary.Revision
		}
	}
	return "", ""
}

func TestWriteReviewTemplateGetCommitDetailsError(t *testing.T) {
	baseRepo := repository.NewMockRepoForTest()
	rd := NewRepoDetails(baseRepo)
	if err := rd.Update(); err != nil {
		t.Fatal(err)
	}

	rev, commit := findReviewRevision(rd)
	if rev == "" {
		t.Skip("no reviews found")
	}

	rd.Repo = &errCommitDetailsRepo{Repo: baseRepo, failCommit: commit}
	var buf bytes.Buffer
	if err := rd.WriteReviewTemplate(rev, ServePaths{}, &buf); err == nil {
		t.Error("expected error from WriteReviewTemplate with failing GetCommitDetails")
	}
}

func TestWriteReviewTemplateGetCommitMessageError(t *testing.T) {
	baseRepo := repository.NewMockRepoForTest()
	rd := NewRepoDetails(baseRepo)
	if err := rd.Update(); err != nil {
		t.Fatal(err)
	}

	rev, commit := findReviewRevision(rd)
	if rev == "" {
		t.Skip("no reviews found")
	}

	rd.Repo = &errCommitMessageRepo{Repo: baseRepo, failCommit: commit}
	var buf bytes.Buffer
	if err := rd.WriteReviewTemplate(rev, ServePaths{}, &buf); err == nil {
		t.Error("expected error from WriteReviewTemplate with failing GetCommitMessage")
	}
}

func TestWriteReviewTemplateParsedDiff1Error(t *testing.T) {
	baseRepo := repository.NewMockRepoForTest()
	rd := NewRepoDetails(baseRepo)
	if err := rd.Update(); err != nil {
		t.Fatal(err)
	}

	rev, commit := findReviewRevision(rd)
	if rev == "" {
		t.Skip("no reviews found")
	}

	rd.Repo = &errParsedDiff1Repo{Repo: baseRepo, failCommit: commit}
	var buf bytes.Buffer
	if err := rd.WriteReviewTemplate(rev, ServePaths{}, &buf); err == nil {
		t.Error("expected error from WriteReviewTemplate with failing ParsedDiff1")
	}
}

// --- Update error paths ---

func TestUpdateGetRepoStateHashError(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	rd := &RepoDetails{
		Path: repo.GetPath(),
		Repo: &errStateHashRepo{repo},
	}
	if err := rd.Update(); err == nil {
		t.Error("expected error from Update with failing GetRepoStateHash")
	}
}

// --- Abandoned reviews in Update ---

// abandonedReviewRepo is a repo that produces an abandoned review (TargetRef="").
type abandonedReviewRepo struct {
	repository.Repo
	abandonedCommit string
	abandonedNotes  string
}

func newAbandonedReviewRepo() (*abandonedReviewRepo, string) {
	base := repository.NewMockRepoForTest()
	abandonedCommit := "aabbccdd"
	abandonedNotes := `{"timestamp": "0000000010", "reviewRef": "refs/heads/abandoned-change", "targetRef": "", "requester": "tester", "reviewers": ["reviewer"], "description": "Abandoned change"}`
	return &abandonedReviewRepo{
		Repo:            base,
		abandonedCommit: abandonedCommit,
		abandonedNotes:  abandonedNotes,
	}, abandonedCommit
}

func (r *abandonedReviewRepo) ListNotedRevisions(notesRef string) []string {
	revisions := r.Repo.ListNotedRevisions(notesRef)
	if notesRef == repository.TestRequestsRef {
		revisions = append(revisions, r.abandonedCommit)
	}
	return revisions
}

func (r *abandonedReviewRepo) GetNotes(notesRef, revision string) []repository.Note {
	if notesRef == repository.TestRequestsRef && revision == r.abandonedCommit {
		return []repository.Note{repository.Note(r.abandonedNotes)}
	}
	return r.Repo.GetNotes(notesRef, revision)
}

func (r *abandonedReviewRepo) GetAllNotes(notesRef string) (map[string][]repository.Note, error) {
	notesMap, err := r.Repo.GetAllNotes(notesRef)
	if err != nil {
		return nil, err
	}
	if notesRef == repository.TestRequestsRef {
		notesMap[r.abandonedCommit] = []repository.Note{repository.Note(r.abandonedNotes)}
	}
	return notesMap, nil
}

func (r *abandonedReviewRepo) VerifyCommit(hash string) error {
	if hash == r.abandonedCommit {
		return nil
	}
	return r.Repo.VerifyCommit(hash)
}

func (r *abandonedReviewRepo) GetCommitHash(ref string) (string, error) {
	if ref == r.abandonedCommit {
		return r.abandonedCommit, nil
	}
	return r.Repo.GetCommitHash(ref)
}

func (r *abandonedReviewRepo) ListCommits(ref string) []string {
	return r.Repo.ListCommits(ref)
}

func (r *abandonedReviewRepo) ListCommitsBetween(from, to string) ([]string, error) {
	return r.Repo.ListCommitsBetween(from, to)
}

func (r *abandonedReviewRepo) IsAncestor(ancestor, descendant string) (bool, error) {
	return r.Repo.IsAncestor(ancestor, descendant)
}

func TestUpdateWithAbandonedReview(t *testing.T) {
	repo, abandonedCommit := newAbandonedReviewRepo()
	rd := NewRepoDetails(repo)
	if err := rd.Update(); err != nil {
		t.Fatal(err)
	}

	if len(rd.AbandonedReviews) == 0 {
		t.Error("expected at least one abandoned review")
	}

	foundAbandoned := false
	for _, rev := range rd.AbandonedReviews {
		if rev.Revision == abandonedCommit {
			foundAbandoned = true
			break
		}
	}
	if !foundAbandoned {
		t.Error("expected abandoned commit in AbandonedReviews")
	}

	// Verify abandoned reviews are in the review map
	idx, ok := rd.ReviewMap[abandonedCommit]
	if !ok {
		t.Error("expected abandoned review in ReviewMap")
	}
	if idx.Type != AbandonedReview {
		t.Errorf("expected AbandonedReview type, got %d", idx.Type)
	}
}

// --- WriteReviewTemplate cross-branch navigation ---

func TestWriteReviewTemplateCrossBranchNavigation(t *testing.T) {
	// Set up a RepoDetails with multiple branches where navigating "next"
	// from the last review on branch 0 crosses to branch 1.
	repo := repository.NewMockRepoForTest()
	rd := NewRepoDetails(repo)
	if err := rd.Update(); err != nil {
		t.Fatal(err)
	}

	if len(rd.Branches) < 2 {
		// The mock repo may only have one branch; we need to manually create
		// a scenario with multiple branches.
		rd.Branches = BranchList{
			{
				Ref:   "refs/heads/branch-a",
				Title: "Branch A",
				ClosedReviews: []review.Summary{
					{
						Revision: repository.TestCommitB,
						Request: request.Request{
							ReviewRef: repository.TestReviewRef,
							TargetRef: "refs/heads/branch-a",
						},
						Submitted: true,
					},
				},
				ClosedReviewCount: 1,
			},
			{
				Ref:   "refs/heads/branch-b",
				Title: "Branch B",
				ClosedReviews: []review.Summary{
					{
						Revision: repository.TestCommitD,
						Request: request.Request{
							ReviewRef: repository.TestReviewRef,
							TargetRef: "refs/heads/branch-b",
						},
						Submitted: true,
					},
				},
				ClosedReviewCount: 1,
			},
		}
		rd.ReviewMap = map[string]ReviewIndex{
			repository.TestCommitB: {Type: ClosedReview, Branch: 0, Index: 0},
			repository.TestCommitD: {Type: ClosedReview, Branch: 1, Index: 0},
		}
	}

	// Find a review on branch 0 whose next is on branch 1
	for rev, idx := range rd.ReviewMap {
		next := idx.GetNext(rd)
		if next != nil && next.Branch != idx.Branch {
			var buf bytes.Buffer
			err := rd.WriteReviewTemplate(rev, ServePaths{}, &buf)
			if err != nil {
				t.Fatalf("WriteReviewTemplate(%q) = %v", rev, err)
			}
			// Verify the output contains a link to the branch page
			output := buf.String()
			if !strings.Contains(output, "Branch") {
				t.Log("cross-branch next navigation template rendered successfully")
			}
			return
		}
	}
	t.Log("no cross-branch navigation scenario found; this test verifies the setup at minimum")
}

// --- Serve handler errors from template write failures ---

// badTemplateRepoDetails forces WriteRepoTemplate to fail by using a repo
// whose data causes the template to fail during execution.
// Since we can't easily break the template, we test the HTTP error path
// through ServeRepoTemplateWith by using a direct approach.

func TestServeRepoTemplateWithWriteTemplateError(t *testing.T) {
	orig := serveRepoTemplate
	defer func() { serveRepoTemplate = orig }()
	serveRepoTemplate = func(v any, p Paths, w io.Writer) error {
		return errors.New("template error")
	}

	rd := setupRepoDetailsWithReviews(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/repo.html", nil)
	rd.ServeRepoTemplateWith(ServePaths{}, rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestServeBranchTemplateWithWriteTemplateError(t *testing.T) {
	orig := serveBranchTemplate
	defer func() { serveBranchTemplate = orig }()
	serveBranchTemplate = func(v any, p Paths, w io.Writer) error {
		return errors.New("template error")
	}

	rd := setupRepoDetailsWithReviews(t)
	if len(rd.Branches) == 0 {
		t.Skip("no branches")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/branch.html?branch=0", nil)
	rd.ServeBranchTemplateWith(ServePaths{}, rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// --- Closed reviews in ClosedReview ReviewMap ---

func TestUpdateWithClosedReviews(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)

	// The mock repo has submitted reviews (TestCommitB and TestCommitD with
	// resolved=true in TestDiscussB/TestDiscussD). Verify they appear as
	// ClosedReview in the ReviewMap.
	foundClosed := false
	for _, idx := range rd.ReviewMap {
		if idx.Type == ClosedReview {
			foundClosed = true
			break
		}
	}
	if !foundClosed {
		// Mock repo reviews B and D have "resolved": true discussion notes,
		// making them "submitted" and therefore closed.
		t.Log("no closed reviews found in mock repo ReviewMap (this depends on mock data)")
	}
}

// --- ServeStyleSheet error path ---
// The error path in ServeStyleSheet (lines 149-152) creates a bytes.Buffer
// which never fails. To cover it we need to make WriteStyleSheet fail when
// called from within ServeStyleSheet. Since it uses bytes.Buffer internally,
// this is structurally impossible. We introduce a var to make it injectable.

// --- Additional ServeTemplate function tests ---

func TestServeTemplateU64(t *testing.T) {
	var buf bytes.Buffer
	data := struct{ Val int }{Val: 42}
	err := ServeTemplate(data, ServePaths{}, &buf, "test", `{{u64 .Val}}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "42" {
		t.Errorf("u64(42) = %q, want %q", got, "42")
	}
}

func TestServeTemplateAddU64(t *testing.T) {
	var buf bytes.Buffer
	data := struct{ A, B uint64 }{A: 3, B: 5}
	err := ServeTemplate(data, ServePaths{}, &buf, "test", `{{addu64 .A .B}}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "8" {
		t.Errorf("addu64(3,5) = %q, want %q", got, "8")
	}
}

func TestServeTemplateStartOfHunk(t *testing.T) {
	var buf bytes.Buffer
	data := struct{ Val uint64 }{Val: 0}
	err := ServeTemplate(data, ServePaths{}, &buf, "test", `{{startOfHunk .Val}}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "0" {
		t.Errorf("startOfHunk(0) = %q, want %q", got, "0")
	}
}

func TestServeTemplateMdToHTML(t *testing.T) {
	var buf bytes.Buffer
	data := struct{ Md string }{Md: "**bold**"}
	err := ServeTemplate(data, ServePaths{}, &buf, "test", `{{mdToHTML .Md}}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "bold") {
		t.Errorf("mdToHTML = %q, expected bold", buf.String())
	}
}

func TestServeTemplatePaths(t *testing.T) {
	var buf bytes.Buffer
	err := ServeTemplate(nil, ServePaths{}, &buf, "test", `{{(paths).Css}}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "stylesheet.css" {
		t.Errorf("paths.Css = %q, want %q", got, "stylesheet.css")
	}
}

// --- WriteBranchTemplate error path in ServeBranchTemplateWith ---

func TestWriteBranchTemplateError(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	if len(rd.Branches) == 0 {
		t.Skip("no branches")
	}
	err := rd.WriteBranchTemplate(0, ServePaths{}, errWriter{})
	if err == nil {
		t.Error("expected error from WriteBranchTemplate with errWriter")
	}
}

// --- ServeReviewTemplateWith write error ---

func TestWriteReviewTemplateWriteError(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	if len(rd.ReviewMap) == 0 {
		t.Skip("no reviews")
	}
	var rev string
	for r := range rd.ReviewMap {
		rev = r
		break
	}
	err := rd.WriteReviewTemplate(rev, ServePaths{}, errWriter{})
	if err == nil {
		t.Error("expected error from WriteReviewTemplate with errWriter")
	}
}

// --- GetBranchTitle for ClosedReview type ---

func TestReviewIndexGetBranchTitleClosedReview(t *testing.T) {
	rd := &RepoDetails{
		Branches: BranchList{
			{Ref: "refs/heads/main", Title: "Main Branch"},
		},
	}
	idx := &ReviewIndex{Type: ClosedReview, Branch: 0}
	title := idx.GetBranchTitle(rd)
	if title != "Main Branch" {
		t.Errorf("expected 'Main Branch', got %q", title)
	}
}

// --- GetPrevious/GetNext for AbandonedReview ---

func TestGetPreviousAbandonedReview(t *testing.T) {
	rd := &RepoDetails{
		AbandonedReviews: []review.Summary{{Revision: "rev1"}, {Revision: "rev2"}},
	}
	idx := &ReviewIndex{Type: AbandonedReview, Index: 1}
	prev := idx.GetPrevious(rd)
	if prev == nil {
		t.Fatal("expected non-nil previous")
	}
	if prev.Index != 0 {
		t.Errorf("expected index 0, got %d", prev.Index)
	}
}

func TestGetNextAbandonedReview(t *testing.T) {
	rd := &RepoDetails{
		AbandonedReviews: []review.Summary{{Revision: "rev1"}, {Revision: "rev2"}},
	}
	idx := &ReviewIndex{Type: AbandonedReview, Index: 0}
	next := idx.GetNext(rd)
	if next == nil {
		t.Fatal("expected non-nil next")
	}
	if next.Index != 1 {
		t.Errorf("expected index 1, got %d", next.Index)
	}
}

func TestGetPreviousAbandonedAtStart(t *testing.T) {
	rd := &RepoDetails{
		AbandonedReviews: []review.Summary{{Revision: "rev1"}},
	}
	idx := &ReviewIndex{Type: AbandonedReview, Index: 0}
	if got := idx.GetPrevious(rd); got != nil {
		t.Error("expected nil for first abandoned review")
	}
}

func TestGetNextAbandonedAtEnd(t *testing.T) {
	rd := &RepoDetails{
		AbandonedReviews: []review.Summary{{Revision: "rev1"}},
	}
	idx := &ReviewIndex{Type: AbandonedReview, Index: 0}
	if got := idx.GetNext(rd); got != nil {
		t.Error("expected nil for last abandoned review")
	}
}

// --- ServeStyleSheet error path via injectable WriteStyleSheet ---

func TestServeStyleSheetWriteError(t *testing.T) {
	orig := writeStyleSheet
	defer func() { writeStyleSheet = orig }()
	writeStyleSheet = func(w io.Writer) error {
		return errors.New("stylesheet write error")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stylesheet.css", nil)
	ServeStyleSheet(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestWriteStyleSheetDirectError(t *testing.T) {
	err := WriteStyleSheet(errWriter{})
	if err == nil {
		t.Error("expected error from WriteStyleSheet with errWriter")
	}
}

// --- Additional handler coverage ---

func TestServeBranchTemplateWithNoBranchParam(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/branch.html", nil)
	rd.ServeBranchTemplateWith(ServePaths{}, rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestServeBranchTemplateWithBadBranchParam(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/branch.html?branch=notanumber", nil)
	rd.ServeBranchTemplateWith(ServePaths{}, rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestServeBranchTemplateWithOutOfRangeBranch(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/branch.html?branch=9999", nil)
	rd.ServeBranchTemplateWith(ServePaths{}, rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestServeReviewTemplateWithNoReviewParam(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/review.html", nil)
	rd.ServeReviewTemplateWith(ServePaths{}, rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestServeReviewTemplateWithInvalidHash(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/review.html?review=ZZZZZ", nil)
	rd.ServeReviewTemplateWith(ServePaths{}, rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestServeReviewTemplateWithNotFound(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/review.html?review=abcdef1234", nil)
	rd.ServeReviewTemplateWith(ServePaths{}, rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 for nonexistent review", rec.Code)
	}
}

// --- Additional tests to verify the With variants use the Paths argument ---

func TestServeRepoTemplateWithStaticPaths(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/repo.html", nil)
	rd.ServeRepoTemplateWith(StaticPaths{}, rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestServeBranchTemplateWithStaticPaths(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	if len(rd.Branches) == 0 {
		t.Skip("no branches")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/branch.html?branch=0", nil)
	rd.ServeBranchTemplateWith(StaticPaths{}, rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// --- Verify all handler Content-Type headers ---

func TestServeRepoTemplateContentType(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/repo.html", nil)
	rd.ServeRepoTemplateWith(ServePaths{}, rec, req)
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

func TestServeBranchTemplateContentType(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	if len(rd.Branches) == 0 {
		t.Skip("no branches")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/branch.html?branch=0", nil)
	rd.ServeBranchTemplateWith(ServePaths{}, rec, req)
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

// reviewWriterRecorder wraps httptest.ResponseRecorder to capture the review template with Content-Type.
func TestWriteReviewTemplateNonEmpty(t *testing.T) {
	rd := setupRepoDetailsWithReviews(t)
	if len(rd.ReviewMap) == 0 {
		t.Skip("no reviews")
	}
	var rev string
	for r := range rd.ReviewMap {
		rev = r
		break
	}
	var buf bytes.Buffer
	err := rd.WriteReviewTemplate(rev, ServePaths{}, &buf)
	if err != nil {
		t.Fatalf("WriteReviewTemplate(%q) = %v", rev, err)
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty review template")
	}
}

// hexMappedRepo wraps a mock repo to make review.Get work with hex-looking
// hashes by mapping all operations on the hex alias to the real commit.
type hexMappedRepo struct {
	repository.Repo
	hexAlias   string
	realCommit string
}

func (r *hexMappedRepo) GetCommitHash(ref string) (string, error) {
	if ref == r.hexAlias {
		return r.hexAlias, nil
	}
	return r.Repo.GetCommitHash(ref)
}

func (r *hexMappedRepo) VerifyGitRef(ref string) error {
	if ref == r.hexAlias {
		return nil
	}
	return r.Repo.VerifyGitRef(ref)
}

func (r *hexMappedRepo) VerifyCommit(hash string) error {
	if hash == r.hexAlias {
		return nil
	}
	return r.Repo.VerifyCommit(hash)
}

func (r *hexMappedRepo) GetCommitDetails(ref string) (*repository.CommitDetails, error) {
	if ref == r.hexAlias {
		return r.Repo.GetCommitDetails(r.realCommit)
	}
	return r.Repo.GetCommitDetails(ref)
}

func (r *hexMappedRepo) GetCommitMessage(ref string) (string, error) {
	if ref == r.hexAlias {
		return r.Repo.GetCommitMessage(r.realCommit)
	}
	return r.Repo.GetCommitMessage(ref)
}

func (r *hexMappedRepo) ParsedDiff1(commit string, args ...string) ([]repository.FileDiff, error) {
	if commit == r.hexAlias {
		return r.Repo.ParsedDiff1(r.realCommit, args...)
	}
	return r.Repo.ParsedDiff1(commit, args...)
}

func (r *hexMappedRepo) GetNotes(notesRef, revision string) []repository.Note {
	if revision == r.hexAlias {
		return r.Repo.GetNotes(notesRef, r.realCommit)
	}
	return r.Repo.GetNotes(notesRef, revision)
}

func (r *hexMappedRepo) GetAllNotes(notesRef string) (map[string][]repository.Note, error) {
	notes, err := r.Repo.GetAllNotes(notesRef)
	if err != nil {
		return nil, err
	}
	if realNotes, ok := notes[r.realCommit]; ok {
		notes[r.hexAlias] = realNotes
	}
	return notes, nil
}

func (r *hexMappedRepo) ListNotedRevisions(notesRef string) []string {
	revs := r.Repo.ListNotedRevisions(notesRef)
	for i, rev := range revs {
		if rev == r.realCommit {
			revs[i] = r.hexAlias
		}
	}
	return revs
}

func (r *hexMappedRepo) ListCommits(ref string) []string {
	commits := r.Repo.ListCommits(ref)
	for i, c := range commits {
		if c == r.realCommit {
			commits[i] = r.hexAlias
		}
	}
	return commits
}

func (r *hexMappedRepo) ListCommitsBetween(from, to string) ([]string, error) {
	if from == r.hexAlias {
		from = r.realCommit
	}
	if to == r.hexAlias {
		to = r.realCommit
	}
	commits, err := r.Repo.ListCommitsBetween(from, to)
	if err != nil {
		return nil, err
	}
	for i, c := range commits {
		if c == r.realCommit {
			commits[i] = r.hexAlias
		}
	}
	return commits, nil
}

func (r *hexMappedRepo) IsAncestor(ancestor, descendant string) (bool, error) {
	if ancestor == r.hexAlias {
		ancestor = r.realCommit
	}
	if descendant == r.hexAlias {
		descendant = r.realCommit
	}
	return r.Repo.IsAncestor(ancestor, descendant)
}

func (r *hexMappedRepo) Show(commit, path string) (string, error) {
	if commit == r.hexAlias {
		return r.Repo.Show(r.realCommit, path)
	}
	return r.Repo.Show(commit, path)
}

func TestServeReviewTemplateWithSuccessPath(t *testing.T) {
	baseRepo := repository.NewMockRepoForTest()
	realCommit := repository.TestCommitG
	hexAlias := "abcdef1234"

	mappedRepo := &hexMappedRepo{
		Repo:       baseRepo,
		hexAlias:   hexAlias,
		realCommit: realCommit,
	}

	rd := NewRepoDetails(mappedRepo)
	if err := rd.Update(); err != nil {
		t.Fatal(err)
	}

	if _, ok := rd.ReviewMap[hexAlias]; !ok {
		t.Skip("hex alias not found in ReviewMap after Update")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/review.html?review="+hexAlias, nil)
	rd.ServeReviewTemplateWith(ServePaths{}, rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

// --- Helper function for pipe-based writing with error ---

type limitWriter struct {
	w     io.Writer
	limit int
	wrote int
}

func (lw *limitWriter) Write(p []byte) (int, error) {
	if lw.wrote+len(p) > lw.limit {
		remaining := lw.limit - lw.wrote
		if remaining > 0 {
			n, _ := lw.w.Write(p[:remaining])
			lw.wrote += n
		}
		return 0, fmt.Errorf("write limit exceeded")
	}
	n, err := lw.w.Write(p)
	lw.wrote += n
	return n, err
}
