package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
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
	if a.Path != "" {
		docDo(t, a, "show", ochazuke.Arguments{"id": a.Path})
	}
	return a.docState()
}

// The doc noun's verbs are the viewer's document verbs under another
// name: one operation each, so parameters, confirm and history are one.
func TestDocVerbsAreTheViewerVerbs(t *testing.T) {
	a := empty(t)
	for _, al := range docVerbs {
		op, inv, ok := a.r.ResolveAction(al.Alias)
		if !ok || !inv.Alias || a.r.CanonicalKey(op.Key.Short()) != a.r.CanonicalKey(al.Action) {
			t.Fatalf("%s: op %q alias %v ok %v", al.Alias, op.Key.Short(), inv.Alias, ok)
		}
	}
	op, inv, ok := a.r.ResolveAction("doc.open")
	if !ok || inv.Alias {
		t.Fatal("doc.open must be a registered viewer operation")
	}
	if p := op.Parameters["path"]; p.Type != ochazuke.PathParameterType || p.Path != "file" {
		t.Fatalf("doc.open's path: %+v", p)
	}
}

func TestDocOpenReplacesTheDocument(t *testing.T) {
	a := empty(t)
	if s := docShow(t, a); s["open"] != false || s["path"] != "" {
		t.Fatalf("empty: %v", s)
	}
	docDo(t, a, "open", ochazuke.Arguments{"path": sample})
	if s := docShow(t, a); filepath.Base(a.Path) != filepath.Base(sample) || s["open"] != true || s["unsaved"] != false {
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
	if a.r.TopModal() == "" || filepath.Base(a.Path) != filepath.Base(sample) {
		t.Fatalf("doc.open over edits did not ask: modal %q path %q", a.r.TopModal(), a.Path)
	}
	finish(a, Press(a.m, "n"))
	settle(a)
	if filepath.Base(a.Path) != filepath.Base(sample) || len(a.Mapping().Map) == 0 {
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
	// Buffer reset stays viewer.new; doc.new now creates a real file.
	if _, err := a.r.Execute(ochazuke.Request{Noun: "viewer", Verb: "new", Target: pageID}); err != nil {
		t.Fatal(err)
	}
	if s := docShow(t, a); s["open"] != false || !strings.Contains(view(a), "No document is open.") {
		t.Fatalf("new: %v\n%s", s, view(a))
	}
}

// The viewer's nouns are thing nouns over the state they answer with: get
// reads a singleton (doc, jq, viewer, restructure), list the many (node,
// marks, value), and the verbs the tool names itself stay.
func TestViewerNounsAreThingNouns(t *testing.T) {
	a := open(t, sample, 100, 30)
	get := func(noun, verb string) any {
		t.Helper()
		v, err := a.r.Execute(ochazuke.Request{Noun: noun, Verb: verb})
		if err != nil {
			t.Fatalf("%s.%s: %v", noun, verb, err)
		}
		settle(a)
		return v
	}
	docDo(t, a, "show", ochazuke.Arguments{"id": a.Path})
	if doc, err := a.documents.get(ochazuke.Ctx{}, a.Path); err != nil || doc["file"] == nil {
		t.Fatalf("doc show %v (%v)", doc, err)
	}
	if v := get("viewer", "get"); v == nil {
		t.Fatal("viewer get answered nothing")
	}
	if v := get("jq", "get"); v == nil {
		t.Fatal("jq get answered nothing")
	}
	press(t, a, "M")
	if v := get("node", "list"); v == nil {
		t.Fatal("node list answered nothing")
	}
}
