package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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

// waitRebuilt drains the rebuild job until the shown result is current,
// waiting for each job as long as it runs (settle's 200 ms would drop a
// slow rebuild's command, and with it the job).
func waitRebuilt(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		view(a)
		if a.ed != nil && a.ed.key == mappingKey(a.Mapping()) && a.ed.doc == a.Doc {
			return
		}
		_, cmd := a.m.Update(tea.WindowSizeMsg{Width: a.m.Width, Height: a.m.Height})
		finishWithin(a, cmd, 10*time.Second)
	}
	t.Fatal("the restructured document was never rebuilt")
}

// rebuildWindowItems is the size of inRebuildWindow's document: 60k
// items, or 6k under the race detector, which runs a rebuild several
// times slower, so 6k still crosses rebuildOnLoop there.
var rebuildWindowItems = map[bool]int{false: 60_000, true: 6_000}[raceOn]

// offLoopWindows counts the inRebuildWindow calls whose rebuild ran off
// the loop and offLoopTries every call: TestMain fails a run in which
// every call skipped, so the off-loop path is never silently untested.
var offLoopWindows, offLoopTries atomic.Int32

func TestMain(m *testing.M) {
	// The suite was written for o-json's ungated value edits; the EDIT
	// tests (edit_test.go) and stories turn modes.edit-gate on.
	editGateDefault = false
	code := m.Run()
	if code == 0 && offLoopTries.Load() > 0 && offLoopWindows.Load() == 0 {
		fmt.Fprintf(os.Stderr, "FAIL: %d inRebuildWindow calls, none ran the rebuild off the loop (rebuildOnLoop %v)\n", offLoopTries.Load(), rebuildOnLoop)
		code = 1
	}
	os.Exit(code)
}

// inRebuildWindow opens a large document (rebuildWindowItems; 60k when
// the smaller one rebuilds on the loop) under rebuild.value-edit mode,
// focuses the data pane's first row on column col (1 is id, 2 name) and
// deletes the id key from the structure: its rebuild runs off the loop,
// and until it lands the table still shows the id column.
// pre are value edits made before (they make the edit's answer read
// the patches' orphans, which must not wait for the rebuild).
func inRebuildWindow(t *testing.T, mode string, col int, pre ...Patch) *App {
	t.Helper()
	if testing.Short() {
		t.Skip("a large document")
	}
	offLoopTries.Add(1)
	var a *App
	for _, n := range []int{rebuildWindowItems, 60_000} {
		a = largeIn(t, n, mode, pre)
		if a.rb.slow || n == 60_000 {
			t.Logf("%d items, rebuild off the loop %v", n, a.rb.slow)
			break
		}
	}
	if !a.rb.slow {
		t.Skip("the rebuild ran on the loop: nothing to race")
	}
	offLoopWindows.Add(1)
	// The root's data pane is the items table: # then id, name, ...
	onRoot(t, a)
	view(a)
	if err := a.data.list.Focus(); err != nil {
		t.Fatal(err)
	}
	if err := a.data.to(0); err != nil {
		t.Fatal(err)
	}
	a.data.list.SetColumn(col)
	if c := a.tab.Columns[0]; c.Name != "id" {
		t.Fatalf("first column %q", c.Name)
	}
	var idNode string
	for _, n := range a.rs.tree.Nodes() {
		if a.rs.meta[n.ID].from == "[].id" {
			idNode = n.ID
		}
	}
	if _, err := a.r.Execute(ochazuke.Request{Noun: "node", Verb: "delete", Target: idNode}); err != nil {
		t.Fatal(err)
	}
	if !a.staleShown() || a.tab == nil || a.tab.Columns[0].Name != "id" {
		t.Fatalf("not in the rebuild window (stale %v)", a.staleShown())
	}
	return a
}

// largeIn opens a document of n items under rebuild.value-edit mode
// with the pre patches made and the restructured document built.
func largeIn(t *testing.T, n int, mode string, pre []Patch) *App {
	t.Helper()
	path := filepath.Join(t.TempDir(), "big.json")
	if err := os.WriteFile(path, bigDocument(n), 0o644); err != nil {
		t.Fatal(err)
	}
	a := open(t, path, 120, 30)
	if err := a.r.Settings().Set(settingRebuildValueEdit, mode); err != nil {
		t.Fatal(err)
	}
	if len(pre) > 0 {
		if err := a.commitPatches("pre", pre); err != nil {
			t.Fatal(err)
		}
	}
	waitRebuilt(t, a)
	return a
}

// editCell is value.edit {text: "edited"} on the cell at the cursor.
func editCell(t *testing.T, a *App) {
	t.Helper()
	if _, err := a.r.Execute(ochazuke.Request{Noun: "value", Verb: "edit", Target: a.r.Focus, Args: ochazuke.Arguments{"text": "edited"}}); err != nil {
		t.Fatal(err)
	}
}

// onePatch checks one value edit at path, orphaned or applied ("edited"
// there in the current document), and that id stayed gone.
func onePatch(t *testing.T, a *App, path string, orphaned bool) {
	t.Helper()
	n := len(a.rs.patches)
	if n == 0 || a.rs.patches[n-1].Path != path {
		t.Fatalf("patches %+v, want the last at %s", a.rs.patches, path)
	}
	if s := a.patchList(); s[n-1].Orphaned != orphaned {
		t.Fatalf("patch %+v, want orphaned %v", s[n-1], orphaned)
	}
	d := a.restructuredDoc()
	if item, _ := Get(d.root, "[0]"); item == nil || item.Field("id") != nil {
		t.Fatal("id came back")
	}
	if orphaned {
		return
	}
	if got, err := Get(d.root, path); err != nil || Compact(got, 0) != `"edited"` {
		t.Fatalf("%s in the current document: %v %v", path, got, err)
	}
}

// rebuild.value-edit current (the default; F7 follow-up): a value edit
// made while the restructured document is rebuilt off the loop acts on
// the current document, never on the rows of the previous result: the
// rebuild is settled first, so the patch names a path of the document
// the mapping now makes (the cursor's column 1 is name there).
func TestValueEditDuringRebuildActsOnTheCurrentDocument(t *testing.T) {
	a := inRebuildWindow(t, rebuildEditCurrent, 1)
	editCell(t, a)
	if strings.HasSuffix(a.rs.patches[0].Path, ".id") {
		t.Fatalf("the patch names the previous result's id column: %s", a.rs.patches[0].Path)
	}
	onePatch(t, a, "[0].name", false)
	waitRebuilt(t, a)
	onePatch(t, a, "[0].name", false)
}

// rebuild.value-edit queue: the edit does not wait; it is recorded
// against the shown row's path and applied when the rebuild lands, the
// path resolved in the new document. A path the new document lacks (the
// id just deleted) is reported and kept as an orphaned patch.
func TestValueEditDuringRebuildQueues(t *testing.T) {
	for _, tc := range []struct {
		col      int
		path     string
		orphaned bool
	}{{2, "[0].name", false}, {1, "[0].id", true}} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel() // each case opens its own large document
			pre := Patch{Op: opSet, Path: "[1].score", Value: []byte("7")}
			a := inRebuildWindow(t, rebuildEditQueue, tc.col, pre)
			editCell(t, a)
			if len(a.rs.patches) != 1 || len(a.queued) != 1 || !a.staleShown() {
				t.Fatalf("not queued: patches %v queued %d stale %v", a.rs.patches, len(a.queued), a.staleShown())
			}
			if !strings.Contains(a.m.Info, "queued") {
				t.Fatalf("info %q", a.m.Info)
			}
			waitRebuilt(t, a)
			if len(a.queued) != 0 {
				t.Fatal("still queued after the rebuild landed")
			}
			onePatch(t, a, tc.path, tc.orphaned)
			if tc.orphaned && !strings.Contains(a.m.Status, "orphaned") {
				t.Fatalf("no error notice: status %q", a.m.Status)
			}
		})
	}
}

// rebuild.value-edit previous: the edit acts at once on the shown
// (previous) result's row, a patch by path the next rebuild resolves:
// applied where the path is still there, orphaned where it is gone.
func TestValueEditDuringRebuildActsOnThePreviousResult(t *testing.T) {
	for _, tc := range []struct {
		col      int
		path     string
		orphaned bool
	}{{2, "[0].name", false}, {1, "[0].id", true}} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel() // each case opens its own large document
			a := inRebuildWindow(t, rebuildEditPrevious, tc.col)
			editCell(t, a)
			if len(a.rs.patches) != 1 || a.rs.patches[0].Path != tc.path || len(a.queued) != 0 {
				t.Fatalf("patches %+v, want one at %s at once", a.rs.patches, tc.path)
			}
			waitRebuilt(t, a)
			onePatch(t, a, tc.path, tc.orphaned)
		})
	}
}
