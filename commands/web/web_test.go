package web

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"msrl.dev/git-appraise/repository"
	"msrl.dev/git-appraise/review"
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
	rd, err := NewRepoDetails(repo)
	if err != nil {
		t.Fatal(err)
	}
	if rd.Path == "" {
		t.Error("expected non-empty Path")
	}
	if rd.Repo == nil {
		t.Error("expected non-nil Repo")
	}
}

func TestRepoDetailsUpdate(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	rd, err := NewRepoDetails(repo)
	if err != nil {
		t.Fatal(err)
	}
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
	rd, err := NewRepoDetails(repo)
	if err != nil {
		t.Fatal(err)
	}
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
	rd, err := NewRepoDetails(repo)
	if err != nil {
		t.Fatal(err)
	}
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
	rd, err := NewRepoDetails(repo)
	if err != nil {
		t.Fatal(err)
	}
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
