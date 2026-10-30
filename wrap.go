package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/unixsuperhero/ochazuke"
)

// The wrap family on the restructured tree: node.wrap {as} and its
// inverse, node.unwrap. In visual-line { wraps the selection in a new
// object and [ in a new array of their values (node.wrap {as: array});
// } and ] lift a wrapper's keys, or an array's items, into its parent at
// its place (node.unwrap). S stays node.wrap's own key.
//
// An object wrap and an object unwrap are tree edits (the mapping says
// them). An array of several fields' values, and an array's items as
// keys, are not shapes a mapping can say: they are reshape steps
// (values on the new object, lift on the array), the tree closing into
// them as the tree edit runs, so each stays one undo step.

const (
	wrapObject = "object"
	wrapArray  = "array"
)

// newKeyBase is the restructure.new-key setting, new-key when it is empty.
func (a *App) newKeyBase() string {
	base := strings.TrimSpace(a.r.Settings().String(settingNewKey))
	if base == "" {
		base = "new-key"
	}
	return base
}

// nodeAt is the node of e at path (nodePaths), "" for none.
func (e edit) nodeAt(path string) string {
	paths := nodePaths(e.nodes, e.meta)
	for _, n := range e.nodes {
		if paths[n.ID] == path && !e.meta[n.ID].item {
			return n.ID
		}
	}
	return ""
}

// wrapAsArray turns the new objects ws of the wrap e into arrays of their
// values (the values reshape step on each), as one undo step from the
// tree as it is now. With key "" and one new array, its key opens
// selected. It tells which keys went.
func (a *App) wrapAsArray(c ochazuke.Ctx, e edit, ws []string, key string) ochazuke.Result {
	paths := nodePaths(e.nodes, e.meta)
	var steps []Stage
	var dropped []string
	for _, w := range ws {
		steps = append(steps, Stage{Op: opValues, Path: paths[w]})
		for _, k := range e.siblings(w) {
			dropped = append(dropped, e.nodes[e.at(k)].Label)
		}
	}
	_, copied, err := a.reshapeFrom(e, steps, "wrap as array")
	if err != nil {
		return ochazuke.Failed(err)
	}
	id := copied.nodeAt(paths[ws[0]])
	a.refocus(id)
	if key == "" && len(ws) == 1 && id != "" {
		if err := a.rs.tree.Edit(id, true); err != nil {
			return ochazuke.Failed(err)
		}
	}
	c.Info(fmt.Sprintf("an array of %d value(s); their keys went (%s): { then g e keeps them as entries; u undoes", len(dropped), strings.Join(dropped, ", ")))
	return ochazuke.Done(a.Mapping())
}

// unwrapObject lifts the keys of id (an object: a wrapper, or a key with
// keys) into its parent at its place; id goes. It refuses the document
// node, a node with no keys, and a key the parent has already.
func (a *App) unwrapObject(e edit, id string) (edit, error) {
	i := e.at(id)
	if i < 0 {
		return e, errors.New("no node to unwrap")
	}
	parent := e.nodes[i].Parent
	label := e.nodes[i].Label
	if parent == "" {
		return e, errors.New("the document has no parent to lift its keys into")
	}
	kids := e.siblings(id)
	if len(kids) == 0 {
		return e, fmt.Errorf("%s has no keys here to lift", label)
	}
	taken := map[string]bool{}
	for _, s := range e.siblings(parent) {
		if s != id {
			taken[e.nodes[e.at(s)].Label] = true
		}
	}
	for _, k := range kids {
		if l := e.nodes[e.at(k)].Label; taken[l] {
			return e, fmt.Errorf("unwrapping %s would repeat the key %s beside it: rename one first", label, l)
		}
	}
	e.nodes = slices.Clone(e.nodes)
	for _, k := range kids {
		e.nodes[e.at(k)].Parent = parent
	}
	e.nodes = slices.Delete(e.nodes, i, i+1)
	delete(e.meta, id)
	return e, nil
}

// unwrapNodes is node.unwrap over ids: each object's keys lift into its
// parent (a tree edit), and at most one array's items lift as numbered
// keys (the lift reshape step). One undo step for all.
func (a *App) unwrapNodes(c ochazuke.Ctx, ids []string) ochazuke.Result {
	now := a.current()
	var tops []string
	for _, id := range now.tops(ids) {
		if now.meta[id].item { // an array's item stands for its array
			id = now.parent(id)
		}
		if id != "" && now.parent(id) != "" && !slices.Contains(tops, id) {
			tops = append(tops, id)
		}
	}
	if len(tops) == 0 {
		return ochazuke.Failed(errors.New("the document has no parent to lift its keys into"))
	}
	var arrays, objects []string
	for _, id := range tops {
		if now.hasItem(id) {
			arrays = append(arrays, id)
		} else {
			objects = append(objects, id)
		}
	}
	if len(arrays) > 1 {
		return ochazuke.Failed(errors.New("unwrap lifts one array at a time: its items become numbered keys"))
	}
	e := edit{nodes: slices.Clone(now.nodes), meta: now.meta}
	focus, lifted := "", 0
	for _, id := range objects {
		kids := e.siblings(id)
		var err error
		if e, err = a.unwrapObject(e, id); err != nil {
			return ochazuke.Failed(err)
		}
		if focus == "" {
			focus = kids[0]
		}
		lifted += len(kids)
	}
	if !a.valid(e.nodes, e.meta) && a.valid(now.nodes, now.meta) {
		return ochazuke.Failed(fmt.Errorf("unwrap does not apply here: %v", a.check(e)))
	}
	if len(arrays) == 0 {
		if err := a.commit("unwrap", focus, e); err != nil {
			return ochazuke.Failed(err)
		}
		a.refocus(focus)
		c.Info(fmt.Sprintf("lifted %d key(s) out of %d object(s); u undoes", lifted, len(objects)))
		return ochazuke.Done(a.Mapping())
	}
	arr := arrays[0]
	prefix := a.newKeyBase()
	parent := e.parent(arr)
	for _, s := range e.siblings(parent) {
		if l := e.nodes[e.at(s)].Label; s != arr && strings.HasPrefix(l, prefix+"-") && isDigits(strings.TrimPrefix(l, prefix+"-")) {
			return ochazuke.Failed(fmt.Errorf("the items would be %s-1, %s-2, ... and %s is there already: rename it, or set %s", prefix, prefix, l, settingNewKey))
		}
	}
	paths := nodePaths(e.nodes, e.meta)
	path, at := paths[arr], paths[parent]
	sps, copied, err := a.reshapeFrom(e, []Stage{{Op: opLift, Path: path, Prefix: prefix}}, "unwrap")
	if err != nil {
		return ochazuke.Failed(err)
	}
	id := copied.nodeAt(dotted(joinPath(at, keyStep(prefix+"-1"))))
	if id == "" {
		id = copied.nodeAt(at)
	}
	a.refocus(id)
	c.Info(stageName(sps[0].st) + " (" + settingNewKey + "); u undoes")
	return ochazuke.Done(a.Mapping())
}

func isDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}
