package text

import (
	"strings"
	"unicode"
)

// Lines splits s into lines, mirroring [bufio.ScanLines]: a lone "\r"
// before a "\n" is dropped and a trailing newline does not yield a final
// empty line. Unlike a bufio.Scanner it has no fixed token limit, so a
// long line is returned intact rather than silently dropped.
func Lines(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{""}
	}
	lines := strings.Split(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	return lines
}

// AlignToLeft trims leading whitespace from every line of s, joining
// the result with "\n" and a trailing newline.
func AlignToLeft(s string) string {
	return joinWith(Lines(s), func(line string) string {
		return strings.TrimLeftFunc(line, unicode.IsSpace)
	})
}

// AlignToRight trims trailing whitespace from every line of s.
func AlignToRight(s string) string {
	return joinWith(Lines(s), func(line string) string {
		return strings.TrimRightFunc(line, unicode.IsSpace)
	})
}

// AlignCenter centers each line of s within width. If width is 0 or
// less, the longest line's width is used. Lines are right-padded so
// every output line has identical visual width.
//
// Example:
//
//	text.AlignCenter("Hello\nWorld", 10) // "   Hello  \n   World  \n"
func AlignCenter(s string, width int) string {
	lines := Lines(s)
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
		if w := len([]rune(lines[i])); w > width {
			width = w
		}
	}
	var sb strings.Builder
	sb.Grow((width + 1) * len(lines))
	for _, line := range lines {
		gap := width - len([]rune(line))
		left := gap / 2
		right := gap - left
		sb.WriteString(strings.Repeat(" ", left))
		sb.WriteString(line)
		sb.WriteString(strings.Repeat(" ", right))
		sb.WriteString("\n")
	}
	return sb.String()
}

// TrimAdjacentBlankLines collapses runs of blank lines into a single
// blank line and removes leading and trailing blank lines. Paragraph
// separations are preserved.
func TrimAdjacentBlankLines(s string) string {
	var sb strings.Builder
	prevBlank := true
	seenContent := false
	for _, line := range Lines(s) {
		if strings.TrimSpace(line) == "" {
			prevBlank = true
			continue
		}
		if prevBlank && seenContent {
			sb.WriteString("\n")
		}
		sb.WriteString(line)
		sb.WriteString("\n")
		prevBlank = false
		seenContent = true
	}
	return sb.String()
}

// DeleteTopLines removes n lines from the start of s. If n is
// non-positive, s is returned unchanged. If s has at most n lines, the
// empty string is returned.
func DeleteTopLines(s string, n int) string {
	if n <= 0 || strings.TrimSpace(s) == "" {
		return s
	}
	lines := Lines(s)
	if len(lines) <= n {
		return ""
	}
	return strings.Join(lines[n:], "\n")
}

// DeleteBottomLines removes n lines from the end of s. Edge cases match
// [DeleteTopLines].
func DeleteBottomLines(s string, n int) string {
	if n <= 0 || strings.TrimSpace(s) == "" {
		return s
	}
	lines := Lines(s)
	if len(lines) <= n {
		return ""
	}
	return strings.Join(lines[:len(lines)-n], "\n")
}

func joinWith(lines []string, fn func(string) string) string {
	var sb strings.Builder
	for _, line := range lines {
		sb.WriteString(fn(line))
		sb.WriteString("\n")
	}
	return sb.String()
}
