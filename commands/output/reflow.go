package output

import "strings"

// Reflows the text `in` with `prefix` before each line, and each line having
// maximum `width` characters (not including `prefix`). Two newlines indicate a
// new paragraph
func Reflow(in, prefix string, width int) string {
	line := strings.Builder{}
	wordStart := -1
	wordEnd := 0
	prefixLen := len(prefix)
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
		wordLen := wordEnd - wordStart        // excluding current space
		wordFits := column+wordLen+1 < maxCol // including separating space
		if wordStart >= 0 {
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
			wordEnd = i + 1
			state = normal
		}
	}
	addWord()

	return line.String()
}
