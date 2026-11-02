package main

import (
	"testing"

	"ofam.dev/ochazuke"
)

func TestJSONSearchCoexistsWithTextFinder(t *testing.T) {
	a := openQuery(t, sample)
	for _, key := range []string{"search.focus", "search.next", "ochazuke.search.find", "ochazuke.search.next"} {
		if _, ok := a.r.Operation(key); !ok {
			t.Fatalf("missing action %s", key)
		}
	}
	press(t, a, ":")
	Type(a.m, "find")
	press(t, a, "enter")
	if a.r.Focus != jqFieldID {
		t.Fatalf(":find focused %q, want the JSON search line", a.r.Focus)
	}
}

func closeActiveModal(t *testing.T, a *App) {
	t.Helper()
	if _, err := a.r.Execute(ochazuke.Request{Noun: "ochazuke.modal", Verb: "close"}); err != nil {
		t.Fatal(err)
	}
	if a.r.TopModal() != "" {
		t.Fatalf("modal %q remained open", a.r.TopModal())
	}
}

func TestSharedCloseCancelsJSONPrompt(t *testing.T) {
	a := open(t, sample, 120, 30)
	committed := false
	for range 2 {
		if err := a.prompt("Search", "", func(string) error { committed = true; return nil }); err != nil {
			t.Fatal(err)
		}
		closeActiveModal(t, a)
		if committed || a.ask != nil {
			t.Fatalf("close left prompt state: committed=%v ask=%v", committed, a.ask != nil)
		}
	}
}

func TestSharedCloseCancelsValueEdit(t *testing.T) {
	a := open(t, sample, 120, 30)
	onData(t, a, ".items", ".items[1]")
	press(t, a, "l", "l", "r")
	typeText(t, a, "unsaved")
	closeActiveModal(t, a)
	if a.vedit != nil || len(a.rs.patches) != 0 || field(t, a, ".items[1].name") != `"beta"` {
		t.Fatalf("close did not cancel the value edit: active=%v patches=%d name=%q", a.vedit != nil, len(a.rs.patches), field(t, a, ".items[1].name"))
	}
}

func TestSharedCloseClosesJQPanel(t *testing.T) {
	a := openReshape(t, 120, 30)
	if err := a.r.Settings().Set(settingJQPanel, "modal"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		press(t, a, "g", "j")
		if a.r.TopModal() != jqPanelModalID {
			t.Fatal("jq panel did not open")
		}
		closeActiveModal(t, a)
		if a.jqp.open || a.jqp.in != nil {
			t.Fatal("close left the jq panel open")
		}
	}
}
