package main

// RAW's diff (Design D): the text the editor gives back is compared with
// the value that was sent, and the difference becomes value patches
// addressed by path, applied in order as one undo step. A changed scalar
// is one set; a key the text lost is a delete, a key it gained an add
// beside its neighbour (after the key before it, else before the key
// after it); an array keeps its common head and tail, its items between
// are compared pairwise, and the rest are added or deleted at their
// index. An object whose kept keys changed order is replaced whole (one
// set at its path): there is no reorder patch, and the set keeps the
// text's order.

import (
	"encoding/json"
	"slices"
	"strconv"
)

// childPath is the path of a child of the value at path.
func childPath(path, step string) string {
	if path == "." {
		return step
	}
	return path + step
}

// rawJSON is n as compact JSON for a patch's value.
func rawJSON(n *Node) json.RawMessage { return json.RawMessage(Compact(n, 0)) }

// diffPatches is the patches that turn old (at path) into new.
func diffPatches(old, new *Node, path string) []Patch {
	if Compact(old, 0) == Compact(new, 0) {
		return nil
	}
	switch {
	case old.Kind == KObject && new.Kind == KObject:
		return diffObject(old, new, path)
	case old.Kind == KArray && new.Kind == KArray:
		return diffArray(old, new, path)
	}
	return []Patch{{Op: opSet, Path: path, Value: rawJSON(new)}}
}

func diffObject(old, new *Node, path string) []Patch {
	kept := func(of, in *Node) []string {
		var keys []string
		for _, c := range of.Children {
			if in.Field(c.Key) != nil {
				keys = append(keys, c.Key)
			}
		}
		return keys
	}
	if !slices.Equal(kept(old, new), kept(new, old)) {
		return []Patch{{Op: opSet, Path: path, Value: rawJSON(new)}}
	}
	var out []Patch
	for _, c := range old.Children {
		if new.Field(c.Key) == nil {
			out = append(out, Patch{Op: opDelete, Path: childPath(path, keyStep(c.Key))})
		}
	}
	for _, c := range new.Children {
		if o := old.Field(c.Key); o != nil {
			out = append(out, diffPatches(o, c, childPath(path, keyStep(c.Key)))...)
		}
	}
	for i, c := range new.Children {
		if old.Field(c.Key) != nil {
			continue
		}
		p := Patch{Op: opAdd, Path: childPath(path, keyStep(c.Key)), Value: rawJSON(c)}
		if i > 0 {
			p.After = new.Children[i-1].Key // there already: kept, or added just before
		} else if j := slices.IndexFunc(new.Children, func(n *Node) bool { return old.Field(n.Key) != nil }); j >= 0 {
			p.Before = new.Children[j].Key
		}
		out = append(out, p)
	}
	return out
}

func diffArray(old, new *Node, path string) []Patch {
	same := func(a, b *Node) bool { return Compact(a, 0) == Compact(b, 0) }
	lo, ln := len(old.Children), len(new.Children)
	head := 0
	for head < lo && head < ln && same(old.Children[head], new.Children[head]) {
		head++
	}
	tail := 0
	for tail < lo-head && tail < ln-head && same(old.Children[lo-1-tail], new.Children[ln-1-tail]) {
		tail++
	}
	om, nm := lo-head-tail, ln-head-tail
	pairs := min(om, nm)
	at := func(i int) string { return childPath(path, "["+strconv.Itoa(i)+"]") }
	var out []Patch
	for i := head; i < head+pairs; i++ {
		out = append(out, diffPatches(old.Children[i], new.Children[i], at(i))...)
	}
	for i := head + pairs; i < head+nm; i++ {
		out = append(out, Patch{Op: opAdd, Path: at(i), Value: rawJSON(new.Children[i])})
	}
	for range om - pairs {
		out = append(out, Patch{Op: opDelete, Path: at(head + pairs)})
	}
	return out
}

func pluralES(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}
