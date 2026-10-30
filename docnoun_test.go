package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

func docDo(t *testing.T, a *App, verb string, args ochazuke.Arguments) any {
	t.Helper()
	v, err := a.r.Execute(ochazuke.Request{Noun: "doc", Verb: verb, Target: pageID, Args: args})
	if err != nil {
		t.Fatalf("doc.%s: %v", verb, err)
	}
	settle(a)
	return v
}

// docShow is doc.show's answer, read after the request settles.
func docShow(t *testing.T, a *App) map[string]any {
	t.Helper()
	docDo(t, a, "show", nil)
	return a.docState()
}

// The doc noun's verbs are the viewer's document verbs under another
// name: one operation each, so parameters, confirm and history are one.
func TestDocVerbsAreTheViewerVerbs(t *testing.T) {
	a := empty(t)
	for _, al := range docVerbs {
		op, inv, ok := a.r.ResolveAction(al.Alias)
		if !ok || !inv.Alias || a.r.CanonicalKey(op.Key()) != a.r.CanonicalKey(al.Action) {
			t.Fatalf("%s: op %q alias %v ok %v", al.Alias, op.Key(), inv.Alias, ok)
		}
	}
	op, _, _ := a.r.ResolveAction("doc.open")
	if p := op.Parameters["path"]; p.Type != ochazuke.PathParameterType || !p.Required {
		t.Fatalf("doc.open's path: %+v", p)
	}
}

func TestDocOpenReplacesTheDocument(t *testing.T) {
	a := empty(t)
	if s := docShow(t, a); s["open"] != false || s["path"] != "" {
		t.Fatalf("empty: %v", s)
	}
	docDo(t, a, "open", ochazuke.Arguments{"path": sample})
	if s := docShow(t, a); a.Path != sample || s["open"] != true || s["path"] != sample || s["unsaved"] != false {
		t.Fatalf("open: %v err %v", s, a.LoadErr)
	}
	other := filepath.Join(t.TempDir(), "other.json")
	if err := os.WriteFile(other, []byte(`{"other": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Edits no write saved: doc.open asks first, and n keeps them and
	// the document.
	press(t, a, "M")
	cleared(t, a) // the page opens on the copy; build from nothing
	sendPaths(t, a, ".name")
	press(t, a, "q")
	docDo(t, a, "open", ochazuke.Arguments{"path": other})
	if a.r.TopModal() == "" || a.Path != sample {
		t.Fatalf("doc.open over edits did not ask: modal %q path %q", a.r.TopModal(), a.Path)
	}
	finish(a, Press(a.m, "n"))
	settle(a)
	if a.Path != sample || len(a.Mapping().Map) == 0 {
		t.Fatalf("n: path %q map %v", a.Path, a.Mapping().Map)
	}
	docDo(t, a, "open", ochazuke.Arguments{"path": other})
	finish(a, Press(a.m, "y"))
	settle(a)
	if a.Path != other || !strings.Contains(view(a), "other") {
		t.Fatalf("y: path %q\n%s", a.Path, view(a))
	}
	// doc.reload reads it again.
	if err := os.WriteFile(other, []byte(`{"reloaded": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	docDo(t, a, "reload", nil)
	if !strings.Contains(view(a), "reloaded") {
		t.Fatalf("reload:\n%s", view(a))
	}
	// doc.new empties it: the data pane says no document is open.
	docDo(t, a, "new", nil)
	if s := docShow(t, a); s["open"] != false || !strings.Contains(view(a), "No document is open.") {
		t.Fatalf("new: %v\n%s", s, view(a))
	}
}

// The empty document's data pane names the keys from the bindings, and
// empty.hint off leaves it an empty list.
func TestEmptyHint(t *testing.T) {
	a := empty(t)
	frame := view(a)
	for _, want := range []string{"No document is open.", "e     open a JSON file (doc.open)", "p     paste JSON"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("no %q\n%s", want, frame)
		}
	}
	if err := a.r.Settings().Set(settingEmptyHint, false); err != nil {
		t.Fatal(err)
	}
	if frame := view(a); strings.Contains(frame, "No document is open.") {
		t.Fatalf("empty.hint off:\n%s", frame)
	}
}
