package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/unixsuperhero/ochazuke"
)

// openQuery opens a document with the query running at once (no pause).
func openQuery(t *testing.T, path string) *App {
	t.Helper()
	a := open(t, path, 120, 30)
	a.q.run.Debounce = 0
	// These tests are the jq type of the search line (search.go).
	if err := a.r.Settings().Set(settingSearchType, "jq"); err != nil {
		t.Fatal(err)
	}
	a.sr.typ = "jq"
	return a
}

// query sets the query and lets its run answer, as the model would.
func query(t *testing.T, a *App, q string) {
	t.Helper()
	if err := a.setQuery(q); err != nil {
		t.Fatal(err)
	}
	settle(a)
	view(a)
}

// The data pane shows the query's results as the query changes; an empty
// query is the document as the tree's selection shows it.
func TestQueryLiveResults(t *testing.T) {
	a := openQuery(t, sample)
	before := a.shown
	query(t, a, ".items[] | .name")
	if !a.queryShowing() || a.q.running {
		t.Fatalf("not showing results: %+v", a.queryState())
	}
	if got := len(a.shown.Values()); got != 4 || a.shown.Values()[0].Value != "alpha" {
		t.Fatalf("results %d %v", got, a.shown.Values())
	}
	frame := view(a)
	if !strings.Contains(frame, "delta") || !strings.Contains(frame, "jq .items[] | .name") {
		t.Fatalf("frame lacks the results:\n%s", frame)
	}
	query(t, a, ".items[0].owner")
	if a.tab == nil || a.tab.Items || len(a.tab.Rows) != 1 || a.tab.Rows[0].Path() != ".items[0].owner" {
		t.Fatalf("one object shows as one row: %v", a.tab)
	}
	query(t, a, "")
	if a.queryShowing() || a.shown != before {
		t.Fatalf("an empty query shows the document: %v", a.shown)
	}
}

// An error shows with its position and the last good results stay; no
// query crashes the viewer.
func TestQueryErrorsInline(t *testing.T) {
	a := openQuery(t, sample)
	query(t, a, ".items[] | .id")
	good := a.shown
	for _, q := range []string{".items[] |", "nosuch(.)", ".name[]", "{", ".items[] | .id □", "error(\"boom\")"} {
		query(t, a, q)
		st := a.queryState()
		if st["error"] == nil {
			t.Fatalf("%q: no error: %v", q, st)
		}
		if a.shown != good {
			t.Fatalf("%q: the last good results went", q)
		}
	}
	query(t, a, ".items[] |")
	if pos := a.q.last.Pos; pos != 10 {
		t.Errorf("parse error position %d, want 10", pos)
	}
	query(t, a, ".id | nosuch(.)")
	if pos := a.q.last.Pos; pos != 6 {
		t.Errorf("unknown function position %d, want 6", pos)
	}
}

// A path query shows the document's own nodes: key order and exact paths
// kept. A query that builds values shows gojq's output, keys sorted, and
// says so.
func TestQueryPathKeepsOrder(t *testing.T) {
	doc := `{"zeta":1,"people":[{"name":"ann","email":"a@x","age":30},{"name":"bob","age":4,"email":"b@x"}]}`
	root, err := Build([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	out := queryDoc(context.Background(), root, ".people[] | select(.age > 10)")
	if out.Err != nil || !out.Paths || len(out.Values) != 1 {
		t.Fatalf("path run: %+v", out)
	}
	if v := out.Values[0]; v != root.Field("people").Children[0] || Compact(v, 0) != `{"name":"ann","email":"a@x","age":30}` {
		t.Fatalf("not the document's node, or order lost: %s", Compact(v, 0))
	}
	out = queryDoc(context.Background(), root, "{zeta, people: [.people[].name]}")
	if out.Err != nil || out.Paths || !out.Sorted || Compact(out.Values[0], 0) != `{"people":["ann","bob"],"zeta":1}` {
		t.Fatalf("value run: %+v %s", out, Compact(out.Values[0], 0))
	}
	out = queryDoc(context.Background(), root, "")
	if out.Values[0] != root {
		t.Fatal("the empty query is the document")
	}
}

// A newer run cancels the older, and the older's late answer is dropped.
func TestQueryRunSupersededIsDropped(t *testing.T) {
	a := openQuery(t, sample)
	if err := a.setQuery(".version"); err != nil {
		t.Fatal(err)
	}
	_, older := a.m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	query(t, a, ".name")
	finish(a, older)
	if got := a.shown.Values(); len(got) != 1 || got[0].Value != "o-json sample" {
		t.Fatalf("the older answer replaced the newer: %v", got)
	}
}

// The query runs on the edited document the viewer shows.
func TestQueryRunsOnTheEditedDocument(t *testing.T) {
	a := openQuery(t, sample)
	selectPath(t, a, ".version")
	press(t, a, "r")
	Type(a.m, "release")
	press(t, a, "enter")
	query(t, a, ".release")
	if got := a.shown.Values(); len(got) != 1 || got[0].Value != "2" {
		t.Fatalf(".release on the edited document: %v %v", got, a.queryState())
	}
}

// o-json query prints what jq prints (jq 1.8, where it is on PATH): the
// same values, and in path queries the same key order.
func TestQueryCLIAgreesWithJQ(t *testing.T) {
	jq, err := exec.LookPath("jq")
	if err != nil {
		t.Skip("jq is not on PATH")
	}
	for _, q := range []string{
		".", ".items[] | .name", ".items[] | select(.tags | length > 0) | .owner",
		"[.items[].id]", ".meta", "{n: .name, ids: [.items[].id]}", ".items | map(.id * 2)",
		".items[] | .owner.email // \"none\"", "keys", ".items[2:]", ".items[1].owner, .name",
	} {
		var b strings.Builder
		if err := Main([]string{"query", "--compact", sample, q}, nil, &b, &b); err != nil {
			t.Fatalf("%q: %v %s", q, err, b.String())
		}
		cmd := exec.Command(jq, "-c", q, sample)
		var want bytes.Buffer
		cmd.Stdout = &want
		if err := cmd.Run(); err != nil {
			t.Fatalf("jq %q: %v", q, err)
		}
		got, exp := strings.Split(strings.TrimSpace(b.String()), "\n"), strings.Split(strings.TrimSpace(want.String()), "\n")
		if len(got) != len(exp) {
			t.Fatalf("%q: %d results, jq %d\n%s\n%s", q, len(got), len(exp), b.String(), want.String())
		}
		path := queryDoc(context.Background(), mustRoot(t), q).Paths
		for i := range got {
			if path && got[i] != exp[i] {
				t.Errorf("%q: %s, jq %s (a path query keeps the key order)", q, got[i], exp[i])
			} else if !sameJSON(t, got[i], exp[i]) {
				t.Errorf("%q: %s, jq %s", q, got[i], exp[i])
			}
		}
	}
	// gojq iterates an object's keys sorted, where jq keeps the
	// document's order: the same values, and a path run says so.
	for _, q := range []string{"..|numbers", ".meta[]"} {
		var b strings.Builder
		if err := Main([]string{"query", "--compact", sample, q}, nil, &b, &b); err != nil {
			t.Fatal(err)
		}
		want, _ := exec.Command(jq, "-c", q, sample).Output()
		got, exp := strings.Fields(b.String()), strings.Fields(string(want))
		slices.Sort(got)
		slices.Sort(exp)
		if !slices.Equal(got, exp) {
			t.Errorf("%q: %v, jq %v", q, got, exp)
		}
		if out := queryDoc(context.Background(), mustRoot(t), q); !out.Paths || !out.Reordered {
			t.Errorf("%q: not flagged as out of document order: %+v", q, out)
		}
	}
	var b strings.Builder
	if err := Main([]string{"query", sample, ".items[] |"}, nil, &b, &b); err == nil || !strings.Contains(err.Error(), "column 11") {
		t.Errorf("an error names its column: %v", err)
	}
}

func mustRoot(t *testing.T) *Node {
	t.Helper()
	doc, err := ReadDoc(sample, nil)
	if err != nil {
		t.Fatal(err)
	}
	return doc.Root
}

var _ = ochazuke.Done

// / focuses the query line, typing at its end; esc goes back to the
// structure pane with the query kept. The line is no stop of tab's ring.
func TestQuerySlashFocusesAndEscLeaves(t *testing.T) {
	a := openQuery(t, sample)
	from := a.r.Focus
	press(t, a, "/")
	// insert while a slot is selected (ochazuke's SelectText; views.go).
	if a.r.Focus != jqFieldID || a.q.in.Mode != "insert" && a.q.in.Mode != a.lineMode() {
		t.Fatalf("/ focus %q mode %q", a.r.Focus, a.q.in.Mode)
	}
	finish(a, Type(a.m, ".name"))
	if got := a.queryText(); got != ".name" {
		t.Fatalf("typed %q", got)
	}
	if !a.queryShowing() || !strings.Contains(view(a), "Value jq .name") {
		t.Fatal("the results do not show while typing")
	}
	press(t, a, "esc")
	if a.r.Focus != from || a.queryText() != ".name" {
		t.Fatalf("esc: focus %q (want %q), query %q", a.r.Focus, from, a.queryText())
	}
	for range 4 {
		press(t, a, "tab")
		if a.r.Focus == jqFieldID {
			t.Fatal("tab reached the query line")
		}
	}
	// enter leaves too; / again types at the end.
	press(t, a, "/")
	Type(a.m, " | length")
	press(t, a, "enter")
	if a.r.Focus == jqFieldID || a.queryText() != ".name | length" {
		t.Fatalf("enter: focus %q query %q", a.r.Focus, a.queryText())
	}
}

// The query line draws field references as chips (the select style,
// reverse video): dimmed while the line is not focused, bright while it
// is; structure characters and names are plain.
func TestQueryChipsRender(t *testing.T) {
	q := `.items[] | select(.id > 1) | {n: .name, "k": ."odd key"[0]}`
	var chips []string
	for _, tok := range jqTokens([]rune(q)) {
		if tok.Kind == tokPath {
			chips = append(chips, q[tok.Start:tok.End])
		}
	}
	if want := []string{".items[]", ".id", ".name", `."odd key"[0]`}; !slices.Equal(chips, want) {
		t.Fatalf("chips %q, want %q", chips, want)
	}
	a := openQuery(t, sample)
	query(t, a, ".items[] | .name")
	if l := line(view(a), "jq .items[]"); !strings.Contains(l, "jq .items[] | .name") {
		t.Fatalf("query line %q", l)
	}
	st := a.m.Styles()
	lineOf := func() string {
		b, _ := a.queryLines(st, 120)
		return b.Text
	}
	dim, bright := st.Tone(ochazuke.ToneChip).Render(".items[]"), st.Tone(ochazuke.ToneChipFocused).Render(".items[]")
	if dim == bright || dim == ".items[]" {
		t.Fatalf("the chip tones draw alike: %q %q", dim, bright)
	}
	if raw := lineOf(); !strings.Contains(raw, dim) || strings.Contains(raw, bright) {
		t.Fatalf("an unfocused chip is not on the dim chip background:\n%q", raw)
	}
	if raw := lineOf(); strings.Contains(raw, st.Tone(ochazuke.ToneChip).Render("|")) {
		t.Fatal("a pipe is drawn as a chip")
	}
	// Focused, the chip under the cursor is bright (query.chip-focus
	// cursor); the others stay dim. With line, every chip is bright.
	press(t, a, "/")
	if raw := lineOf(); strings.Contains(raw, bright) || !strings.Contains(raw, st.Tone(ochazuke.ToneChipFocused).Render(".name")) {
		t.Fatalf("the chip under the cursor is not the bright one:\n%q", raw)
	}
	if err := a.r.Settings().Set(settingChipFocus, "line"); err != nil {
		t.Fatal(err)
	}
	if raw := lineOf(); !strings.Contains(raw, bright) {
		t.Fatalf("query.chip-focus line: a focused line's chips are not bright:\n%q", raw)
	}
	_ = a.r.Settings().Set(settingChipFocus, "cursor")
	// An error points at its column on the line below.
	press(t, a, "esc")
	query(t, a, ".items[] |")
	if l := line(view(a), "^ "); !strings.Contains(l, "unexpected EOF") {
		t.Fatalf("no caret line: %q\n%s", l, view(a))
	}
}

// In the query field a visual selection and [ (or { or () wraps it.
func TestQueryWrapSelection(t *testing.T) {
	a := openQuery(t, sample)
	press(t, a, "/")
	Type(a.m, ".name, .version")
	press(t, a, "ctrl+v", "0")
	finish(a, Press(a.m, "["))
	if got := a.queryText(); got != "[.name, .version]" {
		t.Fatalf("wrapped %q", got)
	}
	if a.q.in.Mode != a.lineMode() || a.q.in.Cursor != len(a.q.in.Text) {
		t.Fatalf("after the wrap: mode %q cursor %d", a.q.in.Mode, a.q.in.Cursor)
	}
	view(a)
	if got := a.shown.Values(); len(got) != 1 || Compact(got[0], 0) != `["o-json sample",2]` {
		t.Fatalf("results %v", a.queryState())
	}
}

// writeDoc writes a document to a temporary file.
func writeDoc(t *testing.T, doc string) string {
	t.Helper()
	p := t.TempDir() + "/doc.json"
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const peopleDoc = `{"a":1,"b":"two","people":[{"name":"ann","email":"a@x"},{"name":"bob","email":"b@x"}]}`

// m switches the structure pane to query mode, where jq's characters
// build the query from the highlighted field; the edit keys stand aside
// and m or esc brings them back.
func TestQueryModeKeysBuildAQuery(t *testing.T) {
	a := openQuery(t, writeDoc(t, peopleDoc))
	press(t, a, "m")
	if !a.queryModeOn() || !strings.Contains(view(a), "query · unchanged") {
		t.Fatalf("m: mode %q\n%s", a.r.BaseMode(), view(a))
	}
	// query extends normal (bindings.json "modes"): the viewer's and the
	// app's normal rows answer there without tables of their own.
	if a.r.ModeParent(queryMode) != "normal" {
		t.Fatalf("query extends %q", a.r.ModeParent(queryMode))
	}
	if got := a.answer(t, "q"); got != "app.quit" {
		t.Fatalf("q in query mode answers %q", got)
	}
	selectPath(t, a, ".people[]")
	press(t, a, "|")
	selectPath(t, a, ".people[].email")
	if !strings.Contains(view(a), ".email · query · unchanged") {
		t.Fatalf("the title does not show the path relative to the query:\n%s", line(view(a), "Structure"))
	}
	finish(a, Press(a.m, "."))
	if got := a.queryText(); got != ".people[] | .email" {
		t.Fatalf("query %q", got)
	}
	view(a)
	if got := a.shown.Values(); len(got) != 2 || got[1].Value != "b@x" {
		t.Fatalf("results %v", a.queryState())
	}
	// d d is an edit key: in query mode it does not delete.
	nodes := len(a.rs.tree.Nodes())
	press(t, a, "esc")
	if a.queryModeOn() {
		t.Fatal("esc stays in query mode")
	}
	press(t, a, "m")
	press(t, a, "d", "d")
	if len(a.rs.tree.Nodes()) != nodes {
		t.Fatal("d d edited the structure in query mode")
	}

	// [ opens an array; . fills it, a comma between; ] steps out.
	query(t, a, "")
	selectPath(t, a, ".a")
	press(t, a, "[", ".")
	selectPath(t, a, ".b")
	press(t, a, ".", "]")
	if got := a.queryText(); got != "[.a, .b]" || a.q.in.Cursor != len(a.q.in.Text) {
		t.Fatalf("query %q cursor %d", got, a.q.in.Cursor)
	}
	// { takes key: path; ] on an array iterates; backspace takes back a
	// whole field; a anchors after a field's reference.
	query(t, a, "")
	press(t, a, "{")
	selectPath(t, a, ".a")
	press(t, a, ".")
	if got := a.queryText(); got != "{a: .a}" {
		t.Fatalf("object %q", got)
	}
	query(t, a, "")
	selectPath(t, a, ".people")
	press(t, a, "]", "|")
	selectPath(t, a, ".people[].name")
	press(t, a, ".")
	if got := a.queryText(); got != ".people[] | .name" {
		t.Fatalf("iterate %q", got)
	}
	press(t, a, "backspace")
	if got := a.queryText(); got != ".people[] | " {
		t.Fatalf("erase %q", got)
	}
	selectPath(t, a, ".people[]")
	press(t, a, "a")
	if a.q.in.Cursor != len(".people[]") {
		t.Fatalf("anchor at %d", a.q.in.Cursor)
	}
	press(t, a, "u")
	if got := a.queryText(); got != ".people[] | .name" {
		t.Fatalf("undo %q", got)
	}
}

// The path context: relative to what the query reads at the cursor,
// chained onto a path ending there, map's argument reads each item.
func TestQueryContext(t *testing.T) {
	for _, c := range []struct{ text, path, want string }{
		{"", ".people[].email", ".people[].email"},
		{".people[] | ", ".people[].email", ".email"},
		{".people", ".people[].email", "[].email"},
		{".people | map(", ".people[].name", ".name"},
		{"[.a", ".b", ", .b"},
		{"{x: .a, ", ".b", "b: .b"},
		{".people[] | select(", ".people[].name", ".name"},
		{".a | ", ".b", ".b"},
	} {
		got := fieldTerm(jqContextAt([]rune(c.text), len([]rune(c.text))), c.path)
		if got != c.want {
			t.Errorf("%q + %s: %q, want %q", c.text, c.path, got, c.want)
		}
	}
}

// Query mode works on the original structure too, and / from it comes
// back to it.
func TestQueryModeOnTheOriginalAndBack(t *testing.T) {
	a := openQuery(t, writeDoc(t, peopleDoc))
	press(t, a, "T", "m")
	selectPath(t, a, ".people[].name")
	press(t, a, ".")
	if got := a.queryText(); got != ".people[].name" {
		t.Fatalf("query %q", got)
	}
	press(t, a, "/")
	Type(a.m, " | length")
	press(t, a, "esc")
	if !a.queryModeOn() || a.queryText() != ".people[].name | length" {
		t.Fatalf("back: mode %q query %q", a.r.BaseMode(), a.queryText())
	}
}

// ctrl+f picks a builtin; one of arity 2 lays out 2 slots, the first
// selected so typing fills it, tab the next; the query runs once filled.
// A slot holds a nested call.
func TestQueryFunctionSlots(t *testing.T) {
	a := openQuery(t, writeDoc(t, peopleDoc))
	press(t, a, "/")
	Type(a.m, ".b | ")
	press(t, a, "ctrl+f")
	c := a.r.Chooser(jqFuncChooser)
	if c == nil {
		t.Fatal("ctrl+f opened no picker")
	}
	Type(a.m, "sub(f; f)")
	if row, ok := c.Highlighted(); !ok || row.ID != "sub/2" {
		t.Fatalf("highlighted %v", row)
	}
	press(t, a, "enter")
	if got := a.queryText(); got != ".b | sub(□; □)" {
		t.Fatalf("slots %q", got)
	}
	if n := strings.Count(a.queryText(), "□"); n != 2 {
		t.Fatalf("%d slots", n)
	}
	// insert while a slot is selected (ochazuke's SelectText; views.go).
	if a.r.Focus != jqFieldID || a.q.in.Mode != "insert" && a.q.in.Mode != a.lineMode() {
		t.Fatalf("back in the line: focus %q mode %q", a.r.Focus, a.q.in.Mode)
	}
	Type(a.m, `"t"`)
	press(t, a, "ctrl+n")
	// The second slot holds a nested call: tostring is arity 0.
	if _, err := a.r.Execute(ochazuke.Request{Noun: "jq", Verb: "function", Args: ochazuke.Arguments{"name": "tostring"}}); err != nil {
		t.Fatal(err)
	}
	if got := a.queryText(); got != `.b | sub("t"; tostring)` {
		t.Fatalf("filled %q", got)
	}
	settle(a)
	view(a)
	if st := a.queryState(); st["error"] != nil || st["results"] != 1 {
		t.Fatalf("the filled call does not run: %v", st)
	}
	// The slot chips: an empty slot draws as a chip, and the query does
	// not run while one is left.
	if out := queryDoc(context.Background(), mustRoot(t), "sub(□; □)"); out.Err == nil || out.Pos != 4 {
		t.Fatalf("a slot left: %+v", out)
	}
	if toks := jqTokens([]rune("sub(□; □)")); toks[2].Kind != tokSlot {
		t.Fatalf("slot token %+v", toks)
	}
}

// answer is the verb the first row of keys answers with at the focus.
func (a *App) answer(t *testing.T, keys string) string {
	t.Helper()
	for _, b := range a.r.ListBindings(a.r.Focus) {
		if b.Keys == keys && !b.Unbind {
			return a.r.CanonicalKey(b.Request.Key())
		}
	}
	return ""
}

// F12: results in gojq's sorted key order say so in the results title, a
// path query's (the document's order) does not.
func TestQueryResultsTitleMarksSortedOrder(t *testing.T) {
	a := openQuery(t, sample)
	query(t, a, "{version, name}")
	if frame := view(a); !strings.Contains(frame, "jq {version, name}  keys sorted") {
		t.Fatalf("sorted results unmarked\n%s", frame)
	}
	query(t, a, ".items[] | .name")
	if frame := view(a); strings.Contains(line(frame, "jq .items[]"), "keys sorted") {
		t.Fatalf("a path query marked sorted\n%s", frame)
	}
}

// o-json's own searches read ochazuke's search.case: smart (the default)
// ignores case unless the query holds a capital, ignore always does,
// sensitive never. The path search and the structure filter (matches),
// the jq function picker and the query line's path completion agree.
func TestSearchesUseSmartCase(t *testing.T) {
	a := openQuery(t, writeDoc(t, `{"Name":1,"nickname":2,"items":[{"NAME":3}],"IN":4}`))
	completeOn(t, a)
	matched := func(q string) []string {
		a.Query = q
		defer func() { a.Query = "" }()
		var out []string
		a.Doc.Shape.Walk(func(s *Shape, _ int) {
			if s.Key != "" && a.matches(s) {
				out = append(out, s.Path)
			}
		})
		return out
	}
	picked := func(q string) []string {
		press(t, a, "/")
		press(t, a, "ctrl+f")
		c := a.r.Chooser(jqFuncChooser)
		if c == nil {
			t.Fatal("ctrl+f opened no picker")
		}
		Type(a.m, q)
		var out []string
		for _, row := range c.Matches() {
			out = append(out, row.ID)
		}
		press(t, a, "esc")
		press(t, a, "esc")
		return out
	}
	for _, tc := range []struct {
		mode, q  string
		paths    []string
		complete []string
	}{
		{ochazuke.SearchCaseSmart, "name", []string{".Name", ".nickname", ".items[].NAME"}, []string{"Name", "nickname"}},
		{ochazuke.SearchCaseSmart, "Name", []string{".Name"}, []string{"Name"}},
		{ochazuke.SearchCaseIgnore, "Name", []string{".Name", ".nickname", ".items[].NAME"}, []string{"Name", "nickname"}},
		{ochazuke.SearchCaseSensitive, "name", []string{".nickname"}, []string{"nickname"}},
	} {
		if err := a.r.Settings().Set(ochazuke.SearchCaseSetting, tc.mode); err != nil {
			t.Fatal(err)
		}
		a.snapshotShape()
		if got := matched(tc.q); !slices.Equal(got, tc.paths) {
			t.Errorf("%s %q: paths %q, want %q", tc.mode, tc.q, got, tc.paths)
		}
		// Completion matches from the key's start: .n completes nickname
		// and, folded, Name.
		prefix := tc.q[:1]
		if got := suggest(t, a, "."+prefix); !slices.Equal(got, tc.complete) {
			t.Errorf("%s .%s: completes %q, want %q", tc.mode, prefix, got, tc.complete)
		}
	}
	if err := a.r.Settings().Set(ochazuke.SearchCaseSetting, ochazuke.SearchCaseSmart); err != nil {
		t.Fatal(err)
	}
	if got := picked("IN"); !slices.Contains(got, "IN/1") || slices.ContainsFunc(got, func(id string) bool { return strings.HasPrefix(id, "index") }) {
		t.Errorf("smart IN: %q, want IN and INDEX only", got)
	}
	if got := picked("in"); !slices.Contains(got, "IN/1") || !slices.ContainsFunc(got, func(id string) bool { return strings.HasPrefix(id, "index") }) {
		t.Errorf("smart in: %q, want in, index and IN", got)
	}
}

// The caret line's row is kept while the query line is typed in: an
// error that comes and goes never moves the structure tree (F12).
func TestQueryErrorKeepsTheTree(t *testing.T) {
	a := openQuery(t, sample)
	press(t, a, "/")
	row := func() int {
		t.Helper()
		for i, l := range strings.Split(view(a), "\n") {
			if strings.Contains(l, ". (root)") {
				return i
			}
		}
		t.Fatalf("no root row\n%s", view(a))
		return -1
	}
	query(t, a, ".items")
	good := row()
	query(t, a, ".items[ | x")
	if !strings.Contains(view(a), "^ ") {
		t.Fatalf("no caret line\n%s", view(a))
	}
	if bad := row(); bad != good {
		t.Fatalf("the error moved the tree from row %d to %d\n%s", good, bad, view(a))
	}
}
