package main

import (
	"testing"

	"ofam.dev/ochazuke"
)

// The viewer declares its pickers (the CLI catalog describes the doc shell, which has none).
func TestDeclaredPickers(t *testing.T) {
	a, e := New()
	if e != nil {
		t.Fatal(e)
	}
	got := map[string]bool{}
	for _, d := range a.r.DeclaredPickers() {
		got[d.Name] = d.About != "" && (d.Noun != "" || d.Verb != "")
	}
	for _, name := range []string{"doc", "node", "modes"} {
		if !got[name] {
			t.Errorf("viewer did not declare the %s picker: %v", name, got)
		}
	}
}

// picker.open modes runs viewer.mode, the same picker g m opens.
func TestModesPickerOpensByName(t *testing.T) {
	a, e := New()
	if e != nil {
		t.Fatal(e)
	}
	if _, e := a.r.Execute(ochazuke.Request{Noun: "ochazuke.picker", Verb: "open", Args: ochazuke.Arguments{"name": "modes"}}); e != nil {
		t.Fatal(e)
	}
	if a.r.Chooser(modeChooser) == nil {
		t.Fatal("picker.open modes did not open the mode picker")
	}
}
