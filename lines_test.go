package main

import (
	"strings"
	"testing"
)

// The FLAT view's line keys: y y the line, y p the path, y v the value,
// enter moves the structure cursor to the line's shape.
func TestFlatLineKeys(t *testing.T) {
	a := open(t, "testdata/sample.json", 110, 30)
	press(t, a, "2", "D", "ctrl+w", "l")
	if a.viewName != viewFlat || a.r.BaseMode() != "flat" {
		t.Fatalf("view %q mode %q", a.viewName, a.r.BaseMode())
	}
	press(t, a, "j", "j", "j")
	for _, c := range []struct{ keys, want string }{
		{"y", `.items[0].name = "alpha"`}, // y y
		{"p", ".items[0].name"},
		{"v", "alpha"},
	} {
		keys := []string{"y", c.keys}
		if c.keys == "y" {
			keys = []string{"y", "y"}
		}
		press(t, a, keys...)
		if y, _ := a.r.ReadYank(""); y.Text != c.want {
			t.Fatalf("y %s yanked %q, want %q", c.keys, y.Text, c.want)
		}
	}
	press(t, a, "enter")
	if s := a.selectedShape(); s == nil || s.Path != ".items[].name" && s.Path != ".items[]" {
		t.Fatalf("enter jumped to %v", s)
	}
	if !strings.Contains(a.m.Info, "structure → .items[0].name") {
		t.Fatalf("info %q", a.m.Info)
	}
}

// The JSON view's line keys: folds (z a, z M, z R), % and the yanks.
func TestJSONLineFoldsAndBracket(t *testing.T) {
	a := open(t, "testdata/sample.json", 110, 40)
	press(t, a, "3", "D", "ctrl+w", "l")
	if a.viewName != viewJSON || a.r.BaseMode() != "json" {
		t.Fatalf("view %q mode %q", a.viewName, a.r.BaseMode())
	}
	press(t, a, "j", "j", "j") // "items": [
	if got := a.lines[a.data.Selected()].text; !strings.Contains(got, `"items": [`) {
		t.Fatalf("on %q", got)
	}
	press(t, a, "%")
	if got := a.lines[a.data.Selected()].text; got != "  ]," {
		t.Fatalf("%% went to %q", got)
	}
	press(t, a, "%", "y", "p")
	if y, _ := a.r.ReadYank(""); y.Text != ".items" {
		t.Fatalf("y p = %q", y.Text)
	}
	press(t, a, "z", "a")
	if got := a.lines[a.data.Selected()].text; got != `  "items": [… 4 items],` {
		t.Fatalf("z a left %q", got)
	}
	press(t, a, "z", "M")
	if !strings.Contains(view(a), `"meta": {… 3 keys}`) || len(a.lines) != 6 {
		t.Fatalf("z M: %q", lineTexts(a.lines))
	}
	press(t, a, "z", "R")
	if strings.Contains(strings.Join(lineTexts(a.lines), "\n"), "…") {
		t.Fatalf("z R kept a fold: %q", lineTexts(a.lines))
	}
	press(t, a, "j")
	press(t, a, "y", "y")
	if y, _ := a.r.ReadYank(""); !strings.HasPrefix(y.Text, "{\n") || !strings.Contains(y.Text, `"alpha"`) {
		t.Fatalf("y y on an item = %q", y.Text)
	}
}

func TestJSONLinesMatchPretty(t *testing.T) {
	a := open(t, "testdata/sample.json", 110, 30)
	got := strings.Join(lineTexts(jsonLines(a.Doc.Shape.Values(), false, nil)), "\n")
	if want := Pretty(a.Doc.Root); got != want {
		t.Fatalf("jsonLines differ from Pretty:\n%s\n---\n%s", got, want)
	}
}
