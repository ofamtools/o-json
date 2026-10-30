package main

import (
	"strings"
	"testing"
)

// scalarsAlone sets structure.scalar-follow show: a plain-value shape is
// shown alone, as before the highlight.
func scalarsAlone(t *testing.T, a *App) {
	t.Helper()
	if err := a.r.Settings().Set(settingScalarFollow, "show"); err != nil {
		t.Fatal(err)
	}
	view(a)
}

// litCells are the data list's rows' toned cells (the highlight).
func litCells(a *App) map[int]bool {
	out := map[int]bool{}
	for _, it := range a.data.list.Items() {
		for _, tn := range it.Tones {
			if tn.Tone == toneFollow {
				out[tn.Cell] = true
			}
		}
	}
	return out
}

// The cursor onto a plain-value path keeps the table that holds it and
// highlights its column: the cursor's column moves there, the row stays.
func TestScalarKeepsTheTableAndHighlightsItsColumn(t *testing.T) {
	a := openOriginal(t, sample, 120, 30)
	selectPath(t, a, ".items")
	press(t, a, "tab", "3", "G", "shift+tab")
	if a.data.Selected() != 2 {
		t.Fatalf("row %d", a.data.Selected())
	}
	selectPath(t, a, ".items[].name")
	frame := view(a)
	if !strings.Contains(frame, "Table .items  3/4") || a.shown.Path != ".items" {
		t.Fatalf("the table changed (shown %s)\n%s", a.shown.Path, frame)
	}
	name := 1 + 1 // "#", id, name
	if a.data.list.Column() != name || a.data.Selected() != 2 || !litCells(a)[name] || len(litCells(a)) != 1 {
		t.Fatalf("column %d row %d lit %v", a.data.list.Column(), a.data.Selected(), litCells(a))
	}
	if !strings.Contains(line(frame, "sample.json  ·"), ".items[].name (string 4/4)") {
		t.Fatalf("the breadcrumb names the cursor's path\n%s", frame)
	}
	// Another plain value of the same rows moves the highlight.
	selectPath(t, a, ".items[].id")
	if a.shown.Path != ".items" || a.data.list.Column() != 1 || !litCells(a)[1] || litCells(a)[name] {
		t.Fatalf("id: shown %s column %d lit %v", a.shown.Path, a.data.list.Column(), litCells(a))
	}
}

// A collection still changes the data pane, and the highlight goes.
func TestCollectionChangesTheData(t *testing.T) {
	a := openOriginal(t, sample, 120, 30)
	selectPath(t, a, ".items[].name")
	if a.shown.Path != ".items[]" || len(litCells(a)) != 1 {
		t.Fatalf("from elsewhere: the nearest holder (shown %s, lit %v)", a.shown.Path, litCells(a))
	}
	selectPath(t, a, ".items[].owner")
	frame := view(a)
	if a.shown.Path != ".items[].owner" || !strings.Contains(frame, "Table .items[].owner") || len(litCells(a)) != 0 || a.lit != nil {
		t.Fatalf("owner: shown %s lit %v\n%s", a.shown.Path, litCells(a), frame)
	}
	// Its fields' plain values highlight in the owners' table.
	selectPath(t, a, ".items[].owner.email")
	if a.shown.Path != ".items[].owner" || a.data.list.Column() != 2 || !litCells(a)[2] {
		t.Fatalf("email: shown %s column %d lit %v", a.shown.Path, a.data.list.Column(), litCells(a))
	}
	// An array of plain values: its (value) column.
	selectPath(t, a, ".items[].tags[]")
	if a.shown.Path != ".items[].tags" || !a.tab.Columns[a.data.list.Column()-1].Self {
		t.Fatalf("tags[]: shown %s column %d", a.shown.Path, a.data.list.Column())
	}
}

// In a fields view the plain value's row is highlighted and the cursor
// goes to it.
func TestScalarHighlightsItsFieldRow(t *testing.T) {
	a := openOriginal(t, sample, 120, 30)
	selectPath(t, a, ".meta.count")
	frame := view(a)
	if a.shown.Path != ".meta" || !strings.Contains(frame, "Fields .meta") || a.data.Selected() != 1 || a.data.litRow != 1 {
		t.Fatalf("shown %s row %d lit %d\n%s", a.shown.Path, a.data.Selected(), a.data.litRow, frame)
	}
	if lit := litCells(a); !lit[0] || !lit[2] {
		t.Fatalf("the row's cells are toned: %v", lit)
	}
}

// structure.scalar-follow show restores the plain values shown alone.
func TestScalarFollowShow(t *testing.T) {
	a := openOriginal(t, sample, 120, 30)
	selectPath(t, a, ".items[].name")
	scalarsAlone(t, a)
	frame := view(a)
	if a.shown.Path != ".items[].name" || !strings.Contains(frame, "Table .items[].name") || len(litCells(a)) != 0 {
		t.Fatalf("show: shown %s lit %v\n%s", a.shown.Path, litCells(a), frame)
	}
	if err := a.r.Settings().Set(settingScalarFollow, "highlight"); err != nil {
		t.Fatal(err)
	}
	if view(a); a.shown.Path != ".items[]" || len(litCells(a)) != 1 {
		t.Fatalf("highlight again: shown %s lit %v", a.shown.Path, litCells(a))
	}
}

// A column off screen scrolls into view; the cursor's row stays.
func TestHighlightedColumnScrollsIntoView(t *testing.T) {
	a := openOriginal(t, sample, 40, 24)
	selectPath(t, a, ".items")
	press(t, a, "tab", "2", "G", "shift+tab")
	if strings.Contains(view(a), "third") {
		t.Fatalf("note is on screen at 40 columns\n%s", view(a))
	}
	selectPath(t, a, ".items[].note")
	frame := view(a)
	if a.shown.Path != ".items" || a.data.Selected() != 1 || a.data.list.ColumnOffset() == 0 {
		t.Fatalf("shown %s row %d offset %d\n%s", a.shown.Path, a.data.Selected(), a.data.list.ColumnOffset(), frame)
	}
	if !strings.Contains(frame, "note?") {
		t.Fatalf("the note column is not on screen\n%s", frame)
	}
}

// The edited view follows the same rule on the restructured document.
func TestScalarInTheEditedView(t *testing.T) {
	a := open(t, sample, 120, 30)
	a.on(t, ".meta.count")
	if a.shown == nil || a.shown.Path != ".meta" || a.data.litRow != 1 {
		t.Fatalf("edited: shown %v lit %d\n%s", a.shown, a.data.litRow, view(a))
	}
}

// A value edit while a plain value is highlighted keeps the cursor on
// the edited cell: lighting the same path again moves nothing.
func TestValueEditKeepsTheCursorOffTheHighlight(t *testing.T) {
	a := open(t, sample, 120, 30)
	a.on(t, ".items[].name")
	if a.shown == nil || a.data.list.Column() != 2 {
		t.Fatalf("name: shown %v column %d", a.shown, a.data.list.Column())
	}
	press(t, a, "tab", "h")
	if a.data.list.Column() != 1 {
		t.Fatalf("h: column %d", a.data.list.Column())
	}
	press(t, a, "r", "ctrl+u")
	press(t, a, "9", "enter")
	if a.data.list.Column() != 1 || !litCells(a)[2] {
		t.Fatalf("after the edit: column %d lit %v\n%s", a.data.list.Column(), litCells(a), view(a))
	}
}
