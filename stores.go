package main

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"ofam.dev/ochazuke"
)

// The nouns of o-json that held a verb named like a core verb (doc get,
// jq set, marks list, node and value delete, value and viewer edit,
// value-editor set, restructure delete) are thing nouns: each has a facts
// store over the state it already answers with, so get (and list, where
// there are many) read it, and create, set, delete, archive and edit are on
// the noun, unavailable as a system fact. The verbs the tool registers under
// a core name stay and win over the generated ones.

// fact is a read-only prop.
func fact(name, typ, desc string) ochazuke.PropSpec {
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
	singleton(r, "doc", []ochazuke.PropSpec{
		fact("path", "", "The open file (empty for none, or a document of no file)"), fact("opened", "boolean", "Whether a document is open"),
		fact("unsaved", "boolean", "Whether the restructure has edits no write saved"), fact("name", "", "The document's name"),
		fact("size", "integer", "Its size in bytes"), fact("loading", "", "The file being read, when a read is in progress"), fact("error", "", "Why the last read failed"),
	}, func() ochazuke.Thing {
		t := ochazuke.Thing(a.docState())
		t["opened"] = t["open"]
		delete(t, "open")
		return t
	})
	singleton(r, "jq", []ochazuke.PropSpec{
		fact("query", "", "The jq query"), fact("running", "boolean", "Whether a run is in progress"), fact("error", "", "Why the query failed"),
		fact("position", "integer", "Where the error points"), fact("results", "integer", "How many results the query has"),
		fact("paths", "object", "The paths its results come from"), fact("sorted", "boolean", "Whether the results were sorted"), fact("cut", "boolean", "Whether the results were cut"),
	}, func() ochazuke.Thing { return a.queryState() })
	singleton(r, "viewer", []ochazuke.PropSpec{
		fact("path", "", "The open file"), fact("opened", "boolean", "Whether a document is open"), fact("editing", "boolean", "Whether the viewer is in EDIT"),
		fact("loading", "", "The file being read, when a read is in progress"), fact("query", "", "The path search filtering the structure"), fact("kind", "", "The value type filtering the structure"),
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
		fact("search_type", "", "The search line's type: substring, fuzzy, regex or jq"), fact("text", "", "The search text"), fact("hits", "integer", "How many hits"),
		fact("view", "", "The overlay shown"), fact("error", "", "Why the search failed"),
	}, func() ochazuke.Thing {
		out := ochazuke.Thing{"search_type": a.sr.typ, "text": a.queryText(), "hits": len(a.sr.hits), "view": a.overlay}
		if a.sr.err != nil {
			out["error"] = a.sr.err.Error()
		}
		return out
	})
	singleton(r, valueEditorID, []ochazuke.PropSpec{fact("opened", "boolean", "Whether the value editor is open")},
		func() ochazuke.Thing { return ochazuke.Thing{"opened": a.vedit != nil && a.vedit.modal != nil} })
	singleton(r, "restructure", []ochazuke.PropSpec{
		fact("nodes", "integer", "How many nodes the restructured tree shows"), fact("marked", "integer", "How many are marked"),
		fact("edits", "integer", "How many value edits the restructure holds"),
	}, func() ochazuke.Thing {
		return ochazuke.Thing{"nodes": len(a.rs.tree.Nodes()), "marked": len(a.markedIDs()), "edits": len(a.rs.patches)}
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
}

// defineDocStore makes the command line's doc noun a thing noun: a document
// is named by its file, so get reads one (its name and size) and there is
// no list. doc get FILE PATH, the value at a path, stays.
func defineDocStore(r *ochazuke.Runtime, stdin io.Reader) {
	r.Define("doc").Props(
		factKey("file", "The JSON file; - reads stdin"), factLabel("name", "The document's name"), fact("size", "integer", "Its size in bytes"),
	).Store(ochazuke.Store{Kind: ochazuke.KindCollection, Facts: true, Remote: true,
		Missing: map[string]string{"list": "a document is named by its file; there is no list of them"},
		Get: func(c ochazuke.Ctx, file string) (ochazuke.Thing, error) {
			v, err := c.Await(ochazuke.StoreCall{Store: "doc", Op: "get", Key: file}, func() ochazuke.Result {
				return ochazuke.Pending(func(ctx context.Context) (any, error) {
					d, err := ReadDoc(file, stdin)
					if err != nil {
						return nil, err
					}
					return ochazuke.Thing{"file": file, "name": d.Name, "size": d.Size}, nil
				})
			}, 0)
			if err != nil {
				return nil, err
			}
			return v.(ochazuke.Thing), nil
		}})
}
