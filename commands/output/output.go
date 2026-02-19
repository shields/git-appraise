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

// Package output contains helper methods for pretty-printing code reviews.
package output

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"msrl.dev/git-appraise/repository"
	"msrl.dev/git-appraise/review"
)

const (
	// Template for printing the summary of a list of reviews.
	reviewListTemplate = `Loaded %d reviews:
`
	// Template for printing the summary of a list of open reviews.
	openReviewListTemplate = `Loaded %d open reviews:
`
	// Template for printing the summary of a list of comment threads.
	commentListTemplate = `Loaded %d comment threads:
`
	// Template for printing the summary of a code review.
	reviewSummaryTemplate = `[%s] %.12s
  %s
`
	// Template for printing the summary of a code review.
	reviewDetailsTemplate = `  %q -> %q
  reviewers: %q
  requester: %q
  build status: %s
`
	// Template for printing the location of an inline comment
	commentLocationTemplate = `%s%q@%.12s
`
	// Template for printing a single comment.
	commentTemplate = `comment: %s
author: %s
time:   %s
status: %s`

	// Template for displaying the summary of the comment threads for a review
	commentSummaryTemplate = `  comments (%d threads):
`

	// Template for printing a single commit
	commitTemplate = `commit: %s
author: %s
time:   %s`

	// Number of lines of context to print for inline comments
	contextLineCount = 5
)

// getStatusString returns a human friendly string encapsulating both the review's
// resolved status, and its submitted status.
func getStatusString(r *review.Summary) string {
	if r.Resolved == nil && r.Submitted {
		return "tbr"
	}
	if r.Resolved == nil {
		return "pending"
	}
	if *r.Resolved && r.Submitted {
		return "submitted"
	}
	if *r.Resolved {
		return "accepted"
	}
	if r.Submitted {
		return "danger"
	}
	if r.Request.TargetRef == "" {
		return "abandon"
	}
	return "rejected"
}

// PrintSummaries prints single-line summaries of a slice of reviews.
func PrintSummaries(reviews []review.Summary, listAll bool) {
	if listAll {
		fmt.Printf(reviewListTemplate, len(reviews))
	} else {
		fmt.Printf(openReviewListTemplate, len(reviews))
	}
	for _, r := range reviews {
		PrintSummary(&r)
	}
}

// PrintSummary prints a single-line summary of a review.
func PrintSummary(r *review.Summary) {
	statusString := getStatusString(r)
	indentedDescription := strings.Replace(r.Request.Description, "\n", "\n  ", -1)
	fmt.Printf(reviewSummaryTemplate, statusString, r.Revision, indentedDescription)
}

// reformatTimestamp takes a timestamp string of the form "0123456789" and changes it
// to the form "Mon Jan _2 13:04:05 UTC 2006".
//
// Timestamps that are not in the format we expect are left alone.
func reformatTimestamp(timestamp string) string {
	parsedTimestamp, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		// The timestamp is an unexpected format, so leave it alone
		return timestamp
	}
	t := time.Unix(parsedTimestamp, 0)
	return t.Format(time.UnixDate)
}

// showThread prints the detailed output for an entire comment thread.
func showThread(repo repository.Repo, thread review.CommentThread, indent string) error {
	comment := thread.Comment
	if comment.Location != nil && comment.Location.Path != "" && comment.Location.Range != nil && comment.Location.Range.StartLine > 0 {
		contents, err := repo.Show(comment.Location.Commit, comment.Location.Path)
		if err != nil {
			return err
		}
		lines := strings.Split(contents, "\n")
		err = comment.Location.Check(repo)
		if err != nil {
			return err
		}
		if comment.Location.Range.StartLine <= uint32(len(lines)) {
			firstLine := comment.Location.Range.StartLine
			lastLine := comment.Location.Range.EndLine

			if lastLine == 0 {
				lastLine = firstLine
			}
			if lastLine > uint32(len(lines)) {
				lastLine = uint32(len(lines))
			}
			if firstLine > lastLine {
				firstLine = lastLine
			}

			if lastLine == firstLine {
				minLine := int(lastLine) - int(contextLineCount)
				if minLine <= 0 {
					minLine = 1
				}
				firstLine = uint32(minLine)
			}

			fmt.Printf(commentLocationTemplate, indent, comment.Location.Path, comment.Location.Commit)
			fmt.Println(indent + "|" + strings.Join(lines[firstLine-1:lastLine], "\n"+indent+"|"))
		}
	}
	showSubThread(repo, thread, indent)
	return nil
}

// showSubThread prints the given comment (sub)thread, indented by the given prefix string.
func showSubThread(repo repository.Repo, thread review.CommentThread, indent string) {
	statusString := "fyi"
	if thread.Resolved != nil {
		if *thread.Resolved {
			statusString = "lgtm"
		} else {
			statusString = "needs work"
		}
	}
	threadHash := thread.Hash
	timestamp := reformatTimestamp(thread.Comment.Timestamp)
	commentSummary := fmt.Sprintf(indent+commentTemplate, threadHash, thread.Comment.Author, timestamp, statusString)
	indent = indent + "  "
	indentedSummary := strings.Replace(commentSummary, "\n", "\n"+indent, -1)
	indentedDescription := Reflow(thread.Comment.Description, indent, 80)
	fmt.Println(indentedSummary)
	fmt.Println(indentedDescription)
	for _, child := range thread.Children {
		showSubThread(repo, child, indent)
	}
}

// printAnalyses prints the static analysis results for the latest commit in the review.
func printAnalyses(r *review.Review) {
	fmt.Println("  analyses: ", r.GetAnalysesMessage())
}

// printCommentsWithIndent prints all of the comment threads with the given indent before each line.
func printCommentsWithIndent(repo repository.Repo, c []review.CommentThread, indent string) error {
	for _, thread := range c {
		err := showThread(repo, thread, indent)
		if err != nil {
			return err
		}
	}
	return nil
}

// PrintComments prints all of the given comment threads.
func PrintComments(repo repository.Repo, c []review.CommentThread) error {
	fmt.Printf(commentListTemplate, len(c))
	return printCommentsWithIndent(repo, c, "  ")
}

// Separates comments into commit message comments and file comments. A line
// number of 0 for either means the comment belongs to the whole file, since
// line 0 is invalid. The line numbers indicate the end line rather than the
// start line (but if there is only start line then it is the start line). This
// way you can display comments just below what they are commenting on.
func SeparateComments(threads []review.CommentThread,
	commitThreads map[uint32][]review.CommentThread,
	lineThreads map[string]map[uint32][]review.CommentThread) {
	for _, thread := range threads {
		c := thread.Comment
		var commentLine uint32
		if c.Location != nil && c.Location.Range != nil {
			// No line is `0` so max picks start line if there is no end line.
			commentLine = max(c.Location.Range.StartLine, c.Location.Range.EndLine)
		}
		if c.Location == nil || c.Location.Path == "" {
			commentThread := commitThreads[commentLine]
			commentThread = append(commentThread, thread)
			commitThreads[commentLine] = commentThread
		} else {
			fileThread := lineThreads[c.Location.Path]
			if fileThread == nil {
				fileThread = make(map[uint32][]review.CommentThread)
			}
			lineThread := fileThread[commentLine]
			lineThread = append(lineThread, thread)
			fileThread[commentLine] = lineThread
			lineThreads[c.Location.Path] = fileThread
		}
	}
}

func PrintInlineComments(r *review.Review, diffArgs ...string) error {
	headCommit, err := r.Repo.GetCommitHash(r.Summary.Revision)
	if err != nil {
		return err
	}
	commitDetails, err := r.Repo.GetCommitDetails(headCommit)
	if err != nil {
		return err
	}
	commitMessage, err := r.Repo.GetCommitMessage(headCommit)
	if err != nil {
		return err
	}
	diffFiles, err := r.Repo.ParsedDiff1(headCommit, diffArgs...)
	if err != nil {
		return err
	}

	var commitThreads = make(map[uint32][]review.CommentThread)
	var lineThreads = make(map[string]map[uint32][]review.CommentThread)
	SeparateComments(r.Summary.Comments, commitThreads, lineThreads)

	// Line 0 is whole commit message comment
	// TODO: Print commit message
	// TODO: Rest of commit threads
	// TODO: Comments with empty file but line numbers are comments on that range
	// on the commit message?
	fmt.Printf(commitTemplate, headCommit, commitDetails.Author, commitDetails.AuthorTime)
	for _, thread := range commitThreads[0] {
		showSubThread(r.Repo, thread, "")
	}
	commitMessageLines := strings.Split(commitMessage, "\n")
	for i, line := range commitMessageLines {
		fmt.Println(line)
		for _, thread := range commitThreads[uint32(i+1)] {
			showSubThread(r.Repo, thread, "")
		}
	}

	for _, file := range diffFiles {
		// TODO: Are comments on old name, new name, or either?
		fmt.Printf(commentLocationTemplate, "", file.NewName, headCommit)
		// Line 0 is whole file comment
		for _, thread := range lineThreads[file.NewName][0] {
			showSubThread(r.Repo, thread, "| ")
		}
		var prevLine uint64 = 1
		for _, frag := range file.Fragments {
			lhs := frag.OldPosition
			rhs := frag.NewPosition
			maxLine := max(lhs+frag.OldLines, rhs+frag.NewLines)
			digits := 0
			for maxLine != 0 {
				maxLine /= 10
				digits++
			}
			if rhs != prevLine {
				fmt.Println("...")
			}
			for _, line := range frag.Lines {
				switch line.Op {
				case repository.OpContext:
					{
						fmt.Printf("%.*d %.*d", digits, lhs, digits, rhs)
						lhs++
						rhs++
					}
				case repository.OpAdd:
					{
						fmt.Printf("%*.s %.*d", digits, "", digits, rhs)
						rhs++
					}
				case repository.OpDelete:
					{
						fmt.Printf("%.*d %*.s", digits, lhs, digits, "")
						lhs++
					}
				}
				fmt.Printf("%s%s\n", line.Op.String(), strings.Trim(line.Line, "\n"))

				if line.Op == repository.OpContext || line.Op == repository.OpAdd {
					if rhs-1 >= 0 {
						for _, thread := range lineThreads[file.NewName][uint32(rhs-1)] {
							indent := strings.Repeat(" ", 2*digits+1)
							showSubThread(r.Repo, thread, indent+"| ")
						}
					}
				}
				prevLine = rhs
			}
		}
	}
	return nil
}

// printComments prints all of the comments for the review, with snippets of the preceding source code.
func printComments(r *review.Review) error {
	fmt.Printf(commentSummaryTemplate, len(r.Comments))
	return printCommentsWithIndent(r.Repo, r.Comments, "    ")
}

// PrintDetails prints a multi-line overview of a review, including all comments.
func PrintDetails(r *review.Review) error {
	PrintSummary(r.Summary)
	fmt.Printf(reviewDetailsTemplate, r.Request.ReviewRef, r.Request.TargetRef,
		strings.Join(r.Request.Reviewers, ", "),
		r.Request.Requester, r.GetBuildStatusMessage())
	printAnalyses(r)
	if err := printComments(r); err != nil {
		return err
	}
	return nil
}

var getCommentsJSON = review.GetCommentsJSON

// PrintCommentsJSON pretty prints the given review in JSON format.
func PrintCommentsJSON(c []review.CommentThread) error {
	json, err := getCommentsJSON(c)
	if err != nil {
		return err
	}
	fmt.Println(json)
	return nil
}

var getReviewJSON = func(r *review.Review) (string, error) {
	return r.GetJSON()
}

// PrintJSON pretty prints the given review in JSON format.
func PrintJSON(r *review.Review) error {
	json, err := getReviewJSON(r)
	if err != nil {
		return err
	}
	fmt.Println(json)
	return nil
}

// PrintDiff prints the diff of the review.
func PrintDiff(r *review.Review, diffArgs ...string) error {
	diff, err := r.GetDiff(diffArgs...)
	if err != nil {
		return err
	}
	fmt.Println(diff)
	return nil
}
