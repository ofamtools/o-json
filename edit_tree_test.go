package main

import (
	"strings"
	"testing"
)

// With modes.edit-gate on, the edited structure's edit keys need EDIT
// too (owner question 22): in SHAPE they say so, i enters EDIT on the
// structure pane, they answer there, esc and leaving the pane leave it.
func TestEditGatesTheStructure(t *testing.T) {
	a := open(t, "testdata/sample.json", 110, 30)
	gated(t, a)
	selectPath(t, a, ".version")
	Press(a.m, "S")
	if !strings.Contains(a.m.Info, "S changes the document: press i first") || a.m.Status != "" || a.r.BaseMode() != "shape" {
		t.Fatalf("S in SHAPE: %q mode %q", a.m.Info, a.r.BaseMode())
	}
	keys(a, "d", "d")
	if !strings.Contains(a.m.Info, "press i first") {
		t.Fatalf("d d in SHAPE: %q", a.m.Info)
	}
	keys(a, "y", "p")
	if got := yanked(t, a, ""); got != ".version" {
		t.Fatalf("y p in SHAPE: %q", got)
	}
	press(t, a, "i")
	if a.r.BaseMode() != "edit" {
		t.Fatalf("i on the structure: mode %q", a.r.BaseMode())
	}
	keys(a, "d", "d")
	if a.m.Status != "" || strings.Contains(Compact(a.shownDoc().root, 0), `"version"`) {
		t.Fatalf("d d in EDIT: %q", a.m.Status)
	}
	if a.r.BaseMode() != "edit" {
		t.Fatalf("after d d: mode %q", a.r.BaseMode())
	}
	press(t, a, "ctrl+w", "l")
	if a.r.BaseMode() == "edit" || a.editing {
		t.Fatalf("leaving the pane: mode %q", a.r.BaseMode())
	}
	press(t, a, "ctrl+w", "h", "i", "esc")
	if a.r.BaseMode() != "shape" || a.editing {
		t.Fatalf("esc: mode %q", a.r.BaseMode())
	}
}
