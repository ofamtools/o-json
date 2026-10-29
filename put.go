package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/unixsuperhero/ochazuke"
)

// Yank, delete and put on the restructured tree, through ochazuke's
// registers (no store of o-json's own). y y (node.yank) and d d
// (node.delete) put the nodes, each with its subtree and its origin, into
// the register the next operation uses ("a d d, else the unnamed one);
// p / P (node.put) put them back. A register holds the nodes as text, a
// JSON object per yank ({"o-json.nodes": [...]}, shape nodesShape), so the
// numbered registers, "A appends and the yank history work unchanged.
//
// Identity: a put keeps a block's node ids when none of them is in the
// tree (a delete then a put is a move: the same nodes, with their origin
// and subtree); otherwise the block gets new ids (a yank then a put is a
// copy: the same shapes mapped again).
//
// A put takes the level of where it lands (putPlace). A put whose mapping
// does not check is refused with a notice (no other row answers), as
// indent's JSON rules are. A key a put lands beside a sibling of the same
// name is numbered, key-2, key-3, as the placeholder rule numbers new keys:
// two siblings with one key would not check (a to path mapped twice).
//
// V ... d and V ... y (restructure.delete, restructure.yank) act on the V
// selection as one undo step; while a selection is on, d d and y y are not
// available, so d and y answer at once.

// nodesShape is the yank shape of restructure nodes in a register.
const nodesShape = "o-json.nodes"

// clipNode is one restructure node in a register: its tree fields and its
// meta. A block's root has no parent.
type clipNode struct {
	ID     string `json:"id"`
	Parent string `json:"parent,omitempty"`
	Label  string `json:"label"`
	From   string `json:"from,omitempty"`
	Item   bool   `json:"item,omitempty"`
	Built  string `json:"built,omitempty"`
}

type clip struct {
	Nodes []clipNode `json:"o-json.nodes"`
}

// clipOf is the register text for ids: each id that is not inside another
// of them (and not the document node), with its subtree, in tree order.
func clipOf(e edit, ids []string) (string, int) {
	set := map[string]bool{}
	for _, id := range ids {
		if e.parent(id) != "" {
			set[id] = true
		}
	}
	var c clip
	blocks := 0
	for _, n := range e.nodes {
		if !set[n.ID] || slices.ContainsFunc(ids, func(o string) bool { return set[o] && e.under(n.ID, o) }) {
			continue
		}
		blocks++
		i, end := e.span(n.ID)
		for _, m := range e.nodes[i:end] {
			md := e.meta[m.ID]
			cn := clipNode{ID: m.ID, Parent: m.Parent, Label: m.Label, From: md.from, Item: md.item, Built: md.built}
			if m.ID == n.ID {
				cn.Parent = ""
			}
			c.Nodes = append(c.Nodes, cn)
		}
	}
	if blocks == 0 {
		return "", 0
	}
	b, _ := json.Marshal(c)
	return string(b) + "\n", blocks
}

// clipBlocks reads a register's text: one clip per yank ("A appends one
// after another), split into blocks (a root and its subtree).
func clipBlocks(text string) ([][]clipNode, error) {
	var blocks [][]clipNode
	dec := json.NewDecoder(strings.NewReader(text))
	for {
		var c clip
		if err := dec.Decode(&c); errors.Is(err, io.EOF) {
			break
		} else if err != nil || c.Nodes == nil {
			return nil, errors.New("the register holds no restructure nodes (y y or d d on a node fills it)")
		}
		for _, n := range c.Nodes {
			if n.Parent == "" || len(blocks) == 0 {
				blocks = append(blocks, nil)
			}
			blocks[len(blocks)-1] = append(blocks[len(blocks)-1], n)
		}
	}
	if len(blocks) == 0 {
		return nil, errors.New("the register is empty: y y or d d on a node fills it")
	}
	return blocks, nil
}

// freeKey is key, or key-2, key-3, ... past the keys taken.
func freeKey(key string, taken map[string]bool) string {
	out := key
	for n := 2; taken[out]; n++ {
		out = fmt.Sprintf("%s-%d", key, n)
	}
	return out
}

// putPlace is where a put at node at lands: the parent the nodes go
// under and their position in the flat list. On the document node, p and
// P both put its first child. On a node with children shown (an expanded
// object, array or item), p puts its first child; on a collapsed one, p
// puts the next sibling after the whole subtree; on a plain node, the next
// sibling. P puts the previous sibling everywhere but on the document.
func (e edit) putPlace(at string, before, collapsed bool) (string, int) {
	i := e.at(at)
	n := e.nodes[i]
	open := len(e.siblings(at)) > 0 && !collapsed
	switch {
	case n.Parent == "", open && !before:
		return at, i + 1
	case before:
		return n.Parent, i
	}
	_, end := e.span(at)
	return n.Parent, end
}

// put inserts blocks at pos under parent. It reports the first block's
// root, the node a put selects.
func (a *App) put(e edit, parent string, pos int, blocks [][]clipNode) (edit, string) {
	taken := map[string]bool{}
	for _, id := range e.siblings(parent) {
		if !e.meta[id].item {
			taken[e.nodes[e.at(id)].Label] = true
		}
	}
	var first string
	e.nodes = slices.Clone(e.nodes)
	for _, block := range blocks {
		ids := map[string]string{}
		keep := !slices.ContainsFunc(block, func(n clipNode) bool { return e.at(n.ID) >= 0 })
		for _, n := range block {
			ids[n.ID] = n.ID
			if !keep {
				ids[n.ID] = a.newNodeID()
			}
		}
		var add []ochazuke.TreeNode
		for _, n := range block {
			tn := ochazuke.TreeNode{ID: ids[n.ID], Parent: ids[n.Parent], Label: n.Label, Noun: "node"}
			if n.Parent == "" {
				tn.Parent = parent
				if !n.Item {
					tn.Label = freeKey(n.Label, taken)
					taken[tn.Label] = true
				}
				if first == "" {
					first = tn.ID
				}
			}
			e.meta[tn.ID] = nodeMeta{from: n.From, item: n.Item, built: n.Built}
			add = append(add, tn)
		}
		e.nodes = slices.Insert(e.nodes, pos, add...)
		pos += len(add)
	}
	return e, first
}

// check is why a node list's mapping does not check, nil when it does
// (an empty one does).
func (a *App) check(e edit) error {
	m := mappingOf(e.nodes, e.meta)
	if len(m.Map) == 0 || a.Doc == nil {
		return nil
	}
	_, err := NewPlan(m, a.Doc.Shape)
	return err
}

// selecting reports a V selection on the restructured tree.
func (a *App) selecting() bool {
	return a.rs.tree != nil && a.rs.tree.List().Visual()
}

// registerName is the register the next operation uses, for a notice.
func (a *App) registerName() string {
	if name := a.r.Registers().Active; name != "" {
		return `"` + name
	}
	return "the register"
}

// yankNodes stores ids (and their subtrees) in the register the next
// operation uses; a deletion also removes them as one undo step.
func (a *App) yankNodes(c ochazuke.Ctx, ids []string, deletion bool) ochazuke.Result {
	e := a.current()
	ids = slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return e.parent(id) == "" })
	text, blocks := clipOf(e, ids)
	if blocks == 0 {
		return ochazuke.Failed(errors.New("the document node stays; y y and d d take the nodes in it"))
	}
	where := a.registerName()
	if err := a.r.StoreYank(ochazuke.Yank{Text: text, Shape: nodesShape}, "", deletion); err != nil {
		return ochazuke.Failed(err)
	}
	if !deletion {
		c.Info(fmt.Sprintf("yanked %d node(s) into %s; p puts a copy", blocks, where))
		return ochazuke.Done(a.Mapping())
	}
	next := a.nextAfter(e, ids)
	if err := a.commit("delete", next, e.remove(ids)); err != nil {
		return ochazuke.Failed(err)
	}
	a.refocus(next)
	c.Info(fmt.Sprintf("deleted %d node(s) into %s; p puts them back, u undoes", blocks, where))
	return ochazuke.Done(a.Mapping())
}

// selected is the V selection's nodes on the restructured tree, ending
// the selection.
func (a *App) selected() []string {
	l := a.rs.tree.List()
	var ids []string
	for _, it := range l.Marked() {
		ids = append(ids, it.ID)
	}
	l.EndVisual()
	return ids
}

func (a *App) registerPutVerbs() {
	r := a.r
	r.DefineGuard("node-yankable", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		id := a.focusedNode(ev.Focus)
		return id != "" && a.current().parent(id) != "" && !a.selecting()
	})
	r.DefineGuard("node-puttable", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return a.focusedNode(ev.Focus) != "" && !a.selecting()
	})
	r.DefineGuard("restructure-selecting", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return a.sideOf(ev.Focus) == rsTreeID && a.selecting()
	})
	r.Define("node").
		Verb("delete", func(c ochazuke.Ctx) ochazuke.Result {
			return a.yankNodes(c, subjectIDs(c), true)
		}, "Delete the nodes and their children from the restructure into the register (\"a d d into a; the document node stays); p puts them back where the cursor is (a move: they keep their origin), u undoes").Many().When("node-yankable").
		Verb("yank", func(c ochazuke.Ctx) ochazuke.Result {
			return a.yankNodes(c, subjectIDs(c), false)
		}, "Yank the nodes and their children into the register (\"a y y into a); p puts a copy, the same shapes mapped again").Many().When("node-yankable").
		Verb("put", func(c ochazuke.Ctx) ochazuke.Result {
			y, err := a.r.ReadYank("")
			a.r.Registers().Active = ""
			if err != nil {
				return ochazuke.Failed(err)
			}
			blocks, err := clipBlocks(y.Text)
			if err != nil {
				c.Info(err.Error())
				return ochazuke.Done(a.Mapping())
			}
			at := c.Instance.ID
			before := c.Args["before"] == true
			now := a.current()
			parent, pos := now.putPlace(at, before, a.rs.tree.Collapsed(at))
			e, first := a.put(now, parent, pos, blocks)
			if err := a.check(e); err != nil && a.check(a.current()) == nil {
				c.Info("put does not go here: " + err.Error())
				return ochazuke.Done(a.Mapping())
			}
			if err := a.commit("put", first, e); err != nil {
				return ochazuke.Failed(err)
			}
			_ = a.rs.tree.SetCollapsed(parent, false)
			a.refocus(first)
			return ochazuke.Done(a.Mapping())
		}, "Put the register's nodes (\"a p from a) where the cursor is, at the level they land on: p below as the next sibling, or as the first child of a node whose children are shown (the document's first child on the root); P above as the previous sibling (on the root, its first child too). A put whose mapping does not check (a key into an array) is refused. Nodes that were deleted come back as they were (a move); a yank is put as a copy").When("node-puttable")
	r.Define("restructure").
		Verb("delete", func(c ochazuke.Ctx) ochazuke.Result {
			return a.yankNodes(c, a.selected(), true)
		}, "Delete the V selection on the restructured tree into the register (V ... d), as one undo step").When("restructure-selecting").
		Verb("yank", func(c ochazuke.Ctx) ochazuke.Result {
			return a.yankNodes(c, a.selected(), false)
		}, "Yank the V selection on the restructured tree into the register (V ... y)").When("restructure-selecting")
	op := r.Operations[r.CanonicalKey("node.put")]
	op.Parameters = map[string]ochazuke.Parameter{"before": {Type: "boolean", Description: "put above (P): the previous sibling; on the root, its first child"}}
	r.Operations[r.CanonicalKey("node.put")] = op
	setHistory(r, ochazuke.HistoryChange, "node.yank", "node.put", "restructure.delete", "restructure.yank")
}
