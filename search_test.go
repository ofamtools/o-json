package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// / searches keys and values; tab cycles the type; enter shows the
// Results view, n / N walk it, enter jumps the structure, esc clears.
func TestSearchLineResultsView(t *testing.T) {
	a := openQuery(t, "testdata/sample.json")
	if err := a.r.Settings().Set(settingSearchType, "substring"); err != nil {
		t.Fatal(err)
	}
	press(t, a, "/")
	if a.r.BaseMode() != "search_·_substr" {
		t.Fatalf("/ mode %q", a.r.BaseMode())
	}
	Type(a.m, "red")
	view(a)
	if len(a.sr.hits) != 2 || a.overlay != overlayResults || !strings.Contains(view(a), ".items[3].tags[0] │ \"red\"") {
		t.Fatalf("hits %d overlay %q", len(a.sr.hits), a.overlay)
	}
	press(t, a, "tab")
	if a.r.BaseMode() != "search_·_fuzzy" || len(a.sr.hits) < 2 {
		t.Fatalf("tab: mode %q hits %d", a.r.BaseMode(), len(a.sr.hits))
	}
	press(t, a, "shift+tab", "enter")
	if !a.inData(a.r.Focus) || a.r.BaseMode() != "search" {
		t.Fatalf("enter: focus %q mode %q", a.r.Focus, a.r.BaseMode())
	}
	press(t, a, "n")
	if a.data.Selected() != 1 {
		t.Fatalf("n: row %d", a.data.Selected())
	}
	press(t, a, "n")
	if a.data.Selected() != 0 {
		t.Fatalf("n wraps: row %d", a.data.Selected())
	}
	press(t, a, "N", "enter")
	if s := a.selectedShape(); s == nil || !strings.HasPrefix(s.Path, ".items[].tags") {
		t.Fatalf("enter jumped to %v", s)
	}
	if a.shown != a.sr.view {
		t.Fatal("the Results view did not stay")
	}
	press(t, a, "D")
	if a.overlay != "" {
		t.Fatalf("D off the results: overlay %q", a.overlay)
	}
	press(t, a, "shift+tab")
	if a.overlay != overlayResults {
		t.Fatalf("shift+tab back to the results: overlay %q", a.overlay)
	}
	press(t, a, "esc")
	if a.overlay != "" || a.queryText() != "" || a.shown == a.sr.view {
		t.Fatalf("esc: overlay %q text %q", a.overlay, a.queryText())
	}
}

// The jq type shows its result as a table view; esc in the line keeps it,
// esc on the pane clears it. A bad regex says why on the line.
func TestSearchLineJQAndRegexError(t *testing.T) {
	a := openQuery(t, "testdata/sample.json")
	if err := a.r.Settings().Set(settingSearchType, "substring"); err != nil {
		t.Fatal(err)
	}
	press(t, a, "/", "shift+tab", "shift+tab")
	Type(a.m, "(")
	if a.sr.err == nil || !strings.Contains(view(a), "regex") {
		t.Fatalf("regex error %v", a.sr.err)
	}
	press(t, a, "backspace", "tab")
	if !a.searchJQ() {
		t.Fatalf("type %q", a.sr.typ)
	}
	query(t, a, ".items")
	press(t, a, "enter")
	settle(a)
	view(a)
	if !a.queryShowing() || a.r.BaseMode() != "jq" {
		t.Fatalf("jq result: showing %v mode %q", a.queryShowing(), a.r.BaseMode())
	}
	press(t, a, "esc")
	if a.queryShowing() || a.queryText() != "" {
		t.Fatalf("esc on the pane kept %q", a.queryText())
	}
}

// A document larger than searchLoopNodes is searched off the loop: the
// key returns at once with a job, and its answer fills the Results view.
func TestSearchLargeDocumentRunsOffTheLoop(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"rows":[`)
	for i := 0; i < searchLoopNodes; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"n":%d}`, i)
	}
	b.WriteString(`]}`)
	path := filepath.Join(t.TempDir(), "big.json")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	a := open(t, path, 100, 30)
	press(t, a, "/")
	if err := a.setQuery("1234"); err != nil {
		t.Fatal(err)
	}
	a.refreshSearch()
	if !a.sr.running || len(a.sr.hits) != 0 {
		t.Fatalf("large search: running %v hits %d", a.sr.running, len(a.sr.hits))
	}
	job := a.searchLaunch()
	if job == nil {
		t.Fatal("no off-loop job")
	}
	_, next := a.m.Update(job())
	finish(a, next)
	if a.sr.running || len(a.sr.hits) == 0 || a.overlay != overlayResults {
		t.Fatalf("landed: running %v hits %d overlay %q", a.sr.running, len(a.sr.hits), a.overlay)
	}
}
