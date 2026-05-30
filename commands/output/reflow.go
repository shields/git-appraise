package output

import (
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// Reflows the text `in` with `prefix` before each line, and each line having
// maximum `width` display columns (not including `prefix`). Lengths are measured
// in display columns (via runewidth) rather than bytes so multibyte and
// East-Asian-wide characters wrap correctly. Two newlines indicate a new
// paragraph.
func Reflow(in, prefix string, width int) string {
	line := strings.Builder{}
	wordStart := -1
	wordEnd := 0
	prefixLen := runewidth.StringWidth(prefix)
	maxCol := width - prefixLen
	column := 0
	const (
		normal int = iota
		space
		oneNewLine
		manyNewLines
	)
	state := normal

	addWord := func() {
		if wordStart >= 0 {
			wordLen := runewidth.StringWidth(in[wordStart:wordEnd]) // excluding current space
			wordFits := column+wordLen+1 <= maxCol                  // including separating space
			if column == 0 || wordFits {
				if column == 0 {
					line.WriteString(prefix)
				} else {
					line.WriteRune(' ')
					column += 1
				}
			} else {
				line.WriteRune('\n')
				line.WriteString(prefix)
				column = 0
			}
			line.WriteString(in[wordStart:wordEnd])
			column += wordLen
			wordStart = -1
		}
	}

	for i, r := range in {
		switch r {
		case ' ', '\r', '\t':
			if state == normal {
				addWord()
				state = space
			} else if state == space || state == manyNewLines {
				// noop
			}
		case '\n':
			if state == normal {
				addWord()
				state = oneNewLine
			} else if state == space {
				state = oneNewLine
			} else if state == oneNewLine {
				line.WriteString("\n")
				line.WriteString(prefix)
				line.WriteString("\n")
				column = 0
				wordStart = -1
				state = manyNewLines
			} else if state == manyNewLines {
				// noop
			}
		default:
			if wordStart < 0 {
				wordStart = i
			}
			// Advance by the rune's actual byte length. DecodeRuneInString
			// reports a size of 1 for an invalid byte (where utf8.RuneLen of the
			// resulting RuneError would be 3), so wordEnd never runs past the
			// end of the string.
			_, size := utf8.DecodeRuneInString(in[i:])
			wordEnd = i + size
			state = normal
		}
	}
	addWord()

	return line.String()
}
