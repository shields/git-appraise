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
	"msrl.dev/git-appraise/repository"
	"msrl.dev/git-appraise/review/comment"
	"msrl.dev/git-appraise/review/request"
	"sort"
	"strings"
	"testing"
)

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
