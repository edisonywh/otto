// Package vim provides pure text-manipulation helpers for vim-style editing.
// All functions are stateless and operate on plain strings; no bubbletea dependency.
package vim

import (
	"strings"
	"unicode"
)

// Position is a cursor position within a text buffer (both 0-indexed).
type Position struct {
	Line int
	Col  int
}

// Lines splits content on newlines.
func Lines(content string) []string {
	return strings.Split(content, "\n")
}

func isWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// runesOf returns all runes as a flat slice with '\n' between logical lines.
func runesOf(lines []string) []rune {
	total := len(lines) - 1 // newlines
	for _, l := range lines {
		total += len([]rune(l))
	}
	buf := make([]rune, 0, total)
	for i, l := range lines {
		buf = append(buf, []rune(l)...)
		if i < len(lines)-1 {
			buf = append(buf, '\n')
		}
	}
	return buf
}

// ToOffset converts a Position to a flat rune offset.
func ToOffset(lines []string, pos Position) int {
	off := 0
	for i, l := range lines {
		if i == pos.Line {
			lr := []rune(l)
			col := pos.Col
			if col > len(lr) {
				col = len(lr)
			}
			return off + col
		}
		off += len([]rune(l)) + 1
	}
	return off
}

// FromOffset converts a flat rune offset back to a Position.
func FromOffset(lines []string, off int) Position {
	for i, l := range lines {
		lr := len([]rune(l))
		if off <= lr {
			return Position{Line: i, Col: off}
		}
		off -= lr + 1
	}
	if len(lines) == 0 {
		return Position{}
	}
	last := len(lines) - 1
	return Position{Line: last, Col: len([]rune(lines[last]))}
}

// WordForward moves to the start of the next word (vim 'w').
func WordForward(lines []string, pos Position) Position {
	runes := runesOf(lines)
	off := ToOffset(lines, pos)
	n := len(runes)
	if off >= n {
		return pos
	}
	cur := runes[off]
	if isWordChar(cur) {
		for off < n && isWordChar(runes[off]) {
			off++
		}
	} else if !unicode.IsSpace(cur) {
		for off < n && !unicode.IsSpace(runes[off]) && !isWordChar(runes[off]) {
			off++
		}
	}
	for off < n && unicode.IsSpace(runes[off]) {
		off++
	}
	if off >= n {
		off = n - 1
	}
	return FromOffset(lines, off)
}

// WordBack moves to the start of the current or previous word (vim 'b').
func WordBack(lines []string, pos Position) Position {
	runes := runesOf(lines)
	off := ToOffset(lines, pos)
	if off <= 0 {
		return Position{}
	}
	off--
	for off > 0 && unicode.IsSpace(runes[off]) {
		off--
	}
	if off == 0 {
		return FromOffset(lines, 0)
	}
	if isWordChar(runes[off]) {
		for off > 0 && isWordChar(runes[off-1]) {
			off--
		}
	} else {
		for off > 0 && !unicode.IsSpace(runes[off-1]) && !isWordChar(runes[off-1]) {
			off--
		}
	}
	return FromOffset(lines, off)
}

// WordEnd moves to the end of the current or next word (vim 'e').
func WordEnd(lines []string, pos Position) Position {
	runes := runesOf(lines)
	off := ToOffset(lines, pos)
	n := len(runes)
	if off >= n-1 {
		return pos
	}
	off++
	for off < n && unicode.IsSpace(runes[off]) {
		off++
	}
	if off >= n {
		return FromOffset(lines, n-1)
	}
	if isWordChar(runes[off]) {
		for off < n-1 && isWordChar(runes[off+1]) {
			off++
		}
	} else {
		for off < n-1 && !unicode.IsSpace(runes[off+1]) && !isWordChar(runes[off+1]) {
			off++
		}
	}
	return FromOffset(lines, off)
}

// WORDForward moves to the start of the next WORD (vim 'W').
// A WORD is a maximal sequence of non-whitespace characters.
func WORDForward(lines []string, pos Position) Position {
	runes := runesOf(lines)
	off := ToOffset(lines, pos)
	n := len(runes)
	for off < n && !unicode.IsSpace(runes[off]) {
		off++
	}
	for off < n && unicode.IsSpace(runes[off]) {
		off++
	}
	if off >= n {
		off = n - 1
	}
	return FromOffset(lines, off)
}

// WORDBack moves to the start of the current or previous WORD (vim 'B').
func WORDBack(lines []string, pos Position) Position {
	runes := runesOf(lines)
	off := ToOffset(lines, pos)
	if off <= 0 {
		return Position{}
	}
	off--
	for off > 0 && unicode.IsSpace(runes[off]) {
		off--
	}
	for off > 0 && !unicode.IsSpace(runes[off-1]) {
		off--
	}
	return FromOffset(lines, off)
}

// WORDEnd moves to the end of the current or next WORD (vim 'E').
func WORDEnd(lines []string, pos Position) Position {
	runes := runesOf(lines)
	off := ToOffset(lines, pos)
	n := len(runes)
	if off >= n-1 {
		return pos
	}
	off++
	for off < n && unicode.IsSpace(runes[off]) {
		off++
	}
	for off < n-1 && !unicode.IsSpace(runes[off+1]) {
		off++
	}
	return FromOffset(lines, off)
}

// DeleteRange removes runes in [from, to) from content.
func DeleteRange(content string, from, to int) string {
	runes := []rune(content)
	if from < 0 {
		from = 0
	}
	if to > len(runes) {
		to = len(runes)
	}
	if from >= to {
		return content
	}
	result := make([]rune, 0, len(runes)-(to-from))
	result = append(result, runes[:from]...)
	result = append(result, runes[to:]...)
	return string(result)
}

// ExtractRange returns runes[from:to] from content.
func ExtractRange(content string, from, to int) string {
	runes := []rune(content)
	if from < 0 {
		from = 0
	}
	if to > len(runes) {
		to = len(runes)
	}
	if from >= to {
		return ""
	}
	return string(runes[from:to])
}

// LineEnd returns the rune offset of the last character on the given line.
func LineEnd(lines []string, line int) int {
	if line < 0 || line >= len(lines) {
		return 0
	}
	lineStart := ToOffset(lines, Position{Line: line, Col: 0})
	return lineStart + len([]rune(lines[line]))
}
