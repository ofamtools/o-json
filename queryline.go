package main

import (
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

// The query line: the query drawn as text with its field references as
// chips, as tag chips sit in an input. Structure characters (| . [ ] { }
// ( ) , ; :) and everything else (names, strings, numbers, operators)
// are plain; each field reference (.a, .a.b[], ."odd key", .[0]) is one
// chip, and an empty argument slot of a function (□) an empty chip.
//
// A chip is drawn in the select style (reverse video): dimmed while the
// line is not focused, bright while it is. ochazuke has no background
// tone yet (ToneStyle.Bg), so reverse video is the chip's background.
// Chips change no cells: the line maps rune for rune onto the input's
// text, so the cursor is the input's.

// jqTok is one token of a query.
type jqTok struct {
	Kind       jqKind
	Start, End int // runes [Start, End)
}

type jqKind int

const (
	tokOther jqKind = iota // names, strings, numbers, operators, spaces
	tokPunct               // a structure character: | . [ ] { } ( ) , ; :
	tokPath                // a field reference: a chip
	tokSlot                // an empty argument slot: an empty chip
)

// jqTokens splits a query into tokens; they cover it whole, in order.
func jqTokens(text []rune) []jqTok {
	var out []jqTok
	n := len(text)
	add := func(k jqKind, s, e int) {
		if l := len(out); l > 0 && k == tokOther && out[l-1].Kind == tokOther && out[l-1].End == s {
			out[l-1].End = e
			return
		}
		out = append(out, jqTok{Kind: k, Start: s, End: e})
	}
	for i := 0; i < n; {
		c := text[i]
		switch {
		case c == jqSlot:
			add(tokSlot, i, i+1)
			i++
		case c == '"':
			e := skipString(text, i)
			add(tokOther, i, e)
			i = e
		case c == '.' && i+1 < n && text[i+1] == '.':
			add(tokOther, i, i+2) // recursion
			i += 2
		case c == '.' && i+1 < n && (isIdentStart(text[i+1]) || text[i+1] == '"' || text[i+1] == '['):
			e := pathEnd(text, i)
			add(tokPath, i, e)
			i = e
		case strings.ContainsRune("|.[]{}(),;:", c):
			add(tokPunct, i, i+1)
			i++
		case c == '$' || isIdentStart(c):
			e := i + 1
			for e < n && (isIdentPart(text[e]) || text[e] == ':' && e+1 < n && text[e+1] == ':') {
				if text[e] == ':' {
					e++
				}
				e++
			}
			add(tokOther, i, e)
			i = e
		default:
			add(tokOther, i, i+1)
			i++
		}
	}
	return out
}

func isIdentStart(c rune) bool { return c == '_' || unicode.IsLetter(c) }
func isIdentPart(c rune) bool  { return c == '_' || unicode.IsLetter(c) || unicode.IsDigit(c) }

// skipString is the index after the string starting at i (a quote).
func skipString(text []rune, i int) int {
	for e := i + 1; e < len(text); e++ {
		switch text[e] {
		case '\\':
			e++
		case '"':
			return e + 1
		}
	}
	return len(text)
}

// pathEnd is the index after the field reference starting at i (a dot):
// .name, ."str", .[…] and the steps chained after them, with ? suffixes.
func pathEnd(text []rune, i int) int {
	n := len(text)
	e := i
	for e < n {
		switch {
		case text[e] == '.' && e+1 < n && isIdentStart(text[e+1]):
			e += 2
			for e < n && isIdentPart(text[e]) {
				e++
			}
		case text[e] == '.' && e+1 < n && text[e+1] == '"':
			e = skipString(text, e+1)
		case text[e] == '.' && e+1 < n && text[e+1] == '[':
			e++
		case text[e] == '[':
			end := closeBracket(text, e)
			if end < 0 {
				return e
			}
			e = end + 1
		case text[e] == '?':
			e++
		default:
			return e
		}
	}
	return e
}

// closeBracket is the index of the ] that closes the [ at i, -1 for none.
func closeBracket(text []rune, i int) int {
	depth := 0
	for e := i; e < len(text); e++ {
		switch text[e] {
		case '"':
			e = skipString(text, e) - 1
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return e
			}
		}
	}
	return -1
}

// queryLines is the query line, and under it the error's caret when the
// last run failed: the rows above the structure pane. The block carries
// the cursor while the query field is focused.
func (a *App) queryLines(s ochazuke.Styles, width int) (ochazuke.Block, int) {
	if !a.queryOn() || a.Doc == nil {
		return ochazuke.Text(""), 0
	}
	a.r.Placed(jqFieldID)
	in := a.q.in
	focused := a.r.Focus == jqFieldID
	prompt := "jq "
	pw := ansi.StringWidth(prompt)
	avail := max(1, width-pw)
	if len(in.Text) == 0 && !focused {
		hint := "query"
		if key := a.keyFor("jq.focus"); key != "" {
			hint = key + " to query"
		}
		return ochazuke.Text(s.Muted.Render(ansi.Truncate(prompt+hint, width, "…"))), 1
	}
	errPos := -1
	var errMsg string
	if l := a.q.last; l != nil && l.Err != nil && l.Query == string(in.Text) {
		errPos, errMsg = l.Pos, l.Err.Error()
		if errPos < 0 {
			errPos = len(in.Text)
		}
	}
	line, cx := chipLine(s, in, a.queryTokens(), focused, errPos, avail)
	block := ochazuke.Text(s.Muted.Render(prompt) + line)
	if focused {
		block.Cursor = &ochazuke.Pos{X: pw + cx}
	}
	if errMsg == "" {
		return block, 1
	}
	col := pw
	if start := viewStart(in, avail); errPos >= start {
		col += min(cellsOf(in.Text, start, errPos), avail-1)
	}
	caret := strings.Repeat(" ", col) + "^ " + errMsg
	return block.WithText(block.Text + "\n" + s.Error.Render(ansi.Truncate(caret, width, "…"))), 2
}

// queryTokens are the query's tokens, cached for its text.
func (a *App) queryTokens() []jqTok {
	text := string(a.q.in.Text)
	if a.q.toksFor != text || a.q.toks == nil {
		a.q.toks, a.q.toksFor = jqTokens(a.q.in.Text), text
	}
	return a.q.toks
}

// viewStart is the first rune the line shows: the text scrolls so the
// cursor stays in view.
func viewStart(in *ochazuke.Input, avail int) int {
	start := 0
	for cellsOf(in.Text, start, min(in.Cursor+1, len(in.Text)+1)) > avail && start < in.Cursor {
		start++
	}
	return start
}

// cellsOf is the width of the runes [from, to) (to may pass the end by
// one: the cursor cell after the text).
func cellsOf(text []rune, from, to int) int {
	w := 0
	for i := from; i < to; i++ {
		if i >= len(text) {
			w++
			continue
		}
		w += max(1, ansi.StringWidth(string(text[i])))
	}
	return w
}

// chipLine draws the query's text in avail cells with its chips, the
// visual selection and the error's rune, and returns the cursor's cell.
func chipLine(s ochazuke.Styles, in *ochazuke.Input, toks []jqTok, focused bool, errPos, avail int) (string, int) {
	text := in.Text
	chip := lipgloss.NewStyle().Reverse(true)
	if !focused {
		chip = chip.Faint(true)
	}
	selLo, selHi := -1, -1
	if focused && strings.HasPrefix(in.Mode, "visual") {
		selLo, selHi = min(in.Anchor, in.Cursor), max(in.Anchor, in.Cursor)+1
	}
	if lo, hi, ok := in.SelectedText(); ok && focused {
		selLo, selHi = lo, hi
	}
	kind := make([]jqKind, len(text))
	for _, t := range toks {
		for i := t.Start; i < t.End && i < len(kind); i++ {
			kind[i] = t.Kind
		}
	}
	start := viewStart(in, avail)
	var b strings.Builder
	used := 0
	// Runes of one look are drawn as one run.
	var run []rune
	look, runLook := 0, -1
	looks := []lipgloss.Style{lipgloss.NewStyle(), chip, lipgloss.NewStyle().Reverse(true).Underline(true), s.Error.Underline(true)}
	flush := func() {
		if len(run) > 0 {
			b.WriteString(looks[runLook].Render(string(run)))
		}
		run = run[:0]
	}
	for i := start; i < len(text); i++ {
		w := max(1, ansi.StringWidth(string(text[i])))
		if used+w > avail {
			break
		}
		used += w
		look = 0
		if kind[i] == tokPath || kind[i] == tokSlot {
			look = 1
		}
		if i >= selLo && i < selHi {
			look = 2
		}
		if i == errPos {
			look = 3
		}
		if look != runLook {
			flush()
			runLook = look
		}
		run = append(run, text[i])
	}
	flush()
	if errPos == len(text) && used < avail {
		b.WriteString(s.Error.Render("◂"))
	}
	return b.String(), cellsOf(text, start, in.Cursor)
}
