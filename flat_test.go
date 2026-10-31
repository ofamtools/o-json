package main

import (
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// flatDoc is a nested document: bindings[0..234], each with keys of
// 1 to 12 items, so every array pads to its own width.
func flatDoc(t *testing.T) *Node {
	t.Helper()
	d, err := ReadDoc("testdata/flat-nested.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	return d.Root
}

// leaf is the node at a jq path of the document.
func leaf(t *testing.T, root *Node, path string) *Node {
	t.Helper()
	var found *Node
	var walk func(*Node)
	walk = func(n *Node) {
		if found == nil && n.Path() == path {
			found = n
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	if found == nil {
		t.Fatalf("no node at %s", path)
	}
	return found
}

// spanText is the run of text a tone span covers.
func spanText(text string, s ochazuke.ToneSpan) string {
	r := []rune(text)
	return string(r[s.From:s.To])
}

// Each step takes its depth's tone, every step but the innermost dimmed;
// the tones cycle through the depth rows.
func TestFlatPathDepthTones(t *testing.T) {
	root := flatDoc(t)
	n := leaf(t, root, ".bindings[1].keys[0]")
	text, tones := flatPath(n, flatLook{depths: 6, pad: true})
	if text != ".bindings[  1].keys[0]" {
		t.Fatalf("path %q", text)
	}
	type run struct{ text, tone string }
	var got []run
	for _, s := range tones {
		got = append(got, run{spanText(text, s), s.Tone})
	}
	want := []run{
		{".bindings", "flat.depth.1.dim"},
		{"[  1]", "flat.depth.2.dim"},
		{".keys", "flat.depth.3.dim"},
		{"[0]", "flat.depth.4"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("tones %v, want %v", got, want)
	}
	// Two depth rows cycle: depth 3 takes row 1 again.
	_, tones = flatPath(n, flatLook{depths: 2})
	var names []string
	for _, s := range tones {
		names = append(names, s.Tone)
	}
	if want := []string{"flat.depth.1.dim", "flat.depth.2.dim", "flat.depth.1.dim", "flat.depth.2"}; !slices.Equal(names, want) {
		t.Fatalf("cycled tones %v, want %v", names, want)
	}
	// No depth rows (flat.depth-colors off): no tones.
	if _, tones := flatPath(n, flatLook{pad: true}); tones != nil {
		t.Fatalf("tones with colours off: %v", tones)
	}
}

// An index pads to its own array's last index: bindings has 235 items
// (width 3), keys of item 11 has 12 (width 2), keys of item 1 has 2.
func TestFlatPathPadsPerArray(t *testing.T) {
	root := flatDoc(t)
	for path, want := range map[string]string{
		".bindings[1].keys[0]":    ".bindings[  1].keys[0]",
		".bindings[11].keys[0]":   ".bindings[ 11].keys[ 0]",
		".bindings[11].keys[11]":  ".bindings[ 11].keys[11]",
		".bindings[234].keys[6]":  ".bindings[234].keys[6]",
		".bindings[1].verb":       ".bindings[  1].verb",
		".bindings[100].keys[4]":  ".bindings[100].keys[4]",
		".bindings[107].keys[10]": ".bindings[107].keys[10]",
	} {
		if got, _ := flatPath(leaf(t, root, path), flatLook{pad: true}); got != want {
			t.Errorf("%s drawn %q, want %q", path, got, want)
		}
		if got, _ := flatPath(leaf(t, root, path), flatLook{}); got != path {
			t.Errorf("%s unpadded drawn %q", path, got)
		}
	}
	// The lines of one array line up: every bindings[i].verb line puts
	// its " = " at the same column.
	lines := flatLines([]*Node{root}, flatLook{pad: true})
	col := -1
	for _, l := range lines {
		if !strings.HasSuffix(l.node.Path(), ".verb") {
			continue
		}
		c := strings.Index(l.drawn(), " = ")
		if col >= 0 && c != col {
			t.Fatalf("%q puts = at %d, others at %d", l.drawn(), c, col)
		}
		col = c
	}
}

// The padding is display only: y y, y p, the list's filter text (the
// row's cells) and the search line read the plain path; the drawn label
// carries the padding and the depth tones.
func TestFlatPaddingIsDisplayOnly(t *testing.T) {
	a := open(t, "testdata/flat-nested.json", 120, 30)
	press(t, a, "2", "D", "ctrl+w", "l")
	if a.viewName != viewFlat {
		t.Fatalf("view %q", a.viewName)
	}
	press(t, a, "j", "j") // .bindings[1].keys[0]
	l := a.lines[a.data.Selected()]
	if l.text != `.bindings[1].keys[0] = "k0"` || l.drawn() != `.bindings[  1].keys[0] = "k0"` {
		t.Fatalf("line text %q drawn %q", l.text, l.drawn())
	}
	if !strings.Contains(view(a), `.bindings[  1].keys[0] = "k0"`) {
		t.Fatalf("the pane does not draw the padded line:\n%s", view(a))
	}
	press(t, a, "y", "y")
	if y, _ := a.r.ReadYank(""); y.Text != `.bindings[1].keys[0] = "k0"` {
		t.Fatalf("y y yanked %q", y.Text)
	}
	press(t, a, "y", "p")
	if y, _ := a.r.ReadYank(""); y.Text != ".bindings[1].keys[0]" {
		t.Fatalf("y p yanked %q", y.Text)
	}
	item, ok := a.data.list.Selected()
	if !ok || item.Cells[0] != l.text || !strings.HasPrefix(item.Label, l.drawn()) {
		t.Fatalf("row cells %q label %q", item.Cells, item.Label)
	}
	if len(item.Tones) == 0 || item.Tones[len(l.tones)-1].Tone != "flat.depth.4" {
		t.Fatalf("row tones %v", item.Tones)
	}
	// The list's filter matches the plain "[1]".
	m := ochazuke.NewMatcher(ochazuke.MatchSubstring, ochazuke.SearchCaseSmart, "[1].keys[0]")
	if !m.Match(item.Cells[0]) {
		t.Fatal("the filter text does not hold [1]")
	}
	// The search line's hits are plain paths too.
	hits, _ := searchHits(a.Doc.Root, ochazuke.NewMatcher(ochazuke.MatchSubstring, ochazuke.SearchCaseSmart, "v1"))
	if len(hits) == 0 || !strings.HasPrefix(hits[0].text, ".bindings[1].verb") {
		t.Fatalf("search hits %q", lineTexts(hits))
	}
}

// The settings turn each part off, and a change redraws the shown lines
// with the cursor kept.
func TestFlatSettingsOptOut(t *testing.T) {
	a := open(t, "testdata/flat-nested.json", 120, 30)
	press(t, a, "2", "D", "ctrl+w", "l", "j", "j")
	if a.flatDrawn.depths != len(flatDepthHues) || !a.flatDrawn.pad {
		t.Fatalf("default look %+v", a.flatDrawn)
	}
	for _, name := range []string{settingFlatPadIndexes, settingFlatDepthColors} {
		if err := a.r.Settings().Set(name, false); err != nil {
			t.Fatal(err)
		}
	}
	view(a)
	if a.data.Selected() != 2 {
		t.Fatalf("cursor moved to %d", a.data.Selected())
	}
	l := a.lines[2]
	if l.shown != "" || l.tones != nil {
		t.Fatalf("off still draws %q %v", l.shown, l.tones)
	}
	if !strings.Contains(view(a), `.bindings[1].keys[0] = "k0"`) {
		t.Fatalf("the pane:\n%s", view(a))
	}
}

// Every built-in depth row has a dim twin, and the count follows the
// rows: a theme may add more.
func TestFlatDepthRowsDeclared(t *testing.T) {
	a := open(t, "testdata/sample.json", 80, 20)
	rows := a.m.Appearance.AppTones.Rows()
	for i := range flatDepthHues {
		name := flatDepthTone + string(rune('1'+i))
		inner, ok := rows[name]
		outer, ok2 := rows[name+".dim"]
		if !ok || !ok2 || inner.Dim || !outer.Dim || inner.Fg != outer.Fg || inner.Fg.Light == "" || inner.Fg.Dark == "" {
			t.Fatalf("%s: %+v %+v", name, inner, outer)
		}
	}
	if n := a.flatDepths(); n != len(flatDepthHues) {
		t.Fatalf("depths %d", n)
	}
	a.m.Appearance.SetTone(flatDepthTone+"7", ochazuke.ToneStyle{Fg: ochazuke.SchemeColor{Light: "#000000", Dark: "#FFFFFF"}})
	if n := a.flatDepths(); n != 7 {
		t.Fatalf("depths with a 7th row %d", n)
	}
}
