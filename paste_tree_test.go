package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
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

// pasteDoc opens a small document on the edited tree under
// paste.text-on-tree mode.
func pasteDoc(t *testing.T, mode string) *App {
	t.Helper()
	path := filepath.Join(t.TempDir(), "paste.json")
	if err := os.WriteFile(path, []byte(`{"list": ["p"], "obj": {"k": 1}, "n": 5, "rows": [{"a": 1}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a := open(t, path, 120, 30)
	if err := a.r.Settings().Set(settingPasteText, mode); err != nil {
		t.Fatal(err)
	}
	return a
}

// pasteOn runs node.paste {text} on the restructured tree's node at
// path (by its label path).
func pasteOn(a *App, path, text string) error {
	id := ""
	for nid, p := range nodePaths(a.rs.tree.Nodes(), a.rs.meta) {
		if p == path {
			id = nid
		}
	}
	if id == "" {
		return errors.New("no node at " + path)
	}
	_, err := a.r.Execute(ochazuke.Request{Noun: "node", Verb: "paste", Target: id, Args: ochazuke.Arguments{"text": text}})
	return err
}

// docAt is the restructured document's value at path, compact.
func docAt(t *testing.T, a *App, path string) string {
	t.Helper()
	n, err := Get(a.restructuredDoc().root, path)
	if err != nil {
		return "<" + err.Error() + ">"
	}
	return Compact(n, 0)
}

// paste.text-on-tree keys (the default): the lines are keys, nested by
// indent, one undo step.
func TestPasteTextOnTreeKeys(t *testing.T) {
	a := pasteDoc(t, pasteTextKeys)
	if err := pasteOn(a, ".obj", "outer\n  inner"); err != nil {
		t.Fatal(err)
	}
	if !hasKey(a, "outer") || !hasKey(a, "inner") || len(a.rs.patches) != 0 {
		t.Fatalf("keys not added (patches %v)", a.rs.patches)
	}
	press(t, a, "u")
	if hasKey(a, "outer") || hasKey(a, "inner") || a.unsaved() {
		t.Fatal("u left pasted keys")
	}
}

// paste.text-on-tree values: each line a string value, as patches: an
// array of them under a new placeholder key inside an object (below a
// plain value), items appended to an array; one undo step; a node under
// [] (every item) refuses.
func TestPasteTextOnTreeValues(t *testing.T) {
	for _, tc := range []struct {
		node, at, want, errText string
	}{
		{".obj", ".obj", `{"k":1,"new-key":["one","two"]}`, ""},
		{".list", ".list", `["p","one","two"]`, ""},
		{".n", ".new-key", `["one","two"]`, ""},
		{".", ".new-key", `["one","two"]`, ""},
		{".rows[].a", "", "", "every item"},
	} {
		t.Run(tc.node, func(t *testing.T) {
			a := pasteDoc(t, pasteTextValues)
			before := docAt(t, a, ".")
			err := pasteOn(a, tc.node, "one\n  two\n\n")
			if tc.errText != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errText) || a.unsaved() {
					t.Fatalf("want error %q, nothing changed: %v (unsaved %v)", tc.errText, err, a.unsaved())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := docAt(t, a, tc.at); got != tc.want {
				t.Fatalf("%s = %s, want %s", tc.at, got, tc.want)
			}
			for _, s := range a.patchList() {
				if s.Orphaned {
					t.Fatalf("orphaned %+v", s)
				}
			}
			press(t, a, "u")
			if got := docAt(t, a, "."); got != before || a.unsaved() {
				t.Fatalf("u: %s, want %s (unsaved %v)", got, before, a.unsaved())
			}
		})
	}
}

// paste.text-on-tree ignore: nothing pasted, a plain notice.
func TestPasteTextOnTreeIgnore(t *testing.T) {
	a := pasteDoc(t, pasteTextIgnore)
	before := docAt(t, a, ".")
	if err := pasteOn(a, ".obj", "one\ntwo"); err != nil {
		t.Fatal(err)
	}
	if got := docAt(t, a, "."); got != before || a.unsaved() || hasKey(a, "one") {
		t.Fatalf("pasted: %s", got)
	}
	if a.m.Info != "not JSON; nothing pasted" || a.m.Status != "" {
		t.Fatalf("info %q status %q", a.m.Info, a.m.Status)
	}
	// Valid JSON still loads under paste.on-tree json.
	a.Paste = nil
	if err := pasteOn(a, ".obj", `{"loaded": true}`); err != nil {
		t.Fatal(err)
	}
	settle(a)
	if a.Doc == nil || a.Doc.Shape.Find(".loaded") == nil {
		t.Fatal("valid JSON did not load")
	}
}
