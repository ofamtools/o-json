package main

import (
	"strings"
	"testing"
)

// A bracketed paste on the edited structure tree is node.paste, decided
// by paste.on-tree: json (the default) loads valid JSON as the buffer, as
// the viewer does, and adds other text as nodes (the outline's paste);
// outline always adds nodes; viewer always loads, invalid JSON an error
// with the buffer kept.
func TestPasteOnTheEditedTree(t *testing.T) {
	const doc = `{"pasted": [1, 2]}`
	for _, tc := range []struct {
		mode, text string
		loads      bool
		errText    string
	}{
		{pasteOnJSON, doc, true, ""},
		{pasteOnJSON, "fresh", false, ""},
		{pasteOnJSON, "outer\n  inner\nnext", false, ""},
		{pasteOnOutline, doc, false, ""},
		{pasteOnViewer, doc, true, ""},
		{pasteOnViewer, "fresh", false, "line 1"},
	} {
		t.Run(tc.mode+"/"+tc.text, func(t *testing.T) {
			a := open(t, nestDoc, 120, 30)
			a.Paste = nil // the paste carries the text: no clipboard read
			if err := a.r.Settings().Set(settingPasteOnTree, tc.mode); err != nil {
				t.Fatal(err)
			}
			if !a.editedShown() || a.sideOf(a.r.Focus) != rsTreeID {
				t.Fatalf("not on the edited tree: focus %q", a.r.Focus)
			}
			finish(a, Paste(a.m, tc.text))
			settle(a)
			frame := view(a)
			switch {
			case tc.loads:
				if a.Doc == nil || a.Doc.Name != noFile || a.Doc.Shape.Find(".pasted") == nil {
					t.Fatalf("did not load the buffer (status %q)\n%s", a.m.Status, frame)
				}
			case tc.errText != "":
				if !strings.Contains(a.m.Status, tc.errText) || a.Path != nestDoc {
					t.Fatalf("want error %q, buffer kept: status %q path %q", tc.errText, a.m.Status, a.Path)
				}
			default:
				if a.Path != nestDoc || !a.unsaved() || !hasKey(a, strings.TrimSpace(strings.SplitN(tc.text, "\n", 2)[0])) {
					t.Fatalf("did not add keys: path %q unsaved %v status %q\n%s", a.Path, a.unsaved(), a.m.Status, frame)
				}
			}
		})
	}
}

// hasKey reports a node of the restructured tree named key.
func hasKey(a *App, key string) bool {
	for _, n := range a.rs.tree.Nodes() {
		if n.Label == key {
			return true
		}
	}
	return false
}

// Pasted lines are keys nested by their indent, one undo step: u takes
// them all back.
func TestPastedKeysNestByIndent(t *testing.T) {
	a := open(t, nestDoc, 120, 30)
	finish(a, Paste(a.m, "outer\n  inner\nnext"))
	settle(a)
	parent := map[string]string{}
	label := map[string]string{}
	for _, n := range a.rs.tree.Nodes() {
		label[n.ID] = n.Label
		parent[n.Label] = n.Parent
	}
	if label[parent["inner"]] != "outer" || parent["next"] != parent["outer"] || parent["outer"] == "" {
		t.Fatalf("nesting: inner under %q, next beside outer %v", label[parent["inner"]], parent["next"] == parent["outer"])
	}
	if !strings.Contains(view(a), "outer") {
		t.Fatalf("frame\n%s", view(a))
	}
	press(t, a, "u")
	if hasKey(a, "outer") || hasKey(a, "inner") || hasKey(a, "next") || a.unsaved() {
		t.Fatalf("u left pasted keys (unsaved %v)", a.unsaved())
	}
}
