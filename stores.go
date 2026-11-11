package main

import (
	"fmt"
	"strconv"

	"ofam.dev/ochazuke"
)

// Viewer facts describe transient query, structure and edit state. Their
// existing commands own mutations; document files and annotations use the
// shared writable declarations in document_store.go.

// fact is a read-only prop.
func fact(name, typ, desc string) ochazuke.PropSpec {
	if typ == "" {
		typ = "string"
	}
	return ochazuke.PropSpec{Name: name, ReadOnly: true, Parameter: ochazuke.Parameter{Type: typ, Description: desc}}
}

// factKey is a read-only key prop.
func factKey(name, desc string) ochazuke.PropSpec {
	p := fact(name, "", desc)
	p.Key, p.Human = true, true
	return p
}

func factLabel(name, desc string) ochazuke.PropSpec {
	p := fact(name, "", desc)
	p.Role = ochazuke.RoleLabel
	return p
}

// singleton declares a noun whose one thing is the state get answers.
func singleton(r *ochazuke.Runtime, noun string, props []ochazuke.PropSpec, get func() ochazuke.Thing) {
	for i := range props {
		props[i].ReadOnly = true
	}
	r.Define(noun).Props(props...).Store(ochazuke.Store{Kind: ochazuke.Singleton, Facts: true,
		Get: func(ochazuke.Ctx, string) (ochazuke.Thing, error) { return get(), nil }})
}

// collection declares a noun whose things are the rows list answers.
func collection(r *ochazuke.Runtime, noun string, props []ochazuke.PropSpec, list func() []ochazuke.Thing) {
	r.Define(noun).Props(props...).Store(ochazuke.Store{Kind: ochazuke.KindCollection, Facts: true,
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) { return list(), nil },
		Get: func(c ochazuke.Ctx, key string) (ochazuke.Thing, error) {
			for _, t := range list() {
				if fmt.Sprint(t["id"]) == key {
					return t, nil
				}
			}
			return nil, fmt.Errorf("unknown %s %q", c.Runtime.DisplayNoun(noun), key)
		}})
}

// defineStores makes the viewer's nouns thing nouns (see above). It runs
// once every verb is registered.
func (a *App) defineStores() {
	r := a.r
	singleton(r, "jq", []ochazuke.PropSpec{
		factLabel("query", "The jq query"), fact("running", "boolean", "Whether a run is in progress"), fact("error", "", "Why the query failed"),
		fact("position", "integer", "Where the error points"), fact("results", "integer", "How many results the query has"),
		fact("paths", "object", "The paths its results come from"), fact("sorted", "boolean", "Whether the results were sorted"), fact("cut", "boolean", "Whether the results were cut"),
	}, func() ochazuke.Thing { return a.queryState() })
	viewerPath := fileProp("path", "The open file")
	viewerPath.Role = ochazuke.RoleLabel
	singleton(r, "viewer", []ochazuke.PropSpec{
		viewerPath, fact("opened", "boolean", "Whether a document is open"), fact("editing", "boolean", "Whether the viewer is in EDIT"),
		fact("source", "", "Where the document came from, as it was opened: its path, stdin, or the name of a new document"),
		fact("is_new", "boolean", "Whether the document was opened for a path that named no file yet (nothing is written until a write)"),
		{Name: "document", ReadOnly: true, Association: true, Parameter: ochazuke.Parameter{Type: "string", Noun: "doc", Description: "The current document file"}},
		fileProp("loading", "The file being read, when a read is in progress"), fact("query", "", "The path search filtering the structure"), fact("kind", "", "The value type filtering the structure"),
		fact("is_raw", "boolean", "Whether the data pane shows the raw text"), fact("error", "", "Why the last read failed"), fact("shape", "", "The structure path shown in the data pane"),
		fact("size", "integer", "The document's size in bytes"), fact("row", "integer", "The data pane's selected row"), fact("view", "", "The data pane's view of an editable document"),
		fact("unsaved", "boolean", "Whether the restructure has edits no write saved"), fact("valueEdits", "integer", "How many value edits the restructure holds"), fact("orphaned", "integer", "How many value edits point at a path the document lacks"),
	}, func() ochazuke.Thing {
		t := ochazuke.Thing(a.state())
		t["opened"], t["editing"] = a.Doc != nil, a.editing
		t["is_raw"] = t["raw"] // a prop never shares a word with the viewer.raw verb
		delete(t, "raw")
		return t
	})
	singleton(r, "search", []ochazuke.PropSpec{
		fact("search_type", "", "The search line's type: substring, fuzzy, regex or jq"), factLabel("text", "The search text"), fact("hits", "integer", "How many hits"),
		fact("view", "", "The overlay shown"), fact("error", "", "Why the search failed"),
	}, func() ochazuke.Thing {
		out := ochazuke.Thing{"search_type": a.sr.typ, "text": a.queryText(), "hits": len(a.sr.hits), "view": a.overlay}
		if a.sr.err != nil {
			out["error"] = a.sr.err.Error()
		}
		return out
	})
	singleton(r, valueEditorID, []ochazuke.PropSpec{factLabel("name", "The editor's name"), fact("opened", "boolean", "Whether the value editor is open")},
		func() ochazuke.Thing {
			return ochazuke.Thing{"name": "Value editor", "opened": a.vedit != nil && a.vedit.modal != nil}
		})
	singleton(r, "restructure", []ochazuke.PropSpec{
		factLabel("name", "The restructure page's name"),
		fact("nodes", "integer", "How many nodes the restructured tree shows"), fact("marked", "integer", "How many are marked"),
		fact("edits", "integer", "How many value edits the restructure holds"),
	}, func() ochazuke.Thing {
		return ochazuke.Thing{"name": "Restructure", "nodes": len(a.rs.tree.Nodes()), "marked": len(a.markedIDs()), "edits": len(a.rs.patches)}
	})
	nodeProps := []ochazuke.PropSpec{factKey("id", "The node's id"), factLabel("label", "The node's key, or its place in an array"), fact("path", "", "Its path in the restructured document")}
	collection(r, "node", nodeProps, func() []ochazuke.Thing {
		nodes := a.rs.tree.Nodes()
		paths := nodePaths(nodes, a.rs.meta)
		out := make([]ochazuke.Thing, 0, len(nodes))
		for _, n := range nodes {
			out = append(out, ochazuke.Thing{"id": n.ID, "label": n.Label, "path": paths[n.ID]})
		}
		return out
	})
	collection(r, marksID, nodeProps, func() []ochazuke.Thing {
		nodes := a.rs.tree.Nodes()
		paths := nodePaths(nodes, a.rs.meta)
		labels := map[string]string{}
		for _, n := range nodes {
			labels[n.ID] = n.Label
		}
		out := []ochazuke.Thing{}
		for _, id := range a.markedIDs() {
			out = append(out, ochazuke.Thing{"id": id, "label": labels[id], "path": paths[id]})
		}
		return out
	})
	collection(r, "value", []ochazuke.PropSpec{
		factKey("id", "The edit's place in the order they apply, from 1"), factLabel("label", "The edit and its path"),
		fact("op", "", "What the edit does"), fact("path", "", "Where it applies"), fact("orphaned", "boolean", "Whether its path is not in the restructured document"),
	}, func() []ochazuke.Thing {
		out := []ochazuke.Thing{}
		for i, p := range a.patchList() {
			out = append(out, ochazuke.Thing{"id": strconv.Itoa(i + 1), "label": p.Op + " " + p.Path, "op": p.Op, "path": p.Path, "orphaned": p.Orphaned})
		}
		return out
	})
	for _, noun := range []string{"shape", "item", "field", "entry", "line"} {
		r.Define(noun).Props(factKey("id", "The live row's identity"), factLabel("label", "The row's displayed label"), fact("row-data", "object", "The live row's JSON data; these rows are a window into the current document")).
			Store(ochazuke.Store{Kind: ochazuke.Instances, Facts: true, Get: func(_ ochazuke.Ctx, key string) (ochazuke.Thing, error) {
				inst := r.Instances[key]
				if inst == nil {
					return nil, fmt.Errorf("no live row %q", key)
				}
				return ochazuke.Thing{"id": key, "label": inst.Label, "row-data": inst.Data}, nil
			}})
	}
	registerDocumentsForViewer(a)
}
