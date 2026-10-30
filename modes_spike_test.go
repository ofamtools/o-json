package main

import (
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
