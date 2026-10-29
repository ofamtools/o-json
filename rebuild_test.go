package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
