package main

import (
	"errors"
	"slices"
	"strings"

	"ofam.dev/ochazuke"
)

// Query mode on the structure pane. m switches the pane between its edit
// keys (normal mode: the restructure) and query mode, where jq's own
// characters build the query from the highlighted field: . appends its
// path, | its path and a pipe, ] its path iterated ([] after it), [ and {
// open a collection to fill (and ] or } steps out of it), , a comma, a
// the anchor: what is chosen next attaches after the highlighted field's
// reference in the query (appended when it has none). backspace takes
// back the token before the anchor, u and ctrl+r undo and redo the
// query's edits. The JQ mode owns the
// pane while it lasts: the edit keys (> > d d r t p ...) do not answer,
// and m or esc goes back to them.
//
// Every key edits the query line's text at its cursor (the anchor); the
// text stays the only query state. Where the cursor sits decides how a
// field is written (jqContext): relative to the input of the term there
// (.people[] | → .email), chained onto a path that ends at the cursor
// (.people → .people[].email), after a comma when a term is already
// there, and as key: path inside an object.

const queryMode = "jq"

// jqContext is what the query says at a cursor.
type jqContext struct {
	input string // the path the term at the cursor reads ("" when the query built it)
	// chain is the path that ends at the cursor (a pure path stage
	// right before it), "" for none: a field under it is chained on.
	chain string
	prev  rune // the last structure character before the cursor, 0 for none or a value
	open  rune // the innermost open bracket: '[', '{' or '(' (0 at the top)
	key   bool // in an object, before the entry's colon
}

type jqFrame struct {
	open  rune
	input string // "" unknown
	stage string // the current stage's path ("" none)
	pure  bool   // the stage is that one path and nothing else
	colon bool
}

// iterating are the functions whose argument reads each item (map(f) is
// [.[] | f]).
var iterating = map[string]bool{"map": true, "any": true, "all": true}

// jqContextAt reads the query up to the cursor.
func jqContextAt(text []rune, cursor int) jqContext {
	stack := []jqFrame{{input: ".", pure: true}}
	top := func() *jqFrame { return &stack[len(stack)-1] }
	var prev rune
	lastIdent := ""
	end := -1 // where the current stage's path ends
	for _, t := range jqTokens(text[:cursor]) {
		s := string(text[t.Start:t.End])
		f := top()
		switch t.Kind {
		case tokPath, tokSlot:
			if t.Kind == tokPath && f.pure && f.stage == "" {
				f.stage, end = normPath(s), t.End
			} else {
				f.pure = false
			}
			prev, lastIdent = 0, ""
		case tokPunct:
			c := []rune(s)[0]
			switch c {
			case '|':
				switch {
				case f.input == "":
				case f.pure && f.stage != "":
					f.input = joinJQ(f.input, f.stage)
				case !f.pure:
					f.input = ""
				}
				f.stage, f.pure = "", true
			case ',', ';', ':':
				f.stage, f.pure = "", true
				if f.open == '{' {
					f.colon = c == ':'
				}
			case '.':
				if f.pure && f.stage == "" {
					f.stage, end = ".", t.End
				} else {
					f.pure = false
				}
			case '(', '[', '{':
				// What is inside reads the stage's input; map(f)'s
				// argument reads each item.
				in := f.input
				if c == '(' && iterating[lastIdent] && in != "" {
					in = joinJQ(in, "[]")
				}
				f.pure = false
				stack = append(stack, jqFrame{open: c, input: in, pure: true})
			case ')', ']', '}':
				if len(stack) > 1 {
					stack = stack[:len(stack)-1]
				}
				top().pure = false
			}
			prev, lastIdent = c, ""
			if c == ')' || c == ']' || c == '}' || c == '.' {
				prev = 0 // a value ends here
			}
		default:
			if strings.TrimSpace(s) == "" {
				continue
			}
			f.pure, prev = false, 0
			lastIdent = strings.TrimSpace(s)
			if strings.HasPrefix(s, "$") || !isIdentStart([]rune(lastIdent)[0]) {
				lastIdent = ""
			}
		}
	}
	f := top()
	ctx := jqContext{input: f.input, prev: prev, open: f.open, key: f.open == '{' && !f.colon}
	if f.pure && f.stage != "" && f.stage != "." && end == cursor && f.input != "" {
		ctx.chain = joinJQ(f.input, f.stage)
	}
	if prev == 0 && strings.TrimSpace(string(text[:cursor])) == "" {
		prev = '^'
		ctx.prev = prev
	}
	return ctx
}

// sepNeeded reports a term already right before the cursor, so a new
// one goes after a comma.
func (c jqContext) sepNeeded() bool { return c.prev == 0 }

// normPath writes a field reference as a structure path writes it:
// ."k" as ["k"] (or .k), .[ as [, no ? suffixes.
func normPath(s string) string {
	s = strings.ReplaceAll(s, "?", "")
	if s == "." {
		return s
	}
	var b strings.Builder
	r := []rune(s)
	for i := 0; i < len(r); {
		switch {
		case r[i] == '.' && i+1 < len(r) && r[i+1] == '"':
			e := skipString(r, i+1)
			key := string(r[i+2 : max(i+2, e-1)])
			b.WriteString(keyStep(key))
			i = e
		case r[i] == '.' && i+1 < len(r) && r[i+1] == '[':
			i++
		case r[i] == '[':
			e := closeBracket(r, i)
			if e < 0 {
				e = len(r) - 1
			}
			inner := string(r[i+1 : e])
			if strings.HasPrefix(inner, `"`) && strings.HasSuffix(inner, `"`) && len(inner) >= 2 {
				b.WriteString(keyStep(inner[1 : len(inner)-1]))
			} else {
				b.WriteString("[" + inner + "]")
			}
			i = e + 1
		default:
			b.WriteRune(r[i])
			i++
		}
	}
	return b.String()
}

// joinJQ appends a path to a base path ("." is the input itself).
func joinJQ(base, rel string) string {
	switch {
	case base == ".":
		if strings.HasPrefix(rel, "[") {
			return "." + rel
		}
		return rel
	case rel == ".":
		return base
	case strings.HasPrefix(rel, ".["):
		return base + rel[1:]
	}
	return base + rel
}

// relJQ is abs below base: the steps after base ("" for base itself);
// ok is false when abs is not under base.
func relJQ(base, abs string) (string, bool) {
	if base == "." || base == "" {
		if abs == "." {
			return "", true
		}
		return strings.TrimPrefix(abs, "."), true
	}
	if abs == base {
		return "", true
	}
	if strings.HasPrefix(abs, base) && strings.ContainsRune(".[", rune(abs[len(base)])) {
		return abs[len(base):], true
	}
	return "", false
}

// asTerm writes steps as a term of their own: .email, .[0], . for none.
func asTerm(steps string) string {
	switch {
	case steps == "":
		return "."
	case strings.HasPrefix(steps, "["):
		return "." + steps
	case strings.HasPrefix(steps, "."):
		return steps
	}
	return "." + steps
}

// fieldTerm is how a field at path p is written at the cursor: the text to
// insert and whether it chains onto the path before the cursor.
func fieldTerm(c jqContext, p string) string {
	if c.chain != "" {
		if rest, ok := relJQ(c.chain, p); ok {
			return rest
		}
		if _, ok := relJQ(p, c.chain); ok {
			return "" // the path before the anchor already reads it
		}
	}
	term := p
	if c.input != "" {
		if rest, ok := relJQ(c.input, p); ok {
			term = asTerm(rest)
		}
	}
	if c.key {
		term = fieldKey(p) + ": " + term
	}
	if c.sepNeeded() {
		term = ", " + term
	}
	return term
}

// fieldKey is the object key a field gets in { }: its own name ("item"
// for an array's item).
func fieldKey(p string) string {
	s := strings.TrimSuffix(p, "[]")
	if s == "" || s == "." {
		return "item"
	}
	i := strings.LastIndexAny(s, ".[")
	key := s[i+1:]
	if s[i] == '[' {
		key = strings.Trim(strings.TrimSuffix(key, "]"), `"`)
		if key == "" || strings.Trim(key, "0123456789") == "" {
			return "item"
		}
	}
	if !identRe.MatchString(key) {
		return quote(key)
	}
	return key
}

// insertAt is where an insertion goes: the cursor, or an empty slot the
// cursor is on or just after (the slot is replaced).
func insertAt(in *ochazuke.Input) (int, int) {
	c := in.Cursor
	switch {
	case c < len(in.Text) && in.Text[c] == jqSlot:
		return c, c + 1
	case c > 0 && in.Text[c-1] == jqSlot:
		return c - 1, c
	}
	return c, c
}

// highlighted is the path of the structure pane's highlighted field, in
// the document the viewer shows.
func (a *App) highlighted() (string, error) {
	if a.editedShown() {
		n, ok := a.rs.tree.Selected()
		if !ok {
			return "", errors.New("no field is highlighted")
		}
		e := a.current()
		return nodePaths(e.nodes, e.meta)[n.ID], nil
	}
	s, ok := a.selectedOriginal()
	if !ok {
		return "", errors.New("no field is highlighted")
	}
	return s.Path, nil
}

// queryInsert puts text at the anchor (replacing an empty slot there),
// the cursor after it.
func (a *App) queryInsert(text string) error {
	in := a.q.in
	lo, hi := insertAt(in)
	if err := in.Replace(lo, hi, text); err != nil {
		return err
	}
	// The input is not focused (normal mode clamps the cursor onto the
	// last character): the anchor goes after the insertion.
	in.Cursor = lo + len([]rune(text))
	return nil
}

// queryContext is the context at the anchor.
func (a *App) queryContext() jqContext {
	lo, _ := insertAt(a.q.in)
	return jqContextAt(a.q.in.Text, lo)
}

// queryField writes the highlighted field (at path p, or the highlighted
// one) at the anchor.
func (a *App) queryField(p string) error {
	if p == "" {
		var err error
		if p, err = a.highlighted(); err != nil {
			return err
		}
	}
	term := fieldTerm(a.queryContext(), p)
	if term == "" {
		return nil // already there: the path ends at the anchor
	}
	return a.queryInsert(term)
}

// nextCloser reports the closing bracket the cursor sits before (spaces
// skipped), and where it ends.
func nextCloser(text []rune, cursor int) (rune, int) {
	for i := cursor; i < len(text); i++ {
		switch text[i] {
		case ' ':
			continue
		case ']', '}', ')':
			return text[i], i + 1
		}
		return 0, 0
	}
	return 0, 0
}

// queryAnchor moves the anchor after the highlighted field's reference in
// the query; with none, the field is appended at the end.
func (a *App) queryAnchor() error {
	p, err := a.highlighted()
	if err != nil {
		return err
	}
	in := a.q.in
	for _, t := range slices.Backward(jqTokens(in.Text)) {
		if t.Kind != tokPath {
			continue
		}
		c := jqContextAt(in.Text, t.Start)
		ref := normPath(string(in.Text[t.Start:t.End]))
		abs := ref
		if c.chain != "" {
			abs = joinJQ(c.chain, ref)
		} else if c.input != "" {
			abs = joinJQ(c.input, ref)
		}
		if abs == p {
			in.Cursor = t.End
			return nil
		}
	}
	in.Cursor = len(in.Text)
	return a.queryField(p)
}

// queryErase takes back the token before the anchor: a field reference
// whole, a pipe with its spaces, a bracket pair, else one character.
func (a *App) queryErase() error {
	in := a.q.in
	end := in.Cursor
	start := end
	for start > 0 && in.Text[start-1] == ' ' {
		start--
	}
	if start == 0 {
		if end == 0 {
			return errors.New("nothing before the anchor")
		}
		if err := in.Replace(0, end, ""); err != nil {
			return err
		}
		in.Cursor = 0
		return nil
	}
	from := start - 1
	for _, t := range jqTokens(in.Text) {
		if t.Start <= start-1 && start-1 < t.End && (t.Kind == tokPath || t.Kind == tokSlot) {
			from = t.Start
		}
	}
	to := end
	if c := in.Text[from]; c == '|' || c == ',' || c == ':' || c == ';' {
		for from > 0 && in.Text[from-1] == ' ' {
			from--
		}
	} else if pair := map[rune]rune{'[': ']', '{': '}', '(': ')'}[c]; pair != 0 && to < len(in.Text) && in.Text[to] == pair {
		to++
	}
	if err := in.Replace(from, to, ""); err != nil {
		return err
	}
	in.Cursor = from
	return nil
}

// registerQueryMode adds the query mode and its verbs.
func (a *App) registerQueryMode() error {
	r := a.r
	if err := r.DefineMode(queryMode, false); err != nil {
		return err
	}
	inStructure := func(focus string) bool {
		if r.Page != pageID {
			return false
		}
		anc := r.Ancestry(focus)
		return slices.Contains(anc, treeID) || slices.Contains(anc, rsTreeID)
	}
	r.DefineGuard("query-structure", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return a.queryOn() && a.Doc != nil && inStructure(ev.Focus)
	})
	r.DefineGuard("query-closable", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		c, _ := nextCloser(a.q.in.Text, a.q.in.Cursor)
		return a.queryOn() && a.Doc != nil && inStructure(ev.Focus) && c != 0
	})
	edit := func(fn func(c ochazuke.Ctx) error) ochazuke.AppHandler {
		return func(c ochazuke.Ctx) ochazuke.Result { return a.queryDone(fn(c)) }
	}
	r.Define("jq").
		Verb("mode", edit(func(c ochazuke.Ctx) error {
			to := ochazuke.StringArg(c.Args, "to", "")
			if to == "" {
				to = queryMode
				if r.BaseMode() == queryMode {
					to = "edit"
				}
			}
			if to == "edit" {
				return r.SetMode(a.backMode())
			}
			// JQ mode writes the query: the line's type is jq.
			a.sr.typ = "jq"
			return r.SetMode(queryMode)
		}), "Switch the structure pane between its edit keys and JQ mode, where jq's characters build the query from the highlighted field (m); to: edit or jq (query is an alias)").When("query-structure").
		Verb("path", edit(func(c ochazuke.Ctx) error {
			return a.queryField(ochazuke.StringArg(c.Args, "path", ""))
		}), "Write the highlighted field's path into the query at the anchor: relative to what the query reads there, chained onto a path that ends there, after a comma when a term is there, as key: path inside { } (.)").When("query-structure").
		Verb("pipe", edit(func(c ochazuke.Ctx) error {
			if err := a.queryField(""); err != nil {
				return err
			}
			return a.queryInsert(" | ")
		}), "Write the highlighted field's path and a pipe after it: what is chosen next reads its values (|)").When("query-structure").
		Verb("iterate", edit(func(c ochazuke.Ctx) error {
			p, err := a.highlighted()
			if err != nil {
				return err
			}
			if !strings.HasSuffix(p, "[]") {
				p = joinJQ(p, "[]")
			}
			return a.queryField(p)
		}), "Write the highlighted field's path iterated, [] after it: each item (])").When("query-structure").
		Verb("open", edit(func(c ochazuke.Ctx) error {
			open := ochazuke.StringArg(c.Args, "bracket", "[")
			cls := map[string]string{"[": "]", "{": "}", "(": ")"}[open]
			if cls == "" {
				return errors.New("bracket is [, { or (")
			}
			text := open + cls
			if a.queryContext().sepNeeded() {
				text = ", " + text
			}
			if err := a.queryInsert(text); err != nil {
				return err
			}
			a.q.in.Cursor--
			return nil
		}), "Open a collection in the query at the anchor, [ ] (an array) or { } (an object), and anchor inside it: the fields chosen next fill it ([ {)").When("query-structure").
		Verb("close", edit(func(c ochazuke.Ctx) error {
			_, after := nextCloser(a.q.in.Text, a.q.in.Cursor)
			a.q.in.Cursor = after
			return nil
		}), "Step the anchor out of the collection it is in (] } ))").When("query-closable").
		Verb("comma", edit(func(c ochazuke.Ctx) error { return a.queryInsert(", ") }), "Write a comma at the anchor: another output, or the next item or key (,)").When("query-structure").
		Verb("anchor", edit(func(c ochazuke.Ctx) error { return a.queryAnchor() }), "Anchor after the highlighted field's reference in the query (appended at the end when it has none): what is chosen next attaches there (a)").When("query-structure").
		Verb("erase", edit(func(c ochazuke.Ctx) error { return a.queryErase() }), "Take back the token before the anchor: a field reference, a pipe, an empty bracket pair, a character (backspace)").When("query-structure").
		Verb("undo", edit(func(c ochazuke.Ctx) error { return a.q.in.Undo(max(1, ochazuke.IntArg(c.Args, "steps", 1))) }), "Undo the query's last edit (u)").When("query-structure").
		Verb("redo", edit(func(c ochazuke.Ctx) error { return a.q.in.Redo(max(1, ochazuke.IntArg(c.Args, "steps", 1)), -1) }), "Redo the query's edit undone (ctrl+r)").When("query-structure")
	params := func(key string, p map[string]ochazuke.Parameter) {
		op := r.Operations[r.CanonicalKey(key)]
		op.Parameters = p
		r.Operations[r.CanonicalKey(key)] = op
	}
	params("jq.mode", map[string]ochazuke.Parameter{"to": {Type: "string", Enum: []string{"edit", "jq", "query"}, Description: "omit to switch; query is an alias of jq"}})
	params("jq.path", map[string]ochazuke.Parameter{"path": {Type: "string", Description: "a structure path; omit for the highlighted field"}})
	params("jq.open", map[string]ochazuke.Parameter{"bracket": {Type: "string", Enum: []string{"[", "{", "("}, Description: "the collection to open"}})
	setHistory(r, ochazuke.HistoryAction, "jq.mode", "jq.close", "jq.anchor")
	setHistory(r, ochazuke.HistoryChange, "jq.path", "jq.pipe", "jq.iterate", "jq.open", "jq.comma", "jq.erase", "jq.undo", "jq.redo")
	return nil
}

// queryModeOn reports the structure pane in query mode.
func (a *App) queryModeOn() bool { return a.r.BaseMode() == queryMode }

// queryModeTerm is how the highlighted field would be written at the
// anchor, for the structure pane's title in query mode ("" outside it).
// It leads the title, so a narrow pane cuts the fixed words, not the path.
func (a *App) queryModeTerm() string {
	if !a.queryModeOn() {
		return ""
	}
	p, err := a.highlighted()
	if err != nil {
		return ""
	}
	term := strings.TrimPrefix(fieldTerm(a.queryContext(), p), ", ")
	if term == "" {
		term = "(anchor)"
	}
	return term
}
