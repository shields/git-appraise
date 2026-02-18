package comment

import (
	"testing"

	"msrl.dev/git-appraise/repository"
)

func TestNew(t *testing.T) {
	c := New("user@example.com", "test description")
	if c.Author != "user@example.com" {
		t.Fatalf("unexpected author: %q", c.Author)
	}
	if c.Description != "test description" {
		t.Fatalf("unexpected description: %q", c.Description)
	}
	if c.Timestamp != "" {
		t.Fatalf("expected empty timestamp, got %q", c.Timestamp)
	}
}

func TestParseValid(t *testing.T) {
	note := repository.Note(`{"timestamp":"1234567890","author":"user@example.com","description":"hello"}`)
	c, err := Parse(note)
	if err != nil {
		t.Fatal(err)
	}
	if c.Timestamp != "1234567890" {
		t.Fatalf("unexpected timestamp: %q", c.Timestamp)
	}
	if c.Author != "user@example.com" {
		t.Fatalf("unexpected author: %q", c.Author)
	}
	if c.Description != "hello" {
		t.Fatalf("unexpected description: %q", c.Description)
	}
}

func TestParseInvalid(t *testing.T) {
	_, err := Parse(repository.Note(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseWrongVersion(t *testing.T) {
	note := repository.Note(`{"v":1,"description":"hello"}`)
	c, err := Parse(note)
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != 1 {
		t.Fatalf("expected version 1, got %d", c.Version)
	}
}

func TestParseAllValid(t *testing.T) {
	notes := []repository.Note{
		repository.Note(`{"timestamp":"1","author":"a","description":"first"}`),
		repository.Note(`not json`),
		repository.Note(`{"v":1,"description":"wrong version"}`),
		repository.Note(`{"timestamp":"2","author":"b","description":"second"}`),
	}
	comments := ParseAllValid(notes)
	if len(comments) != 2 {
		t.Fatalf("expected 2 valid comments, got %d", len(comments))
	}
}

func TestWriteRoundtrip(t *testing.T) {
	c := New("user@example.com", "roundtrip test")
	c.Timestamp = "1234567890"
	note, err := c.Write()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(note)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Author != c.Author || parsed.Description != c.Description || parsed.Timestamp != c.Timestamp {
		t.Fatalf("roundtrip failed: got %+v", parsed)
	}
}

func TestHashDeterministic(t *testing.T) {
	c := New("user@example.com", "hash test")
	c.Timestamp = "1234567890"
	h1, err := c.Hash()
	if err != nil {
		t.Fatal(err)
	}
	h2, err := c.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("hash not deterministic: %q vs %q", h1, h2)
	}
}

func TestHashTimestampPadding(t *testing.T) {
	c1 := New("user@example.com", "padding test")
	c1.Timestamp = "1"
	h1, err := c1.Hash()
	if err != nil {
		t.Fatal(err)
	}
	c2 := New("user@example.com", "padding test")
	c2.Timestamp = "0000000001"
	h2, err := c2.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("padding should produce same hash: %q vs %q", h1, h2)
	}
}

func TestRangeSetEmpty(t *testing.T) {
	r := &Range{}
	if err := r.Set(""); err != nil {
		t.Fatal(err)
	}
	if r.StartLine != 0 || r.StartColumn != 0 || r.EndLine != 0 || r.EndColumn != 0 {
		t.Fatalf("expected zero range, got %+v", r)
	}
}

func TestRangeSetLineOnly(t *testing.T) {
	r := &Range{}
	if err := r.Set("5"); err != nil {
		t.Fatal(err)
	}
	if r.StartLine != 5 || r.StartColumn != 0 {
		t.Fatalf("unexpected range: %+v", r)
	}
}

func TestRangeSetLineAndColumn(t *testing.T) {
	r := &Range{}
	if err := r.Set("5+3"); err != nil {
		t.Fatal(err)
	}
	if r.StartLine != 5 || r.StartColumn != 3 {
		t.Fatalf("unexpected range: %+v", r)
	}
}

func TestRangeSetStartEnd(t *testing.T) {
	r := &Range{}
	if err := r.Set("5:10"); err != nil {
		t.Fatal(err)
	}
	if r.StartLine != 5 || r.EndLine != 10 {
		t.Fatalf("unexpected range: %+v", r)
	}
}

func TestRangeSetFull(t *testing.T) {
	r := &Range{}
	if err := r.Set("5+3:10+4"); err != nil {
		t.Fatal(err)
	}
	if r.StartLine != 5 || r.StartColumn != 3 || r.EndLine != 10 || r.EndColumn != 4 {
		t.Fatalf("unexpected range: %+v", r)
	}
}

func TestRangeSetStartGreaterThanEnd(t *testing.T) {
	r := &Range{}
	if err := r.Set("10:5"); err == nil {
		t.Fatal("expected error for start > end")
	}
}

func TestRangeSetInvalidNonNumeric(t *testing.T) {
	r := &Range{}
	if err := r.Set("abc"); err == nil {
		t.Fatal("expected error for non-numeric input")
	}
}

func TestRangeSetTooManyParts(t *testing.T) {
	r := &Range{}
	if err := r.Set("1:2:3"); err == nil {
		t.Fatal("expected error for too many colon-separated parts")
	}
}

func TestRangeSetTooManyPlusParts(t *testing.T) {
	r := &Range{}
	if err := r.Set("1+2+3"); err == nil {
		t.Fatal("expected error for too many plus-separated parts")
	}
}

func TestRangeSetColumnWithoutLine(t *testing.T) {
	r := &Range{}
	if err := r.Set("0+5"); err == nil {
		t.Fatal("expected error for column on line 0")
	}
}

func TestRangeStringRoundtrip(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"5", "5"},
		{"5+3", "5+3"},
		{"5:10", "5:10"},
		{"5+3:10+4", "5+3:10+4"},
	}
	for _, tt := range tests {
		r := &Range{}
		if err := r.Set(tt.input); err != nil {
			t.Fatalf("Set(%q) failed: %v", tt.input, err)
		}
		got := r.String()
		if got != tt.expected {
			t.Fatalf("String() = %q, want %q (input %q)", got, tt.expected, tt.input)
		}
	}
}

func TestLocationCheck(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	loc := &Location{
		Commit: repository.TestCommitB,
		Path:   "file.txt",
		Range:  &Range{StartLine: 1},
	}
	if err := loc.Check(repo); err != nil {
		t.Fatal(err)
	}
}

func TestLocationCheckOutOfRange(t *testing.T) {
	repo := repository.NewMockRepoForTest()
	loc := &Location{
		Commit: repository.TestCommitB,
		Path:   "file.txt",
		Range:  &Range{StartLine: 9999},
	}
	if err := loc.Check(repo); err == nil {
		t.Fatal("expected error for out-of-range line")
	}
}
