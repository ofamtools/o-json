package main

import (
	"strings"
	"testing"
)

// ctrl+w o keeps the mode's pane alone (SHAPE: structure, TABLE: data);
// again, or ctrl+w =, restores; ctrl+w h / l stop at the edge.
func TestWindowOnlyAndEdges(t *testing.T) {
	a := open(t, "testdata/sample.json", 110, 30)
	press(t, a, "ctrl+w", "h")
	if !a.structureFocused() {
		t.Fatalf("ctrl+w h at the left edge moved to %q", a.r.Focus)
	}
	press(t, a, "ctrl+w", "o")
	if a.r.PaneOnly(pageID) == "" || !strings.Contains(view(a), "Structure") || strings.Contains(view(a), "Object .") {
		t.Fatalf("SHAPE ctrl+w o: only %q", a.r.PaneOnly(pageID))
	}
	press(t, a, "ctrl+w", "o")
	if a.r.PaneOnly(pageID) != "" {
		t.Fatalf("ctrl+w o again: only %q", a.r.PaneOnly(pageID))
	}
	press(t, a, "ctrl+w", "l", "ctrl+w", "l")
	if !a.inData(a.r.Focus) {
		t.Fatalf("ctrl+w l twice: focus %q", a.r.Focus)
	}
	press(t, a, "ctrl+w", "o")
	if a.r.PaneOnly(pageID) == "" || strings.Contains(view(a), "Structure") {
		t.Fatalf("TABLE ctrl+w o: only %q", a.r.PaneOnly(pageID))
	}
	press(t, a, "ctrl+w", "=")
	if a.r.PaneOnly(pageID) != "" {
		t.Fatalf("ctrl+w =: only %q", a.r.PaneOnly(pageID))
	}
	press(t, a, "ctrl+w", "o", "ctrl+a", "o")
	view(a)
	if a.r.PaneOnly(pageID) != "" || !a.structureFocused() {
		t.Fatalf("focus to the hidden pane: only %q focus %q", a.r.PaneOnly(pageID), a.r.Focus)
	}
}

// On the split page ctrl+w o keeps the focused side; ctrl+w = restores.
func TestSplitOnlySide(t *testing.T) {
	a := open(t, "testdata/sample.json", 110, 30)
	press(t, a, "ctrl+w", "v")
	press(t, a, "ctrl+w", "o")
	if a.r.PaneOnly(rsPageID) == "" || !strings.Contains(view(a), "Original") || strings.Contains(view(a), "Restructured") {
		t.Fatalf("split ctrl+w o: only %q", a.r.PaneOnly(rsPageID))
	}
	press(t, a, "ctrl+w", "=")
	if a.r.PaneOnly(rsPageID) != "" || !strings.Contains(view(a), "Restructured") {
		t.Fatalf("split ctrl+w =: only %q", a.r.PaneOnly(rsPageID))
	}
}

// SPLIT: d p on the original pushes the shape, d o on the restructured
// side pulls the original's, and the other direction refuses.
func TestSplitPushPull(t *testing.T) {
	a := open(t, "testdata/sample.json", 110, 30)
	press(t, a, "ctrl+w", "v")
	cleared(t, a)
	press(t, a, "j", "d", "p")
	if got := len(a.Mapping().Map); got != 1 {
		t.Fatalf("d p: %d entries", got)
	}
	press(t, a, "j", "ctrl+w", "l", "d", "o")
	if got := len(a.Mapping().Map); got != 2 {
		t.Fatalf("d o: %d entries", got)
	}
	Press(a.m, "d")
	Press(a.m, "p")
	if !strings.Contains(a.m.Status, "read-only") {
		t.Fatalf("d p on the restructured side: %q", a.m.Status)
	}
}
