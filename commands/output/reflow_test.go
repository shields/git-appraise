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
	// The width is an inclusive bound: a line that exactly fills the width is
	// kept on one line (matching the way `fmt` fills up to the column).
	input := "ab cd"
	got := Reflow(input, "", 5)
	want := "ab cd"
	if got != want {
		t.Errorf("Reflow(%q, %q, 5) = %q, want %q", input, "", got, want)
	}
}

func TestReflowDisplayWidth(t *testing.T) {
	// The wide character 世 occupies two display columns but three bytes;
	// wrapping is measured in display columns, so "世 x" (four columns) fits at
	// width 4 rather than wrapping as it would under a byte-length measurement.
	input := "世 x"
	got := Reflow(input, "", 4)
	want := "世 x"
	if got != want {
		t.Errorf("Reflow(%q, %q, 4) = %q, want %q", input, "", got, want)
	}
}

func TestReflowInvalidUTF8(t *testing.T) {
	// An invalid UTF-8 byte must be treated as a single byte rather than
	// over-advancing and panicking with a slice-bounds error.
	got := Reflow("\xff", "", 10)
	if got != "\xff" {
		t.Errorf("Reflow(invalid) = %q, want %q", got, "\xff")
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
