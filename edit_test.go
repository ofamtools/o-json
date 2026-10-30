package main

import (
	"strings"
	"testing"
)

func gated(t *testing.T, a *App) {
	t.Helper()
	if err := a.r.Settings().Set(settingEditGate, true); err != nil {
		t.Fatal(err)
	}
}

// With modes.edit-gate on, the edit keys refuse in TABLE; i enters EDIT
// where they answer; esc and leaving the pane leave it; r enters EDIT
// and opens the value editor.
func TestEditModeGatesValueEdits(t *testing.T) {
	a := open(t, "testdata/sample.json", 110, 30)
	gated(t, a)
	selectPath(t, a, ".items")
	press(t, a, "ctrl+w", "l")
	before := len(a.rs.patches)
	Press(a.m, "d")
	Press(a.m, "d")
	if !strings.Contains(a.m.Status, "press i first") || len(a.rs.patches) != before {
		t.Fatalf("d d in TABLE: status %q patches %d", a.m.Status, len(a.rs.patches))
	}
	press(t, a, "i")
	if a.r.BaseMode() != "edit" {
		t.Fatalf("i: mode %q", a.r.BaseMode())
	}
	press(t, a, "j", "l", "x")
	if len(a.rs.patches) != before+1 || a.r.BaseMode() != "edit" {
		t.Fatalf("x in EDIT: patches %d mode %q", len(a.rs.patches), a.r.BaseMode())
	}
	press(t, a, "esc")
	if a.r.BaseMode() != "table" || a.editing {
		t.Fatalf("esc: mode %q", a.r.BaseMode())
	}
	press(t, a, "i", "ctrl+w", "h", "ctrl+w", "l")
	if a.r.BaseMode() != "table" {
		t.Fatalf("EDIT is per pane: back on data in %q", a.r.BaseMode())
	}
	press(t, a, "r")
	if a.r.TopModal() != valueEditorID || !a.editing {
		t.Fatalf("r: modal %q editing %v", a.r.TopModal(), a.editing)
	}
	press(t, a, "esc", "esc")
	view(a)
	if a.r.TopModal() != "" || a.r.BaseMode() != "edit" {
		t.Fatalf("after the editor: modal %q mode %q", a.r.TopModal(), a.r.BaseMode())
	}
}
