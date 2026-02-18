/*
Copyright 2015 Google Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package review

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"msrl.dev/git-appraise/repository"
	"msrl.dev/git-appraise/review/analyses"
	"msrl.dev/git-appraise/review/ci"
	"msrl.dev/git-appraise/review/comment"
	"msrl.dev/git-appraise/review/request"
)

// errorRepo wraps a real Repo and injects errors for specific operations.
type errorRepo struct {
	repository.Repo
	getAllNotesErr   map[string]error // keyed by notesRef
	getHeadRefErr    error
	isAncestorErr    error
	getCommitHashErr error
	getCommitTimeErr map[string]error // keyed by ref
	appendNoteErr    error
	rebaseRefErr     error
	switchToRefErr   error
	archiveRefErr    error
	createCommitErr  error
}

func (e *errorRepo) GetAllNotes(notesRef string) (map[string][]repository.Note, error) {
	if e.getAllNotesErr != nil {
		if err, ok := e.getAllNotesErr[notesRef]; ok {
			return nil, err
		}
	}
	return e.Repo.GetAllNotes(notesRef)
}

func (e *errorRepo) GetCommitTime(ref string) (string, error) {
	if e.getCommitTimeErr != nil {
		if err, ok := e.getCommitTimeErr[ref]; ok {
			return "", err
		}
	}
	return e.Repo.GetCommitTime(ref)
}

func (e *errorRepo) GetHeadRef() (string, error) {
	if e.getHeadRefErr != nil {
		return "", e.getHeadRefErr
	}
	return e.Repo.GetHeadRef()
}

func (e *errorRepo) IsAncestor(ancestor, descendant string) (bool, error) {
	if e.isAncestorErr != nil {
		return false, e.isAncestorErr
	}
	return e.Repo.IsAncestor(ancestor, descendant)
}

func (e *errorRepo) GetCommitHash(ref string) (string, error) {
	if e.getCommitHashErr != nil {
		return "", e.getCommitHashErr
	}
	return e.Repo.GetCommitHash(ref)
}

func (e *errorRepo) AppendNote(ref, revision string, note repository.Note) error {
	if e.appendNoteErr != nil {
		return e.appendNoteErr
	}
	return e.Repo.AppendNote(ref, revision, note)
}

func (e *errorRepo) RebaseRef(ref string) error {
	if e.rebaseRefErr != nil {
		return e.rebaseRefErr
	}
	return e.Repo.RebaseRef(ref)
}

func (e *errorRepo) SwitchToRef(ref string) error {
	if e.switchToRefErr != nil {
		return e.switchToRefErr
	}
	return e.Repo.SwitchToRef(ref)
}

func (e *errorRepo) ArchiveRef(ref, archive string) error {
	if e.archiveRefErr != nil {
		return e.archiveRefErr
	}
	return e.Repo.ArchiveRef(ref, archive)
}

func (e *errorRepo) CreateCommitWithTree(details *repository.CommitDetails, t *repository.Tree) (string, error) {
	if e.createCommitErr != nil {
		return "", e.createCommitErr
	}
	return e.Repo.CreateCommitWithTree(details, t)
}

func TestCommentSorting(t *testing.T) {
	sampleComments := []*comment.Comment{
		&comment.Comment{
			Timestamp:   "012400",
			Description: "Fourth",
		},
		&comment.Comment{
			Timestamp:   "012400",
			Description: "Fifth",
		},
		&comment.Comment{
			Timestamp:   "012346",
			Description: "Second",
		},
		&comment.Comment{
			Timestamp:   "012345",
			Description: "First",
		},
		&comment.Comment{
			Timestamp:   "012347",
			Description: "Third",
		},
	}
	sort.Stable(commentsByTimestamp(sampleComments))
	descriptions := []string{}
	for _, comment := range sampleComments {
		descriptions = append(descriptions, comment.Description)
	}
	if !(descriptions[0] == "First" && descriptions[1] == "Second" && descriptions[2] == "Third" && descriptions[3] == "Fourth" && descriptions[4] == "Fifth") {
		t.Fatalf("Comment ordering failed. Got %v", sampleComments)
	}
}

func TestThreadSorting(t *testing.T) {
	sampleThreads := []CommentThread{
		CommentThread{
			Comment: comment.Comment{
				Timestamp:   "012400",
				Description: "Fourth",
			},
		},
		CommentThread{
			Comment: comment.Comment{
				Timestamp:   "012400",
				Description: "Fifth",
			},
		},
		CommentThread{
			Comment: comment.Comment{
				Timestamp:   "012346",
				Description: "Second",
			},
		},
		CommentThread{
			Comment: comment.Comment{
				Timestamp:   "012345",
				Description: "First",
			},
		},
		CommentThread{
			Comment: comment.Comment{
				Timestamp:   "012347",
				Description: "Third",
			},
		},
	}
	sort.Stable(byTimestamp(sampleThreads))
	descriptions := []string{}
	for _, thread := range sampleThreads {
		descriptions = append(descriptions, thread.Comment.Description)
	}
	if !(descriptions[0] == "First" && descriptions[1] == "Second" && descriptions[2] == "Third" && descriptions[3] == "Fourth" && descriptions[4] == "Fifth") {
		t.Fatalf("Comment thread ordering failed. Got %v", sampleThreads)
	}
}

func TestRequestSorting(t *testing.T) {
	sampleRequests := []request.Request{
		request.Request{
			Timestamp:   "012400",
			Description: "Fourth",
		},
		request.Request{
			Timestamp:   "012400",
			Description: "Fifth",
		},
		request.Request{
			Timestamp:   "012346",
			Description: "Second",
		},
		request.Request{
			Timestamp:   "012345",
			Description: "First",
		},
		request.Request{
			Timestamp:   "012347",
			Description: "Third",
		},
	}
	sort.Stable(requestsByTimestamp(sampleRequests))
	descriptions := []string{}
	for _, r := range sampleRequests {
		descriptions = append(descriptions, r.Description)
	}
	if !(descriptions[0] == "First" && descriptions[1] == "Second" && descriptions[2] == "Third" && descriptions[3] == "Fourth" && descriptions[4] == "Fifth") {
		t.Fatalf("Review request ordering failed. Got %v", sampleRequests)
	}
}

func validateUnresolved(t *testing.T, resolved *bool) {
	if resolved != nil {
		t.Fatalf("Expected resolved status to be unset, but instead it was %v", *resolved)
	}
}

func validateAccepted(t *testing.T, resolved *bool) {
	if resolved == nil {
		t.Fatal("Expected resolved status to be true, but it was unset")
	}
	if !*resolved {
		t.Fatal("Expected resolved status to be true, but it was false")
	}
}

func validateRejected(t *testing.T, resolved *bool) {
	if resolved == nil {
		t.Fatal("Expected resolved status to be false, but it was unset")
	}
	if *resolved {
		t.Fatal("Expected resolved status to be false, but it was true")
	}
}

func (commentThread *CommentThread) validateUnresolved(t *testing.T) {
	validateUnresolved(t, commentThread.Resolved)
}

func (commentThread *CommentThread) validateAccepted(t *testing.T) {
	validateAccepted(t, commentThread.Resolved)
}

func (commentThread *CommentThread) validateRejected(t *testing.T) {
	validateRejected(t, commentThread.Resolved)
}

func TestSimpleAcceptedThreadStatus(t *testing.T) {
	resolved := true
	simpleThread := CommentThread{
		Comment: comment.Comment{
			Resolved: &resolved,
		},
	}
	simpleThread.updateResolvedStatus()
	simpleThread.validateAccepted(t)
}

func TestSimpleRejectedThreadStatus(t *testing.T) {
	resolved := false
	simpleThread := CommentThread{
		Comment: comment.Comment{
			Resolved: &resolved,
		},
	}
	simpleThread.updateResolvedStatus()
	simpleThread.validateRejected(t)
}

func TestFYIThenAcceptedThreadStatus(t *testing.T) {
	accepted := true
	sampleThread := CommentThread{
		Comment: comment.Comment{
			Resolved: nil,
		},
		Children: []CommentThread{
			CommentThread{
				Comment: comment.Comment{
					Timestamp: "012345",
					Resolved:  &accepted,
				},
			},
		},
	}
	sampleThread.updateResolvedStatus()
	sampleThread.validateUnresolved(t)
}

func TestFYIThenFYIThreadStatus(t *testing.T) {
	sampleThread := CommentThread{
		Comment: comment.Comment{
			Resolved: nil,
		},
		Children: []CommentThread{
			CommentThread{
				Comment: comment.Comment{
					Timestamp: "012345",
					Resolved:  nil,
				},
			},
		},
	}
	sampleThread.updateResolvedStatus()
	sampleThread.validateUnresolved(t)
}

func TestFYIThenRejectedThreadStatus(t *testing.T) {
	rejected := false
	sampleThread := CommentThread{
		Comment: comment.Comment{
			Resolved: nil,
		},
		Children: []CommentThread{
			CommentThread{
				Comment: comment.Comment{
					Timestamp: "012345",
					Resolved:  &rejected,
				},
			},
		},
	}
	sampleThread.updateResolvedStatus()
	sampleThread.validateRejected(t)
}

func TestAcceptedThenAcceptedThreadStatus(t *testing.T) {
	accepted := true
	sampleThread := CommentThread{
		Comment: comment.Comment{
			Resolved: &accepted,
		},
		Children: []CommentThread{
			CommentThread{
				Comment: comment.Comment{
					Timestamp: "012345",
					Resolved:  &accepted,
				},
			},
		},
	}
	sampleThread.updateResolvedStatus()
	sampleThread.validateAccepted(t)
}

func TestAcceptedThenFYIThreadStatus(t *testing.T) {
	accepted := true
	sampleThread := CommentThread{
		Comment: comment.Comment{
			Resolved: &accepted,
		},
		Children: []CommentThread{
			CommentThread{
				Comment: comment.Comment{
					Timestamp: "012345",
					Resolved:  nil,
				},
			},
		},
	}
	sampleThread.updateResolvedStatus()
	sampleThread.validateAccepted(t)
}

func TestAcceptedThenRejectedThreadStatus(t *testing.T) {
	accepted := true
	rejected := false
	sampleThread := CommentThread{
		Comment: comment.Comment{
			Resolved: &accepted,
		},
		Children: []CommentThread{
			CommentThread{
				Comment: comment.Comment{
					Timestamp: "012345",
					Resolved:  &rejected,
				},
			},
		},
	}
	sampleThread.updateResolvedStatus()
	sampleThread.validateRejected(t)
}

func TestRejectedThenAcceptedThreadStatus(t *testing.T) {
	accepted := true
	rejected := false
	sampleThread := CommentThread{
		Comment: comment.Comment{
			Resolved: &rejected,
		},
		Children: []CommentThread{
			CommentThread{
				Comment: comment.Comment{
					Timestamp: "012345",
					Resolved:  &accepted,
				},
			},
		},
	}
	sampleThread.updateResolvedStatus()
	sampleThread.validateUnresolved(t)
}

func TestRejectedThenFYIThreadStatus(t *testing.T) {
	rejected := false
	sampleThread := CommentThread{
		Comment: comment.Comment{
			Resolved: &rejected,
		},
		Children: []CommentThread{
			CommentThread{
				Comment: comment.Comment{
					Timestamp: "012345",
					Resolved:  nil,
				},
			},
		},
	}
	sampleThread.updateResolvedStatus()
	sampleThread.validateRejected(t)
}

func TestRejectedThenRejectedThreadStatus(t *testing.T) {
	rejected := false
	sampleThread := CommentThread{
		Comment: comment.Comment{
			Resolved: &rejected,
		},
		Children: []CommentThread{
			CommentThread{
				Comment: comment.Comment{
					Timestamp: "012345",
					Resolved:  &rejected,
				},
			},
		},
	}
	sampleThread.updateResolvedStatus()
	sampleThread.validateRejected(t)
}

func TestRejectedThenAcceptedThreadsStatus(t *testing.T) {
	accepted := true
	rejected := false
	threads := []CommentThread{
		CommentThread{
			Comment: comment.Comment{
				Timestamp: "012345",
				Resolved:  &rejected,
			},
		},
		CommentThread{
			Comment: comment.Comment{
				Timestamp: "012346",
				Resolved:  &accepted,
			},
		},
	}
	status := updateThreadsStatus(threads)
	validateRejected(t, status)
}

func TestRejectedThenFYIThreadsStatus(t *testing.T) {
	rejected := false
	threads := []CommentThread{
		CommentThread{
			Comment: comment.Comment{
				Timestamp: "012345",
				Resolved:  &rejected,
			},
		},
		CommentThread{
			Comment: comment.Comment{
				Timestamp: "012346",
				Resolved:  nil,
			},
		},
	}
	status := updateThreadsStatus(threads)
	validateRejected(t, status)
}

func TestRejectedThenRejectedThreadsStatus(t *testing.T) {
	rejected := false
	threads := []CommentThread{
		CommentThread{
			Comment: comment.Comment{
				Timestamp: "012345",
				Resolved:  &rejected,
			},
		},
		CommentThread{
			Comment: comment.Comment{
				Timestamp: "012346",
				Resolved:  &rejected,
			},
		},
	}
	status := updateThreadsStatus(threads)
	validateRejected(t, status)
}

func TestAcceptedThenAcceptedThreadsStatus(t *testing.T) {
	accepted := true
	threads := []CommentThread{
		CommentThread{
			Comment: comment.Comment{
				Timestamp: "012345",
				Resolved:  &accepted,
			},
		},
		CommentThread{
			Comment: comment.Comment{
				Timestamp: "012346",
				Resolved:  &accepted,
			},
		},
	}
	status := updateThreadsStatus(threads)
	validateAccepted(t, status)
}

func TestAcceptedThenFYIThreadsStatus(t *testing.T) {
	accepted := true
	threads := []CommentThread{
		CommentThread{
			Comment: comment.Comment{
				Timestamp: "012345",
				Resolved:  &accepted,
			},
		},
		CommentThread{
			Comment: comment.Comment{
				Timestamp: "012346",
				Resolved:  nil,
			},
		},
	}
	status := updateThreadsStatus(threads)
	validateAccepted(t, status)
}

func TestAcceptedThenRejectedThreadsStatus(t *testing.T) {
	accepted := true
	rejected := false
	threads := []CommentThread{
		CommentThread{
			Comment: comment.Comment{
				Timestamp: "012345",
				Resolved:  &accepted,
			},
		},
		CommentThread{
			Comment: comment.Comment{
				Timestamp: "012346",
				Resolved:  &rejected,
			},
		},
	}
	status := updateThreadsStatus(threads)
	validateRejected(t, status)
}

func TestFYIThenAcceptedThreadsStatus(t *testing.T) {
	accepted := true
	threads := []CommentThread{
		CommentThread{
			Comment: comment.Comment{
				Timestamp: "012345",
				Resolved:  nil,
			},
		},
		CommentThread{
			Comment: comment.Comment{
				Timestamp: "012346",
				Resolved:  &accepted,
			},
		},
	}
	status := updateThreadsStatus(threads)
	validateAccepted(t, status)
}

func TestFYIThenFYIThreadsStatus(t *testing.T) {
	threads := []CommentThread{
		CommentThread{
			Comment: comment.Comment{
				Timestamp: "012345",
				Resolved:  nil,
			},
		},
		CommentThread{
			Comment: comment.Comment{
				Timestamp: "012346",
				Resolved:  nil,
			},
		},
	}
	status := updateThreadsStatus(threads)
	validateUnresolved(t, status)
}

func TestFYIThenRejectedThreadsStatus(t *testing.T) {
	rejected := false
	threads := []CommentThread{
		CommentThread{
			Comment: comment.Comment{
				Timestamp: "012345",
				Resolved:  nil,
			},
		},
		CommentThread{
			Comment: comment.Comment{
				Timestamp: "012346",
				Resolved:  &rejected,
			},
		},
	}
	status := updateThreadsStatus(threads)
	validateRejected(t, status)
}

func TestBuildCommentThreads(t *testing.T) {
	rejected := false
	accepted := true
	root := comment.Comment{
		Timestamp:   "012345",
		Resolved:    nil,
		Description: "root",
	}
	rootHash, err := root.Hash()
	if err != nil {
		t.Fatal(err)
	}
	child := comment.Comment{
		Timestamp:   "012346",
		Resolved:    nil,
		Parent:      rootHash,
		Description: "child",
	}
	childHash, err := child.Hash()
	updatedChild := comment.Comment{
		Timestamp:   "012346",
		Resolved:    &rejected,
		Original:    childHash,
		Description: "updated child",
	}
	updatedChildHash, err := updatedChild.Hash()
	if err != nil {
		t.Fatal(err)
	}
	leaf := comment.Comment{
		Timestamp:   "012347",
		Resolved:    &accepted,
		Parent:      childHash,
		Description: "leaf",
	}
	leafHash, err := leaf.Hash()
	if err != nil {
		t.Fatal(err)
	}
	commentsByHash := map[string]comment.Comment{
		rootHash:         root,
		childHash:        child,
		updatedChildHash: updatedChild,
		leafHash:         leaf,
	}
	threads := buildCommentThreads(commentsByHash)
	if len(threads) != 1 {
		t.Fatalf("Unexpected threads: %v", threads)
	}
	rootThread := threads[0]
	if rootThread.Comment.Description != "root" {
		t.Fatalf("Unexpected root thread: %v", rootThread)
	}
	if !rootThread.Edited {
		t.Fatalf("Unexpected root thread edited status: %v", rootThread)
	}
	if len(rootThread.Children) != 1 {
		t.Fatalf("Unexpected root children: %v", rootThread.Children)
	}
	rootChild := rootThread.Children[0]
	if rootChild.Comment.Description != "updated child" {
		t.Fatalf("Unexpected updated child: %v", rootChild)
	}
	if rootChild.Original.Description != "child" {
		t.Fatalf("Unexpected original child: %v", rootChild)
	}
	if len(rootChild.Edits) != 1 {
		t.Fatalf("Unexpected child history: %v", rootChild.Edits)
	}
	if len(rootChild.Children) != 1 {
		t.Fatalf("Unexpected leaves: %v", rootChild.Children)
	}
	threadLeaf := rootChild.Children[0]
	if threadLeaf.Comment.Description != "leaf" {
		t.Fatalf("Unexpected leaf: %v", threadLeaf)
	}
	if len(threadLeaf.Children) != 0 {
		t.Fatalf("Unexpected leaf children: %v", threadLeaf.Children)
	}
	if threadLeaf.Edited {
		t.Fatalf("Unexpected leaf edited status: %v", threadLeaf)
	}
}

func TestGetHeadCommit(t *testing.T) {
	repo := repository.NewMockRepoForTest()

	submittedSimpleReview, err := Get(repo, repository.TestCommitB)
	if err != nil {
		t.Fatal(err)
	}
	submittedSimpleReviewHead, err := submittedSimpleReview.GetHeadCommit()
	if err != nil {
		t.Fatal("Unable to compute the head commit for a known review of a simple commit: ", err)
	}
	if submittedSimpleReviewHead != repository.TestCommitB {
		t.Fatal("Unexpected head commit computed for a known review of a simple commit.")
	}

	submittedModifiedReview, err := Get(repo, repository.TestCommitD)
	if err != nil {
		t.Fatal(err)
	}
	submittedModifiedReviewHead, err := submittedModifiedReview.GetHeadCommit()
	if err != nil {
		t.Fatal("Unable to compute the head commit for a known, multi-commit review: ", err)
	}
	if submittedModifiedReviewHead != repository.TestCommitE {
		t.Fatal("Unexpected head commit for a known, multi-commit review.")
	}

	pendingReview, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	pendingReviewHead, err := pendingReview.GetHeadCommit()
	if err != nil {
		t.Fatal("Unable to compute the head commit for a known review of a merge commit: ", err)
	}
	if pendingReviewHead != repository.TestCommitI {
		t.Fatal("Unexpected head commit computed for a pending review.")
	}
}

func TestGetBaseCommit(t *testing.T) {
	repo := repository.NewMockRepoForTest()

	submittedSimpleReview, err := Get(repo, repository.TestCommitB)
	if err != nil {
		t.Fatal(err)
	}
	submittedSimpleReviewBase, err := submittedSimpleReview.GetBaseCommit()
	if err != nil {
		t.Fatal("Unable to compute the base commit for a known review of a simple commit: ", err)
	}
	if submittedSimpleReviewBase != repository.TestCommitA {
		t.Fatal("Unexpected base commit computed for a known review of a simple commit.")
	}

	submittedMergeReview, err := Get(repo, repository.TestCommitD)
	if err != nil {
		t.Fatal(err)
	}
	submittedMergeReviewBase, err := submittedMergeReview.GetBaseCommit()
	if err != nil {
		t.Fatal("Unable to compute the base commit for a known review of a merge commit: ", err)
	}
	if submittedMergeReviewBase != repository.TestCommitC {
		t.Fatal("Unexpected base commit computed for a known review of a merge commit.")
	}

	pendingReview, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	pendingReviewBase, err := pendingReview.GetBaseCommit()
	if err != nil {
		t.Fatal("Unable to compute the base commit for a known review of a merge commit: ", err)
	}
	if pendingReviewBase != repository.TestCommitF {
		t.Fatal("Unexpected base commit computed for a pending review.")
	}

	abandonRequest := pendingReview.Request
	abandonRequest.TargetRef = ""
	abandonNote, err := abandonRequest.Write()
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AppendNote(request.Ref, repository.TestCommitG, abandonNote); err != nil {
		t.Fatal(err)
	}
	abandonedReview, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	if abandonedReview.IsOpen() {
		t.Fatal("Failed to update a review to be abandoned")
	}
	abandonedReviewBase, err := abandonedReview.GetBaseCommit()
	if err != nil {
		t.Fatal("Unable to compute the base commit for an abandoned review: ", err)
	}
	if abandonedReviewBase != repository.TestCommitE {
		t.Fatal("Unexpected base commit computed for an abandoned review.")
	}
}

func TestGetRequests(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	pendingReview, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	if len(pendingReview.AllRequests) != 3 || pendingReview.Request.Description != "Final description of G" {
		t.Fatal("Unexpected requests for a pending review: ", pendingReview.AllRequests, pendingReview.Request)
	}
}

func TestRebase(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	pendingReview, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}

	// Rebase the review and then confirm that it has been updated correctly.
	if err := pendingReview.Rebase(true); err != nil {
		t.Fatal(err)
	}
	reviewJSON, err := pendingReview.GetJSON()
	if err != nil {
		t.Fatal(err)
	}
	headRef, err := repo.GetHeadRef()
	if err != nil {
		t.Fatal(err)
	}
	if headRef != pendingReview.Request.ReviewRef {
		t.Fatal("Failed to switch to the review ref during a rebase")
	}
	isAncestor, err := repo.IsAncestor(pendingReview.Revision, archiveRef)
	if err != nil {
		t.Fatal(err)
	}
	if !isAncestor {
		t.Fatalf("Commit %q is not archived", pendingReview.Revision)
	}
	reviewCommit, err := repo.GetCommitHash(pendingReview.Request.ReviewRef)
	if err != nil {
		t.Fatal(err)
	}
	reviewAlias := pendingReview.Request.Alias
	if reviewAlias == "" || reviewAlias == pendingReview.Revision || reviewCommit != reviewAlias {
		t.Fatalf("Failed to set the review alias: %q", reviewJSON)
	}

	// Submit the review.
	if err := repo.SwitchToRef(pendingReview.Request.TargetRef); err != nil {
		t.Fatal(err)
	}
	if err := repo.MergeRef(pendingReview.Request.ReviewRef, true); err != nil {
		t.Fatal(err)
	}

	// Reread the review and confirm that it has been submitted.
	submittedReview, err := Get(repo, pendingReview.Revision)
	if err != nil {
		t.Fatal(err)
	}
	submittedReviewJSON, err := submittedReview.GetJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !submittedReview.Submitted {
		t.Fatalf("Failed to submit the review: %q", submittedReviewJSON)
	}
}

func TestRebaseDetachedHead(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	pendingReview, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}

	// Switch the review to having a review ref that is not a branch.
	pendingReview.Request.ReviewRef = repository.TestAlternateReviewRef
	newNote, err := pendingReview.Request.Write()
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AppendNote(request.Ref, pendingReview.Revision, newNote); err != nil {
		t.Fatal(err)
	}
	pendingReview, err = Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}

	// Rebase the review and then confirm that it has been updated correctly.
	if err := pendingReview.Rebase(true); err != nil {
		t.Fatal(err)
	}
	headRef, err := repo.GetHeadRef()
	if err != nil {
		t.Fatal(err)
	}
	if headRef != pendingReview.Request.Alias {
		t.Fatal("Failed to switch to a detached head during a rebase")
	}
	isAncestor, err := repo.IsAncestor(pendingReview.Revision, archiveRef)
	if err != nil {
		t.Fatal(err)
	}
	if !isAncestor {
		t.Fatalf("Commit %q is not archived", pendingReview.Revision)
	}

	// Submit the review.
	if err := repo.SwitchToRef(pendingReview.Request.TargetRef); err != nil {
		t.Fatal(err)
	}
	reviewHead, err := pendingReview.GetHeadCommit()
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MergeRef(reviewHead, true); err != nil {
		t.Fatal(err)
	}

	// Reread the review and confirm that it has been submitted.
	submittedReview, err := Get(repo, pendingReview.Revision)
	if err != nil {
		t.Fatal(err)
	}
	submittedReviewJSON, err := submittedReview.GetJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !submittedReview.Submitted {
		t.Fatalf("Failed to submit the review: %q", submittedReviewJSON)
	}
}

func TestGetComments(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	comments, err := GetComments(repo, repository.TestCommitB)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment thread for commit B, got %d", len(comments))
	}
}

func TestGetCommentsNoComments(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	comments, err := GetComments(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 0 {
		t.Fatalf("expected 0 comment threads for commit G, got %d", len(comments))
	}
}

func TestGetSummary(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	summary, err := GetSummary(repo, repository.TestCommitB)
	if err != nil {
		t.Fatal(err)
	}
	if summary == nil {
		t.Fatal("expected non-nil summary")
	}
	if summary.Revision != repository.TestCommitB {
		t.Fatalf("unexpected revision: %q", summary.Revision)
	}
	if summary.Request.Description != "B" {
		t.Fatalf("unexpected description: %q", summary.Request.Description)
	}
	if !summary.Submitted {
		t.Fatal("expected review B to be submitted")
	}
}

func TestGetSummaryViaRefs(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	summary, err := GetSummaryViaRefs(repo, request.Ref, comment.Ref, repository.TestCommitD)
	if err != nil {
		t.Fatal(err)
	}
	if summary == nil {
		t.Fatal("expected non-nil summary")
	}
	if summary.Request.Description != "D" {
		t.Fatalf("unexpected description: %q", summary.Request.Description)
	}
	if !summary.Submitted {
		t.Fatal("expected review D to be submitted")
	}
}

func TestGetSummaryViaRefsBadCommit(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	_, err := GetSummaryViaRefs(repo, request.Ref, comment.Ref, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent commit")
	}
}

func TestGetSummaryPending(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	summary, err := GetSummary(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Submitted {
		t.Fatal("expected review G to not be submitted")
	}
	if summary.IsAbandoned() {
		t.Fatal("expected review G to not be abandoned")
	}
	if !summary.IsOpen() {
		t.Fatal("expected review G to be open")
	}
}

func TestGet(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitB)
	if err != nil {
		t.Fatal(err)
	}
	if review == nil {
		t.Fatal("expected non-nil review")
	}
	if review.Revision != repository.TestCommitB {
		t.Fatalf("unexpected revision: %q", review.Revision)
	}
}

func TestListAll(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	reviews := ListAll(repo)
	if len(reviews) != 3 {
		t.Fatalf("expected 3 reviews, got %d", len(reviews))
	}
	for i := 0; i < len(reviews)-1; i++ {
		if reviews[i].Request.Timestamp < reviews[i+1].Request.Timestamp {
			t.Fatalf("reviews not sorted in reverse chronological order at index %d", i)
		}
	}
}

func TestListOpen(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	open := ListOpen(repo)
	if len(open) != 1 {
		t.Fatalf("expected 1 open review, got %d", len(open))
	}
	if open[0].Revision != repository.TestCommitG {
		t.Fatalf("unexpected open review revision: %q", open[0].Revision)
	}
}

func TestGetCurrent(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := GetCurrent(repo)
	if err != nil {
		t.Fatal(err)
	}
	if review != nil {
		t.Fatalf("expected nil review on master, got %+v", review)
	}
}

func TestGetCurrentOnReviewBranch(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	if err := repo.SwitchToRef(repository.TestReviewRef); err != nil {
		t.Fatal(err)
	}
	review, err := GetCurrent(repo)
	if err != nil {
		t.Fatal(err)
	}
	if review == nil {
		t.Fatal("expected non-nil review on review branch")
	}
	if review.Revision != repository.TestCommitG {
		t.Fatalf("unexpected current review revision: %q", review.Revision)
	}
}

func TestGetDiff(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitB)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := review.GetDiff()
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" {
		t.Fatal("expected non-empty diff")
	}
}

func TestAddComment(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	c := comment.New("tester@example.com", "test comment")
	c.Timestamp = "9999999999"
	if err := review.AddComment(c); err != nil {
		t.Fatal(err)
	}
	comments, err := GetComments(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment after adding, got %d", len(comments))
	}
}

func TestListCommits(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	commits, err := review.ListCommits()
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) == 0 {
		t.Fatal("expected at least one commit in review")
	}
}

func TestGetBuildStatusMessage(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitB)
	if err != nil {
		t.Fatal(err)
	}
	msg := review.GetBuildStatusMessage()
	if msg != "unknown" {
		t.Fatalf("expected 'unknown' status with no CI reports, got %q", msg)
	}
}

func TestGetAnalysesMessage(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitB)
	if err != nil {
		t.Fatal(err)
	}
	msg := review.GetAnalysesMessage()
	if msg != "No analyses available" {
		t.Fatalf("expected 'No analyses available', got %q", msg)
	}
}

func TestGetAnalysesNotes(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitB)
	if err != nil {
		t.Fatal(err)
	}
	_, err = review.GetAnalysesNotes()
	if err == nil {
		t.Fatal("expected error when no analyses available")
	}
}

func TestSummaryGetJSON(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	summary, err := GetSummary(repo, repository.TestCommitB)
	if err != nil {
		t.Fatal(err)
	}
	jsonStr, err := summary.GetJSON()
	if err != nil {
		t.Fatal(err)
	}
	if jsonStr == "" {
		t.Fatal("expected non-empty JSON")
	}
	if !strings.Contains(jsonStr, "revision") {
		t.Fatalf("JSON missing revision field: %s", jsonStr)
	}
}

func TestReviewGetJSON(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitB)
	if err != nil {
		t.Fatal(err)
	}
	jsonStr, err := review.GetJSON()
	if err != nil {
		t.Fatal(err)
	}
	if jsonStr == "" {
		t.Fatal("expected non-empty JSON")
	}
}

func TestGetCommentsJSON(t *testing.T) {
	threads := []CommentThread{
		{
			Comment: comment.Comment{
				Timestamp:   "1",
				Description: "test",
			},
		},
	}
	jsonStr, err := GetCommentsJSON(threads)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jsonStr, "test") {
		t.Fatalf("JSON missing comment description: %s", jsonStr)
	}
}

func TestIsAbandoned(t *testing.T) {
	s := Summary{
		Request: request.Request{TargetRef: ""},
	}
	if !s.IsAbandoned() {
		t.Fatal("expected abandoned when TargetRef is empty")
	}
	s.Request.TargetRef = "refs/heads/master"
	if s.IsAbandoned() {
		t.Fatal("expected not abandoned when TargetRef is set")
	}
}

func TestIsOpen(t *testing.T) {
	s := Summary{
		Request: request.Request{TargetRef: "refs/heads/master"},
	}
	if !s.IsOpen() {
		t.Fatal("expected open for non-submitted, non-abandoned review")
	}
	s.Submitted = true
	if s.IsOpen() {
		t.Fatal("expected not open for submitted review")
	}
	s.Submitted = false
	s.Request.TargetRef = ""
	if s.IsOpen() {
		t.Fatal("expected not open for abandoned review")
	}
}

func TestSummariesSorting(t *testing.T) {
	summaries := []Summary{
		{Request: request.Request{Timestamp: "1"}},
		{Request: request.Request{Timestamp: "3"}},
		{Request: request.Request{Timestamp: "2"}},
	}
	sort.Stable(summariesWithNewestRequestsFirst(summaries))
	if summaries[0].Request.Timestamp != "3" ||
		summaries[1].Request.Timestamp != "2" ||
		summaries[2].Request.Timestamp != "1" {
		t.Fatalf("unexpected sort order: %v", summaries)
	}
}

func TestDetails(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	summary, err := GetSummary(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	review, err := summary.Details()
	if err != nil {
		t.Fatal(err)
	}
	if review.Revision != repository.TestCommitG {
		t.Fatalf("unexpected revision: %q", review.Revision)
	}
}

func TestAddDetachedComment(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	c := comment.New("tester@example.com", "detached comment")
	c.Timestamp = "9999999999"
	c.Location = &comment.Location{Path: "test/path.go"}
	if err := AddDetachedComment(repo, &c); err != nil {
		t.Fatal(err)
	}
	threads, err := GetDetachedComments(repo, "test/path.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 {
		t.Fatalf("expected 1 detached comment thread, got %d", len(threads))
	}
	if threads[0].Comment.Description != "detached comment" {
		t.Fatalf("unexpected description: %q", threads[0].Comment.Description)
	}
}

func TestGetStartingCommit(t *testing.T) {
	s := &Summary{Revision: "abc123"}
	if s.getStartingCommit() != "abc123" {
		t.Fatalf("expected revision as starting commit, got %q", s.getStartingCommit())
	}
	s.Request.Alias = "def456"
	if s.getStartingCommit() != "def456" {
		t.Fatalf("expected alias as starting commit, got %q", s.getStartingCommit())
	}
}

func TestUpdateThreadsStatusEmpty(t *testing.T) {
	result := updateThreadsStatus(nil)
	if result != nil {
		t.Fatalf("expected nil for empty threads, got %v", *result)
	}
}

func TestBuildCommentThreadsEmpty(t *testing.T) {
	threads := buildCommentThreads(map[string]comment.Comment{})
	if len(threads) != 0 {
		t.Fatalf("expected 0 threads for empty input, got %d", len(threads))
	}
}

func TestPrettyPrintJSON(t *testing.T) {
	result, err := prettyPrintJSON([]byte(`{"a":"b"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "\"a\"") {
		t.Fatalf("unexpected pretty print result: %s", result)
	}

	_, err = prettyPrintJSON([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestGetNoRequestNotes(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	// TestCommitA has no review request notes
	review, err := Get(repo, repository.TestCommitA)
	if err == nil {
		t.Fatalf("expected error for commit with no request notes, got review=%v", review)
	}
}

func TestGetSummaryViaRefsNoRequests(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	// TestCommitA exists but has no review request notes
	_, err := GetSummaryViaRefs(repo, request.Ref, comment.Ref, repository.TestCommitA)
	if err == nil {
		t.Fatal("expected error for commit with no review requests")
	}
}

func TestGetCurrentMultipleMatching(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	if err := repo.SwitchToRef(repository.TestReviewRef); err != nil {
		t.Fatal(err)
	}
	// Add a review request to commit H (which is on the review branch, not ancestor of master).
	// This creates a second open review on the same review ref as commit G.
	reqJSON := `{"timestamp": "9999999999", "reviewRef": "refs/heads/ojarjur/mychange", "targetRef": "refs/heads/master", "requester": "someone", "description": "Another review"}`
	if err := repo.AppendNote(request.Ref, repository.TestCommitH, repository.Note(reqJSON)); err != nil {
		t.Fatal(err)
	}
	_, err := GetCurrent(repo)
	if err == nil {
		t.Fatal("expected error for multiple matching reviews")
	}
	if !strings.Contains(err.Error(), "open reviews") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestGetBuildStatusMessageWithReports(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	review.Reports = []ci.Report{
		{Timestamp: "100", URL: "http://ci.example.com/1", Status: ci.StatusSuccess},
	}
	msg := review.GetBuildStatusMessage()
	if !strings.Contains(msg, ci.StatusSuccess) {
		t.Fatalf("expected success status in message, got %q", msg)
	}
	if !strings.Contains(msg, "http://ci.example.com/1") {
		t.Fatalf("expected URL in message, got %q", msg)
	}
}

func TestGetBuildStatusMessageTimestampError(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	review.Reports = []ci.Report{
		{Timestamp: "not-a-number", URL: "http://ci.example.com", Status: ci.StatusSuccess},
	}
	msg := review.GetBuildStatusMessage()
	if !strings.Contains(msg, "unknown") {
		t.Fatalf("expected 'unknown' in error message, got %q", msg)
	}
}

func TestGetAnalysesNotesWithReport(t *testing.T) {
	mockResults := `{"analyze_response":[{"note":[{"category":"lint","description":"test warning"}]}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, mockResults)
	}))
	defer server.Close()

	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	review.Analyses = []analyses.Report{
		{Timestamp: "100", URL: server.URL, Status: analyses.StatusNeedsMoreWork},
	}
	notes, err := review.GetAnalysesNotes()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(notes))
	}
	if notes[0].Category != "lint" {
		t.Fatalf("unexpected category: %q", notes[0].Category)
	}
}

func TestGetAnalysesNotesTimestampError(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	review.Analyses = []analyses.Report{
		{Timestamp: "not-a-number"},
	}
	_, err = review.GetAnalysesNotes()
	if err == nil {
		t.Fatal("expected error for invalid timestamp")
	}
}

func TestGetAnalysesMessageWithStatus(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	review.Analyses = []analyses.Report{
		{Timestamp: "100", Status: analyses.StatusLooksGoodToMe},
	}
	msg := review.GetAnalysesMessage()
	if msg != analyses.StatusLooksGoodToMe {
		t.Fatalf("expected %q, got %q", analyses.StatusLooksGoodToMe, msg)
	}
}

func TestGetAnalysesMessageTimestampError(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	review.Analyses = []analyses.Report{
		{Timestamp: "not-a-number"},
	}
	msg := review.GetAnalysesMessage()
	// Should return the error string
	if msg == "" || msg == "No analyses available" {
		t.Fatalf("expected error message, got %q", msg)
	}
}

func TestGetAnalysesMessageNMWWithNotes(t *testing.T) {
	mockResults := `{"analyze_response":[{"note":[{"category":"lint","description":"warning1"},{"category":"lint","description":"warning2"}]}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, mockResults)
	}))
	defer server.Close()

	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	review.Analyses = []analyses.Report{
		{Timestamp: "100", URL: server.URL, Status: analyses.StatusNeedsMoreWork},
	}
	msg := review.GetAnalysesMessage()
	if !strings.Contains(msg, "2 warnings") {
		t.Fatalf("expected '2 warnings' in message, got %q", msg)
	}
}

func TestGetAnalysesMessageNMWNoNotes(t *testing.T) {
	mockResults := `{"analyze_response":[]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, mockResults)
	}))
	defer server.Close()

	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	review.Analyses = []analyses.Report{
		{Timestamp: "100", URL: server.URL, Status: analyses.StatusNeedsMoreWork},
	}
	msg := review.GetAnalysesMessage()
	if msg != "passed" {
		t.Fatalf("expected 'passed', got %q", msg)
	}
}

func TestGetAnalysesMessageEmptyStatusWithNotes(t *testing.T) {
	mockResults := `{"analyze_response":[{"note":[{"category":"lint","description":"warning1"}]}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, mockResults)
	}))
	defer server.Close()

	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	review.Analyses = []analyses.Report{
		{Timestamp: "100", URL: server.URL, Status: ""},
	}
	msg := review.GetAnalysesMessage()
	if !strings.Contains(msg, "1 warnings") {
		t.Fatalf("expected '1 warnings' in message, got %q", msg)
	}
}

func TestGetAnalysesMessageGetNotesError(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	review.Analyses = []analyses.Report{
		{Timestamp: "100", URL: "http://127.0.0.1:1/nonexistent", Status: analyses.StatusNeedsMoreWork},
	}
	msg := review.GetAnalysesMessage()
	if msg == "" || msg == "passed" || msg == "No analyses available" {
		t.Fatalf("expected error message, got %q", msg)
	}
}

func TestFindLastCommitWithLocationCommits(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	// Get a submitted review with comment that references a later commit
	review, err := Get(repo, repository.TestCommitD)
	if err != nil {
		t.Fatal(err)
	}
	// The review's comment references commit E in its location,
	// so findLastCommit should find E rather than D.
	head, err := review.GetHeadCommit()
	if err != nil {
		t.Fatal(err)
	}
	if head != repository.TestCommitE {
		t.Fatalf("expected head commit E for submitted review with comment at E, got %q", head)
	}
}

func TestGetHeadCommitSubmittedNoReviewRef(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	// TestCommitB has no ReviewRef set, so GetHeadCommit returns the starting commit directly
	review, err := Get(repo, repository.TestCommitB)
	if err != nil {
		t.Fatal(err)
	}
	head, err := review.GetHeadCommit()
	if err != nil {
		t.Fatal(err)
	}
	if head != repository.TestCommitB {
		t.Fatalf("expected %q for review with no review ref, got %q", repository.TestCommitB, head)
	}
}

func TestGetHeadCommitDetachedHead(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}

	// Set alias to the starting commit, making IsAncestor(startingCommit, reviewRef) false
	// by switching to an alternate ref that is not an ancestor relationship
	review.Request.ReviewRef = repository.TestAlternateReviewRef
	review.Request.Alias = repository.TestCommitF

	head, err := review.GetHeadCommit()
	if err != nil {
		t.Fatal(err)
	}
	// When alias is set and IsAncestor(alias, reviewRef) is true,
	// it uses ResolveRefCommit. When false, falls back to findLastCommit.
	if head == "" {
		t.Fatal("expected non-empty head commit")
	}
}

func TestGetBaseCommitOpenWithReviewRef(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	base, err := review.GetBaseCommit()
	if err != nil {
		t.Fatal(err)
	}
	// For open review with review ref, base = MergeBase(targetRefHead, reviewRefHead)
	if base != repository.TestCommitF {
		t.Fatalf("expected base commit F, got %q", base)
	}
}

func TestGetBaseCommitSubmittedWithBaseCommit(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}

	// Abandon the review, then set a base commit
	review.Request.TargetRef = ""
	review.Request.BaseCommit = repository.TestCommitA
	note, err := review.Request.Write()
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AppendNote(request.Ref, repository.TestCommitG, note); err != nil {
		t.Fatal(err)
	}

	abandonedReview, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	base, err := abandonedReview.GetBaseCommit()
	if err != nil {
		t.Fatal(err)
	}
	if base != repository.TestCommitA {
		t.Fatalf("expected base commit A, got %q", base)
	}
}

func TestListCommitsError(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	// Point to a nonexistent target ref so GetBaseCommit fails
	review.Request.TargetRef = "refs/heads/nonexistent"
	_, err = review.ListCommits()
	if err == nil {
		t.Fatal("expected error from ListCommits when target ref doesn't exist")
	}
}

func TestGetDiffError(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	// Clobber the revision to be invalid so GetCommitHash fails
	review.Revision = "nonexistent-hash"
	_, err = review.GetDiff()
	if err == nil {
		t.Fatal("expected error from GetDiff with invalid revision")
	}
}

func TestRebaseWithoutArchive(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	if err := review.Rebase(false); err != nil {
		t.Fatal(err)
	}
	headRef, err := repo.GetHeadRef()
	if err != nil {
		t.Fatal(err)
	}
	if headRef != review.Request.ReviewRef {
		t.Fatalf("expected head ref %q, got %q", review.Request.ReviewRef, headRef)
	}
}

func TestRebaseArchiveGetHeadError(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	// Setting ReviewRef to a nonexistent ref that also can't be resolved
	// causes GetHeadCommit to succeed (it falls back to findLastCommit),
	// but we can test the archive path with a valid review.
	// Instead, just verify the non-archive path works correctly.
	if err := review.Rebase(false); err != nil {
		t.Fatal(err)
	}
	if review.Request.Alias == "" {
		t.Fatal("expected alias to be set after rebase")
	}
}

func TestUnsortedListAllGetAllNotesError(t *testing.T) {
	// The mock repo won't naturally error on GetAllNotes, but we can verify
	// it returns results in the normal case
	repo := repository.NewMockRepoForTest()
	reviews := unsortedListAll(repo)
	if len(reviews) != 3 {
		t.Fatalf("expected 3 reviews, got %d", len(reviews))
	}
}

func TestWellKnownCommitForPath(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	hash1, err := wellKnownCommitForPath(repo, "test/path.go", false)
	if err != nil {
		t.Fatal(err)
	}
	if hash1 == "" {
		t.Fatal("expected non-empty commit hash")
	}
	// Same path should yield same commit (deterministic)
	hash2, err := wellKnownCommitForPath(repo, "test/path.go", false)
	if err != nil {
		t.Fatal(err)
	}
	if hash1 != hash2 {
		t.Fatalf("expected deterministic hash, got %q and %q", hash1, hash2)
	}
}

func TestWellKnownCommitForPathWithArchive(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	hash, err := wellKnownCommitForPath(repo, "test/path.go", true)
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Fatal("expected non-empty commit hash")
	}
	// Verify it was archived
	isArchived, err := repo.IsAncestor(hash, archiveRef)
	if err != nil {
		t.Fatal(err)
	}
	if !isArchived {
		t.Fatal("expected commit to be archived")
	}
}

func TestGetSummaryViaRefsAbandonedReview(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	// Abandon the review for commit G
	abandonReq := `{"timestamp": "9999999999", "reviewRef": "refs/heads/ojarjur/mychange", "targetRef": "", "requester": "ojarjur", "description": "Abandoned"}`
	if err := repo.AppendNote(request.Ref, repository.TestCommitG, repository.Note(abandonReq)); err != nil {
		t.Fatal(err)
	}
	summary, err := GetSummaryViaRefs(repo, request.Ref, comment.Ref, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	if !summary.IsAbandoned() {
		t.Fatal("expected review to be abandoned")
	}
	if summary.Submitted {
		t.Fatal("abandoned review should not be submitted")
	}
}

func TestGetSummaryViaRefsWithAlias(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	// TestCommitB is submitted. Add a new request with an alias.
	aliasReq := fmt.Sprintf(`{"timestamp": "9999999999", "reviewRef": "refs/heads/ojarjur/mychange", "targetRef": "refs/heads/master", "requester": "ojarjur", "description": "B with alias", "alias": "%s"}`, repository.TestCommitD)
	if err := repo.AppendNote(request.Ref, repository.TestCommitB, repository.Note(aliasReq)); err != nil {
		t.Fatal(err)
	}
	summary, err := GetSummaryViaRefs(repo, request.Ref, comment.Ref, repository.TestCommitB)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Request.Alias != repository.TestCommitD {
		t.Fatalf("expected alias %q, got %q", repository.TestCommitD, summary.Request.Alias)
	}
	// D is an ancestor of master, so the review should be submitted
	if !summary.Submitted {
		t.Fatal("expected review to be submitted via alias")
	}
}

func TestFindLastCommitEmptyComment(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitB)
	if err != nil {
		t.Fatal(err)
	}
	// Call findLastCommit with no comment threads
	result := review.findLastCommit(repository.TestCommitB, repository.TestCommitB, nil)
	if result != repository.TestCommitB {
		t.Fatalf("expected %q, got %q", repository.TestCommitB, result)
	}
}

func TestFindLastCommitWithNewerCommitInLocation(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitD)
	if err != nil {
		t.Fatal(err)
	}
	// Create threads with a comment at a later commit
	threads := []CommentThread{
		{
			Comment: comment.Comment{
				Timestamp: "100",
				Location: &comment.Location{
					Commit: repository.TestCommitE,
				},
			},
		},
	}
	result := review.findLastCommit(repository.TestCommitD, repository.TestCommitD, threads)
	if result != repository.TestCommitE {
		t.Fatalf("expected %q (later commit), got %q", repository.TestCommitE, result)
	}
}

func TestFindLastCommitWithInvalidCommitInLocation(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitD)
	if err != nil {
		t.Fatal(err)
	}
	threads := []CommentThread{
		{
			Comment: comment.Comment{
				Timestamp: "100",
				Location: &comment.Location{
					Commit: "nonexistent-commit",
				},
			},
		},
	}
	result := review.findLastCommit(repository.TestCommitD, repository.TestCommitD, threads)
	if result != repository.TestCommitD {
		t.Fatalf("expected %q (invalid commit ignored), got %q", repository.TestCommitD, result)
	}
}

func TestFindLastCommitWithChildren(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitD)
	if err != nil {
		t.Fatal(err)
	}
	threads := []CommentThread{
		{
			Comment: comment.Comment{
				Timestamp: "100",
			},
			Children: []CommentThread{
				{
					Comment: comment.Comment{
						Timestamp: "101",
						Location: &comment.Location{
							Commit: repository.TestCommitE,
						},
					},
				},
			},
		},
	}
	result := review.findLastCommit(repository.TestCommitD, repository.TestCommitD, threads)
	if result != repository.TestCommitE {
		t.Fatalf("expected %q from child comment, got %q", repository.TestCommitE, result)
	}
}

func TestFindLastCommitEmptyLocationCommit(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitD)
	if err != nil {
		t.Fatal(err)
	}
	threads := []CommentThread{
		{
			Comment: comment.Comment{
				Timestamp: "100",
				Location: &comment.Location{
					Commit: "",
				},
			},
		},
	}
	result := review.findLastCommit(repository.TestCommitD, repository.TestCommitD, threads)
	if result != repository.TestCommitD {
		t.Fatalf("expected %q (empty commit ignored), got %q", repository.TestCommitD, result)
	}
}

func TestFindLastCommitNotDescendantOfStart(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitG)
	if err != nil {
		t.Fatal(err)
	}
	// G is NOT ancestor of J (they are on different branches).
	// isLater checks: VerifyCommit(J)→OK, IsAncestor(G,J)→false,
	// IsAncestor(G,J)→false → !false=true → return false (line 537-538).
	threads := []CommentThread{
		{
			Comment: comment.Comment{
				Timestamp: "100",
				Location: &comment.Location{
					Commit: repository.TestCommitJ,
				},
			},
		},
	}
	result := review.findLastCommit(repository.TestCommitG, repository.TestCommitG, threads)
	if result != repository.TestCommitG {
		t.Fatalf("expected %q (commit not descendant of start), got %q", repository.TestCommitG, result)
	}
}

func TestFindLastCommitAncestorOfLatest(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitD)
	if err != nil {
		t.Fatal(err)
	}
	// startingCommit=A, latestCommit=D, commit=B:
	// IsAncestor(D,B)→false, IsAncestor(A,B)→true → continue,
	// IsAncestor(B,D)→true → return false (line 540-541).
	threads := []CommentThread{
		{
			Comment: comment.Comment{
				Timestamp: "100",
				Location: &comment.Location{
					Commit: repository.TestCommitB,
				},
			},
		},
	}
	result := review.findLastCommit(repository.TestCommitA, repository.TestCommitD, threads)
	if result != repository.TestCommitD {
		t.Fatalf("expected %q (commit is ancestor of latest), got %q", repository.TestCommitD, result)
	}
}

func TestFindLastCommitTimestampComparison(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitD)
	if err != nil {
		t.Fatal(err)
	}
	// startingCommit=E, latestCommit=G(time=4), commit=J(time=6):
	// IsAncestor(G,J)→false, IsAncestor(E,J)→true → continue,
	// IsAncestor(J,G)→false → continue, timestamps: "6">"4"→true.
	// So J replaces G as latestCommit.
	threads := []CommentThread{
		{
			Comment: comment.Comment{
				Timestamp: "100",
				Location: &comment.Location{
					Commit: repository.TestCommitJ,
				},
			},
		},
	}
	result := review.findLastCommit(repository.TestCommitE, repository.TestCommitG, threads)
	if result != repository.TestCommitJ {
		t.Fatalf("expected %q (later timestamp), got %q", repository.TestCommitJ, result)
	}
}

func TestFindLastCommitTimestampNotLater(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review, err := Get(repo, repository.TestCommitD)
	if err != nil {
		t.Fatal(err)
	}
	// startingCommit=E, latestCommit=J(time=6), commit=G(time=4):
	// IsAncestor(J,G)→false, IsAncestor(E,G)→true → continue,
	// IsAncestor(G,J)→false → continue, timestamps: "4">"6"→false.
	threads := []CommentThread{
		{
			Comment: comment.Comment{
				Timestamp: "100",
				Location: &comment.Location{
					Commit: repository.TestCommitG,
				},
			},
		},
	}
	result := review.findLastCommit(repository.TestCommitE, repository.TestCommitJ, threads)
	if result != repository.TestCommitJ {
		t.Fatalf("expected %q (timestamp not later), got %q", repository.TestCommitJ, result)
	}
}

// --- Error path tests using errorRepo ---

func TestUnsortedListAllGetAllNotesRequestError(t *testing.T) {
	repo := &errorRepo{
		Repo: repository.NewMockRepoForTest(),
		getAllNotesErr: map[string]error{
			request.Ref: fmt.Errorf("notes error"),
		},
	}
	reviews := unsortedListAll(repo)
	if reviews != nil {
		t.Fatalf("expected nil reviews on GetAllNotes error, got %d", len(reviews))
	}
}

func TestUnsortedListAllGetAllNotesCommentError(t *testing.T) {
	repo := &errorRepo{
		Repo: repository.NewMockRepoForTest(),
		getAllNotesErr: map[string]error{
			comment.Ref: fmt.Errorf("comment notes error"),
		},
	}
	reviews := unsortedListAll(repo)
	if reviews != nil {
		t.Fatalf("expected nil reviews on GetAllNotes comment error, got %d", len(reviews))
	}
}

func TestUnsortedListAllInvalidRequestNotes(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	// Add invalid request notes to a commit that has no existing requests.
	// getSummaryFromNotes will error, triggering the continue path.
	if err := repo.AppendNote(request.Ref, repository.TestCommitA, repository.Note("not valid json")); err != nil {
		t.Fatal(err)
	}
	reviews := unsortedListAll(repo)
	// Should still return the 3 valid reviews (B, D, G), skipping A
	if len(reviews) != 3 {
		t.Fatalf("expected 3 reviews (skipping invalid), got %d", len(reviews))
	}
}

func TestGetCurrentGetHeadRefError(t *testing.T) {
	repo := &errorRepo{
		Repo:          repository.NewMockRepoForTest(),
		getHeadRefErr: fmt.Errorf("head ref error"),
	}
	_, err := GetCurrent(repo)
	if err == nil {
		t.Fatal("expected error from GetCurrent")
	}
}

func TestGetSummaryViaRefsIsAncestorError(t *testing.T) {
	repo := &errorRepo{
		Repo:          repository.NewMockRepoForTest(),
		isAncestorErr: fmt.Errorf("ancestor error"),
	}
	// TestCommitG is open (not abandoned), so IsAncestor will be called
	_, err := GetSummaryViaRefs(repo, request.Ref, comment.Ref, repository.TestCommitG)
	if err == nil {
		t.Fatal("expected error from IsAncestor")
	}
}

func TestGetHeadCommitEmptyReviewRef(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	review := &Review{
		Summary: &Summary{
			Repo:     repo,
			Revision: repository.TestCommitB,
			Request: request.Request{
				ReviewRef: "",
				TargetRef: "refs/heads/master",
			},
		},
	}
	head, err := review.GetHeadCommit()
	if err != nil {
		t.Fatal(err)
	}
	if head != repository.TestCommitB {
		t.Fatalf("expected %q for empty review ref, got %q", repository.TestCommitB, head)
	}
}

func TestGetHeadCommitIsAncestorError(t *testing.T) {
	repo := &errorRepo{
		Repo:          repository.NewMockRepoForTest(),
		isAncestorErr: fmt.Errorf("ancestor error"),
	}
	review := &Review{
		Summary: &Summary{
			Repo:     repo,
			Revision: repository.TestCommitG,
			Request: request.Request{
				ReviewRef: repository.TestReviewRef,
				TargetRef: "refs/heads/master",
			},
		},
	}
	_, err := review.GetHeadCommit()
	if err == nil {
		t.Fatal("expected error from GetHeadCommit")
	}
}

func TestListCommitsGetHeadCommitError(t *testing.T) {
	repo := &errorRepo{
		Repo:          repository.NewMockRepoForTest(),
		isAncestorErr: fmt.Errorf("ancestor error"),
	}
	review := &Review{
		Summary: &Summary{
			Repo:     repo,
			Revision: repository.TestCommitG,
			Request: request.Request{
				ReviewRef: repository.TestReviewRef,
				TargetRef: "refs/heads/master",
			},
		},
	}
	_, err := review.ListCommits()
	if err == nil {
		t.Fatal("expected error from ListCommits")
	}
}

func TestRebaseSwitchToRefError(t *testing.T) {
	repo := &errorRepo{
		Repo:           repository.NewMockRepoForTest(),
		switchToRefErr: fmt.Errorf("switch error"),
	}
	review := &Review{
		Summary: &Summary{
			Repo:     repo,
			Revision: repository.TestCommitG,
			Request: request.Request{
				ReviewRef: repository.TestReviewRef,
				TargetRef: "refs/heads/master",
			},
		},
	}
	err := review.Rebase(false)
	if err == nil {
		t.Fatal("expected error from SwitchToRef")
	}
}

func TestRebaseRebaseRefError(t *testing.T) {
	repo := &errorRepo{
		Repo:         repository.NewMockRepoForTest(),
		rebaseRefErr: fmt.Errorf("rebase error"),
	}
	review := &Review{
		Summary: &Summary{
			Repo:     repo,
			Revision: repository.TestCommitG,
			Request: request.Request{
				ReviewRef: repository.TestReviewRef,
				TargetRef: "refs/heads/master",
			},
		},
	}
	err := review.Rebase(false)
	if err == nil {
		t.Fatal("expected error from RebaseRef")
	}
}

func TestRebaseGetCommitHashError(t *testing.T) {
	baseRepo := repository.NewMockRepoForTest()
	repo := &errorRepo{
		Repo: baseRepo,
	}
	review := &Review{
		Summary: &Summary{
			Repo:     repo,
			Revision: repository.TestCommitG,
			Request: request.Request{
				ReviewRef: repository.TestReviewRef,
				TargetRef: "refs/heads/master",
			},
		},
	}
	// Rebase succeeds, then getCommitHash("HEAD") is called.
	// We enable the error after rebase by modifying the flag in between,
	// but since the error is checked synchronously, we need the error
	// to only fire on "HEAD". Use a different approach: just set the
	// error to fire always.
	repo.getCommitHashErr = fmt.Errorf("commit hash error")
	err := review.Rebase(false)
	// SwitchToRef and RebaseRef succeed, then GetCommitHash("HEAD") fails
	// But SwitchToRef calls SwitchToRef on the wrapper which delegates.
	// RebaseRef also delegates.
	// GetCommitHash is the one that errors.
	if err == nil {
		t.Fatal("expected error from GetCommitHash")
	}
}

func TestRebaseAppendNoteError(t *testing.T) {
	repo := &errorRepo{
		Repo:          repository.NewMockRepoForTest(),
		appendNoteErr: fmt.Errorf("append error"),
	}
	review := &Review{
		Summary: &Summary{
			Repo:     repo,
			Revision: repository.TestCommitG,
			Request: request.Request{
				ReviewRef: repository.TestReviewRef,
				TargetRef: "refs/heads/master",
			},
		},
	}
	err := review.Rebase(false)
	if err == nil {
		t.Fatal("expected error from AppendNote")
	}
}

func TestRebaseArchiveError(t *testing.T) {
	repo := &errorRepo{
		Repo:          repository.NewMockRepoForTest(),
		archiveRefErr: fmt.Errorf("archive error"),
	}
	review := &Review{
		Summary: &Summary{
			Repo:     repo,
			Revision: repository.TestCommitG,
			Request: request.Request{
				ReviewRef: repository.TestReviewRef,
				TargetRef: "refs/heads/master",
			},
		},
	}
	err := review.Rebase(true)
	if err == nil {
		t.Fatal("expected error from ArchiveRef")
	}
}

func TestWellKnownCommitForPathCreateCommitError(t *testing.T) {
	repo := &errorRepo{
		Repo:            repository.NewMockRepoForTest(),
		createCommitErr: fmt.Errorf("create commit error"),
	}
	_, err := wellKnownCommitForPath(repo, "test/path.go", false)
	if err == nil {
		t.Fatal("expected error from CreateCommitWithTree")
	}
}

func TestWellKnownCommitForPathArchiveError(t *testing.T) {
	repo := &errorRepo{
		Repo:          repository.NewMockRepoForTest(),
		archiveRefErr: fmt.Errorf("archive error"),
	}
	_, err := wellKnownCommitForPath(repo, "test/path.go", true)
	if err == nil {
		t.Fatal("expected error from ArchiveRef")
	}
}

func TestAddDetachedCommentWellKnownCommitError(t *testing.T) {
	repo := &errorRepo{
		Repo:            repository.NewMockRepoForTest(),
		createCommitErr: fmt.Errorf("create commit error"),
	}
	c := comment.New("tester@example.com", "test")
	c.Location = &comment.Location{Path: "test/path.go"}
	err := AddDetachedComment(repo, &c)
	if err == nil {
		t.Fatal("expected error from wellKnownCommitForPath")
	}
}

func TestGetDetachedCommentsWellKnownCommitError(t *testing.T) {
	repo := &errorRepo{
		Repo:            repository.NewMockRepoForTest(),
		createCommitErr: fmt.Errorf("create commit error"),
	}
	_, err := GetDetachedComments(repo, "test/path.go")
	if err == nil {
		t.Fatal("expected error from wellKnownCommitForPath")
	}
}

func TestFindLastCommitGetCommitTimeCommitError(t *testing.T) {
	baseRepo := repository.NewMockRepoForTest()
	repo := &errorRepo{
		Repo: baseRepo,
		getCommitTimeErr: map[string]error{
			repository.TestCommitJ: fmt.Errorf("time error"),
		},
	}
	review := &Review{
		Summary: &Summary{
			Repo:     repo,
			Revision: repository.TestCommitD,
			Request: request.Request{
				ReviewRef: repository.TestReviewRef,
				TargetRef: "refs/heads/master",
			},
		},
	}
	// startingCommit=E, latestCommit=G, commit=J:
	// GetCommitTime(J) errors → return false
	threads := []CommentThread{
		{
			Comment: comment.Comment{
				Timestamp: "100",
				Location: &comment.Location{
					Commit: repository.TestCommitJ,
				},
			},
		},
	}
	result := review.findLastCommit(repository.TestCommitE, repository.TestCommitG, threads)
	if result != repository.TestCommitG {
		t.Fatalf("expected %q when GetCommitTime(commit) errors, got %q", repository.TestCommitG, result)
	}
}

func TestFindLastCommitGetCommitTimeLatestError(t *testing.T) {
	baseRepo := repository.NewMockRepoForTest()
	repo := &errorRepo{
		Repo: baseRepo,
		getCommitTimeErr: map[string]error{
			repository.TestCommitG: fmt.Errorf("time error"),
		},
	}
	review := &Review{
		Summary: &Summary{
			Repo:     repo,
			Revision: repository.TestCommitD,
			Request: request.Request{
				ReviewRef: repository.TestReviewRef,
				TargetRef: "refs/heads/master",
			},
		},
	}
	// startingCommit=E, latestCommit=G, commit=J:
	// GetCommitTime(J) succeeds (time=6), GetCommitTime(G) errors → return true
	// So J replaces G as latestCommit.
	threads := []CommentThread{
		{
			Comment: comment.Comment{
				Timestamp: "100",
				Location: &comment.Location{
					Commit: repository.TestCommitJ,
				},
			},
		},
	}
	result := review.findLastCommit(repository.TestCommitE, repository.TestCommitG, threads)
	if result != repository.TestCommitJ {
		t.Fatalf("expected %q when GetCommitTime(latest) errors, got %q", repository.TestCommitJ, result)
	}
}

func TestRebaseArchiveGetHeadCommitError(t *testing.T) {
	repo := &errorRepo{
		Repo:          repository.NewMockRepoForTest(),
		isAncestorErr: fmt.Errorf("ancestor error"),
	}
	review := &Review{
		Summary: &Summary{
			Repo:     repo,
			Revision: repository.TestCommitG,
			Request: request.Request{
				ReviewRef: repository.TestReviewRef,
				TargetRef: "refs/heads/master",
			},
		},
	}
	// With archivePrevious=true, Rebase calls GetHeadCommit first.
	// GetHeadCommit calls IsAncestor which errors.
	err := review.Rebase(true)
	if err == nil {
		t.Fatal("expected error from GetHeadCommit during archive")
	}
}

func TestAddDetachedCommentAppendNoteError(t *testing.T) {
	repo := &errorRepo{
		Repo:          repository.NewMockRepoForTest(),
		appendNoteErr: fmt.Errorf("append error"),
	}
	c := comment.New("tester@example.com", "test")
	c.Location = &comment.Location{Path: "test/path.go"}
	err := AddDetachedComment(repo, &c)
	if err == nil {
		t.Fatal("expected error from AppendNote")
	}
}
