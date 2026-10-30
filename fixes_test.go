package main

import (
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// hasReverse reports reverse video (SGR 7) in text.
func hasReverse(text string) bool { return reverseCount(text) > 0 }

// reverseCount counts the escape sequences in text that turn reverse
// video (SGR 7) on.
func reverseCount(text string) int {
	n := 0
	for _, seq := range strings.Split(text, "\x1b[")[1:] {
		end := strings.IndexByte(seq, 'm')
		if end < 0 {
			continue
		}
		params := strings.Split(seq[:end], ";")
		for k := 0; k < len(params); k++ {
			switch p := params[k]; {
			case p == "38" || p == "48" || p == "58":
				if k+1 < len(params) && params[k+1] == "2" {
					k += 4
				} else {
					k += 2
				}
			case p == "7":
				n++
			}
		}
	}
	return n
}

// Beneath the detail the data pane draws its row without the cursor's
// bar, which would peek out beside the modal (6-edit.tape): o-json gives
// ochazuke's modal-highlights the default plain; keep draws the bar
// (dimmed) again, and closing the detail brings it back either way.
func TestDetailHidesTheRowCursorBeneathIt(t *testing.T) {
	a := open(t, sample, 120, 30)
	if got := a.r.Settings().String(ochazuke.ModalBackdropHighlightsSetting); got != "plain" {
		t.Fatalf("modal-highlights %q, want o-json's default plain", got)
	}
	onData(t, a, ".items", ".items[1]")
	page := reverseCount(a.m.View().Content)
	if page == 0 {
		t.Fatal("the data pane draws no cursor bar")
	}
	press(t, a, "enter")
	if a.r.TopModal() != detailID {
		t.Fatalf("enter: modal %q", a.r.TopModal())
	}
	plain := reverseCount(a.m.View().Content)
	if err := a.r.Settings().Set(ochazuke.ModalBackdropHighlightsSetting, "keep"); err != nil {
		t.Fatal(err)
	}
	if keep := reverseCount(a.m.View().Content); plain >= keep {
		t.Fatalf("beneath the detail plain draws %d reverse spans, keep %d: plain must drop the page's bar", plain, keep)
	}
	press(t, a, "q")
	if got := reverseCount(a.m.View().Content); got != page {
		t.Fatalf("the cursor bar did not come back when the detail closed: %d reverse spans, %d before", got, page)
	}
}

// A value edit keeps the table's column order: K on one row's name puts
// the new key's column beside name? (both optional now), not among the
// optional columns at the end; the cursor follows to the edited cell; u
// brings the old order back; table.stable-columns off orders afresh.
func TestRenameKeepsTheColumnWhereItWas(t *testing.T) {
	header := func(a *App) string {
		l := line(view(a), "#  id")
		if i := strings.Index(l, "┃"); i >= 0 {
			l = l[i+len("┃"):]
		}
		return strings.Join(strings.Fields(l), " ")
	}
	for _, stable := range []bool{true, false} {
		a := open(t, sample, 120, 30)
		if err := a.r.Settings().Set(settingStableColumns, stable); err != nil {
			t.Fatal(err)
		}
		onData(t, a, ".items", ".items[1]")
		press(t, a, "l", "l", "K")
		press(t, a, "ctrl+u")
		typeText(t, a, "title")
		press(t, a, "enter")
		got := header(a)
		if stable {
			if !strings.HasPrefix(got, "# id name? title? owner tags archived?") {
				t.Fatalf("stable: %q", got)
			}
			if c := a.tab.Columns[a.data.list.Column()-1]; c.Name != "title" || a.data.Selected() != 1 {
				t.Fatalf("the cursor is on %q row %d, not the edited cell", c.Name, a.data.Selected())
			}
			press(t, a, "u")
			if got := header(a); !strings.HasPrefix(got, "# id name owner tags archived? note?") {
				t.Fatalf("u: %q", got)
			}
			// o on a row adds its key's column beside the cell's (owner:
			// the cursor stayed on the column it was in).
			press(t, a, "o")
			typeText(t, a, "rank")
			press(t, a, "enter")
			typeText(t, a, "1")
			press(t, a, "enter")
			if got := header(a); !strings.HasPrefix(got, "# id name owner rank? tags") {
				t.Fatalf("o: %q", got)
			}
			if c := a.tab.Columns[a.data.list.Column()-1]; c.Name != "rank" {
				t.Fatalf("the cursor is on %q after o, not rank", c.Name)
			}
			continue
		}
		if !strings.HasPrefix(got, "# id owner tags name? title? archived?") {
			t.Fatalf("not stable: %q", got)
		}
	}
}

// stableColumns keeps the old order, puts a new column beside its anchor
// (after, or before) or after its predecessor in the new order, and drops
// the columns that went.
func TestStableColumns(t *testing.T) {
	cols := func(names ...string) []TableColumn {
		var out []TableColumn
		for _, n := range names {
			out = append(out, TableColumn{Name: n})
		}
		return out
	}
	names := func(cs []TableColumn) string {
		var out []string
		for _, c := range cs {
			out = append(out, c.Name)
		}
		return strings.Join(out, " ")
	}
	for _, c := range []struct {
		prev, next []TableColumn
		anchors    map[string]columnAnchor
		want       string
	}{
		{cols("a", "b", "c"), cols("a", "c", "b"), nil, "a b c"},
		{cols("a", "b", "c"), cols("a", "c", "b", "x"), map[string]columnAnchor{"x": {key: "b"}}, "a b x c"},
		{cols("a", "b", "c"), cols("a", "c", "x"), map[string]columnAnchor{"x": {key: "b"}}, "a x c"},
		{cols("a", "b", "c"), cols("x", "a", "b", "c"), map[string]columnAnchor{"x": {key: "b", before: true}}, "a x b c"},
		{cols("a", "b", "c"), cols("a", "x", "b", "c"), nil, "a x b c"},
		{cols("a", "b"), cols("x", "a", "b"), nil, "x a b"},
	} {
		if got := names(stableColumns(c.prev, c.next, c.anchors)); got != c.want {
			t.Errorf("%s -> %s: %q, want %q", names(c.prev), names(c.next), got, c.want)
		}
	}
}
