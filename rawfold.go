package main

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// jsonBlock is one multi-line JSON object or array closure in formatted text.
type jsonBlock struct {
	openLine  int
	closeLine int
	depth     int
	openChar  byte // '{' or '['
}

// findJSONBlocks scans text for balanced multi-line { ... } and [ ... ] blocks,
// ignoring braces and brackets inside JSON string literals.
func findJSONBlocks(text string) []jsonBlock {
	type frame struct {
		line int
		ch   byte
	}
	var stack []frame
	var out []jsonBlock
	line := 0
	inStr := false
	esc := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == '\n' {
			line++
			esc = false
			continue
		}
		if inStr {
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{', '[':
			stack = append(stack, frame{line: line, ch: c})
		case '}', ']':
			if len(stack) == 0 {
				continue
			}
			top := stack[len(stack)-1]
			if (top.ch == '{' && c == '}') || (top.ch == '[' && c == ']') {
				stack = stack[:len(stack)-1]
				if line > top.line {
					out = append(out, jsonBlock{
						openLine:  top.line,
						closeLine: line,
						depth:     len(stack),
						openChar:  top.ch,
					})
				}
			}
		}
	}
	return out
}

// enclosingBlock returns the innermost multi-line closure covering line.
func enclosingBlock(blocks []jsonBlock, line int) (jsonBlock, bool) {
	bestIdx := -1
	for i, b := range blocks {
		if line < b.openLine || line > b.closeLine {
			continue
		}
		if bestIdx < 0 || b.depth > blocks[bestIdx].depth ||
			(b.depth == blocks[bestIdx].depth && (b.closeLine-b.openLine) < (blocks[bestIdx].closeLine-blocks[bestIdx].openLine)) {
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		return jsonBlock{}, false
	}
	return blocks[bestIdx], true
}

// cursorLine returns the 0-based line number of rune offset cursor in text.
func cursorLine(text string, cursor int) int {
	line, idx := 0, 0
	for _, r := range text {
		if idx >= cursor {
			break
		}
		if r == '\n' {
			line++
		}
		idx++
	}
	return line
}

// foldRawView collapses every folded multi-line JSON block in text into a
// single summary line and maps cursorRune onto the folded display string.
func foldRawView(text string, cursorRune int, folds map[int]bool) (string, int) {
	if len(folds) == 0 {
		return text, cursorRune
	}
	blocks := findJSONBlocks(text)
	byOpen := map[int]jsonBlock{}
	for _, b := range blocks {
		if folds[b.openLine] {
			if prev, ok := byOpen[b.openLine]; !ok || b.closeLine > prev.closeLine {
				byOpen[b.openLine] = b
			}
		}
	}
	if len(byOpen) == 0 {
		return text, cursorRune
	}

	lines := strings.Split(text, "\n")
	curLine := cursorLine(text, cursorRune)
	// Column (in runes) on curLine.
	lineStartRune := 0
	for i := 0; i < curLine && i < len(lines); i++ {
		lineStartRune += utf8.RuneCountInString(lines[i]) + 1
	}
	curCol := max(0, cursorRune-lineStartRune)

	var out []string
	mappedCursor := -1
	runeOff := 0
	for i := 0; i < len(lines); i++ {
		if b, ok := byOpen[i]; ok && b.closeLine < len(lines) {
			closeChar := "}"
			if b.openChar == '[' {
				closeChar = "]"
			}
			tail := ""
			if strings.HasSuffix(strings.TrimSpace(lines[b.closeLine]), ",") {
				tail = ","
			}
			summary := fmt.Sprintf("%s … %d lines %s%s", strings.TrimRight(lines[i], " \t"), b.closeLine-b.openLine, closeChar, tail)
			if curLine >= b.openLine && curLine <= b.closeLine && mappedCursor < 0 {
				mappedCursor = runeOff + min(curCol, utf8.RuneCountInString(summary))
			}
			out = append(out, summary)
			runeOff += utf8.RuneCountInString(summary) + 1
			i = b.closeLine
			continue
		}
		if i == curLine && mappedCursor < 0 {
			mappedCursor = runeOff + min(curCol, utf8.RuneCountInString(lines[i]))
		}
		out = append(out, lines[i])
		runeOff += utf8.RuneCountInString(lines[i]) + 1
	}
	joined := strings.Join(out, "\n")
	if mappedCursor < 0 {
		mappedCursor = utf8.RuneCountInString(joined)
	}
	return joined, mappedCursor
}
