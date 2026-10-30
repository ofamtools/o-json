package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// nestedDoc is an array of people whose cells hold objects (address,
// meta), arrays of objects (orders, their items) and scalar lists (tags).
const nestedDoc = "testdata/nested.json"

func build(t *testing.T, text string) *Node {
	t.Helper()
	n, err := Build([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestHoverGridPerKind(t *testing.T) {
	for _, c := range []struct {
		name, doc string
		header    []string
		rows      [][]string
	}{
		{"object", `{"city": "Paris", "geo": {"lat": 1}, "tags": [1, 2]}`,
			[]string{"city", "geo", "tags"}, [][]string{{"Paris", "{1}", "[2]"}}},
		{"array of objects", `[{"id": 1, "x": {"a": 1, "b": 2}}, {"id": 2, "y": true}]`,
			[]string{"#", "id", "x", "y"}, [][]string{{"0", "1", "{2}", ""}, {"1", "2", "", "true"}}},
		{"array of scalars", `["a", 2, null, true]`,
			nil, [][]string{{"a · 2 · null · true"}}},
		{"mixed array", `[1, [2, 3], "s"]`,
			[]string{"#", "value"}, [][]string{{"0", "1"}, {"1", "[2]"}, {"2", "s"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := gridOf(build(t, c.doc))
			if !slices.Equal(g.header, c.header) {
				t.Errorf("header %q, want %q", g.header, c.header)
			}
			if len(g.rows) != len(c.rows) {
				t.Fatalf("rows %q, want %q", g.rows, c.rows)
			}
			for i := range g.rows {
				if !slices.Equal(g.rows[i], c.rows[i]) {
					t.Errorf("row %d %q, want %q", i, g.rows[i], c.rows[i])
				}
			}
			if len(g.lines) != len(g.rows) {
				t.Errorf("%d line values for %d lines", len(g.lines), len(g.rows))
			}
		})
	}
}

// hoverAt opens the nested document with the data pane focused on row
// 2 (Cy C) and column col (6 address, 7 orders, 4 tags).
func hoverAt(t *testing.T, width, height, col int) *App {
	t.Helper()
	a := open(t, nestedDoc, width, height)
	press(t, a, "tab", "j", "j")
	for range col {
		press(t, a, "l")
	}
	if a.data.Selected() != 2 || a.data.list.Column() != col {
		t.Fatalf("row %d column %d", a.data.Selected(), a.data.list.Column())
	}
	return a
}

func TestHoverOpensUnderTheRowAndClosesOnMove(t *testing.T) {
	a := hoverAt(t, 120, 30, 6)
	frame := view(a)
	if !strings.Contains(frame, "Austin  73301  US") {
		t.Fatalf("no address sub-table\n%s", frame)
	}
	if got := len(a.hoverLevels()); got != 1 {
		t.Fatalf("%d levels open", got)
	}
	// Along the row, the next cell (orders) replaces it.
	press(t, a, "l")
	frame = view(a)
	if strings.Contains(frame, "Austin") || !strings.Contains(frame, "o3-0") {
		t.Fatalf("the orders sub-table did not replace the address\n%s", frame)
	}
	// The # column is the row itself: nothing opens.
	press(t, a, "0")
	if len(a.hoverLevels()) != 0 || strings.Contains(view(a), "o3-0") {
		t.Fatalf("# opened a sub-table\n%s", view(a))
	}
	// The tree focused: the pane draws no sub-table.
	press(t, a, "$")
	press(t, a, "tab")
	if strings.Contains(view(a), "    #  id") {
		t.Fatalf("a sub-table with the tree focused\n%s", view(a))
	}
}

func TestHoverChainOnlyUnderTheCursor(t *testing.T) {
	a := hoverAt(t, 120, 30, 7) // orders
	press(t, a, "j", "l", "l", "l")
	if got := len(a.hoverLevels()); got != 2 {
		t.Fatalf("%d levels, want orders and its items", got)
	}
	frame := view(a)
	if !strings.Contains(frame, "mug") || strings.Contains(frame, "spring") {
		t.Fatalf("frame\n%s", frame)
	}
	// Down to order 1's line (the cursor keeps its items column): order
	// 1's items open, order 0's close.
	press(t, a, "0", "j")
	if p := a.hover.path; len(p) != 1 || p[0].row != 1 {
		t.Fatalf("path %v, want order 1", p)
	}
	press(t, a, "$")
	if got := len(a.hoverLevels()); got != 2 {
		t.Fatalf("%d levels", got)
	}
	frame = view(a)
	if strings.Contains(frame, "kettle") || !strings.Contains(frame, "tray") {
		t.Fatalf("order 0's items still open, or order 1's not\n%s", frame)
	}
}

func TestHoverWalkAcrossLevelsAndOutTheBottom(t *testing.T) {
	a := hoverAt(t, 120, 30, 7)
	press(t, a, "j") // order 0's line
	press(t, a, "l", "l", "l")
	press(t, a, "j") // into its items: mug
	if p := a.hover.path; len(p) != 2 || p[1] != (hoverPos{0, 0}) {
		t.Fatalf("path %v", p)
	}
	press(t, a, "j", "j") // kettle, filter
	press(t, a, "j")      // out of the items: order 1
	if p := a.hover.path; len(p) != 1 || p[0].row != 1 {
		t.Fatalf("path %v, want order 1", p)
	}
	// order 1's items are the next lines drawn: j walks into them.
	press(t, a, "j")
	if p := a.hover.path; len(p) != 2 || p[0].row != 1 {
		t.Fatalf("path %v, want order 1's first item", p)
	}
	press(t, a, "k", "0", "j") // on order 1's #: nothing open below
	if a.data.Selected() != 3 || len(a.hover.path) != 0 {
		t.Fatalf("row %d path %v, want row 3 on the row", a.data.Selected(), a.hover.path)
	}
	// k from row 3 goes to row 2 (its sub-table opens below it), and k
	// again to row 1: the sub-table under row 2 is not walked from below.
	press(t, a, "k")
	if a.data.Selected() != 2 || len(a.hover.path) != 0 {
		t.Fatalf("row %d path %v", a.data.Selected(), a.hover.path)
	}
}

func TestHoverCounts(t *testing.T) {
	one := hoverAt(t, 120, 30, 7)
	press(t, one, "j", "l", "l", "l", "j", "j", "j", "j")
	many := hoverAt(t, 120, 30, 7)
	press(t, many, "j", "3", "l", "4", "j")
	if one.data.Selected() != many.data.Selected() || !slices.Equal(one.hover.path, many.hover.path) {
		t.Fatalf("one at a time %d %v, counted %d %v", one.data.Selected(), one.hover.path, many.data.Selected(), many.hover.path)
	}
	// Up does not walk into a sub-table it meets from below (only the
	// chain under the cursor is open): order 0, then the row.
	press(t, many, "2", "k")
	if many.data.Selected() != 2 || len(many.hover.path) != 0 {
		t.Fatalf("2k: row %d path %v", many.data.Selected(), many.hover.path)
	}
}

func TestHoverJumpsLeaveTheSubTable(t *testing.T) {
	a := hoverAt(t, 120, 30, 6)
	press(t, a, "j")
	if !a.hoverIn() {
		t.Fatal("j did not step into the address")
	}
	press(t, a, "G")
	if a.data.Selected() != 19 || a.hoverIn() {
		t.Fatalf("G: row %d in %v", a.data.Selected(), a.hoverIn())
	}
	b := open(t, nestedDoc, 120, 30)
	press(t, b, "tab")
	for range 6 {
		press(t, b, "l")
	}
	press(t, b, "j") // row 0's address line
	press(t, b, "g", "g")
	if b.data.Selected() != 0 || b.hoverIn() {
		t.Fatalf("gg: row %d in %v", b.data.Selected(), b.hoverIn())
	}
}

func TestHoverYankAndOpenTheCell(t *testing.T) {
	a := hoverAt(t, 120, 30, 6)
	press(t, a, "j", "l", "l", "y")
	if y, _ := a.r.ReadYank(""); y.Text != "US" {
		t.Fatalf("yanked %q, want US (the country cell)", y.Text)
	}
	// enter on a scalar opens its line's object, on a nested cell the cell.
	press(t, a, "enter")
	if len(a.trail) != 1 || a.trail[0].Path() != "[2].address" {
		t.Fatalf("detail on %v", a.trail)
	}
	press(t, a, "esc")
	b := hoverAt(t, 120, 30, 7)
	press(t, b, "j", "l", "l", "l", "enter")
	if len(b.trail) != 1 || b.trail[0].Path() != "[2].orders[0].items" {
		t.Fatalf("detail on %v", b.trail)
	}
}

// The keys in a sub-table are the table's: the same edit hints show, r
// edits the value under the sub-table's cursor in an input drawn on its
// line, and the write is one undoable value edit; p and d d act there too.
func TestHoverEditsTheSubTableCell(t *testing.T) {
	a := hoverAt(t, 120, 30, 6)
	inlineEditor(t, a)
	editHints := []string{"r/i edit", "d d delete", "o/O add", "K rename key", "p put"}
	edits := []string{"value.edit", "value.delete", "value.add", "value.rename", "value.paste"}
	hints := func() string { return ochazuke.BindingHints(a.r, a.r.Focus) }
	onRow := hints()
	press(t, a, "j") // the address line, on city
	if !a.hoverIn() {
		t.Fatal("j did not step into the sub-table")
	}
	for _, h := range editHints {
		if !strings.Contains(onRow, h) || !strings.Contains(hints(), h) {
			t.Errorf("%q: on the row %v, in the sub-table %v\n%s", h, strings.Contains(onRow, h), strings.Contains(hints(), h), hints())
		}
	}
	for _, verb := range edits {
		if !a.r.VerbShown(verb, a.r.Focus, a.r.Mode) {
			t.Errorf("%s is not available in a sub-table", verb)
		}
	}
	press(t, a, "r")
	if a.r.Focus != valueInputID || selected(a) != "Austin" {
		t.Fatalf("r: focus %q selected %q", a.r.Focus, selected(a))
	}
	frame := view(a)
	row, input := line(frame, "Cy C"), line(frame, "city (string): Austin")
	head := line(frame, "city    zip")
	if row == "" || head == "" || input == "" || !strings.Contains(frame, head+"\n"+input) || strings.Index(frame, row) > strings.Index(frame, head) {
		t.Fatalf("the input is not on the sub-table's line under its header\n%s", frame)
	}
	if !strings.Contains(input, "┃     city (string)") {
		t.Fatalf("the input is not at the sub-table's indent: %q", input)
	}
	typeText(t, a, "Lyon")
	press(t, a, "enter")
	if got := field(t, a, ".[2].address.city"); got != `"Lyon"` {
		t.Fatalf("city after the edit: %s", got)
	}
	if !a.hoverIn() || !strings.Contains(view(a), "Lyon  73301") {
		t.Fatalf("the walk did not stay on the edited line (in %v)\n%s", a.hoverIn(), view(a))
	}
	press(t, a, "u")
	if got := field(t, a, ".[2].address.city"); got != `"Austin"` {
		t.Fatalf("u: %s", got)
	}
	// y on country, then p over zip; d d deletes city.
	press(t, a, "$", "y", "h", "p")
	if got := field(t, a, ".[2].address.zip"); got != `"US"` {
		t.Fatalf("p over zip: %s", got)
	}
	press(t, a, "0", "d", "d")
	if got := field(t, a, ".[2].address"); got != `{"zip":"US","country":"US"}` {
		t.Fatalf("d d on city: %s", got)
	}
	if !a.hoverIn() {
		t.Fatal("the walk left the sub-table after a delete")
	}
}

// A cell an item of a sub-table lacks is added by r, as on a table row.
func TestHoverEditAddsAMissingCell(t *testing.T) {
	a := open(t, "testdata/nested.json", 120, 30)
	press(t, a, "tab")
	// Row 0 has no orders items to lack; build one: an object array whose
	// second item lacks a key, through a value edit on the row.
	for range 7 {
		press(t, a, "l")
	}
	valueDo(t, a, "edit", ochazuke.Arguments{"text": `[{"a": 1, "b": 2}, {"a": 3}]`})
	press(t, a, "j", "j", "l", "l") // the second item's b
	v, ok := a.valueOf(a.r.Instances[a.r.Focus])
	if !ok || v.node != nil || v.key != "b" {
		t.Fatalf("the missing cell: %+v %v", v, ok)
	}
	valueDo(t, a, "edit", ochazuke.Arguments{"text": "4"})
	if got := field(t, a, ".[0].orders[1]"); got != `{"a":3,"b":4}` {
		t.Fatalf("added: %s", got)
	}
}

func TestHoverSummarySetting(t *testing.T) {
	a := open(t, nestedDoc, 120, 30)
	if err := a.r.Settings().Set(settingNested, nestedSummary); err != nil {
		t.Fatal(err)
	}
	press(t, a, "tab", "j", "j", "l", "l", "l", "l", "l", "l")
	frame := view(a)
	if strings.Contains(frame, "Austin") {
		t.Fatalf("summary opened a sub-table\n%s", frame)
	}
	if !strings.Contains(frame, "{3}") {
		t.Fatalf("summary lost the cell's summary\n%s", frame)
	}
	press(t, a, "j")
	if a.data.Selected() != 3 || a.hoverIn() {
		t.Fatalf("j under summary: row %d", a.data.Selected())
	}
}

// The frames at 80 and 120 columns with the orders sub-table open and
// its first order's items under the cursor.
func TestHoverRenderGolden(t *testing.T) {
	for _, width := range []int{80, 120} {
		a := hoverAt(t, width, 24, 7)
		press(t, a, "j", "l", "l", "l", "j")
		got := view(a)
		golden := filepath.Join("testdata", "hover-"+itoa(width)+".golden")
		if os.Getenv("UPDATE_GOLDEN") != "" {
			_ = os.WriteFile(golden, []byte(got), 0o644)
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Errorf("%d columns\n%s\nwant\n%s", width, got, want)
		}
	}
}

// The row's other keys keep their meaning on a sub-table line: N| goes to
// cell N, the half and page motions walk the drawn lines, and the list's
// column chooser and resizing (which act on the row's columns) are not
// offered there.
func TestHoverRowKeysOnASubTableLine(t *testing.T) {
	a := hoverAt(t, 120, 30, 6)
	press(t, a, "j", "3", "|")
	if p := a.hover.path; len(p) != 1 || p[0].col != 2 {
		t.Fatalf("3|: path %v, want the third cell", p)
	}
	if a.data.list.Column() != 6 {
		t.Fatalf("3| moved the row's column to %d", a.data.list.Column())
	}
	for _, key := range []string{"g c", "z >", "z p"} {
		if strings.Contains(ochazuke.BindingHints(a.r, a.r.Focus), key+" ") {
			t.Errorf("%s offered in a sub-table", key)
		}
	}
	press(t, a, "g", "c")
	if a.r.TopModal() != "" || !a.hoverIn() {
		t.Fatalf("g c in a sub-table opened %q (in %v)", a.r.TopModal(), a.hoverIn())
	}
	// The guard is the verbs' availability, not their rows': the palette
	// and the help leave them out too, and the row's columns get them back.
	for _, verb := range []string{"ochazuke.list.choose-column", "ochazuke.list.resize-column", "ochazuke.list.reset-columns", "ochazuke.list.toggle-pin-column"} {
		if a.r.VerbShown(verb, a.r.Focus, a.r.Mode) {
			t.Errorf("%s shown in a sub-table", verb)
		}
	}
	if on := hoverAt(t, 120, 30, 6); !on.r.VerbShown("ochazuke.list.choose-column", on.r.Focus, on.r.Mode) {
		t.Error("choose-column not shown on the row")
	}
	// ctrl+d walks half a screen of lines: the same as that many j.
	b := hoverAt(t, 120, 30, 7)
	half := b.data.page(true)
	press(t, b, "ctrl+d")
	c := hoverAt(t, 120, 30, 7)
	for range half {
		press(t, c, "j")
	}
	if b.data.Selected() != c.data.Selected() || !slices.Equal(b.hover.path, c.hover.path) {
		t.Fatalf("ctrl+d %d %v, %d j %d %v", b.data.Selected(), b.hover.path, half, c.data.Selected(), c.hover.path)
	}
}

// enter on a table cell holding an object or an array opens that value in
// the detail (the guide, chapter 4), with table.nested summary too; on a
// plain cell or # it opens the row.
func TestEnterOnANestedCellOpensIt(t *testing.T) {
	for _, nested := range []string{"hover", "summary"} {
		a := openOriginal(t, sample, 120, 30)
		if err := a.r.Settings().Set(settingNested, nested); err != nil {
			t.Fatal(err)
		}
		selectPath(t, a, ".items")
		press(t, a, "tab", "l", "l", "l") // row 0, owner
		press(t, a, "enter")
		if a.r.TopModal() != detailID || len(a.trail) != 1 || a.trail[0].Path() != ".items[0].owner" {
			t.Fatalf("%s: enter on owner opened %v\n%s", nested, a.trail, view(a))
		}
		press(t, a, "q", "h", "h") // name
		press(t, a, "enter")
		if a.r.TopModal() != detailID || a.trail[0].Path() != ".items[0]" {
			t.Fatalf("%s: enter on name opened %v", nested, a.trail)
		}
	}
}
