package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

func TestJQPanelSplitFollowsEditsAndYanks(t *testing.T) {
	a := openReshape(t, 120, 30)
	press(t, a, "g", "j")
	if !a.jqp.open || a.r.Focus != jqPanelTextID {
		t.Fatalf("g j: open %v focus %q", a.jqp.open, a.r.Focus)
	}
	frame := view(a)
	if !strings.Contains(frame, "jq program") || !strings.Contains(frame, `"title": .title`) {
		t.Fatalf("panel\n%s", frame)
	}
	// The panel keeps its height as the program grows.
	height := func() int { return strings.Count(frame[strings.Index(frame, "jq program"):], "\n") }
	h0 := height()
	// An edit beside it: the panel follows.
	press(t, a, "esc")
	if a.r.Focus == jqPanelTextID || !a.jqp.open {
		t.Fatalf("esc: focus %q open %v", a.r.Focus, a.jqp.open)
	}
	markPaths(t, a, ".people[].name", ".people[].address.city")
	press(t, a, "F")
	frame = view(a)
	if !strings.Contains(a.panelText(), "_frags") || height() != h0 {
		t.Fatalf("after flatten: %d rows (was %d)\n%s", height(), h0, frame)
	}
	if p, _ := a.plan(); a.panelText() != strings.TrimSpace(p.JQ()) {
		t.Fatal("the panel is not Plan.JQ")
	}
	// Edits in the panel are no-ops.
	press(t, a, "g", "j") // focus it again
	if !a.jqp.open || a.r.Focus != jqPanelTextID {
		t.Fatal("g j on the tree with the panel open does not focus it")
	}
	before := a.panelText()
	press(t, a, "x", "i", "z", "z")
	view(a)
	if got := string(a.jqp.in.Text); got != before {
		t.Fatalf("an edit stuck: %q", got[:min(40, len(got))])
	}
	// Y copies all of it into the registers; v then y the selection.
	if a.r.Focus != jqPanelTextID {
		t.Fatalf("focus %q before Y", a.r.Focus)
	}
	press(t, a, "Y")
	if y, err := a.r.ReadYank(""); err != nil || y.Text != before {
		t.Fatalf("Y: %v %q", err, y.Text)
	}
	press(t, a, "0", "v", "l", "l", "y")
	if y, _ := a.r.ReadYank(""); y.Text != before[:3] {
		t.Fatalf("v l l y: %q, want %q", y.Text, before[:3])
	}
	// tab shows the mapping, several lines.
	press(t, a, "tab")
	view(a)
	if !strings.HasPrefix(a.panelText(), `{"map": [`) || !strings.Contains(a.panelText(), `"then": [`) || !a.jqp.in.Multiline {
		t.Fatalf("mapping view %q", a.panelText())
	}
	if !strings.Contains(view(a), "Mapping") {
		t.Fatalf("title\n%s", view(a))
	}
	press(t, a, "q")
	if a.jqp.open || a.r.Instances[jqPanelTextID] != nil {
		t.Fatal("q left the panel")
	}
	// restructure jq still copies the program.
	if _, err := a.r.Execute(ochazuke.Request{Noun: "restructure", Verb: "jq"}); err != nil {
		t.Fatal(err)
	}
	settle(a)
	if y, _ := a.r.ReadYank(""); y.Text != before+"\n" {
		t.Fatalf("restructure.jq copied %q", y.Text)
	}
}

func TestJQPanelModalAndOff(t *testing.T) {
	a := openReshape(t, 120, 30)
	if err := a.r.Settings().Set(settingJQPanel, "modal"); err != nil {
		t.Fatal(err)
	}
	press(t, a, "g", "j")
	if a.r.TopModal() != jqPanelModalID || !strings.Contains(view(a), "jq program") {
		t.Fatalf("modal %q\n%s", a.r.TopModal(), view(a))
	}
	press(t, a, "esc")
	view(a)
	if a.r.TopModal() != "" || a.jqp.open {
		t.Fatalf("esc: modal %q", a.r.TopModal())
	}
	if err := a.r.Settings().Set(settingJQPanel, "off"); err != nil {
		t.Fatal(err)
	}
	if a.available("jq-panel", "toggle") {
		t.Fatal("jq.panel off: toggle available")
	}
}

// The panel is a read-only input (ochazuke.ReadOnly): the keys that edit
// stand aside, and neither they nor the viewer's page keys behind them
// (the page's rows answer after the panel's own, so bindings.json shadows
// p R T M n N t / W u ctrl+r and keeps w e v as the input's motions) change
// the text, the restructure, the focus or the page: split or modal, in
// normal, visual and visual-line mode, over the edited and the original
// view (where n, N and t are the viewer's).
func TestJQPanelKeysChangeNothing(t *testing.T) {
	keys := []string{"p", "P", "R", "T", "M", "n", "N", "t", "/", "W", "w", "e", "u", "ctrl+r", "x", "d", "c", "s", "i", "a", "o", "O", "A", "I", "r", "J", "~", "D", "C", "S", "K"}
	for _, where := range []string{"split", "modal"} {
		for _, original := range []bool{false, true} {
			for _, prefix := range [][]string{nil, {"v"}, {"V"}} {
				name := fmt.Sprintf("%s original=%v %s", where, original, strings.Join(prefix, ""))
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					for _, key := range keys {
						a := openReshape(t, 120, 30)
						if err := a.r.Settings().Set(settingJQPanel, where); err != nil {
							t.Fatal(err)
						}
						// An edit first, so an undo that reached the tree shows;
						// in the original view a search, so n and N have a match.
						markPaths(t, a, ".people[].name", ".people[].address.city")
						press(t, a, "F")
						if original {
							press(t, a, "T", "/", "n", "a", "m", "e", "enter")
						}
						press(t, a, "g", "j")
						press(t, a, prefix...)
						text, mapping, page, modal, side := a.panelText(), mappingKey(a.Mapping()), a.r.Page, a.r.TopModal(), a.original
						Press(a.m, key)
						status := a.m.Status
						Press(a.m, "w") // completes an operator d or c, as d w would
						status += a.m.Status
						view(a)
						switch {
						case status != "":
							t.Errorf("%s: %s", key, status)
						case a.r.Quit:
							t.Errorf("%s: quit", key)
						case !a.jqp.open || a.jqp.in == nil:
							t.Errorf("%s: the panel closed", key)
						case string(a.jqp.in.Text) != text:
							t.Errorf("%s: the text changed", key)
						case mappingKey(a.Mapping()) != mapping:
							t.Errorf("%s: the restructure changed", key)
						case a.r.Focus != jqPanelTextID:
							t.Errorf("%s: focus %q", key, a.r.Focus)
						case a.original != side:
							t.Errorf("%s: the viewer flipped", key)
						case a.r.Page != page || a.r.TopModal() != modal:
							t.Errorf("%s: page %q modal %q (was %q %q)", key, a.r.Page, a.r.TopModal(), page, modal)
						case a.r.Mode.Name == "insert" || a.r.Mode.Name == "replace":
							t.Errorf("%s: mode %q", key, a.r.Mode.Name)
						}
					}
				})
			}
		}
	}
}

// : restructure jq and : restructure mapping open what they answer in
// ochazuke's text view (TextResult): the program as it is, the mapping as
// indented JSON; restructure jq still copies the program, q closes the
// view back on the tree, and palette.text-results plain shows neither.
func TestRestructureTextResultsOpenTheTextView(t *testing.T) {
	a := openReshape(t, 120, 30)
	focus := a.r.Focus
	p, err := a.plan()
	if err != nil {
		t.Fatal(err)
	}
	press(t, a, append([]string{":"}, append(strings.Split("restructure jq", ""), "enter")...)...)
	settle(a)
	view(a)
	if a.r.TopModal() != ochazuke.TextViewID {
		t.Fatalf(": restructure jq: modal %q\n%s", a.r.TopModal(), view(a))
	}
	if in := a.r.Input(a.r.Focus); in == nil || string(in.Text) != strings.TrimRight(p.JQ(), "\n") || !in.ReadOnly {
		t.Fatalf("the text view does not hold the program read-only: %+v", in)
	}
	if y, _ := a.r.ReadYank(""); y.Text != p.JQ() {
		t.Fatalf("restructure jq copied %q", y.Text)
	}
	press(t, a, "q")
	if a.r.TopModal() != "" || a.r.Focus != focus {
		t.Fatalf("q: modal %q focus %q, want %q", a.r.TopModal(), a.r.Focus, focus)
	}
	press(t, a, append([]string{":"}, append(strings.Split("restructure mapping", ""), "enter")...)...)
	settle(a)
	if in := a.r.Input(a.r.Focus); a.r.TopModal() != ochazuke.TextViewID || in == nil || !strings.Contains(string(in.Text), "\n  \"map\": [") {
		t.Fatalf(": restructure mapping: modal %q\n%s", a.r.TopModal(), view(a))
	}
	press(t, a, "q")
	if err := a.r.Settings().Set(ochazuke.PaletteTextResultsSetting, "plain"); err != nil {
		t.Fatal(err)
	}
	press(t, a, append([]string{":"}, append(strings.Split("restructure jq", ""), "enter")...)...)
	settle(a)
	if a.r.TopModal() == ochazuke.TextViewID {
		t.Fatal("palette.text-results plain: the text view opened")
	}
}
