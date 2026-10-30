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
