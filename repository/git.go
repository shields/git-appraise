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

// Package repository contains helper methods for working with the Git repo.
package repository

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

const (
	branchRefPrefix         = "refs/heads/"
	notesRefPrefix          = "refs/notes/"
	devtoolsRefPrefix       = "refs/devtools/"
	remoteDevtoolsRefPrefix = "refs/remoteDevtools/"
)

// GitRepo represents an instance of a (local) git repository.
type GitRepo struct {
	Path  string
	gogit *gogit.Repository
}

// execGitCommand is a test seam for injecting command execution failures.
// Tests must not run in parallel when overriding this variable (Go test
// packages run sequentially by default; t.Parallel is not used).
var execGitCommand = func(cmd *exec.Cmd) error {
	return cmd.Run()
}

// storeObject is a test seam for injecting object-storage failures.
var storeObject = func(repo *GitRepo, obj plumbing.EncodedObject) (plumbing.Hash, error) {
	return repo.gogit.Storer.SetEncodedObject(obj)
}

// gogitConfig is a test seam for injecting config read failures.
var gogitConfig = func(repo *GitRepo) (*config.Config, error) {
	return repo.gogit.ConfigScoped(config.SystemScope)
}

// gogitStatus is a test seam for injecting worktree status failures.
var gogitStatus = func(repo *GitRepo) (gogit.Status, error) {
	wt, err := repo.gogit.Worktree()
	if err != nil {
		return nil, err
	}
	return wt.Status()
}

// gogitHeadRef is a test seam for injecting HEAD reference read failures.
var gogitHeadRef = func(repo *GitRepo) (*plumbing.Reference, error) {
	return repo.gogit.Reference(plumbing.HEAD, false)
}

// gogitReferences is a test seam for injecting reference iterator failures.
var gogitReferences = func(repo *GitRepo) (refIter, error) {
	return repo.gogit.References()
}

// gogitRemotes is a test seam for injecting remote list failures.
var gogitRemotes = func(repo *GitRepo) ([]*gogit.Remote, error) {
	return repo.gogit.Remotes()
}

// refIter abstracts storer.ReferenceIter for the test seam.
type refIter interface {
	ForEach(func(*plumbing.Reference) error) error
}

// Run the given git command with the given I/O reader/writers and environment, returning an error if it fails.
func (repo *GitRepo) runGitCommandWithIOAndEnv(stdin io.Reader, stdout, stderr io.Writer, env []string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = repo.Path
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = env
	return execGitCommand(cmd)
}

// Run the given git command with the given I/O reader/writers, returning an error if it fails.
func (repo *GitRepo) runGitCommandWithIO(stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	return repo.runGitCommandWithIOAndEnv(stdin, stdout, stderr, nil, args...)
}

// Run the given git command and return its stdout, or an error if the command fails.
func (repo *GitRepo) runGitCommandRaw(args ...string) (string, string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := repo.runGitCommandWithIO(nil, &stdout, &stderr, args...)
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

// Run the given git command and return its stdout, or an error if the command fails.
func (repo *GitRepo) runGitCommand(args ...string) (string, error) {
	stdout, stderr, err := repo.runGitCommandRaw(args...)
	if err != nil {
		if stderr == "" {
			stderr = "Error running git command: " + strings.Join(args, " ")
		}
		err = fmt.Errorf("%s", stderr)
	}
	return stdout, err
}

// Run the given git command and return its stdout, or an error if the command fails.
func (repo *GitRepo) runGitCommandWithEnv(env []string, args ...string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := repo.runGitCommandWithIOAndEnv(nil, &stdout, &stderr, env, args...)
	if err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr == "" {
			stderrStr = "Error running git command: " + strings.Join(args, " ")
		}
		err = fmt.Errorf("%s", stderrStr)
	}
	return strings.TrimSpace(stdout.String()), err
}

// Run the given git command using the same stdin, stdout, and stderr as the review tool.
func (repo *GitRepo) runGitCommandInline(args ...string) error {
	return repo.runGitCommandWithIO(os.Stdin, os.Stdout, os.Stderr, args...)
}

// NewGitRepo determines if the given working directory is inside of a git repository,
// and returns the corresponding GitRepo instance if it is.
func NewGitRepo(path string) (*GitRepo, error) {
	// Try PlainOpen first (handles bare repos and exact paths), then fall
	// back to DetectDotGit for opening from subdirectories.
	r, err := gogit.PlainOpen(path)
	if err == gogit.ErrRepositoryNotExists {
		r, err = gogit.PlainOpenWithOptions(path, &gogit.PlainOpenOptions{
			DetectDotGit: true,
		})
	}
	if err != nil {
		return nil, err
	}
	// go-git's Worktree() returns ErrIsBareRepository for bare repos
	// and nil otherwise; no other error conditions exist.
	wt, err := r.Worktree()
	if err != nil {
		return &GitRepo{Path: path, gogit: r}, nil
	}
	// Path is set to the repo root so GetDataDir and CLI commands operate
	// from a consistent location. The remaining CLI methods (notes, diff,
	// fetch, push, merge, rebase) use absolute refs and commit hashes,
	// so running from the root rather than a subdirectory is correct.
	return &GitRepo{Path: wt.Filesystem.Root(), gogit: r}, nil
}

var errNotInitialized = fmt.Errorf("repository not initialized")

// resolveRevision resolves a ref string (which may be HEAD, a full ref, or a
// commit hash) to a plumbing.Hash using go-git's ResolveRevision.
func (repo *GitRepo) resolveRevision(ref string) (plumbing.Hash, error) {
	if repo.gogit == nil {
		return plumbing.ZeroHash, errNotInitialized
	}
	h, err := repo.gogit.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return plumbing.ZeroHash, err
	}
	return *h, nil
}

// resolveToCommit resolves a ref string to a commit object.
func (repo *GitRepo) resolveToCommit(ref string) (*object.Commit, error) {
	h, err := repo.resolveRevision(ref)
	if err != nil {
		return nil, err
	}
	return repo.gogit.CommitObject(h)
}

func (repo *GitRepo) HasRef(ref string) (bool, error) {
	if repo.gogit == nil {
		return false, errNotInitialized
	}
	_, err := repo.gogit.Reference(plumbing.ReferenceName(ref), false)
	if err == plumbing.ErrReferenceNotFound {
		return false, nil
	}
	return err == nil, err
}

// HasObject returns whether or not the repo contains an object with the given hash.
func (repo *GitRepo) HasObject(hash string) (bool, error) {
	if repo.gogit == nil {
		return false, errNotInitialized
	}
	h := plumbing.NewHash(hash)
	_, err := repo.gogit.Storer.EncodedObject(plumbing.AnyObject, h)
	if err == plumbing.ErrObjectNotFound {
		return false, nil
	}
	return err == nil, err
}

// GetPath returns the path to the repo.
func (repo *GitRepo) GetPath() string {
	return repo.Path
}

// GetDataDir returns the path to the repo data area, e.g. `.git` directory for git.
func (repo *GitRepo) GetDataDir() (string, error) {
	if repo.gogit == nil {
		return "", errNotInitialized
	}
	// NewGitRepo uses PlainOpen which always creates filesystem storage.
	return repo.gogit.Storer.(*filesystem.Storage).Filesystem().Root(), nil
}

// GetRepoStateHash returns a hash which embodies the entire current state of a repository.
func (repo *GitRepo) GetRepoStateHash() (string, error) {
	if repo.gogit == nil {
		return "", errNotInitialized
	}
	refs, err := gogitReferences(repo)
	if err != nil {
		return "", err
	}
	type refLine struct {
		name string
		line string
	}
	var entries []refLine
	err = refs.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name().String()
		// Match git show-ref behavior: only include refs under refs/
		if !strings.HasPrefix(name, "refs/") {
			return nil
		}
		entries = append(entries, refLine{
			name: name,
			line: fmt.Sprintf("%s %s", ref.Hash().String(), name),
		})
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})
	var lines []string
	for _, e := range entries {
		lines = append(lines, e.line)
	}
	stateSummary := strings.Join(lines, "\n")
	return fmt.Sprintf("%x", sha1.Sum([]byte(stateSummary))), nil
}

// GetUserEmail returns the email address that the user has used to configure git.
func (repo *GitRepo) GetUserEmail() (string, error) {
	if repo.gogit == nil {
		return "", errNotInitialized
	}
	cfg, err := gogitConfig(repo)
	if err != nil {
		return "", err
	}
	if email := cfg.User.Email; email != "" {
		return email, nil
	}
	return "", fmt.Errorf("user email not configured")
}

// GetCoreEditor returns the name of the editor that the user has used to configure git.
func (repo *GitRepo) GetCoreEditor() (string, error) {
	return repo.runGitCommand("var", "GIT_EDITOR")
}

// GetSubmitStrategy returns the way in which a review is submitted
func (repo *GitRepo) GetSubmitStrategy() (string, error) {
	if repo.gogit == nil {
		return "", nil
	}
	cfg, err := gogitConfig(repo)
	if err != nil {
		return "", nil
	}
	raw := cfg.Raw
	sec := raw.Section("appraise")
	val := sec.Option("submit")
	return val, nil
}

// HasUncommittedChanges returns true if there are local, uncommitted changes.
func (repo *GitRepo) HasUncommittedChanges() (bool, error) {
	if repo.gogit == nil {
		return false, errNotInitialized
	}
	status, err := gogitStatus(repo)
	if err != nil {
		return false, err
	}
	return !status.IsClean(), nil
}

// VerifyCommit verifies that the supplied hash points to a known commit.
func (repo *GitRepo) VerifyCommit(hash string) error {
	if repo.gogit == nil {
		return errNotInitialized
	}
	h := plumbing.NewHash(hash)
	obj, err := repo.gogit.Storer.EncodedObject(plumbing.AnyObject, h)
	if err != nil {
		return fmt.Errorf("Hash %q not found: %v", hash, err)
	}
	if obj.Type() != plumbing.CommitObject {
		return fmt.Errorf("Hash %q points to a non-commit object of type %q", hash, obj.Type())
	}
	return nil
}

// VerifyGitRef verifies that the supplied ref points to a known commit.
func (repo *GitRepo) VerifyGitRef(ref string) error {
	if repo.gogit == nil {
		return errNotInitialized
	}
	_, err := repo.gogit.Reference(plumbing.ReferenceName(ref), false)
	if err != nil {
		return fmt.Errorf("reference %q not found: %v", ref, err)
	}
	return nil
}

// GetHeadRef returns the ref that is the current HEAD.
func (repo *GitRepo) GetHeadRef() (string, error) {
	if repo.gogit == nil {
		return "", errNotInitialized
	}
	ref, err := gogitHeadRef(repo)
	if err != nil {
		return "", err
	}
	if ref.Type() != plumbing.SymbolicReference {
		return "", fmt.Errorf("HEAD is not a symbolic reference")
	}
	return ref.Target().String(), nil
}

// GetCommitHash returns the hash of the commit pointed to by the given ref.
func (repo *GitRepo) GetCommitHash(ref string) (string, error) {
	h, err := repo.resolveRevision(ref)
	if err != nil {
		return "", err
	}
	return h.String(), nil
}

// ResolveRefCommit returns the commit pointed to by the given ref, which may be a remote ref.
//
// This differs from GetCommitHash which only works on exact matches, in that it will try to
// intelligently handle the scenario of a ref not existing locally, but being known to exist
// in a remote repo.
//
// This method should be used when a command may be performed by either the reviewer or the
// reviewee, while GetCommitHash should be used when the encompassing command should only be
// performed by the reviewee.
func (repo *GitRepo) ResolveRefCommit(ref string) (string, error) {
	if err := repo.VerifyGitRef(ref); err == nil {
		return repo.GetCommitHash(ref)
	}
	if after, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
		// The ref is a branch. Check if it exists in exactly one remote
		suffix := after
		var matchingRefs []string
		refs, err := gogitReferences(repo)
		if err != nil {
			return "", err
		}
		err = refs.ForEach(func(r *plumbing.Reference) error {
			name := r.Name().String()
			if strings.HasPrefix(name, "refs/remotes/") && strings.HasSuffix(name, "/"+suffix) {
				matchingRefs = append(matchingRefs, name)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
		if len(matchingRefs) == 1 {
			return repo.GetCommitHash(matchingRefs[0])
		}
		return "", fmt.Errorf("Unable to find a git ref matching the pattern %q", "refs/remotes/*/"+suffix)
	}
	return "", fmt.Errorf("Unknown git ref %q", ref)
}

// GetCommitMessage returns the message stored in the commit pointed to by the given ref.
func (repo *GitRepo) GetCommitMessage(ref string) (string, error) {
	c, err := repo.resolveToCommit(ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(c.Message), nil
}

// GetCommitTime returns the commit time of the commit pointed to by the given ref.
func (repo *GitRepo) GetCommitTime(ref string) (string, error) {
	c, err := repo.resolveToCommit(ref)
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(c.Committer.When.Unix(), 10), nil
}

// GetLastParent returns the last parent of the given commit (as ordered by git).
// For merge commits, this is the second parent (the merged-in branch head),
// which is the intended behavior for the review diff base calculation.
func (repo *GitRepo) GetLastParent(ref string) (string, error) {
	c, err := repo.resolveToCommit(ref)
	if err != nil {
		return "", err
	}
	if len(c.ParentHashes) == 0 {
		return "", nil
	}
	return c.ParentHashes[len(c.ParentHashes)-1].String(), nil
}

// GetCommitDetails returns the details of a commit's metadata.
func (repo GitRepo) GetCommitDetails(ref string) (*CommitDetails, error) {
	c, err := repo.resolveToCommit(ref)
	if err != nil {
		return nil, err
	}
	var parents []string
	for _, p := range c.ParentHashes {
		parents = append(parents, p.String())
	}
	if parents == nil {
		parents = []string{""}
	}
	return &CommitDetails{
		Author:         c.Author.Name,
		AuthorEmail:    c.Author.Email,
		Committer:      c.Committer.Name,
		CommitterEmail: c.Committer.Email,
		Tree:           c.TreeHash.String(),
		Time:           strconv.FormatInt(c.Author.When.Unix(), 10),
		Parents:        parents,
		Summary:        strings.SplitN(c.Message, "\n", 2)[0],
	}, nil
}

// MergeBase determines if the first commit that is an ancestor of the two arguments.
func (repo *GitRepo) MergeBase(a, b string) (string, error) {
	cA, err := repo.resolveToCommit(a)
	if err != nil {
		return "", err
	}
	cB, err := repo.resolveToCommit(b)
	if err != nil {
		return "", err
	}
	// go-git's MergeBase returns empty bases (not an error) for disconnected histories.
	bases, _ := cA.MergeBase(cB)
	if len(bases) == 0 {
		return "", fmt.Errorf("no merge base found")
	}
	return bases[0].Hash.String(), nil
}

// IsAncestor determines if the first argument points to a commit that is an ancestor of the second.
func (repo *GitRepo) IsAncestor(ancestor, descendant string) (bool, error) {
	cAnc, err := repo.resolveToCommit(ancestor)
	if err != nil {
		return false, fmt.Errorf("Error while trying to determine commit ancestry: %v", err)
	}
	cDesc, err := repo.resolveToCommit(descendant)
	if err != nil {
		return false, fmt.Errorf("Error while trying to determine commit ancestry: %v", err)
	}
	if cAnc.Hash == cDesc.Hash {
		return true, nil
	}
	isAnc, err := cAnc.IsAncestor(cDesc)
	if err != nil {
		return false, fmt.Errorf("Error while trying to determine commit ancestry: %v", err)
	}
	return isAnc, nil
}

// Diff computes the diff between two given commits.
func (repo *GitRepo) Diff(left, right string, diffArgs ...string) (string, error) {
	args := []string{"diff"}
	args = append(args, diffArgs...)
	args = append(args, fmt.Sprintf("%s..%s", left, right))
	return repo.runGitCommand(args...)
}

func (repo *GitRepo) Diff1(commit string, diffArgs ...string) (string, error) {
	args := []string{"show", "--format=", "--patch"}
	args = append(args, diffArgs...)
	args = append(args, commit)
	args = append(args, "--")
	return repo.runGitCommand(args...)
}

// Diff computes the diff between two given commits.
func (repo *GitRepo) ParsedDiff(left, right string, diffArgs ...string) ([]FileDiff, error) {
	if !slices.Contains(diffArgs, "--no-ext-diff") {
		diffArgs = append(diffArgs, "--no-ext-diff")
	}
	diff, err := repo.Diff(left, right, diffArgs...)
	if err != nil {
		return nil, err
	}

	return parsedDiff(diff)
}

func (repo *GitRepo) ParsedDiff1(commit string, diffArgs ...string) ([]FileDiff, error) {
	if !slices.Contains(diffArgs, "--no-ext-diff") {
		diffArgs = append(diffArgs, "--no-ext-diff")
	}
	diff, err := repo.Diff1(commit, diffArgs...)
	if err != nil {
		return nil, err
	}

	return parsedDiff(diff)
}

func parsedDiff(diff string) ([]FileDiff, error) {
	files, _, err := gitdiff.Parse(strings.NewReader(diff))
	if err != nil {
		return nil, err
	}

	var fileDiff []FileDiff

	for _, file := range files {

		var fragments []DiffFragment
		for _, fragment := range file.TextFragments {

			var lines []DiffLine
			for _, line := range fragment.Lines {
				var op DiffOp
				switch line.Op {
				case gitdiff.OpContext:
					op = OpContext
				case gitdiff.OpAdd:
					op = OpAdd
				case gitdiff.OpDelete:
					op = OpDelete
				}
				lines = append(lines, DiffLine{
					Op:   op,
					Line: strings.Trim(line.Line, "\n"),
				})
			}

			fragments = append(fragments, DiffFragment{
				Comment:         fragment.Comment,
				OldPosition:     uint64(fragment.OldPosition),
				OldLines:        uint64(fragment.OldLines),
				NewPosition:     uint64(fragment.NewPosition),
				NewLines:        uint64(fragment.NewLines),
				LinesAdded:      uint64(fragment.LinesAdded),
				LinesDeleted:    uint64(fragment.LinesDeleted),
				LeadingContext:  uint64(fragment.LeadingContext),
				TrailingContext: uint64(fragment.TrailingContext),
				Lines:           lines,
			})
		}

		fileDiff = append(fileDiff, FileDiff{
			OldName:   file.OldName,
			NewName:   file.NewName,
			Fragments: fragments,
		})
	}

	return fileDiff, nil
}

// Show returns the contents of the given file at the given commit.
func (repo *GitRepo) Show(commit, path string) (string, error) {
	c, err := repo.resolveToCommit(commit)
	if err != nil {
		return "", err
	}
	f, err := c.File(path)
	if err != nil {
		return "", err
	}
	contents, err := f.Contents()
	return strings.TrimSpace(contents), err
}

// SwitchToRef changes the currently-checked-out ref.
func (repo *GitRepo) SwitchToRef(ref string) error {
	if repo.gogit == nil {
		return errNotInitialized
	}
	wt, err := repo.gogit.Worktree()
	if err != nil {
		return err
	}
	// If the ref is a branch, use the branch name to avoid a detached HEAD state.
	if strings.HasPrefix(ref, branchRefPrefix) {
		branchName := plumbing.ReferenceName(ref)
		return wt.Checkout(&gogit.CheckoutOptions{
			Branch: branchName,
		})
	}
	h, err := repo.resolveRevision(ref)
	if err != nil {
		return err
	}
	return wt.Checkout(&gogit.CheckoutOptions{
		Hash: h,
	})
}

// mergeArchives merges two archive refs.
func (repo *GitRepo) mergeArchives(archive, remoteArchive string) error {
	hasRemote, err := repo.HasRef(remoteArchive)
	if err != nil {
		return err
	}
	if !hasRemote {
		return nil
	}
	remoteHash, err := repo.GetCommitHash(remoteArchive)
	if err != nil {
		return err
	}

	hasLocal, _ := repo.HasRef(archive)
	if !hasLocal {
		// The local archive does not exist, so we merely need to set it
		return repo.SetRef(archive, remoteHash, "")
	}
	archiveHash, err := repo.GetCommitHash(archive)
	if err != nil {
		return err
	}

	isAncestor, err := repo.IsAncestor(archiveHash, remoteHash)
	if err != nil {
		return err
	}
	if isAncestor {
		// The archive can simply be fast-forwarded
		return repo.SetRef(archive, remoteHash, archiveHash)
	}

	// Create a merge commit of the two archives.
	// resolveToCommit will succeed because remoteArchive was already
	// verified as a valid commit ref above.
	cRemote, _ := repo.resolveToCommit(remoteArchive)
	newDetails := &CommitDetails{
		Summary: "Merge local and remote archives",
		Tree:    cRemote.TreeHash.String(),
		Parents: []string{remoteHash, archiveHash},
	}
	newArchiveHash, err := repo.CreateCommit(newDetails)
	if err != nil {
		return err
	}
	return repo.SetRef(archive, newArchiveHash, archiveHash)
}

// ArchiveRef adds the current commit pointed to by the 'ref' argument
// under the ref specified in the 'archive' argument.
//
// Both the 'ref' and 'archive' arguments are expected to be the fully
// qualified names of git refs (e.g. 'refs/heads/my-change' or
// 'refs/devtools/archives/reviews').
//
// If the ref pointed to by the 'archive' argument does not exist
// yet, then it will be created.
func (repo *GitRepo) ArchiveRef(ref, archive string) error {
	cRef, err := repo.resolveToCommit(ref)
	if err != nil {
		return err
	}
	refHash := cRef.Hash.String()

	var parents []string
	archiveHash, err := repo.GetCommitHash(archive)
	if err != nil {
		archiveHash = ""
	} else {
		if isAncestor, err := repo.IsAncestor(refHash, archiveHash); err != nil {
			return err
		} else if isAncestor {
			// The ref has already been archived, so we have nothing to do
			return nil
		}
		parents = append(parents, archiveHash)
	}
	parents = append(parents, refHash)

	newDetails := &CommitDetails{
		Summary: fmt.Sprintf("Archive %s", refHash),
		Tree:    cRef.TreeHash.String(),
		Parents: parents,
	}
	newArchiveHash, err := repo.CreateCommit(newDetails)
	if err != nil {
		return err
	}
	updatePrevious := ""
	if archiveHash != "" {
		updatePrevious = archiveHash
	}
	return repo.SetRef(archive, newArchiveHash, updatePrevious)
}

// MergeRef merges the given ref into the current one.
//
// The ref argument is the ref to merge, and fastForward indicates that the
// current ref should only move forward, as opposed to creating a bubble merge.
// The messages argument(s) provide text that should be included in the default
// merge commit message (separated by blank lines).
func (repo *GitRepo) MergeRef(ref string, fastForward bool, messages ...string) error {
	args := []string{"merge"}
	if fastForward {
		args = append(args, "--ff", "--ff-only")
	} else {
		args = append(args, "--no-ff")
	}
	if len(messages) > 0 {
		commitMessage := strings.Join(messages, "\n\n")
		args = append(args, "-e", "-m", commitMessage)
	}
	args = append(args, ref)
	return repo.runGitCommandInline(args...)
}

// RebaseRef rebases the current ref onto the given one.
func (repo *GitRepo) RebaseRef(ref string) error {
	return repo.runGitCommandInline("rebase", "-i", ref)
}

// ListCommits returns the list of commits reachable from the given ref.
//
// The generated list is in chronological order (with the oldest commit first).
//
// If the specified ref does not exist, then this method returns an empty result.
func (repo *GitRepo) ListCommits(ref string) []string {
	if repo.gogit == nil {
		return nil
	}
	h, err := repo.resolveRevision(ref)
	if err != nil {
		return nil
	}
	// Log returns the iterator without error; errors surface during iteration.
	iter, _ := repo.gogit.Log(&gogit.LogOptions{From: h, Order: gogit.LogOrderCommitterTime})
	var commits []string
	_ = iter.ForEach(func(c *object.Commit) error {
		commits = append(commits, c.Hash.String())
		return nil
	})
	slices.Reverse(commits)
	return commits
}

// ListCommitsBetween returns the list of commits between the two given revisions.
//
// The "from" parameter is the starting point (exclusive), and the "to"
// parameter is the ending point (inclusive).
//
// The "from" commit does not need to be an ancestor of the "to" commit. If it
// is not, then the merge base of the two is used as the starting point.
// Admittedly, this makes calling these the "between" commits is a bit of a
// misnomer, but it also makes the method easier to use when you want to
// generate the list of changes in a feature branch, as it eliminates the need
// to explicitly calculate the merge base. This also makes the semantics of the
// method compatible with git's built-in "rev-list" command.
//
// The generated list is in chronological order (with the oldest commit first).
func (repo *GitRepo) ListCommitsBetween(from, to string) ([]string, error) {
	if repo.gogit == nil {
		return nil, errNotInitialized
	}
	fromHash, err := repo.resolveRevision(from)
	if err != nil {
		return nil, err
	}
	toHash, err := repo.resolveRevision(to)
	if err != nil {
		return nil, err
	}
	if fromHash == toHash {
		return nil, nil
	}

	// Build exclusion set: all commits reachable from "from".
	exclude := make(map[plumbing.Hash]struct{})
	fromIter, err := repo.gogit.Log(&gogit.LogOptions{From: fromHash})
	if err != nil {
		return nil, err
	}
	fromIter.ForEach(func(c *object.Commit) error {
		exclude[c.Hash] = struct{}{}
		return nil
	})

	// Collect commits reachable from "to" not in exclude set.
	toIter, err := repo.gogit.Log(&gogit.LogOptions{From: toHash})
	if err != nil {
		return nil, err
	}
	var commits []string
	toIter.ForEach(func(c *object.Commit) error {
		if _, excluded := exclude[c.Hash]; !excluded {
			commits = append(commits, c.Hash.String())
		}
		return nil
	})
	slices.Reverse(commits)
	return commits, nil
}

// StoreBlob writes the given file to the repository and returns its hash.
func (repo *GitRepo) StoreBlob(contents string) (string, error) {
	if repo.gogit == nil {
		return "", fmt.Errorf("failure storing a git blob: repository not initialized")
	}
	obj := repo.gogit.Storer.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	// Writer, WriteString, and Close operate on an in-memory buffer
	// and cannot fail.
	w, _ := obj.Writer()
	io.WriteString(w, contents)
	w.Close()
	h, err := storeObject(repo, obj)
	if err != nil {
		return "", fmt.Errorf("failure storing a git blob: %v", err)
	}
	return h.String(), nil
}

// StoreTree writes the given file tree to the repository and returns its hash.
func (repo *GitRepo) StoreTree(contents map[string]TreeChild) (string, error) {
	if repo.gogit == nil {
		return "", fmt.Errorf("failure storing a git tree: repository not initialized")
	}
	var entries []object.TreeEntry
	for path, obj := range contents {
		objHash, err := obj.Store(repo)
		if err != nil {
			return "", err
		}
		mode := filemode.Dir
		if obj.Type() == "blob" {
			mode = filemode.Regular
		}
		entries = append(entries, object.TreeEntry{
			Name: path,
			Mode: mode,
			Hash: plumbing.NewHash(objHash),
		})
	}
	sort.Sort(object.TreeEntrySorter(entries))
	t := &object.Tree{Entries: entries}
	obj := repo.gogit.Storer.NewEncodedObject()
	// Encode writes to an in-memory buffer and cannot fail.
	t.Encode(obj)
	h, err := storeObject(repo, obj)
	if err != nil {
		return "", fmt.Errorf("failure storing a git tree: %v", err)
	}
	return h.String(), nil
}

func (repo *GitRepo) readBlob(objHash string) (*Blob, error) {
	if repo.gogit == nil {
		return nil, fmt.Errorf("failure reading the file contents of %q: repository not initialized", objHash)
	}
	h := plumbing.NewHash(objHash)
	obj, err := repo.gogit.BlobObject(h)
	if err != nil {
		return nil, fmt.Errorf("failure reading the file contents of %q: %v", objHash, err)
	}
	// Reader and ReadAll operate on the already-loaded blob and cannot fail.
	r, _ := obj.Reader()
	defer r.Close()
	data, _ := io.ReadAll(r)
	return &Blob{contents: string(data), savedHashes: map[Repo]string{repo: objHash}}, nil
}

func (repo *GitRepo) ReadTree(ref string) (*Tree, error) {
	return repo.readTreeWithHash(ref, "")
}

func (repo *GitRepo) readTreeWithHash(ref, hash string) (*Tree, error) {
	if repo.gogit == nil {
		return nil, fmt.Errorf("failure listing the file contents of %q: repository not initialized", ref)
	}
	h := plumbing.NewHash(ref)
	t, err := repo.gogit.TreeObject(h)
	if err != nil {
		return nil, fmt.Errorf("failure listing the file contents of %q: %v", ref, err)
	}
	contents := make(map[string]TreeChild)
	for _, entry := range t.Entries {
		var child TreeChild
		entryHash := entry.Hash.String()
		if entry.Mode == filemode.Dir {
			child, err = repo.readTreeWithHash(entryHash, entryHash)
		} else if entry.Mode == filemode.Submodule {
			return nil, fmt.Errorf("unrecognized tree object type for entry %q: submodule", entry.Name)
		} else {
			child, err = repo.readBlob(entryHash)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read a tree child object: %v", err)
		}
		contents[entry.Name] = child
	}
	result := NewTree(contents)
	result.savedHashes[repo] = hash
	return result, nil
}

// CreateCommit creates a commit object and returns its hash.
func (repo *GitRepo) CreateCommit(details *CommitDetails) (string, error) {
	if repo.gogit == nil {
		return "", fmt.Errorf("failure creating commit: repository not initialized")
	}
	now := time.Now()
	author := object.Signature{
		Name:  details.Author,
		Email: details.AuthorEmail,
		When:  now,
	}
	committer := object.Signature{
		Name:  details.Committer,
		Email: details.CommitterEmail,
		When:  now,
	}

	if details.AuthorTime != "" {
		if t, err := parseGitTime(details.AuthorTime); err == nil {
			author.When = t
		}
	}
	if details.Time != "" {
		if t, err := parseGitTime(details.Time); err == nil {
			committer.When = t
		}
	}

	// Fill in missing author/committer from config
	if author.Name == "" || author.Email == "" || committer.Name == "" || committer.Email == "" {
		cfg, err := repo.gogit.ConfigScoped(config.SystemScope)
		if err == nil {
			if author.Name == "" {
				author.Name = cfg.User.Name
			}
			if author.Email == "" {
				author.Email = cfg.User.Email
			}
			if committer.Name == "" {
				committer.Name = cfg.User.Name
			}
			if committer.Email == "" {
				committer.Email = cfg.User.Email
			}
		}
	}

	var parentHashes []plumbing.Hash
	for _, p := range details.Parents {
		if p == "" {
			continue
		}
		parentHashes = append(parentHashes, plumbing.NewHash(p))
	}

	c := &object.Commit{
		Author:       author,
		Committer:    committer,
		Message:      details.Summary,
		TreeHash:     plumbing.NewHash(details.Tree),
		ParentHashes: parentHashes,
	}
	obj := repo.gogit.Storer.NewEncodedObject()
	// Encode writes to an in-memory buffer and cannot fail.
	c.Encode(obj)
	h, err := storeObject(repo, obj)
	if err != nil {
		return "", fmt.Errorf("failure creating commit: %v", err)
	}
	return h.String(), nil
}

// parseGitTime parses a git time string (unix timestamp with optional timezone).
func parseGitTime(s string) (time.Time, error) {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return time.Time{}, fmt.Errorf("empty time string")
	}
	ts, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	loc := time.UTC
	if len(parts) > 1 {
		if tz, err := time.Parse("-0700", parts[1]); err == nil {
			loc = tz.Location()
		}
	}
	return time.Unix(ts, 0).In(loc), nil
}

// CreateCommitWithTree creates a commit object with the given tree and returns its hash.
func (repo *GitRepo) CreateCommitWithTree(details *CommitDetails, t *Tree) (string, error) {
	treeHash, err := repo.StoreTree(t.Contents())
	if err != nil {
		return "", fmt.Errorf("failure storing a tree: %v", err)
	}
	details.Tree = treeHash
	return repo.CreateCommit(details)
}

// SetRef sets the commit pointed to by the specified ref to `newCommitHash`,
// iff the ref currently points `previousCommitHash`.
func (repo *GitRepo) SetRef(ref, newCommitHash, previousCommitHash string) error {
	if repo.gogit == nil {
		return errNotInitialized
	}
	newRef := plumbing.NewHashReference(plumbing.ReferenceName(ref), plumbing.NewHash(newCommitHash))
	if previousCommitHash != "" {
		oldRef := plumbing.NewHashReference(plumbing.ReferenceName(ref), plumbing.NewHash(previousCommitHash))
		return repo.gogit.Storer.CheckAndSetReference(newRef, oldRef)
	}
	return repo.gogit.Storer.SetReference(newRef)
}

// readNotesTree resolves a notes ref to the commit's tree.
// Returns nil, nil if the ref doesn't exist.
func (repo *GitRepo) readNotesTree(notesRef string) (*object.Tree, error) {
	c, err := repo.readNotesCommit(notesRef)
	if err != nil || c == nil {
		return nil, err
	}
	return c.Tree()
}

type notesEntry struct {
	ObjectHash string
	BlobHash   plumbing.Hash
}

// collectNotesEntries collects (annotatedObjectHash, blobHash) pairs from a
// notes tree, handling both flat and fan-out (2-char directory prefix) layouts.
func collectNotesEntries(tree *object.Tree, prefix string) ([]notesEntry, error) {
	var entries []notesEntry
	for _, entry := range tree.Entries {
		if entry.Mode == filemode.Dir {
			subtree, err := tree.Tree(entry.Name)
			if err != nil {
				return nil, fmt.Errorf("reading subtree %s%s: %w", prefix, entry.Name, err)
			}
			sub, err := collectNotesEntries(subtree, prefix+entry.Name)
			if err != nil {
				return nil, err
			}
			entries = append(entries, sub...)
		} else {
			entries = append(entries, notesEntry{prefix + entry.Name, entry.Hash})
		}
	}
	return entries, nil
}

// readBlobContents reads a blob and returns its content as a string.
func (repo *GitRepo) readBlobContents(h plumbing.Hash) (string, error) {
	obj, err := repo.gogit.BlobObject(h)
	if err != nil {
		return "", err
	}
	r, err := obj.Reader()
	if err != nil {
		return "", err
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// splitNotesBlob splits a notes blob's content into individual Note values,
// matching the behavior of `git notes show` (which trims trailing whitespace).
func splitNotesBlob(contents string) []Note {
	contents = strings.TrimRight(contents, "\n")
	var notes []Note
	for line := range strings.SplitSeq(contents, "\n") {
		notes = append(notes, Note([]byte(line)))
	}
	return notes
}

// lookupNoteEntry looks up the notes tree entry for the given revision hash,
// handling arbitrary fan-out depth. Git notes fan-out always splits at byte
// boundaries (2 hex chars per level), so entries are either flat or nested in
// 2-char directory segments (e.g. "ab/cdef..." or "ab/cd/ef01...").
func lookupNoteEntry(tree *object.Tree, remaining string) (*object.TreeEntry, error) {
	// Try direct entry at this level (handles flat layout and leaf of fan-out).
	entry, err := tree.FindEntry(remaining)
	if err == nil {
		return entry, nil
	}
	// Try fan-out: find a directory entry that is a prefix of the remaining
	// hash and recurse into it. If a matching directory doesn't contain the
	// note, continue checking other entries for longer prefix matches.
	for _, entry := range tree.Entries {
		if entry.Mode == filemode.Dir && strings.HasPrefix(remaining, entry.Name) {
			subtree, err := tree.Tree(entry.Name)
			if err != nil {
				continue
			}
			found, err := lookupNoteEntry(subtree, remaining[len(entry.Name):])
			if err == nil {
				return found, nil
			}
		}
	}
	return nil, plumbing.ErrObjectNotFound
}

// GetNotes reads the notes from the given ref for a given revision.
func (repo *GitRepo) GetNotes(notesRef, revision string) []Note {
	tree, err := repo.readNotesTree(notesRef)
	if err != nil || tree == nil {
		return nil
	}
	entry, err := lookupNoteEntry(tree, revision)
	if err != nil {
		return nil
	}
	contents, err := repo.readBlobContents(entry.Hash)
	if err != nil {
		return nil
	}
	return splitNotesBlob(contents)
}

// GetAllNotes reads the contents of the notes under the given ref for every commit.
//
// The returned value is a mapping from commit hash to the list of notes for that commit.
func (repo *GitRepo) GetAllNotes(notesRef string) (map[string][]Note, error) {
	tree, err := repo.readNotesTree(notesRef)
	if err != nil {
		return nil, err
	}
	if tree == nil {
		return nil, nil
	}
	entries, err := collectNotesEntries(tree, "")
	if err != nil {
		return nil, err
	}
	commitNotesMap := make(map[string][]Note)
	for _, e := range entries {
		// Only include notes for commit objects. Requesting CommitObject
		// directly lets the storer check the type from the object header
		// without fully decompressing the object.
		if _, err := repo.gogit.Storer.EncodedObject(plumbing.CommitObject, plumbing.NewHash(e.ObjectHash)); err != nil {
			continue
		}
		contents, err := repo.readBlobContents(e.BlobHash)
		if err != nil {
			continue
		}
		commitNotesMap[e.ObjectHash] = splitNotesBlob(contents)
	}
	return commitNotesMap, nil
}

// readNotesCommit resolves a notes ref to its commit object.
// Returns nil, nil if the ref doesn't exist. Returns an error if the
// ref exists but doesn't point to a commit (e.g. corrupt ref).
func (repo *GitRepo) readNotesCommit(notesRef string) (*object.Commit, error) {
	if repo.gogit == nil {
		return nil, errNotInitialized
	}
	// Check if the ref actually exists before resolving.
	_, err := repo.gogit.Reference(plumbing.ReferenceName(notesRef), true)
	if err == plumbing.ErrReferenceNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// ResolveRevision handles annotated tags: it tries CommitObject first,
	// then falls back to TagObject → Commit() to peel to the underlying
	// commit (see go-git repository.go ResolveRevision).
	h, err := repo.gogit.ResolveRevision(plumbing.Revision(notesRef))
	if err != nil {
		return nil, fmt.Errorf("resolving notes ref %s: %w", notesRef, err)
	}
	return repo.gogit.CommitObject(*h)
}

// notesSignature returns a Signature for notes commits from git config.
func (repo *GitRepo) notesSignature() object.Signature {
	sig := object.Signature{When: time.Now()}
	cfg, err := repo.gogit.ConfigScoped(config.SystemScope)
	if err == nil {
		sig.Name = cfg.User.Name
		sig.Email = cfg.User.Email
	}
	return sig
}

// detectFanout checks if a notes tree uses fan-out by looking for directory
// entries. Returns true if any entry is a directory.
func detectFanout(tree *object.Tree) bool {
	for _, entry := range tree.Entries {
		if entry.Mode == filemode.Dir {
			return true
		}
	}
	return false
}

// buildNotesTree creates a new notes tree with the given entry added or
// replaced. It preserves the existing tree's fan-out layout.
func (repo *GitRepo) buildNotesTree(existing *object.Tree, revision string, blobHash plumbing.Hash) (plumbing.Hash, error) {
	var entries []object.TreeEntry
	entryName := revision
	fanout := existing != nil && detectFanout(existing)
	if fanout {
		entryName = revision[:2] + "/" + revision[2:]
	}

	if existing != nil {
		for _, e := range existing.Entries {
			if e.Mode == filemode.Dir && fanout {
				// Rebuild subtree if it might contain the target entry.
				if e.Name == revision[:2] {
					subtree, err := existing.Tree(e.Name)
					if err != nil {
						return plumbing.ZeroHash, err
					}
					newSubEntries := make([]object.TreeEntry, 0, len(subtree.Entries))
					for _, se := range subtree.Entries {
						if se.Name != revision[2:] {
							newSubEntries = append(newSubEntries, se)
						}
					}
					newSubEntries = append(newSubEntries, object.TreeEntry{
						Name: revision[2:],
						Mode: filemode.Regular,
						Hash: blobHash,
					})
					sort.Sort(object.TreeEntrySorter(newSubEntries))
					subTree := &object.Tree{Entries: newSubEntries}
					obj := repo.gogit.Storer.NewEncodedObject()
					if err := subTree.Encode(obj); err != nil {
						return plumbing.ZeroHash, err
					}
					h, err := storeObject(repo, obj)
					if err != nil {
						return plumbing.ZeroHash, err
					}
					entries = append(entries, object.TreeEntry{
						Name: e.Name,
						Mode: filemode.Dir,
						Hash: h,
					})
					continue
				}
				entries = append(entries, e)
			} else if e.Name != revision {
				entries = append(entries, e)
			}
		}
	}

	if fanout {
		// Check if we already added the subtree above.
		found := false
		for _, e := range entries {
			if e.Name == revision[:2] {
				found = true
				break
			}
		}
		if !found {
			// Create new subtree for this fan-out prefix.
			subEntries := []object.TreeEntry{{
				Name: revision[2:],
				Mode: filemode.Regular,
				Hash: blobHash,
			}}
			subTree := &object.Tree{Entries: subEntries}
			obj := repo.gogit.Storer.NewEncodedObject()
			if err := subTree.Encode(obj); err != nil {
				return plumbing.ZeroHash, err
			}
			h, err := storeObject(repo, obj)
			if err != nil {
				return plumbing.ZeroHash, err
			}
			entries = append(entries, object.TreeEntry{
				Name: revision[:2],
				Mode: filemode.Dir,
				Hash: h,
			})
		}
	} else {
		entries = append(entries, object.TreeEntry{
			Name: entryName,
			Mode: filemode.Regular,
			Hash: blobHash,
		})
	}

	sort.Sort(object.TreeEntrySorter(entries))
	t := &object.Tree{Entries: entries}
	obj := repo.gogit.Storer.NewEncodedObject()
	if err := t.Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	return storeObject(repo, obj)
}

// AppendNote appends a note to a revision under the given ref.
func (repo *GitRepo) AppendNote(notesRef, revision string, note Note) error {
	if repo.gogit == nil {
		return errNotInitialized
	}

	// Get existing notes commit and tree (may be nil if ref doesn't exist).
	parentCommit, err := repo.readNotesCommit(notesRef)
	if err != nil {
		return err
	}

	var existingTree *object.Tree
	if parentCommit != nil {
		existingTree, err = parentCommit.Tree()
		if err != nil {
			return err
		}
	}

	// Read existing note content for this revision, if any.
	var newContent string
	if existingTree != nil {
		entry, err := lookupNoteEntry(existingTree, revision)
		if err == nil {
			existing, err := repo.readBlobContents(entry.Hash)
			if err == nil && existing != "" {
				newContent = strings.TrimRight(existing, "\n") + "\n"
			}
		}
	}
	newContent += string(note) + "\n"

	// Store the new blob.
	blobHashStr, err := repo.StoreBlob(newContent)
	if err != nil {
		return err
	}
	blobHash := plumbing.NewHash(blobHashStr)

	// Build the updated notes tree.
	treeHash, err := repo.buildNotesTree(existingTree, revision, blobHash)
	if err != nil {
		return err
	}

	// Create notes commit.
	sig := repo.notesSignature()
	var parentHashes []plumbing.Hash
	if parentCommit != nil {
		parentHashes = []plumbing.Hash{parentCommit.Hash}
	}
	c := &object.Commit{
		Author:       sig,
		Committer:    sig,
		Message:      "Notes added by 'git notes append'\n",
		TreeHash:     treeHash,
		ParentHashes: parentHashes,
	}
	obj := repo.gogit.Storer.NewEncodedObject()
	if err := c.Encode(obj); err != nil {
		return err
	}
	commitHash, err := storeObject(repo, obj)
	if err != nil {
		return err
	}

	// Update the notes ref.
	var previousHash string
	if parentCommit != nil {
		previousHash = parentCommit.Hash.String()
	}
	return repo.SetRef(notesRef, commitHash.String(), previousHash)
}

// ListNotedRevisions returns the collection of revisions that are annotated by notes in the given ref.
func (repo *GitRepo) ListNotedRevisions(notesRef string) []string {
	tree, err := repo.readNotesTree(notesRef)
	if err != nil || tree == nil {
		return nil
	}
	entries, err := collectNotesEntries(tree, "")
	if err != nil {
		return nil
	}
	var revisions []string
	for _, e := range entries {
		if _, err := repo.gogit.Storer.EncodedObject(plumbing.CommitObject, plumbing.NewHash(e.ObjectHash)); err != nil {
			continue
		}
		revisions = append(revisions, e.ObjectHash)
	}
	return revisions
}

// Remotes returns a list of the remotes.
func (repo *GitRepo) Remotes() ([]string, error) {
	if repo.gogit == nil {
		return nil, errNotInitialized
	}
	remotes, err := gogitRemotes(repo)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, r := range remotes {
		result = append(result, r.Config().Name)
	}
	sort.Strings(result)
	return result, nil
}

// Fetch fetches from the given remote using the supplied refspecs.
func (repo *GitRepo) Fetch(remote string, refspecs ...string) error {
	args := []string{"fetch", remote}
	args = append(args, refspecs...)
	return repo.runGitCommandInline(args...)
}

// PushNotes pushes git notes to a remote repo.
func (repo *GitRepo) PushNotes(remote, notesRefPattern string) error {
	refspec := fmt.Sprintf("%s:%s", notesRefPattern, notesRefPattern)

	// The push is liable to fail if the user forgot to do a pull first, so
	// we treat errors as user errors rather than fatal errors.
	err := repo.runGitCommandInline("push", remote, refspec)
	if err != nil {
		return fmt.Errorf("Failed to push to the remote '%s': %v", remote, err)
	}
	return nil
}

// PushNotesAndArchive pushes the given notes and archive refs to a remote repo.
func (repo *GitRepo) PushNotesAndArchive(remote, notesRefPattern, archiveRefPattern string) error {
	notesRefspec := fmt.Sprintf("%s:%s", notesRefPattern, notesRefPattern)
	archiveRefspec := fmt.Sprintf("%s:%s", archiveRefPattern, archiveRefPattern)
	err := repo.runGitCommandInline("push", remote, notesRefspec, archiveRefspec)
	if err != nil {
		return fmt.Errorf("Failed to push the local archive to the remote '%s': %v", remote, err)
	}
	return nil
}

func (repo *GitRepo) getRefHashes(refPattern string) (map[string]string, error) {
	if !strings.HasSuffix(refPattern, "/*") {
		return nil, fmt.Errorf("unsupported ref pattern %q", refPattern)
	}
	if repo.gogit == nil {
		return nil, errNotInitialized
	}
	refPrefix := strings.TrimSuffix(refPattern, "*")
	refs, err := gogitReferences(repo)
	if err != nil {
		return nil, err
	}
	refsMap := make(map[string]string)
	err = refs.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name().String()
		if strings.HasPrefix(name, refPrefix) {
			refsMap[name] = ref.Hash().String()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return refsMap, nil
}

func getRemoteNotesRef(remote, localNotesRef string) string {
	// Note: The pattern for remote notes deviates from that of remote heads and devtools,
	// because the git command line tool requires all notes refs to be located under the
	// "refs/notes/" prefix.
	//
	// Because of that, we make the remote refs a subset of the local refs instead of
	// a parallel tree, which is the pattern used for heads and devtools.
	//
	// E.G. ("refs/notes/..." -> "refs/notes/remotes/<remote>/...")
	//   versus ("refs/heads/..." -> "refs/remotes/<remote>/...")
	relativeNotesRef := strings.TrimPrefix(localNotesRef, notesRefPrefix)
	return notesRefPrefix + "remotes/" + remote + "/" + relativeNotesRef
}

func getLocalNotesRef(remote, remoteNotesRef string) string {
	relativeNotesRef := strings.TrimPrefix(remoteNotesRef, notesRefPrefix+"remotes/"+remote+"/")
	return notesRefPrefix + relativeNotesRef
}

// MergeNotes merges in the remote's state of the notes reference into the
// local repository's.
// catSortUniq implements the cat_sort_uniq merge strategy: concatenate lines
// from both sources, sort, and deduplicate.
func catSortUniq(local, remote string) string {
	lines := make(map[string]struct{})
	for line := range strings.SplitSeq(local, "\n") {
		if line != "" {
			lines[line] = struct{}{}
		}
	}
	for line := range strings.SplitSeq(remote, "\n") {
		if line != "" {
			lines[line] = struct{}{}
		}
	}
	return strings.Join(slices.Sorted(maps.Keys(lines)), "\n")
}

func (repo *GitRepo) MergeNotes(remote, notesRefPattern string) error {
	remoteRefPattern := getRemoteNotesRef(remote, notesRefPattern)
	refsMap, err := repo.getRefHashes(remoteRefPattern)
	if err != nil {
		return err
	}
	for remoteRef := range refsMap {
		localRef := getLocalNotesRef(remote, remoteRef)
		if err := repo.mergeNotesRef(localRef, remoteRef); err != nil {
			return err
		}
	}
	return nil
}

// mergeNotesRef merges a remote notes ref into a local notes ref using
// the cat_sort_uniq strategy.
func (repo *GitRepo) mergeNotesRef(localRef, remoteRef string) error {
	localCommit, err := repo.readNotesCommit(localRef)
	if err != nil {
		return err
	}
	remoteCommit, err := repo.readNotesCommit(remoteRef)
	if err != nil {
		return err
	}
	if remoteCommit == nil {
		return nil
	}

	var localEntries []notesEntry
	var localTree *object.Tree
	if localCommit != nil {
		localTree, err = localCommit.Tree()
		if err != nil {
			return err
		}
		localEntries, err = collectNotesEntries(localTree, "")
		if err != nil {
			return err
		}
	}

	remoteTree, err := remoteCommit.Tree()
	if err != nil {
		return err
	}
	remoteEntries, err := collectNotesEntries(remoteTree, "")
	if err != nil {
		return err
	}

	// Build maps for merging.
	localMap := make(map[string]plumbing.Hash, len(localEntries))
	for _, e := range localEntries {
		localMap[e.ObjectHash] = e.BlobHash
	}
	remoteMap := make(map[string]plumbing.Hash, len(remoteEntries))
	for _, e := range remoteEntries {
		remoteMap[e.ObjectHash] = e.BlobHash
	}

	// Merge: union of all keys, cat_sort_uniq for conflicts.
	allKeys := make(map[string]struct{})
	for k := range localMap {
		allKeys[k] = struct{}{}
	}
	for k := range remoteMap {
		allKeys[k] = struct{}{}
	}

	// Build flat notes tree entries.
	var treeEntries []object.TreeEntry
	for objHash := range allKeys {
		localBlobHash, inLocal := localMap[objHash]
		remoteBlobHash, inRemote := remoteMap[objHash]

		var blobHash plumbing.Hash
		switch {
		case inLocal && inRemote && localBlobHash == remoteBlobHash:
			blobHash = localBlobHash
		case inLocal && inRemote:
			localContent, err := repo.readBlobContents(localBlobHash)
			if err != nil {
				return err
			}
			remoteContent, err := repo.readBlobContents(remoteBlobHash)
			if err != nil {
				return err
			}
			merged := catSortUniq(localContent, remoteContent) + "\n"
			hashStr, err := repo.StoreBlob(merged)
			if err != nil {
				return err
			}
			blobHash = plumbing.NewHash(hashStr)
		case inLocal:
			blobHash = localBlobHash
		default:
			blobHash = remoteBlobHash
		}

		treeEntries = append(treeEntries, object.TreeEntry{
			Name: objHash,
			Mode: filemode.Regular,
			Hash: blobHash,
		})
	}

	sort.Sort(object.TreeEntrySorter(treeEntries))
	t := &object.Tree{Entries: treeEntries}
	obj := repo.gogit.Storer.NewEncodedObject()
	if err := t.Encode(obj); err != nil {
		return err
	}
	treeHash, err := storeObject(repo, obj)
	if err != nil {
		return err
	}

	// Create merge commit (or regular commit if local didn't exist).
	sig := repo.notesSignature()
	var parentHashes []plumbing.Hash
	if localCommit != nil {
		parentHashes = append(parentHashes, localCommit.Hash)
	}
	parentHashes = append(parentHashes, remoteCommit.Hash)

	c := &object.Commit{
		Author:       sig,
		Committer:    sig,
		Message:      "notes merge by go-git\n",
		TreeHash:     treeHash,
		ParentHashes: parentHashes,
	}
	commitObj := repo.gogit.Storer.NewEncodedObject()
	if err := c.Encode(commitObj); err != nil {
		return err
	}
	commitHash, err := storeObject(repo, commitObj)
	if err != nil {
		return err
	}

	var previousHash string
	if localCommit != nil {
		previousHash = localCommit.Hash.String()
	}
	return repo.SetRef(localRef, commitHash.String(), previousHash)
}

// PullNotes fetches the contents of the given notes ref from a remote repo,
// and then merges them with the corresponding local notes using the
// "cat_sort_uniq" strategy.
func (repo *GitRepo) PullNotes(remote, notesRefPattern string) error {
	remoteNotesRefPattern := getRemoteNotesRef(remote, notesRefPattern)
	fetchRefSpec := fmt.Sprintf("+%s:%s", notesRefPattern, remoteNotesRefPattern)
	err := repo.Fetch(remote, fetchRefSpec)
	if err != nil {
		return err
	}

	return repo.MergeNotes(remote, notesRefPattern)
}

func getRemoteDevtoolsRef(remote, devtoolsRefPattern string) string {
	relativeRef := strings.TrimPrefix(devtoolsRefPattern, devtoolsRefPrefix)
	return remoteDevtoolsRefPrefix + remote + "/" + relativeRef
}

func getLocalDevtoolsRef(remote, remoteDevtoolsRef string) string {
	relativeRef := strings.TrimPrefix(remoteDevtoolsRef, remoteDevtoolsRefPrefix+remote+"/")
	return devtoolsRefPrefix + relativeRef
}

// MergeArchives merges in the remote's state of the archives reference into
// the local repository's.
func (repo *GitRepo) MergeArchives(remote, archiveRefPattern string) error {
	remoteRefPattern := getRemoteDevtoolsRef(remote, archiveRefPattern)
	refsMap, err := repo.getRefHashes(remoteRefPattern)
	if err != nil {
		return err
	}
	for remoteRef := range refsMap {
		localRef := getLocalDevtoolsRef(remote, remoteRef)
		if err := repo.mergeArchives(localRef, remoteRef); err != nil {
			return err
		}
	}
	return nil
}

// FetchAndReturnNewReviewHashes fetches the notes "branches" and then susses
// out the IDs (the revision the review points to) of any new reviews, then
// returns that list of IDs.
//
// This is accomplished by determining which files in the notes tree have
// changed because the _names_ of these files correspond to the revisions they
// point to.
func (repo *GitRepo) FetchAndReturnNewReviewHashes(remote, notesRefPattern string, devtoolsRefPatterns ...string) ([]string, error) {
	for _, refPattern := range devtoolsRefPatterns {
		if !strings.HasPrefix(refPattern, devtoolsRefPrefix) {
			return nil, fmt.Errorf("Unsupported devtools ref: %q", refPattern)
		}
	}
	remoteNotesRefPattern := getRemoteNotesRef(remote, notesRefPattern)
	notesFetchRefSpec := fmt.Sprintf("+%s:%s", notesRefPattern, remoteNotesRefPattern)

	localDevtoolsRefPattern := devtoolsRefPrefix + "*"
	remoteDevtoolsRefPattern := getRemoteDevtoolsRef(remote, localDevtoolsRefPattern)
	devtoolsFetchRefSpec := fmt.Sprintf("+%s:%s", localDevtoolsRefPattern, remoteDevtoolsRefPattern)

	priorRefHashes, err := repo.getRefHashes(remoteNotesRefPattern)
	if err != nil {
		return nil, fmt.Errorf("failure reading the existing ref hashes for the remote %q: %v", remote, err)
	}

	if err := repo.Fetch(remote, notesFetchRefSpec, devtoolsFetchRefSpec); err != nil {
		return nil, fmt.Errorf("failure fetching from the remote %q: %v", remote, err)
	}

	updatedRefHashes, err := repo.getRefHashes(remoteNotesRefPattern)
	if err != nil {
		return nil, fmt.Errorf("failure reading the updated ref hashes for the remote %q: %v", remote, err)
	}

	updatedReviewSet := make(map[string]struct{})
	for ref, hash := range updatedRefHashes {
		priorHash, ok := priorRefHashes[ref]
		if priorHash == hash {
			continue
		}
		var notes string
		var err error
		if !ok {
			// This is a new ref, so include every noted object
			notes, err = repo.runGitCommand("ls-tree", "-r", "--name-only", hash)
		} else {
			notes, err = repo.runGitCommand("diff", "--name-only", priorHash, hash)
		}
		if err != nil {
			return nil, err
		}
		// The name of the review matches the name of the notes tree entry, with slashes removed
		reviews := strings.SplitSeq(strings.Replace(notes, "/", "", -1), "\n")
		for review := range reviews {
			updatedReviewSet[review] = struct{}{}
		}
	}

	updatedReviews := make([]string, 0, len(updatedReviewSet))
	for key := range updatedReviewSet {
		updatedReviews = append(updatedReviews, key)
	}
	return updatedReviews, nil
}

// PullNotesAndArchive fetches the contents of the notes and archives refs from
// a remote repo, and merges them with the corresponding local refs.
//
// For notes refs, we assume that every note can be automatically merged using
// the 'cat_sort_uniq' strategy (the git-appraise schemas fit that requirement),
// so we automatically merge the remote notes into the local notes.
//
// For "archive" refs, they are expected to be used solely for maintaining
// reachability of commits that are part of the history of any reviews,
// so we do not maintain any consistency with their tree objects. Instead,
// we merely ensure that their history graph includes every commit that we
// intend to keep.
func (repo *GitRepo) PullNotesAndArchive(remote, notesRefPattern, archiveRefPattern string) error {
	if _, err := repo.FetchAndReturnNewReviewHashes(remote, notesRefPattern, archiveRefPattern); err != nil {
		return fmt.Errorf("failure fetching from the remote %q: %v", remote, err)
	}
	if err := repo.MergeArchives(remote, archiveRefPattern); err != nil {
		return fmt.Errorf("failure merging archives from the remote %q: %v", remote, err)
	}
	if err := repo.MergeNotes(remote, notesRefPattern); err != nil {
		return fmt.Errorf("failure merging notes from the remote %q: %v", remote, err)
	}
	return nil
}

// Push pushes the given refs to a remote repo.
func (repo *GitRepo) Push(remote string, refSpecs ...string) error {
	pushArgs := append([]string{"push", remote}, refSpecs...)
	err := repo.runGitCommandInline(pushArgs...)
	if err != nil {
		return fmt.Errorf("Failed to push the local refs to the remote '%s': %v", remote, err)
	}
	return nil
}
