package request

import (
	"testing"

	"msrl.dev/git-appraise/repository"
)

func TestNew(t *testing.T) {
	r := New("user@example.com", []string{"reviewer1", "reviewer2"}, "refs/heads/feature", "refs/heads/master", "description")
	if r.Requester != "user@example.com" {
		t.Fatalf("unexpected requester: %q", r.Requester)
	}
	if len(r.Reviewers) != 2 {
		t.Fatalf("unexpected reviewers count: %d", len(r.Reviewers))
	}
	if r.ReviewRef != "refs/heads/feature" {
		t.Fatalf("unexpected review ref: %q", r.ReviewRef)
	}
	if r.TargetRef != "refs/heads/master" {
		t.Fatalf("unexpected target ref: %q", r.TargetRef)
	}
	if r.Description != "description" {
		t.Fatalf("unexpected description: %q", r.Description)
	}
}

func TestParseValid(t *testing.T) {
	note := repository.Note(`{"timestamp":"123","reviewRef":"refs/heads/feature","targetRef":"refs/heads/master","requester":"user@example.com","description":"hello"}`)
	r, err := Parse(note)
	if err != nil {
		t.Fatal(err)
	}
	if r.Timestamp != "123" {
		t.Fatalf("unexpected timestamp: %q", r.Timestamp)
	}
	if r.Requester != "user@example.com" {
		t.Fatalf("unexpected requester: %q", r.Requester)
	}
}

func TestParseInvalid(t *testing.T) {
	_, err := Parse(repository.Note(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseAllValid(t *testing.T) {
	notes := []repository.Note{
		repository.Note(`{"timestamp":"1","targetRef":"refs/heads/master","description":"first"}`),
		repository.Note(`not json`),
		repository.Note(`{"v":1,"targetRef":"refs/heads/master","description":"wrong version"}`),
		repository.Note(`{"timestamp":"2","targetRef":"refs/heads/master","description":"second"}`),
	}
	requests := ParseAllValid(notes)
	if len(requests) != 2 {
		t.Fatalf("expected 2 valid requests, got %d", len(requests))
	}
}

func TestWriteRoundtrip(t *testing.T) {
	r := New("user@example.com", []string{"reviewer"}, "refs/heads/feature", "refs/heads/master", "roundtrip")
	r.Timestamp = "1234567890"
	note, err := r.Write()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(note)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Requester != r.Requester || parsed.Description != r.Description || parsed.Timestamp != r.Timestamp {
		t.Fatalf("roundtrip failed: got %+v", parsed)
	}
	if parsed.TargetRef != r.TargetRef || parsed.ReviewRef != r.ReviewRef {
		t.Fatalf("roundtrip refs failed: got %+v", parsed)
	}
}
