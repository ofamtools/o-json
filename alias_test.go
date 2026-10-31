package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ofam.dev/ochazuke"
)

// o-json's alias rows are bare words with preset args: w writes what w
// writes (the edited document while it is shown), w! replaces an existing
// file, wq and x write then quit, e opens, q asks with edits pending, q!
// quits without asking.
func TestAliases(t *testing.T) {
	for _, al := range aliases {
		a := open(t, sample, 120, 30)
		alias, found := a.r.ActionAlias(al.Alias)
		if !found || !alias.Bare || alias.Action != a.r.CanonicalKey(al.Action) {
			t.Errorf("alias %s: %+v (found %v)", al.Alias, alias, found)
		}
	}
	dir := t.TempDir()
	do := func(a *App, verb string, args ochazuke.Arguments) error {
		_, err := a.r.Execute(ochazuke.Request{Verb: verb, Args: args})
		settle(a)
		return err
	}
	// w on the edited view writes the restructured document.
	a := open(t, sample, 120, 30)
	a.on(t, ".meta.count")
	press(t, a, "<", "<")
	out := filepath.Join(dir, "edited.json")
	if err := do(a, "w", ochazuke.Arguments{"path": out}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); !strings.Contains(string(b), `"count"`) || a.unsaved() {
		t.Fatalf("w wrote %q, unsaved %v (%q)", b, a.unsaved(), a.m.Status)
	}
	// w again: it exists, so it asks; w! replaces it without asking.
	press(t, a, "<", "<")
	if err := do(a, "w", ochazuke.Arguments{"path": out}); err != nil || a.r.TopModal() == "" {
		t.Fatalf("w over an existing file did not ask: %v", err)
	}
	press(t, a, "n")
	if err := do(a, "w!", ochazuke.Arguments{"path": out}); err != nil || a.r.TopModal() != "" || a.unsaved() {
		t.Fatalf("w! asked or did not write: %v modal %q", err, a.r.TopModal())
	}
	// w and w! with no path: the API names it (path is required); the
	// palette asks for it in the args form, w! keeping its force.
	for _, word := range []string{"w", "w!"} {
		if err := do(a, word, nil); err == nil || !strings.Contains(err.Error(), "path is required") || a.r.TopModal() != "" {
			t.Fatalf("%s with no path: %v modal %q", word, err, a.r.TopModal())
		}
	}
	// :w! with no path opens the path chooser at once (ochazuke's
	// forms.single-path), keeping the preset force: picking the existing
	// file replaces it without asking.
	press(t, a, "<", "<")
	press(t, a, ":")
	for _, ch := range "w!" {
		press(t, a, string(ch))
	}
	press(t, a, "enter")
	if a.r.TopModal() != "path-chooser" || a.r.Focus != "path-chooser/query" {
		t.Fatalf(":w! with no path: modal %q focus %q", a.r.TopModal(), a.r.Focus)
	}
	for range 2 { // browse, recent, then typed
		finish(a, Press(a.m, "tab"))
		settle(a)
	}
	finish(a, Type(a.m, out))
	for i := 0; i < 20 && strings.Contains(view(a), "loading"); i++ {
		settle(a)
		time.Sleep(20 * time.Millisecond)
	}
	finish(a, Press(a.m, "enter"))
	settle(a)
	if a.r.TopModal() != "" || a.unsaved() {
		t.Fatalf(":w! picked lost its force: modal %q unsaved %v status %q", a.r.TopModal(), a.unsaved(), a.m.Status)
	}
	// wq and x write, then quit.
	for _, word := range []string{"wq", "x"} {
		a := open(t, sample, 120, 30)
		a.on(t, ".meta.count")
		press(t, a, "<", "<")
		if err := do(a, word, ochazuke.Arguments{"path": filepath.Join(dir, word+".json")}); err != nil {
			t.Fatal(err)
		}
		if !a.r.Quit {
			t.Fatalf("%s did not quit (%q)", word, a.m.Status)
		}
	}
	// wq over an existing file quits once the confirm writes it.
	a = open(t, sample, 120, 30)
	a.on(t, ".meta.count")
	press(t, a, "<", "<")
	if err := do(a, "wq", ochazuke.Arguments{"path": out}); err != nil || a.r.Quit {
		t.Fatalf("wq over an existing file: %v quit %v", err, a.r.Quit)
	}
	if a.r.TopModal() == "" {
		t.Fatalf("wq over an existing file did not ask: %q", a.m.Status)
	}
	finish(a, Press(a.m, "y"))
	if !a.r.Quit {
		t.Fatalf("wq did not quit after the confirmed write: info %q status %q modal %q unsaved %v", a.m.Info, a.m.Status, a.r.TopModal(), a.unsaved())
	}
	// q asks with edits pending; q! quits.
	a = open(t, sample, 120, 30)
	a.on(t, ".meta.count")
	press(t, a, "<", "<")
	if err := do(a, "q", nil); a.r.Quit || a.r.TopModal() == "" {
		t.Fatalf("q did not ask: %v", err)
	}
	press(t, a, "n")
	if err := do(a, "q!", nil); err != nil || !a.r.Quit {
		t.Fatalf("q! did not quit: %v", err)
	}
	// e opens a file.
	a = open(t, sample, 120, 30)
	if err := do(a, "e", ochazuke.Arguments{"path": "testdata/duplicate-keys.json"}); err != nil {
		t.Fatal(err)
	}
	if a.Path != "testdata/duplicate-keys.json" {
		t.Fatalf("e opened %q", a.Path)
	}
}
