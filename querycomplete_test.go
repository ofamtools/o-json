package main

import (
	"slices"
	"testing"

	"ofam.dev/ochazuke"
)

// suggest is what the "." trigger offers with the cursor at the end of
// text, over the sample's shape.
func suggest(t *testing.T, a *App, text string) []string {
	t.Helper()
	trig := []ochazuke.CompletionTrigger{a.pathTrigger()}
	r := []rune(text)
	tok, _, ok := ochazuke.FindCompletionTokenIn(r, len(r), trig)
	if !ok {
		return nil
	}
	var out []string
	for _, s := range pathSuggestions(a.Doc.Shape, a.r.SearchCase(), ochazuke.CompletionQuery{Query: tok.Query, Text: text, Cursor: len(r), Token: tok}) {
		out = append(out, s.Text)
	}
	return out
}

// completeOn turns query.complete-paths on (it is off by default).
func completeOn(t *testing.T, a *App) {
	t.Helper()
	if err := a.r.Settings().Set(settingCompletePaths, true); err != nil {
		t.Fatal(err)
	}
}

// "." completes the keys of the shape the query reads where it is typed:
// the document at the start, a stage's input after |, a path's keys after
// its ".", each item inside map(.
func TestQueryCompletesPathsAfterDot(t *testing.T) {
	a := openQuery(t, sample)
	completeOn(t, a)
	if trig := a.r.CompletionTriggers(jqFieldID); len(trig) != 1 || trig[0].Trigger != "." || trig[0].Boundaries != "|()," {
		t.Fatalf("triggers %+v", trig)
	}
	for _, tc := range []struct {
		text string
		want []string
	}{
		{".", []string{"name", "version", "items", "items[]", "meta"}},
		{".it", []string{"items", "items[]"}},
		{".items[] | .", []string{"id", "name", "owner", "tags", "tags[]", "archived", "note"}},
		{".items[] | .owner.", []string{"owner.name", "owner.email"}},
		{".items[]|.na", []string{"name"}},
		{".items | map(.ow", []string{"owner"}},
		{".meta, .co", nil}, // after a comma the term reads the document: no co key there
		{".meta | .co", []string{"count"}},
	} {
		if got := suggest(t, a, tc.text); !slices.Equal(got, tc.want) {
			t.Errorf("%q: %q, want %q", tc.text, got, tc.want)
		}
	}
	// query.complete-paths off: the snapshot is empty, nothing is offered.
	if err := a.r.Settings().Set(settingCompletePaths, false); err != nil {
		t.Fatal(err)
	}
	a.snapshotShape()
	if a.qc.shape.Load() != nil {
		t.Fatal("completion off still snapshots a shape")
	}
	if trig := a.r.CompletionTriggers(jqFieldID); len(trig) != 0 {
		t.Fatalf("completion off keeps triggers %+v", trig)
	}
}

// By default (query.complete-paths off) "." in the query line just types a
// dot: no trigger, nothing opens, not even an empty list.
func TestQueryDotJustTypes(t *testing.T) {
	a := openQuery(t, sample)
	press(t, a, "/")
	view(a)
	Type(a.m, ".items[] | .")
	c := a.r.Completer()
	if msg, ok := c.DebounceTick(); ok {
		_, cmd := a.m.Update(msg)
		finish(a, cmd)
	}
	finish(a, c.PendingCommand())
	if c.Open() || string(a.q.in.Text) != ".items[] | ." {
		t.Fatalf("open %v, text %q", c.Open(), string(a.q.in.Text))
	}
}

// Typed in the focused line, "." opens the suggestions; accepting one
// puts the key after the ".".
func TestQueryDotOpensSuggestions(t *testing.T) {
	a := openQuery(t, sample)
	completeOn(t, a)
	press(t, a, "/")
	view(a)
	Type(a.m, ".items[] | .ow")
	c := a.r.Completer()
	for range 5 {
		if msg, ok := c.DebounceTick(); ok {
			_, cmd := a.m.Update(msg)
			finish(a, cmd)
		}
		finish(a, c.PendingCommand())
		if len(c.Suggestions()) > 0 {
			break
		}
	}
	got := c.Suggestions()
	if !c.Open() || len(got) != 1 || got[0].Text != "owner" {
		t.Fatalf("suggestions %+v (open %v, err %v)", got, c.Open(), c.Err())
	}
}
