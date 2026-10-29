package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/unixsuperhero/ochazuke"
)

// F7: an edit on a large document never waits for the restructured
// document on the loop: the key and its frame stay within 50 ms (the
// rebuild runs off the loop and the last result stays on screen), and
// once the rebuild lands the data pane shows the edited document.
func TestLargeEditStaysFast(t *testing.T) {
	if raceOn || testing.Short() {
		t.Skip("timing: not under -race or -short")
	}
	path := filepath.Join(t.TempDir(), "big.json")
	if err := os.WriteFile(path, bigDocument(60_000), 0o644); err != nil {
		t.Fatal(err)
	}
	a := open(t, path, 120, 30)
	waitRebuilt(t, a)
	for _, n := range a.rs.tree.Nodes() {
		if a.rs.meta[n.ID].from == "[].name" {
			if err := a.rs.tree.Select(n.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := a.rs.tree.Focus(); err != nil {
		t.Fatal(err)
	}
	view(a)
	var worst time.Duration
	for _, key := range []string{"d", "d", "u", "ctrl+r", "u"} {
		start := time.Now()
		cmd := Press(a.m, key)
		view(a)
		worst = max(worst, time.Since(start))
		finish(a, cmd)
	}
	t.Logf("worst key-to-frame %v", worst)
	if worst > 50*time.Millisecond {
		t.Errorf("worst key-to-frame %v, want at most 50ms", worst)
	}
	waitRebuilt(t, a)
	if d := a.restructuredDoc(); d.err != nil || d.root == nil || d.key != mappingKey(a.Mapping()) {
		t.Fatalf("rebuilt doc %+v", d.err)
	}
}

// waitRebuilt drains the rebuild job until the shown result is current.
func waitRebuilt(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		view(a)
		if a.ed != nil && a.ed.key == mappingKey(a.Mapping()) && a.ed.doc == a.Doc {
			return
		}
		settle(a)
	}
	t.Fatal("the restructured document was never rebuilt")
}

// F7 follow-up: a value edit made while the restructured document is
// rebuilt off the loop acts on the current document, never on the rows
// of the previous result: the rebuild is settled first, so the patch
// names a path of the document the mapping now makes.
func TestValueEditDuringRebuildActsOnTheCurrentDocument(t *testing.T) {
	if testing.Short() {
		t.Skip("a 60k document")
	}
	path := filepath.Join(t.TempDir(), "big.json")
	if err := os.WriteFile(path, bigDocument(60_000), 0o644); err != nil {
		t.Fatal(err)
	}
	a := open(t, path, 120, 30)
	waitRebuilt(t, a)
	if !a.rb.slow {
		t.Skip("the rebuild ran on the loop: nothing to race")
	}
	// The root's data pane is the items table: # then id, name, ...
	onRoot(t, a)
	view(a)
	if err := a.data.list.Focus(); err != nil {
		t.Fatal(err)
	}
	if err := a.data.to(0); err != nil {
		t.Fatal(err)
	}
	a.data.list.SetColumn(1)
	if c := a.tab.Columns[0]; c.Name != "id" {
		t.Fatalf("first column %q", c.Name)
	}
	// A structure edit drops id: its rebuild runs off the loop, and
	// until it lands the table still shows the id column.
	var idNode string
	for _, n := range a.rs.tree.Nodes() {
		if a.rs.meta[n.ID].from == "[].id" {
			idNode = n.ID
		}
	}
	if _, err := a.r.Execute(ochazuke.Request{Noun: "node", Verb: "delete", Target: idNode}); err != nil {
		t.Fatal(err)
	}
	if a.ed.key == mappingKey(a.Mapping()) || a.tab == nil || a.tab.Columns[0].Name != "id" {
		t.Fatalf("not in the rebuild window (stale %v)", a.ed.key != mappingKey(a.Mapping()))
	}
	// The value edit on the cell the cursor is on (column 1).
	if _, err := a.r.Execute(ochazuke.Request{Noun: "value", Verb: "edit", Target: a.r.Focus, Args: ochazuke.Arguments{"text": "edited"}}); err != nil {
		t.Fatal(err)
	}
	if len(a.rs.patches) != 1 {
		t.Fatalf("patches %v", a.rs.patches)
	}
	p := a.rs.patches[0]
	if strings.HasSuffix(p.Path, ".id") {
		t.Fatalf("the patch names the previous result's id column: %s", p.Path)
	}
	for _, s := range a.patchList() {
		if s.Orphaned {
			t.Fatalf("orphaned patch %+v", s)
		}
	}
	waitRebuilt(t, a)
	d := a.restructuredDoc()
	got, err := Get(d.root, p.Path)
	if err != nil || Compact(got, 0) != `"edited"` {
		t.Fatalf("%s in the current document: %v %v", p.Path, got, err)
	}
	if item, _ := Get(d.root, "[0]"); item == nil || item.Field("id") != nil {
		t.Fatal("id came back")
	}
}
