package main

import (
	"encoding/json"
	"io"
	"testing"

	"github.com/unixsuperhero/ochazuke"
	ocli "github.com/unixsuperhero/ochazuke/cli"
)

// frameworkOperations are the operations ochazuke registers on its own for
// the parts o-json builds on (the runtime, the model, a tree): every other
// operation is a verb o-json defines.
func frameworkOperations() map[string]bool {
	r := ochazuke.NewRuntime()
	ochazuke.NewModel(r)
	r.Instance("page", ochazuke.Kind("viewer"))
	if _, err := r.AddTree("tree", nil, ochazuke.Parent("page")); err != nil {
		panic(err)
	}
	out := map[string]bool{}
	for key := range r.Operations {
		out[key] = true
	}
	return out
}

// Every verb o-json defines declares its history kind (Operation.HistoryKind)
// rather than leaving it to the default, in the viewer and in the catalog, and
// the catalog's history is that kind. A new verb without one fails here.
func TestEveryVerbDeclaresItsHistory(t *testing.T) {
	framework := frameworkOperations()
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for key, op := range a.r.Operations {
		if framework[key] {
			continue
		}
		n++
		if op.HistoryKind == "" {
			t.Errorf("viewer verb %s declares no history kind", key)
		}
	}
	if n == 0 {
		t.Fatal("the viewer defines no verbs")
	}

	var doc ocli.CatalogDoc
	if err := json.Unmarshal([]byte(run(t, "", "catalog")), &doc); err != nil {
		t.Fatal(err)
	}
	ops := newShell(nil, io.Discard, io.Discard).Runtime.Operations
	n = 0
	for _, noun := range doc.Nouns {
		for _, v := range noun.Verbs {
			key := noun.Name + "." + v.Name
			if framework[key] {
				continue
			}
			n++
			if v.History == "" || ops[key].HistoryKind == "" || v.History != ops[key].HistoryKind {
				t.Errorf("catalog verb %s: history %q, declared %q", key, v.History, ops[key].HistoryKind)
			}
		}
	}
	if n != 4 {
		t.Fatalf("the catalog has %d o-json verbs, want the doc noun's 4", n)
	}
}

// A spot check of the kinds: reads are fetches, yanks changes, view moves
// actions.
func TestVerbHistoryKinds(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	ops := newShell(nil, io.Discard, io.Discard).Runtime.Operations
	for key, want := range map[string]string{
		"doc.structure": ochazuke.HistoryFetch, "doc.table": ochazuke.HistoryFetch, "doc.get": ochazuke.HistoryFetch, "doc.rows": ochazuke.HistoryFetch,
	} {
		if got := ops[key].HistoryKind; got != want {
			t.Errorf("%s: %q, want %q", key, got, want)
		}
	}
	for key, want := range map[string]string{
		"viewer.state": ochazuke.HistoryFetch, "viewer.load": ochazuke.HistoryFetch,
		"shape.yank": ochazuke.HistoryChange, "item.yank": ochazuke.HistoryChange,
		"rows.move": ochazuke.HistoryAction, "viewer.search": ochazuke.HistoryAction, "entry.open": ochazuke.HistoryAction, "query.commit": ochazuke.HistoryAction,
	} {
		if got := a.r.Operations[key].HistoryKind; got != want {
			t.Errorf("%s: %q, want %q", key, got, want)
		}
	}
}
