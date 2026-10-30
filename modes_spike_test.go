package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestTrialViewCycleAndDeclaredMode(t *testing.T) {
	a := open(t, "testdata/sample.json", 100, 30)
	if got := a.r.BaseMode(); got != "shape" {
		t.Fatalf("structure mode = %q", got)
	}
	press(t, a, "D")
	if a.viewName != objectFields {
		t.Fatalf("first view = %q", a.viewName)
	}
	press(t, a, "tab")
	if a.viewName != viewFlat || !strings.Contains(view(a), " = ") {
		t.Fatalf("flat view = %q", a.viewName)
	}
	press(t, a, "shift+tab")
	if a.viewName != objectFields {
		t.Fatalf("backward view = %q", a.viewName)
	}
	press(t, a, "2", "D")
	if a.viewName != viewJSON || !strings.Contains(view(a), "JSON .") {
		t.Fatalf("counted view = %q", a.viewName)
	}
	if got := a.r.BaseMode(); got != "shape" {
		t.Fatalf("structure focus changed mode to %q", got)
	}
	if err := a.data.list.Focus(); err != nil {
		t.Fatal(err)
	}
	view(a)
	if got := a.r.BaseMode(); got != "json" {
		t.Fatalf("data mode = %q", got)
	}
	press(t, a, "v")
	if got := a.r.BaseMode(); got != "visual-line" {
		t.Fatalf("data selection mode = %q", got)
	}
}

func TestTrialModePickerChoosesFuzzyViewAndSplit(t *testing.T) {
	a := open(t, "testdata/sample.json", 120, 32)
	press(t, a, "g", "m")
	if a.r.Chooser(modeChooser) == nil {
		t.Fatal("g m did not open the mode picker")
	}
	press(t, a, "s", "o", "n", "enter")
	if a.viewName != viewJSON || !a.inData(a.r.Focus) || a.r.BaseMode() != "json" {
		t.Fatalf("fuzzy JSON choice: view %q focus %q mode %q", a.viewName, a.r.Focus, a.r.BaseMode())
	}
	press(t, a, "g", "m", "s", "p", "l", "enter")
	if a.r.Page != rsPageID || a.r.BaseMode() != "split" {
		t.Fatalf("SPLIT choice: page %q mode %q", a.r.Page, a.r.BaseMode())
	}
}

func TestTrialPaneKeysAndSplitBadge(t *testing.T) {
	a := open(t, "testdata/sample.json", 100, 30)
	shapeColour := fmt.Sprint(a.m.Appearance.Accent)
	press(t, a, "ctrl+a", "o")
	if !a.inData(a.r.Focus) || a.r.BaseMode() != "table" {
		t.Fatalf("other pane: focus %q mode %q", a.r.Focus, a.r.BaseMode())
	}
	if tableColour := fmt.Sprint(a.m.Appearance.Accent); tableColour == shapeColour {
		t.Fatalf("shape and table accents are both %q", tableColour)
	}
	press(t, a, "ctrl+w", "h")
	if a.inData(a.r.Focus) || a.r.BaseMode() != "shape" {
		t.Fatalf("left pane: focus %q mode %q", a.r.Focus, a.r.BaseMode())
	}
	press(t, a, "ctrl+w", "l")
	if !a.inData(a.r.Focus) || a.r.BaseMode() != "table" {
		t.Fatalf("right pane: focus %q mode %q", a.r.Focus, a.r.BaseMode())
	}
	press(t, a, "ctrl+w", "v")
	view(a)
	if a.r.Page != rsPageID || a.r.BaseMode() != "split" {
		t.Fatalf("split: page %q mode %q", a.r.Page, a.r.BaseMode())
	}
	from := a.sideOf(a.r.Focus)
	press(t, a, "ctrl+a", "o")
	if a.sideOf(a.r.Focus) == from || a.r.BaseMode() != "split" {
		t.Fatalf("other side: focus %q mode %q", a.r.Focus, a.r.BaseMode())
	}
}

func TestTrialNestedBadge(t *testing.T) {
	a := hoverAt(t, 120, 30, 6)
	press(t, a, "j")
	view(a)
	if !a.hoverIn() || a.r.BaseMode() != "nested" {
		t.Fatalf("sub-table: mode %q", a.r.BaseMode())
	}
	press(t, a, "k")
	view(a)
	if a.hoverIn() || a.r.BaseMode() != "table" {
		t.Fatalf("table row: mode %q", a.r.BaseMode())
	}
}

func TestTrialJQResultsBadge(t *testing.T) {
	a := openQuery(t, "testdata/sample.json")
	query(t, a, ".items[] | .name")
	press(t, a, "ctrl+a", "o")
	view(a)
	if !a.queryShowing() || a.r.BaseMode() != "jq" {
		t.Fatalf("jq results: mode %q", a.r.BaseMode())
	}
}

func TestTrialConfigUsesBinaryName(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if got := a.r.Config().App; got != "" {
		t.Fatalf("test binary should have no owner config, got %q", got)
	}
}

func TestTrialDeclaredModesCanBeDisabled(t *testing.T) {
	a := open(t, "testdata/sample.json", 100, 30)
	if err := a.r.Settings().Set(settingDeclaredModes, "off"); err != nil {
		t.Fatal(err)
	}
	view(a)
	if got := a.r.BaseMode(); got != "normal" {
		t.Fatalf("mode with declaration off = %q", got)
	}
	press(t, a, "D")
	if a.viewName != objectFields {
		t.Fatalf("view cycle with declaration off = %q", a.viewName)
	}
}

// The picker enters every mode it lists.
func TestTrialModePickerEntersEveryMode(t *testing.T) {
	want := map[string]string{
		"SHAPE": "shape", "TABLE": "table", "NESTED": "nested", "FLAT": "flat", "JSON": "json",
		"SEARCH": "search_·_substr", "JQ": "jq_·_line", "EDIT": "edit", "VISUAL": "visual-line", "SPLIT": "split",
	}
	for _, c := range modeChoices {
		if c.name == "RAW" {
			continue // TestTrialModePickerRAW
		}
		t.Run(c.name, func(t *testing.T) {
			a := open(t, "testdata/sample.json", 110, 30)
			if err := a.r.Settings().Set(settingEditGate, true); err != nil {
				t.Fatal(err)
			}
			selectPath(t, a, ".items")
			press(t, a, "g", "m")
			for _, ch := range strings.ToLower(c.name) {
				press(t, a, string(ch))
			}
			press(t, a, "enter")
			view(a)
			if got := a.r.BaseMode(); got != want[c.name] {
				t.Fatalf("%s: mode %q hover %v focus %q col %d cell %v", c.name, got, a.hover.path, a.r.Focus, a.data.list.Column(), a.dataCell())
			}
		})
	}
}

func TestTrialModePickerRAW(t *testing.T) {
	a := open(t, "testdata/sample.json", 110, 30)
	if err := a.r.Settings().Set(settingRawEditor, "fake-editor"); err != nil {
		t.Fatal(err)
	}
	var argv [][]string
	rawEditorFake(a, func(s string) string { return s }, &argv)
	press(t, a, "g", "m", "r", "a", "w")
	finish(a, Press(a.m, "enter"))
	settle(a)
	if len(argv) != 1 || argv[0][0] != "fake-editor" {
		t.Fatalf("RAW from the picker ran %v", argv)
	}
}
