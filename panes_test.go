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
	if a.only != paneStructure {
		t.Fatalf("SHAPE ctrl+w o: only %q", a.only)
	}
	press(t, a, "ctrl+w", "o")
	if a.only != "" {
		t.Fatalf("ctrl+w o again: only %q", a.only)
	}
	press(t, a, "ctrl+w", "l", "ctrl+w", "l")
	if !a.inData(a.r.Focus) {
		t.Fatalf("ctrl+w l twice: focus %q", a.r.Focus)
	}
	press(t, a, "ctrl+w", "o")
	if a.only != paneData {
		t.Fatalf("TABLE ctrl+w o: only %q", a.only)
	}
	press(t, a, "ctrl+w", "=")
	if a.only != "" {
		t.Fatalf("ctrl+w =: only %q", a.only)
	}
	press(t, a, "ctrl+w", "o", "ctrl+a", "o")
	view(a)
	if a.only != "" || !a.structureFocused() {
		t.Fatalf("focus to the hidden pane: only %q focus %q", a.only, a.r.Focus)
	}
}

// On the split page ctrl+w o keeps the focused side; ctrl+w = restores.
func TestSplitOnlySide(t *testing.T) {
	a := open(t, "testdata/sample.json", 110, 30)
	press(t, a, "ctrl+w", "v", "ctrl+w", "o")
	if a.rsOnly != rsOrigID || !strings.Contains(view(a), "Original") || strings.Contains(view(a), "Restructured") {
		t.Fatalf("split ctrl+w o: only %q", a.rsOnly)
	}
	press(t, a, "ctrl+w", "=")
	if a.rsOnly != "" || !strings.Contains(view(a), "Restructured") {
		t.Fatalf("split ctrl+w =: only %q", a.rsOnly)
	}
}
