package main

import (
	"bytes"
	"context"
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
	if a.fields == nil || a.fields.Path() != ".items[0].owner" {
		t.Fatalf("one object shows as fields: %v", a.fields)
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
	if a.r.Focus != jqFieldID || a.q.in.Mode != "insert" {
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
	raw := a.m.View().Content
	chip := func(focused bool) string {
		if focused {
			return "\x1b[7m.items[]\x1b[m"
		}
		return "\x1b[7;2m.items[]\x1b[m"
	}
	if !strings.Contains(raw, chip(false)) || strings.Contains(raw, chip(true)) {
		t.Fatalf("an unfocused chip is not dimmed reverse video:\n%q", line(raw, "items[]"))
	}
	if strings.Contains(raw, "\x1b[7;2m|") || strings.Contains(raw, "\x1b[7m|") {
		t.Fatal("a pipe is drawn as a chip")
	}
	press(t, a, "/")
	if raw := a.m.View().Content; !strings.Contains(raw, chip(true)) {
		t.Fatalf("a focused chip is not bright:\n%q", line(raw, "items[]"))
	}
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
	if a.q.in.Mode != "insert" || a.q.in.Cursor != len(a.q.in.Text) {
		t.Fatalf("after the wrap: mode %q cursor %d", a.q.in.Mode, a.q.in.Cursor)
	}
	view(a)
	if got := a.shown.Values(); len(got) != 1 || Compact(got[0], 0) != `["o-json sample",2]` {
		t.Fatalf("results %v", a.queryState())
	}
}
