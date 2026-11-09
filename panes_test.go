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
	if !a.structureFocused(a.r.FocusID()) {
		t.Fatalf("ctrl+w h at the left edge moved to %q", a.r.FocusID())
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
	if !a.inData(a.r.FocusID()) {
		t.Fatalf("ctrl+w l twice: focus %q", a.r.FocusID())
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
	if a.r.PaneOnly(pageID) != "" || !a.structureFocused(a.r.FocusID()) {
		t.Fatalf("focus to the hidden pane: only %q focus %q", a.r.PaneOnly(pageID), a.r.FocusID())
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

// Under a modal the page is drawn in the interaction the modal opened
// over (PresentationAt): the pane the modal came from is the drawn focus
// and keeps its focused border, while the live focus is the modal's.
// Closing the modal gives the live focus back to that pane, in its mode.
func TestPageUnderModalDrawsItsOwnFocus(t *testing.T) {
	a := open(t, "testdata/sample.json", 110, 30)
	press(t, a, "ctrl+w", "l")
	data, mode := a.r.FocusID(), a.r.CurrentMode().Name
	if !a.inData(data) {
		t.Fatalf("ctrl+w l: focus %q", data)
	}
	press(t, a, ":")
	if a.r.TopModal() == "" || a.inData(a.r.FocusID()) {
		t.Fatalf(": opened no modal over the data pane: modal %q focus %q", a.r.TopModal(), a.r.FocusID())
	}
	if got := a.drawnFocus(); got != data {
		t.Fatalf("drawn focus under the palette %q, want the data pane's %q", got, data)
	}
	if got := a.pageView().Mode.Name; got != mode {
		t.Fatalf("drawn mode under the palette %q, want %q", got, mode)
	}
	if a.pageView().Live {
		t.Fatal("the page under a modal reads as the live interaction")
	}
	press(t, a, "esc", "q")
	if a.r.TopModal() != "" || a.r.FocusID() != data || a.r.CurrentMode().Name != mode {
		t.Fatalf("esc: modal %q focus %q mode %q, want none, %q, %q", a.r.TopModal(), a.r.FocusID(), a.r.CurrentMode().Name, data, mode)
	}
	if !a.pageView().Live || a.drawnFocus() != data {
		t.Fatalf("after the modal: view %+v", a.pageView())
	}
}
