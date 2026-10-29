package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Value patches: the second layer of a restructure. The mapping maps
// shapes (every item alike); a patch changes one value of the
// restructured document, at a concrete path in it (".people[1].email").
// Patches apply after the restructure, in order, each to the document the
// ones before it left:
//
//	{"op": "set",    "path": P, "value": V}   replace the value at P
//	{"op": "delete", "path": P}               remove the field or item at P
//	{"op": "add",    "path": P, "value": V}   P names a key the object lacks: add it
//	                                          (after the key "after", else last);
//	                                          P names an index of an array: insert V there
//	{"op": "rename", "path": P, "key": K}     rename the key P ends in, in that object only
//
// The orphan rule: a patch applies only where its target is there. For
// set, delete and rename every step of P must be (a key of an object, an
// index of an array within its length); for add the container P ends in
// must be (and, for a key, not have it yet; for an index, at most its
// length); a rename whose new key the object has already is not there
// either. A patch whose target is not there is orphaned: it is kept, as it
// is, and skipped, never an error; it applies again as soon as its target
// is back (an undo, another edit). The Go apply and the jq steps (JQ)
// test the same thing, so they agree on any document.

// Patch is one value edit.
type Patch struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value,omitempty"`
	Key   string          `json:"key,omitempty"`
	After string          `json:"after,omitempty"`
	// Before places an added key just before that key (O), as After
	// places it just after (o); neither, at the object's end.
	Before string `json:"before,omitempty"`
}

// The patch ops.
const (
	opSet    = "set"
	opDelete = "delete"
	opAdd    = "add"
	opRename = "rename"
)

// steps checks a patch and parses its path: keys and indexes only (no
// [], no index from the end), a value where one is needed.
func (p Patch) steps() ([]pathStep, error) {
	steps, err := parsePath(p.Path)
	if err != nil {
		return nil, err
	}
	for _, st := range steps {
		switch {
		case st.kind == 'a':
			return nil, fmt.Errorf("path %q: a patch names one value, so it has no []", p.Path)
		case st.kind == 'i' && st.index < 0:
			return nil, fmt.Errorf("path %q: a patch counts indexes from the start", p.Path)
		}
	}
	needsValue := func() error {
		if len(p.Value) == 0 || !json.Valid(p.Value) {
			return fmt.Errorf("%s %s: \"value\" is a JSON value", p.Op, p.Path)
		}
		return nil
	}
	switch p.Op {
	case opSet:
		return steps, needsValue()
	case opDelete, opAdd, opRename:
		if len(steps) == 0 {
			return nil, fmt.Errorf("%s: the path names a field or an item, not the document", p.Op)
		}
	default:
		return nil, fmt.Errorf("op %q: a patch is set, delete, add or rename", p.Op)
	}
	last := steps[len(steps)-1]
	switch p.Op {
	case opAdd:
		if (p.After != "" || p.Before != "") && last.kind != 'k' {
			return nil, fmt.Errorf("add %s: \"after\" and \"before\" name a key, so the path ends in one", p.Path)
		}
		if p.After != "" && p.Before != "" {
			return nil, fmt.Errorf("add %s: \"after\" or \"before\", not both", p.Path)
		}
		return steps, needsValue()
	case opRename:
		if last.kind != 'k' || p.Key == "" {
			return nil, fmt.Errorf("rename %s: the path ends in a key and \"key\" is the new one", p.Path)
		}
	}
	return steps, nil
}

// valuePatch is a patch's value as a node under key.
func valuePatch(raw json.RawMessage, key string) *Node {
	n, err := Build(raw)
	if err != nil {
		return &Node{Kind: KNull, Value: "null", Key: key}
	}
	n.Key = key
	return n
}

// applyPatches applies the patches to root in order. root and its nodes
// are never changed: a patch copies the nodes along its path. orphans
// lists the patches skipped (the orphan rule above).
func applyPatches(root *Node, patches []Patch) (out *Node, orphans []int) {
	out = root
	for i, p := range patches {
		steps, err := p.steps()
		next, ok := out, false
		if err == nil {
			next, ok = patchNode(out, steps, p)
		}
		if !ok {
			orphans = append(orphans, i)
			continue
		}
		out = next
	}
	return out, orphans
}

// childAt is the index of n's child one step names, -1 when it has none.
func childAt(n *Node, st pathStep) int {
	switch {
	case st.kind == 'k' && n.Kind == KObject:
		return slices.IndexFunc(n.Children, func(c *Node) bool { return c.Key == st.key })
	case st.kind == 'i' && n.Kind == KArray && st.index >= 0 && st.index < len(n.Children):
		return st.index
	}
	return -1
}

// cloneNode is a shallow copy of n with its own children slice.
func cloneNode(n *Node) *Node {
	c := *n
	c.Children = slices.Clone(n.Children)
	return &c
}

// patchNode is n with the patch applied below it, steps the rest of its
// path, and whether its target was there.
func patchNode(n *Node, steps []pathStep, p Patch) (*Node, bool) {
	if len(steps) == 0 { // set on the document itself
		return valuePatch(p.Value, n.Key), true
	}
	if len(steps) > 1 {
		i := childAt(n, steps[0])
		if i < 0 {
			return nil, false
		}
		child, ok := patchNode(n.Children[i], steps[1:], p)
		if !ok {
			return nil, false
		}
		c := cloneNode(n)
		c.Children[i] = child
		return c, true
	}
	st := steps[0]
	i := childAt(n, st)
	c := cloneNode(n)
	switch p.Op {
	case opSet:
		if i < 0 {
			return nil, false
		}
		c.Children[i] = valuePatch(p.Value, n.Children[i].Key)
	case opDelete:
		if i < 0 {
			return nil, false
		}
		c.Children = slices.Delete(c.Children, i, i+1)
	case opRename:
		if i < 0 || p.Key != st.key && n.Field(p.Key) != nil {
			return nil, false
		}
		c.Children[i] = rekey(n.Children[i], p.Key)
	case opAdd:
		switch {
		case st.kind == 'k' && n.Kind == KObject && i < 0:
			at := len(c.Children)
			if j := slices.IndexFunc(n.Children, func(c *Node) bool { return c.Key == p.After }); p.After != "" && j >= 0 {
				at = j + 1
			}
			if j := slices.IndexFunc(n.Children, func(c *Node) bool { return c.Key == p.Before }); p.Before != "" && j >= 0 {
				at = j
			}
			c.Children = slices.Insert(c.Children, at, valuePatch(p.Value, st.key))
		case st.kind == 'i' && n.Kind == KArray && st.index <= len(n.Children):
			c.Children = slices.Insert(c.Children, st.index, valuePatch(p.Value, ""))
		default:
			return nil, false
		}
	}
	return c, true
}

// jqPathArray is steps as a jq path array: ["people",1,"email"].
func jqPathArray(steps []pathStep) string {
	parts := make([]string, len(steps))
	for i, st := range steps {
		if st.kind == 'k' {
			parts[i] = quote(st.key)
		} else {
			parts[i] = strconv.Itoa(st.index)
		}
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// jqHasPath is the jq definition the patch steps test their target with:
// every step of $p is there (a key of an object, an index of an array
// within its length), as childAt reads it.
const jqHasPath = `def _there($p): reduce $p[] as $k ([true, .]; if .[0] and (((.[1] | type) == "object" and ($k | type) == "string" and (.[1] | has($k))) or ((.[1] | type) == "array" and ($k | type) == "number" and $k >= 0 and $k < (.[1] | length))) then [true, .[1][$k]] else [false, null] end) | .[0];`

// patchesJQ is the jq steps that apply the patches (" | step ..."), and
// whether they use jqHasPath.
func patchesJQ(patches []Patch) (string, bool) {
	var b strings.Builder
	uses := false
	for _, p := range patches {
		steps, err := p.steps()
		if err != nil {
			continue // NewPlan refuses it; never reached
		}
		path := jqPathArray(steps)
		value := string(p.Value)
		if len(steps) == 0 {
			b.WriteString(" | " + value)
			continue
		}
		uses = true
		parent := jqPathArray(steps[:len(steps)-1])
		last := steps[len(steps)-1]
		var step string
		switch p.Op {
		case opSet:
			step = "if _there(" + path + ") then setpath(" + path + "; " + value + ") else . end"
		case opDelete:
			step = "if _there(" + path + ") then delpaths([" + path + "]) else . end"
		case opRename:
			step = "if _there(" + path + ") and (getpath(" + parent + ") | has(" + quote(p.Key) + ") | not) then setpath(" + parent + "; getpath(" + parent +
				") | to_entries | map(if .key == " + quote(last.key) + " then .key = " + quote(p.Key) + " else . end) | from_entries) else . end"
			if p.Key == last.key {
				step = "."
			}
		case opAdd:
			if last.kind == 'i' {
				i := strconv.Itoa(last.index)
				step = "if _there(" + parent + ") and (getpath(" + parent + ") | type == \"array\" and length >= " + i + ") then setpath(" + parent + "; getpath(" + parent +
					") | .[:" + i + "] + [" + value + "] + .[" + i + ":]) else . end"
				break
			}
			cond := "if _there(" + parent + ") and (getpath(" + parent + ") | type == \"object\" and (has(" + quote(last.key) + ") | not)) then "
			anchor, cut := p.After, "$i + 1"
			if p.Before != "" {
				anchor, cut = p.Before, "$i"
			}
			if anchor == "" {
				step = cond + "setpath(" + path + "; " + value + ") else . end"
				break
			}
			entry := "[{\"key\": " + quote(last.key) + ", \"value\": " + value + "}]"
			step = cond + "setpath(" + parent + "; getpath(" + parent + ") | to_entries | (map(.key) | index(" + quote(anchor) + ")) as $i | if $i == null then . + " + entry +
				" else .[:" + cut + "] + " + entry + " + .[" + cut + ":] end | from_entries) else . end"
		}
		b.WriteString(" | " + step)
	}
	return b.String(), uses
}

// checkPatches checks every patch of a mapping.
func checkPatches(patches []Patch) error {
	for i, p := range patches {
		if _, err := p.steps(); err != nil {
			return fmt.Errorf("patch %d: %w", i+1, err)
		}
	}
	return nil
}

// formatPatch is one patch as a line of a mapping file: op, path, then
// value, key and after where they are set.
func formatPatch(p Patch, str func(string) string) string {
	fields := []string{`"op": ` + str(p.Op), `"path": ` + str(p.Path)}
	if len(p.Value) > 0 {
		fields = append(fields, `"value": `+compactJSON(p.Value))
	}
	if p.Key != "" {
		fields = append(fields, `"key": `+str(p.Key))
	}
	if p.After != "" {
		fields = append(fields, `"after": `+str(p.After))
	}
	if p.Before != "" {
		fields = append(fields, `"before": `+str(p.Before))
	}
	return "{" + strings.Join(fields, ", ") + "}"
}

// compactJSON is a JSON value on one line, its keys in their order.
func compactJSON(raw json.RawMessage) string {
	n, err := Build(raw)
	if err != nil {
		return string(raw)
	}
	return Compact(n, 0)
}
