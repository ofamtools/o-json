package main

import (
	"context"
	"strings"
	"sync/atomic"

	"github.com/unixsuperhero/ochazuke"
)

// Path completion in the query line, opt-in (query.complete-paths, default
// off: by the owner's decision "." just types a dot). On, typing "." (at
// the start, or right after | ( ) or , ) offers the field keys of the shape the query reads at
// that point (jqContextAt): after ".items[] | ." the keys of .items[], and
// after ".owner." in that stage the keys of .items[].owner. An array field
// is offered as itself and with [] (each item). Accepting replaces the
// token after the ".", as one undoable edit. Off, the line has no trigger
// at all (applyCompletePaths), so "." opens nothing, not even an empty
// list.
//
// The search runs off the loop, so it reads a snapshot: the shape of the
// document the query runs on, stored while the line is drawn focused
// (shapes are never changed once inferred).

const settingCompletePaths = "query.complete-paths"

func declareCompleteSettings(r *ochazuke.Runtime) error {
	return declareOwn(r, ochazuke.Setting{
		Name: settingCompletePaths, Type: ochazuke.SettingBoolean, Default: false,
		Description: "On, typing . in the jq query line offers the field keys of the shape the query reads there (after | the stage's input, after .a. the keys of .a); off (the default), . just types a dot and nothing opens",
	})
}

// applyCompletePaths gives the query line its "." trigger while
// query.complete-paths is on, and none while it is off.
func (a *App) applyCompletePaths() {
	if a.q.in == nil {
		return
	}
	a.q.in.Completions = nil
	if a.r.Settings().Bool(settingCompletePaths) {
		a.q.in.Completions = []ochazuke.CompletionTrigger{a.pathTrigger()}
	}
}

// completion is the query line's completion snapshot.
type completion struct {
	shape atomic.Pointer[Shape]  // nil: nothing to offer
	cases atomic.Pointer[string] // search.case, read on the loop
}

// pathTrigger is the query line's "." trigger.
func (a *App) pathTrigger() ochazuke.CompletionTrigger {
	return ochazuke.CompletionTrigger{Trigger: ".", Boundaries: "|(),", SearchAt: func(_ context.Context, q ochazuke.CompletionQuery) ([]ochazuke.Suggestion, error) {
		mode := ochazuke.SearchCaseSmart
		if m := a.qc.cases.Load(); m != nil {
			mode = *m
		}
		return pathSuggestions(a.qc.shape.Load(), mode, q), nil
	}}
}

// snapshotShape stores the shape the query runs on for the search, or
// nil when completion is off.
func (a *App) snapshotShape() {
	var s *Shape
	switch {
	case a.Doc == nil || !a.r.Settings().Bool(settingCompletePaths):
	case a.editedShown():
		s = a.shownDoc().shape
	default:
		s = a.Doc.Shape
	}
	a.qc.shape.Store(s)
	mode := a.r.SearchCase()
	a.qc.cases.Store(&mode)
}

// pathSuggestions are the keys under the path the token q names, read
// in root (the document's shape): the term's input at the token, then the
// token's steps up to its last "."; a key matches the rest from its start,
// case as search.case (cases) says for that rest.
func pathSuggestions(root *Shape, cases string, q ochazuke.CompletionQuery) []ochazuke.Suggestion {
	if root == nil {
		return nil
	}
	text := []rune(q.Text)
	start := min(max(q.Token.Start, 0), len(text))
	base := jqContextAt(text, start).input
	if base == "" {
		return nil // the query built the value here: no shape to read
	}
	head, prefix := "", q.Query
	if i := strings.LastIndex(q.Query, "."); i >= 0 {
		head, prefix = q.Query[:i], q.Query[i+1:]
	}
	at := base
	if head != "" {
		at = joinJQ(base, normPath("."+head))
	}
	s := root.Find(at)
	if s == nil {
		return nil
	}
	lead := ""
	if head != "" {
		lead = head + "."
	}
	prefix = strings.Trim(prefix, `"`)
	m := ochazuke.CaseFor(cases, prefix)
	var out []ochazuke.Suggestion
	for _, c := range s.Children {
		if !c.IsField() || !m.HasPrefix(c.Key, prefix) {
			continue
		}
		key := strings.TrimPrefix(keyStep(c.Key), ".")
		if strings.HasPrefix(key, "[") {
			key = quote(c.Key) // ."odd key" reads as .["odd key"]
		}
		detail := c.Types() + " " + c.Presence()
		out = append(out, ochazuke.Suggestion{Text: lead + key, Label: key, Detail: detail})
		if c.items != nil {
			out = append(out, ochazuke.Suggestion{Text: lead + key + "[]", Label: key + "[]", Detail: "each item"})
		}
	}
	return out
}
