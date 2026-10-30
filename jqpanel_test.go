package main

import (
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
