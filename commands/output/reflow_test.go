package output

import "testing"

func TestReflowBasicWrapping(t *testing.T) {
	input := "the quick brown fox jumps over the lazy dog"
	got := Reflow(input, "", 20)
	want := "the quick brown fox\njumps over the lazy\ndog"
	if got != want {
		t.Errorf("Reflow(%q, %q, 20) =\n%q\nwant\n%q", input, "", got, want)
	}
}

func TestReflowParagraphPreservation(t *testing.T) {
	input := "first paragraph\n\nsecond paragraph"
	got := Reflow(input, "", 80)
	want := "first paragraph\n\nsecond paragraph"
	if got != want {
		t.Errorf("Reflow(%q, %q, 80) =\n%q\nwant\n%q", input, "", got, want)
	}
}

func TestReflowPrefix(t *testing.T) {
	input := "hello world foo bar"
	got := Reflow(input, "> ", 15)
	want := "> hello world\n> foo bar"
	if got != want {
		t.Errorf("Reflow(%q, %q, 15) =\n%q\nwant\n%q", input, "> ", got, want)
	}
}

func TestReflowEmptyString(t *testing.T) {
	got := Reflow("", "", 80)
	if got != "" {
		t.Errorf("Reflow(%q, %q, 80) = %q, want %q", "", "", got, "")
	}
}

func TestReflowSingleLongWord(t *testing.T) {
	input := "supercalifragilisticexpialidocious"
	got := Reflow(input, "", 10)
	// A single word longer than width cannot be broken, so it stays on one line
	want := "supercalifragilisticexpialidocious"
	if got != want {
		t.Errorf("Reflow(%q, %q, 10) = %q, want %q", input, "", got, want)
	}
}

func TestReflowSingleNewlineJoinsLines(t *testing.T) {
	input := "hello\nworld"
	got := Reflow(input, "", 80)
	want := "hello world"
	if got != want {
		t.Errorf("Reflow(%q, %q, 80) = %q, want %q", input, "", got, want)
	}
}

func TestReflowExactWidthBoundary(t *testing.T) {
	// Reflow uses strict less-than (column+wordLen+1 < maxCol) so a word
	// that would land exactly at the width wraps to the next line. This
	// documents current behavior; the width acts as an exclusive bound.
	input := "ab cd"
	got := Reflow(input, "", 5)
	want := "ab\ncd"
	if got != want {
		t.Errorf("Reflow(%q, %q, 5) = %q, want %q", input, "", got, want)
	}
}

func TestReflowMultipleInternalSpaces(t *testing.T) {
	input := "foo   bar"
	got := Reflow(input, "", 80)
	want := "foo bar"
	if got != want {
		t.Errorf("Reflow(%q, %q, 80) = %q, want %q", input, "", got, want)
	}
}

func TestReflowTrailingWhitespace(t *testing.T) {
	input := "hello world   "
	got := Reflow(input, "", 80)
	want := "hello world"
	if got != want {
		t.Errorf("Reflow(%q, %q, 80) = %q, want %q", input, "", got, want)
	}
}
