/*
Copyright 2016 Google Inc. All rights reserved.

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

package repository

import (
	"slices"
	"strings"
	"testing"
)

func TestEnvWithoutStripsGitLocationVars(t *testing.T) {
	// t.Setenv guarantees these are present in os.Environ() during the test
	// and restored afterwards.
	t.Setenv("GIT_DIR", "/somewhere/.git")
	t.Setenv("GIT_INDEX_FILE", "/somewhere/.git/index")
	t.Setenv("APPRAISE_TEST_KEEP", "keep-me")

	env := envWithout(gitLocationEnvVars...)

	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if slices.Contains(gitLocationEnvVars, name) {
			t.Errorf("expected %q to be stripped, found %q", name, kv)
		}
	}
	if !slices.Contains(env, "APPRAISE_TEST_KEEP=keep-me") {
		t.Error("expected unrelated variable APPRAISE_TEST_KEEP to be preserved")
	}
}

func TestMockGetNotesEmptyReturnsNil(t *testing.T) {
	t.Parallel()
	repo := NewMockRepoForTest()
	// A revision with no notes must yield no Note values, matching the real
	// GitRepo.GetNotes (rather than a single empty Note).
	if notes := repo.GetNotes(TestRequestsRef, "no-such-revision"); notes != nil {
		t.Fatalf("expected nil for a revision with no notes, got %#v", notes)
	}
	// A notes ref that does not exist at all must also yield nil.
	if notes := repo.GetNotes("refs/notes/missing", TestCommitB); notes != nil {
		t.Fatalf("expected nil for a missing notes ref, got %#v", notes)
	}
}

func TestMockAppendNoteFreshRef(t *testing.T) {
	t.Parallel()
	repo := NewMockRepoForTest()
	// Appending to a notes ref absent from the initial mock data must not
	// panic and must create the ref, matching the real implementation.
	if err := repo.AppendNote("refs/notes/fresh", TestCommitA, Note("first")); err != nil {
		t.Fatalf("AppendNote to fresh ref failed: %v", err)
	}
	notes := repo.GetNotes("refs/notes/fresh", TestCommitA)
	if len(notes) != 1 || string(notes[0]) != "first" {
		t.Fatalf("expected exactly [first] with no leading blank note, got %#v", notes)
	}
	// Appending a second note must append, not prepend a blank line.
	if err := repo.AppendNote("refs/notes/fresh", TestCommitA, Note("second")); err != nil {
		t.Fatal(err)
	}
	notes = repo.GetNotes("refs/notes/fresh", TestCommitA)
	if len(notes) != 2 || string(notes[0]) != "first" || string(notes[1]) != "second" {
		t.Fatalf("expected [first second], got %#v", notes)
	}
}

func TestMockAppendNoteNilNotesMap(t *testing.T) {
	t.Parallel()
	repo := NewMockRepoForTest().(*mockRepoForTest)
	// Exercise the path where the entire Notes map is nil.
	repo.Notes = nil
	if err := repo.AppendNote("refs/notes/fresh", TestCommitA, Note("only")); err != nil {
		t.Fatalf("AppendNote with nil Notes map failed: %v", err)
	}
	notes := repo.GetNotes("refs/notes/fresh", TestCommitA)
	if len(notes) != 1 || string(notes[0]) != "only" {
		t.Fatalf("expected exactly [only], got %#v", notes)
	}
}
