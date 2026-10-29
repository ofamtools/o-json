package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

// The restructure page: an editable tree over shapes whose state is a
// restructure mapping (restructure.go), shown live and runnable. Its first
// node is the new document, ". (root)", an object or (t) an array; it has
// no parent, so the edits that need one do not apply to it. It starts
// empty; s on a shape (here, or in the viewer's structure) sends the shape
// across, with the ancestors it needs to stay valid and its descendants.
// Every node keeps the shape path it came from (meta, by node id), so
// renames and moves never lose the origin: the mapping is a walk of the
// tree, {from: origin, to: the path the node now has}.
//
// The tree is ochazuke's outline (OnChange set, so u and ctrl+r undo,
// J and K move). o-json's own edits (send, indent and dedent, wrap, a new
// key, array and object, rename with a key, delete) replace the nodes and
// go on the same undo stack as one TreeAction each, holding the nodes and
// meta before and after. Indent (> >, g >, > j, > k) and dedent (< <)
// follow the JSON rules as availability; alone > and < are prefixes that
// do nothing here (the divider is ctrl+w > < =). A new key (S, o O a)
// opens with a placeholder selected: typing names it, esc keeps it.
//
// Two layouts, one mechanism, setting restructure.layout: split draws
// the original beside the restructured tree; single draws one of them,
// the one holding the focus, and tab (restructure.show) swaps them. L
// (restructure.layout) flips the setting; the page opens on the
// original either way (focusOriginal).

const (
	rsPageID = "restructure"
	rsOrigID = "restructure/original"
	rsTreeID = "restructure/new"
	// rsRootID is the restructured tree's first node: the new document
	// itself, path ".". It is an ordinary node (an object or, with t, an
	// array); what sets it apart is data, that it has no parent, so the
	// edits that need one (delete, rename, dedent, wrap, a sibling) do not
	// apply to it.
	rsRootID      = "r:root"
	settingLayout = "restructure.layout"
	settingNewKey = "restructure.new-key"
	// toneOrigin marks, on one tree, where the other tree's selected node
	// comes from or went to: the built-in queued tone, the scheme's accent
	// as the foreground (a background tone waits for the library).
	toneOrigin = ochazuke.ToneQueued
)

// nodeMeta is what o-json keeps for a restructure node beside the tree:
// the shape path its value comes from ("" for a wrapper object), whether
// it is an array's "[]" item, and built: the kind ("object" or "array")
// the node was built as once it first had children. A built node left
// with no children is an empty object in the mapping (a wrapper), not a
// copy of its origin; the origin stays for the from column and undo.
type nodeMeta struct {
	from  string
	item  bool
	built string
}

type restructureState struct {
	orig *ochazuke.Tree
	tree *ochazuke.Tree
	// origTones are the original tree's own tones by node id (a repeated
	// key), under the origin highlight.
	origTones map[string][]ochazuke.ToneSpan
	meta      map[string]nodeMeta
	seq       int
	dirty     bool
	// saved is the mapping (mappingKey) the last write or export-map
	// wrote, so viewer.new asks only about edits nothing saved.
	saved string
	// shown is the tree a single layout draws when the focus is in
	// neither: the last one that held it.
	shown string
}

func declareSettings(r *ochazuke.Runtime) error {
	return r.DeclareSetting(ochazuke.Setting{
		Name: settingLayout, Type: ochazuke.SettingString, Default: "split", Enum: []string{"split", "single"},
		Description: "The restructure page: split shows the original beside the restructured tree; single shows one, tab swaps them",
	}, ochazuke.Setting{
		Name: settingNewKey, Type: ochazuke.SettingString, Default: "new-key",
		Description: "The placeholder key a new object key gets on the restructure page (S wrapping a node, o and O): opened selected, typing replaces it; a sibling with it already gives new-key-2, -3, ...",
	})
}

// placeholder is the key a new node under parent gets: the new-key
// setting, numbered past the keys its siblings have (skip is a node that
// leaves the parent, as a wrapped one does).
func (a *App) placeholder(e edit, parent, skip string) string {
	base := strings.TrimSpace(a.r.Settings().String(settingNewKey))
	if base == "" {
		base = "new-key"
	}
	taken := map[string]bool{}
	for _, id := range e.siblings(parent) {
		if id != skip {
			taken[e.nodes[e.at(id)].Label] = true
		}
	}
	return freeKey(base, taken)
}

func (a *App) layout() string {
	if a.r.Settings().String(settingLayout) == "single" {
		return "single"
	}
	return "split"
}

// registerRestructure adds the page, its two trees, the nouns and verbs.
func (a *App) registerRestructure() error {
	r := a.r
	r.Instance(rsPageID, ochazuke.Kind("restructure"))
	cols := func(c ...ochazuke.Column) ochazuke.InstanceOption { return ochazuke.Columns(c...) }
	orig, err := r.AddTree(rsOrigID, nil, ochazuke.Parent(rsPageID), ochazuke.NoSpill(),
		cols(ochazuke.Column{Title: "path", Min: 8}, ochazuke.Column{Title: "type", Priority: 2}, ochazuke.Column{Title: "count", Align: ochazuke.AlignRight, Priority: 1}))
	if err != nil {
		return err
	}
	tree, err := r.AddTree(rsTreeID, nil, ochazuke.Parent(rsPageID), ochazuke.NoSpill(),
		cols(ochazuke.Column{Title: "key", Min: 8}, ochazuke.Column{Title: "type", Priority: 2}, ochazuke.Column{Title: "from", Priority: 1}))
	if err != nil {
		return err
	}
	tree.OnChange = func(ochazuke.TreeChange) error { a.rs.dirty = true; return nil }
	tree.RowNoun = "node"
	tree.NewID = a.newNodeID
	a.rs = restructureState{orig: orig, tree: tree, meta: map[string]nodeMeta{}, shown: rsOrigID, dirty: true}
	a.registerRestructureVerbs()
	if err := tree.SetNodes([]ochazuke.TreeNode{{ID: rsRootID, Label: ".", Noun: "node"}}); err != nil {
		return err
	}
	page, err := r.AddPage(rsPageID, "Restructure", a.renderRestructure, ochazuke.Focus(rsOrigID))
	if err != nil {
		return err
	}
	// However the page opens (M, the page picker, the CLI), it lands on
	// the original, at the shape the viewer has selected.
	page.OnOpen = func(*ochazuke.Runtime, string) { _ = a.focusOriginal() }
	return nil
}

func (a *App) newNodeID() string {
	for {
		a.rs.seq++
		id := fmt.Sprintf("r:%d", a.rs.seq)
		if a.r.Instances[id] == nil {
			return id
		}
	}
}

// setOriginal fills the page's original tree from the document's shapes
// (ids "o:PATH", so they never meet the viewer's rows).
func (a *App) setOriginal() error {
	if a.Doc == nil {
		return a.rs.orig.SetNodes(nil)
	}
	nodes, _ := shapeNodes(a.Doc.Shape, nil, "o:")
	a.rs.origTones = map[string][]ochazuke.ToneSpan{}
	for _, n := range nodes {
		a.rs.origTones[n.ID] = n.Tones
	}
	return a.rs.orig.SetNodes(nodes)
}

// --- the mapping ---

// nodePaths is each restructure node's path in the new document, from
// the labels: a node without a parent is the document, ".", and every
// path starts with a dot.
func nodePaths(nodes []ochazuke.TreeNode, meta map[string]nodeMeta) map[string]string {
	paths := map[string]string{}
	for _, n := range nodes {
		if n.Parent == "" {
			paths[n.ID] = "."
			continue
		}
		step := "[]"
		if !meta[n.ID].item {
			step = keyStep(n.Label)
		}
		paths[n.ID] = dotted(joinPath(paths[n.Parent], step)) // .[], .["odd key"], as jq writes them
	}
	return paths
}

// mappingOf is the mapping a restructure tree stands for: one entry per
// node in display order, to its path from the labels, from its origin.
// The document node is in it only with an origin (it is otherwise made
// from its keys or its item), and so is an item without one (an array
// waiting for a shape, as t leaves an empty document) only when it has
// children, where the mapping says why it does not check.
func mappingOf(nodes []ochazuke.TreeNode, meta map[string]nodeMeta) *Mapping {
	m := &Mapping{Map: []MapEntry{}}
	paths := nodePaths(nodes, meta)
	parents := map[string]bool{}
	for _, n := range nodes {
		parents[n.Parent] = true
	}
	for _, n := range nodes {
		md := meta[n.ID]
		from := md.from
		if md.built != "" && !md.item && !parents[n.ID] {
			from = "" // emptied: an empty object, not a copy of the origin
		}
		if from == "" && (n.Parent == "" || md.item && !parents[n.ID]) {
			continue
		}
		to := paths[n.ID]
		m.Map = append(m.Map, MapEntry{From: from, To: &to})
	}
	return m
}

// markBuilt records, on every node that has children, the kind it is
// built as (nodeMeta.built).
func (e edit) markBuilt() {
	for _, n := range e.nodes {
		if n.Parent == "" {
			continue
		}
		md := e.meta[n.Parent]
		md.built = "object"
		if e.hasItem(n.Parent) {
			md.built = "array"
		}
		e.meta[n.Parent] = md
	}
}

// Mapping is the page's mapping now.
func (a *App) Mapping() *Mapping { return mappingOf(a.rs.tree.Nodes(), a.rs.meta) }

// plan checks the page's mapping against the document.
func (a *App) plan() (*Plan, error) {
	if a.Doc == nil {
		return nil, errors.New("no document")
	}
	m := a.Mapping()
	if len(m.Map) == 0 {
		return nil, errors.New("the restructure is empty: s on a shape sends it here")
	}
	return NewPlan(m, a.Doc.Shape)
}

// valid reports a node list whose mapping checks (an empty one does).
func (a *App) valid(nodes []ochazuke.TreeNode, meta map[string]nodeMeta) bool {
	m := mappingOf(nodes, meta)
	if len(m.Map) == 0 || a.Doc == nil {
		return true
	}
	_, err := NewPlan(m, a.Doc.Shape)
	return err == nil
}

// mappingText is what the Mapping panel shows: the --mapping form (every
// left-out shape as a drop), or why the mapping does not check.
func (a *App) mappingText() (string, error) {
	p, err := a.plan()
	if err != nil {
		return "", err
	}
	out, err := FormatMapping(p.Complete())
	return string(out), err
}

// --- edits: pure functions over a copy of the nodes ---

type edit struct {
	nodes []ochazuke.TreeNode
	meta  map[string]nodeMeta
}

func (a *App) current() edit {
	return edit{nodes: a.rs.tree.Nodes(), meta: maps.Clone(a.rs.meta)}
}

func (e edit) at(id string) int {
	return slices.IndexFunc(e.nodes, func(n ochazuke.TreeNode) bool { return n.ID == id })
}

func (e edit) siblings(parent string) []string {
	var out []string
	for _, n := range e.nodes {
		if n.Parent == parent {
			out = append(out, n.ID)
		}
	}
	return out
}

// parent is id's parent, "" for the document node (and an unknown id).
func (e edit) parent(id string) string {
	if i := e.at(id); i >= 0 {
		return e.nodes[i].Parent
	}
	return ""
}

// root is the document node: the one without a parent.
func (e edit) root() string {
	for _, n := range e.nodes {
		if n.Parent == "" {
			return n.ID
		}
	}
	return ""
}

// itemOf is the "[]" item of the array id, "" when id holds none.
func (e edit) itemOf(id string) string {
	for _, n := range e.nodes {
		if n.Parent == id && e.meta[n.ID].item {
			return n.ID
		}
	}
	return ""
}

func (e edit) hasItem(id string) bool {
	for _, n := range e.nodes {
		if n.Parent == id && e.meta[n.ID].item {
			return true
		}
	}
	return false
}

// objectLike reports a node that can take keys: not an item, not an
// array's key, and a wrapper, a node with keys already, or one whose
// shape holds objects.
func (a *App) objectLike(e edit, id string) bool {
	md := e.meta[id]
	if md.item || e.hasItem(id) {
		return false
	}
	if md.from == "" || md.built != "" || len(e.siblings(id)) > 0 {
		return true
	}
	s := a.shapeAt(md.from)
	return s != nil && s.Kinds[KObject] > 0
}

func (a *App) shapeAt(path string) *Shape {
	if a.Doc == nil {
		return nil
	}
	return a.Doc.Shape.Find(path)
}

// span is id's subtree in the flat list, [i, end).
func (e edit) span(id string) (int, int) {
	i := e.at(id)
	if i < 0 {
		return -1, -1
	}
	end := i + 1
	for end < len(e.nodes) && e.under(e.nodes[end].ID, id) {
		end++
	}
	return i, end
}

// place moves id's subtree in the flat list: just before anchor, as
// anchor's first child (right after it) or as its last (after its
// subtree). The caller sets id's parent.
func (e edit) place(id, anchor, where string) edit {
	i, end := e.span(id)
	block := slices.Clone(e.nodes[i:end])
	rest := edit{nodes: slices.Delete(slices.Clone(e.nodes), i, end), meta: e.meta}
	var pos int
	switch where {
	case "before":
		pos = rest.at(anchor)
	case "first":
		pos = rest.at(anchor) + 1
	default:
		_, pos = rest.span(anchor)
	}
	e.nodes = slices.Insert(rest.nodes, pos, block...)
	return e
}

// indent nests id into its nearest sibling that takes keys (objectLike):
// into "previous", the nearest above, as its last child; into "next", the
// nearest below, as its first. Plain siblings between are skipped. JSON
// rule: id is not an item.
func (a *App) indent(e edit, id, into string) (edit, bool) {
	i := e.at(id)
	if i < 0 || e.meta[id].item || e.nodes[i].Parent == "" {
		return e, false
	}
	kids := e.siblings(e.nodes[i].Parent)
	pos := slices.Index(kids, id)
	step, where := -1, "last"
	if into == "next" {
		step, where = 1, "first"
	}
	for k := pos + step; k >= 0 && k < len(kids); k += step {
		if a.objectLike(e, kids[k]) {
			e.nodes = slices.Clone(e.nodes)
			e.nodes[i].Parent = kids[k]
			return e.place(id, kids[k], where), true
		}
	}
	return e, false
}

// indentAll indents ids in turn (into as indent takes it); it reports
// how many moved.
func (a *App) indentAll(e edit, ids []string, into string) (edit, int) {
	n := 0
	for _, id := range ids {
		var ok bool
		if e, ok = a.indent(e, id, into); ok {
			n++
		}
	}
	return e, n
}

// indentSubjects is what node.indent works on: the selection (else the
// node), and with "with" (next or previous) the node beside it in the
// tree too, as vim's >j and >k take this line and the next.
func (a *App) indentSubjects(e edit, ids []string, with string) []string {
	if len(ids) != 1 || with == "" {
		return ids
	}
	i := e.at(ids[0])
	switch {
	case with == "next" && i >= 0 && i+1 < len(e.nodes):
		return []string{ids[0], e.nodes[i+1].ID}
	case with == "previous" && i > 0:
		return []string{e.nodes[i-1].ID, ids[0]}
	}
	return ids
}

// dedent moves id out to its parent's level, just before the parent (so
// dedenting a parent's children one by one stacks them before it, in
// order). Out of an array item it moves out of the array (an item takes
// no siblings), just before the array's key, and collects: the node
// becomes a key holding an array of its values, a new "[]" item that
// keeps its origin (.items[].id dedented gives .id[] from .items[].id).
func (a *App) dedent(e edit, id string) (edit, bool) {
	i := e.at(id)
	if i < 0 || e.meta[id].item {
		return e, false
	}
	// Nothing goes beside the document node: a node moves out only to a
	// parent that has one.
	parent := e.nodes[i].Parent
	if e.parent(parent) == "" {
		return e, false
	}
	if !e.meta[parent].item {
		e.nodes = slices.Clone(e.nodes)
		e.nodes[i].Parent = e.parent(parent)
		return e.place(id, parent, "before"), true
	}
	array := e.parent(parent)
	if e.parent(array) == "" {
		return e, false // the document's item: there is nothing to follow
	}
	md := e.meta[id]
	if md.from == "" {
		return e, false // a wrapper has no values to collect
	}
	item := a.newNodeID()
	e.nodes = slices.Clone(e.nodes)
	e.nodes[i].Parent = e.nodes[e.at(array)].Parent
	for j := range e.nodes {
		if e.nodes[j].Parent == id {
			e.nodes[j].Parent = item
		}
	}
	e.nodes = slices.Insert(e.nodes, i+1, ochazuke.TreeNode{ID: item, Parent: id, Label: "[]", Noun: "node"})
	e.meta[item] = nodeMeta{from: md.from, item: true, built: md.built}
	e.meta[id] = nodeMeta{}
	return e.place(id, array, "before"), true
}

// remove deletes ids and their subtrees.
func (e edit) remove(ids []string) edit {
	gone := map[string]bool{}
	for _, id := range ids {
		gone[id] = true
	}
	var out []ochazuke.TreeNode
	for _, n := range e.nodes {
		if gone[n.Parent] {
			gone[n.ID] = true
		}
		if !gone[n.ID] {
			out = append(out, n)
		}
	}
	e.nodes = out
	return e
}

// wrap puts id in a new object in its place: a wrapper (no from) holding
// key, with id as its one child. JSON rule: an item is not wrapped (an
// array's item has no key). It reports the wrapper's id.
func (a *App) wrap(e edit, id, key string) (edit, string, bool) {
	i := e.at(id)
	if i < 0 || e.meta[id].item || e.nodes[i].Parent == "" {
		return e, "", false // an item has no key; the document has no place
	}
	parent := e.nodes[i].Parent
	if key == "" {
		key = a.placeholder(e, parent, id)
	}
	w := a.newNodeID()
	e.nodes = slices.Insert(slices.Clone(e.nodes), i, ochazuke.TreeNode{ID: w, Parent: parent, Label: key, Noun: "node"})
	e.nodes[i+1].Parent = w
	e.meta[w] = nodeMeta{}
	return e, w, true
}

// addKey adds an empty object key (a wrapper) below or above at, or as
// at's last child; with no at, as the document's last key. Below the
// document node is its first key (nothing goes beside it, so above it is
// refused). It reports the new node's id.
func (a *App) addKey(e edit, at, where, key string) (edit, string, error) {
	if e.at(at) < 0 {
		at, where = e.root(), "child"
	}
	i := e.at(at)
	if i < 0 {
		return e, "", errors.New("no restructure tree")
	}
	end := i + 1
	for end < len(e.nodes) && e.under(e.nodes[end].ID, at) {
		end++
	}
	var parent string
	var pos int
	switch where {
	case "above":
		parent, pos = e.nodes[i].Parent, i
	case "child":
		parent, pos = at, end
	case "below", "":
		parent, pos = e.nodes[i].Parent, end
		if parent == "" {
			parent, pos = at, i+1
		}
	default:
		return e, "", errors.New("where is below, above or child")
	}
	if parent == "" {
		return e, "", errors.New("nothing goes beside the document: a adds a key inside it")
	}
	if key == "" {
		key = a.placeholder(e, parent, "")
	}
	id := a.newNodeID()
	e.nodes = slices.Insert(slices.Clone(e.nodes), pos, ochazuke.TreeNode{ID: id, Parent: parent, Label: key, Noun: "node"})
	e.meta[id] = nodeMeta{}
	return e, id, nil
}

// under reports a node inside the subtree of anc (not anc itself).
func (e edit) under(id, anc string) bool {
	for i := e.at(id); i >= 0; {
		p := e.nodes[i].Parent
		if p == "" {
			return false
		}
		if p == anc {
			return true
		}
		i = e.at(p)
	}
	return false
}

// toArray turns a key into an array of itself: a "[]" item that takes
// the key's origin and its children, the key left a wrapper (as dedent
// collects). JSON rule: a key with an origin, not an item, not an array
// already.
//
// A node without an origin (a wrapper, the document) has no values of its
// own: empty, it becomes an array waiting for its item (s on the original
// fills the document's); holding one key that is an array, it becomes
// that array (the key goes, its item moves up). The document holding keys
// becomes an array of itself, [ {keys} ]: an item from "." holding them.
func (a *App) toArray(e edit, id string) (edit, bool) {
	i := e.at(id)
	md := e.meta[id]
	if i < 0 || md.item || e.hasItem(id) {
		return e, false
	}
	if md.from == "" {
		kids := e.siblings(id)
		switch {
		case len(kids) == 0:
			item := a.newNodeID()
			e.nodes = slices.Insert(slices.Clone(e.nodes), i+1, ochazuke.TreeNode{ID: item, Parent: id, Label: "[]", Noun: "node"})
			e.meta[item] = nodeMeta{item: true}
			return e, true
		case len(kids) == 1 && e.hasItem(kids[0]):
			key := kids[0]
			item := e.itemOf(key)
			e.nodes = slices.Clone(e.nodes)
			e.nodes[e.at(item)].Parent = id
			e.nodes = slices.Delete(e.nodes, e.at(key), e.at(key)+1)
			delete(e.meta, key)
			return e, true
		case e.nodes[i].Parent == "":
			md = nodeMeta{from: "."} // the document's origin is itself
		default:
			return e, false
		}
	}
	item := a.newNodeID()
	e.nodes = slices.Clone(e.nodes)
	for j := range e.nodes {
		if e.nodes[j].Parent == id {
			e.nodes[j].Parent = item
		}
	}
	e.nodes = slices.Insert(e.nodes, i+1, ochazuke.TreeNode{ID: item, Parent: id, Label: "[]", Noun: "node"})
	e.meta[item] = nodeMeta{from: md.from, item: true, built: md.built}
	e.meta[id] = nodeMeta{}
	return e, true
}

// arrayOf is the array a node stands for in toObject: the node itself
// when it holds an item, its parent when it is the item.
func (e edit) arrayOf(id string) string {
	if e.meta[id].item {
		if i := e.at(id); i >= 0 {
			return e.nodes[i].Parent
		}
		return ""
	}
	if e.hasItem(id) {
		return id
	}
	return ""
}

// toObject turns an array back into the key its item was: the item goes,
// its children and its origin pass to the array's key. JSON rule: an
// array that is a key (not the root, not an item).
//
// Where that does not check (an item that fans out, as .items[] does,
// cannot be one key's value) and the array is a wrapper whose item comes
// from an array's items, the array goes back under the key it came from
// (.[] from .items[] becomes .items[]): t undoes t on a node holding one
// array key.
//
// The document's own array (the top level of an array document) has no
// key to go back under: it goes under a new key, the placeholder, which
// toObjectKey reports so the verb opens it selected.
func (a *App) toObject(e edit, id string) (edit, bool) {
	e, _, ok := a.toObjectKey(e, id)
	return e, ok
}

func (a *App) toObjectKey(e edit, id string) (edit, string, bool) {
	arr := e.arrayOf(id)
	if arr == "" || e.meta[arr].item {
		return e, "", false
	}
	item := e.itemOf(arr)
	plain := edit{nodes: slices.Clone(e.nodes), meta: maps.Clone(e.meta)}
	for j := range plain.nodes {
		if plain.nodes[j].Parent == item {
			plain.nodes[j].Parent = arr
		}
	}
	from := e.meta[item].from
	if from == "." && e.parent(arr) == "" {
		from = "" // the document as its own item: back to the document
	}
	plain.meta[arr] = nodeMeta{from: from, built: e.meta[item].built}
	delete(plain.meta, item)
	plain.nodes = slices.Delete(plain.nodes, plain.at(item), plain.at(item)+1)
	if a.valid(plain.nodes, plain.meta) {
		return plain, "", true
	}
	s := a.shapeAt(e.meta[item].from)
	if s == nil || s.Key != "[]" || s.parent == nil || e.meta[arr].from != "" {
		return plain, "", true // the caller's check refuses it
	}
	label, opened := s.parent.Key, ""
	key := a.newNodeID()
	if s.parent.parent == nil {
		label, opened = a.placeholder(e, arr, ""), key
	}
	e.nodes = slices.Clone(e.nodes)
	at := e.at(item)
	e.nodes[at].Parent = key
	e.nodes = slices.Insert(e.nodes, at, ochazuke.TreeNode{ID: key, Parent: arr, Label: label, Noun: "node"})
	e.meta[key] = nodeMeta{from: s.parent.Path}
	return e, opened, true
}

// send adds shapes: each with the ancestors it needs (a node that came
// from the same shape is reused, wherever it is now) and its descendants
// (at most maxFields fields per shape, as the tree lists). It reports the
// nodes it added.
func (a *App) send(e edit, shapes []*Shape) (edit, int) {
	byFrom := map[string]string{}
	for _, n := range e.nodes {
		if f := e.meta[n.ID].from; f != "" && byFrom[f] == "" {
			byFrom[f] = n.ID
		}
	}
	added := 0
	// An array document waiting for its item takes the first shape sent:
	// the array it is in (.items[] for .items[].id), else the shape.
	root := e.root()
	if item := e.itemOf(root); item != "" && e.meta[item].from == "" && len(shapes) > 0 {
		anchor := shapes[0]
		for at := anchor; at.parent != nil; at = at.parent {
			if at.Key == "[]" {
				anchor = at
				break
			}
		}
		e.meta[item] = nodeMeta{from: anchor.Path, item: true}
		byFrom[anchor.Path] = item
		added++
	}
	var ensure func(s *Shape) string
	ensure = func(s *Shape) string {
		if s.parent == nil {
			return root
		}
		if id, ok := byFrom[s.Path]; ok {
			return id
		}
		parent := ensure(s.parent)
		id := a.newNodeID()
		e.nodes = append(e.nodes, ochazuke.TreeNode{ID: id, Parent: parent, Label: s.Key, Noun: "node"})
		e.meta[id] = nodeMeta{from: s.Path, item: s.Key == "[]"}
		byFrom[s.Path] = id
		added++
		return id
	}
	var down func(s *Shape)
	down = func(s *Shape) {
		ensure(s)
		fields := 0
		for _, c := range s.Children {
			if c.IsField() {
				if fields++; fields > maxFields {
					continue
				}
			}
			down(c)
		}
	}
	for _, s := range shapes {
		down(s)
	}
	return e, added
}

// commit makes e the tree's state and puts it on the tree's undo stack,
// with focus the node selected after undo or redo.
func (a *App) commit(label, focus string, e edit) error {
	before := a.current()
	e.markBuilt()
	set := func(to edit) func() error {
		return func() error {
			a.rs.meta, a.rs.dirty = maps.Clone(to.meta), true
			return a.rs.tree.SetNodes(to.nodes)
		}
	}
	if err := set(e)(); err != nil {
		return err
	}
	return a.rs.tree.Record(ochazuke.TreeAction{ID: focus, Label: label, Undo: set(before), Redo: set(e)})
}

// decorate gives every restructure node its cells (key, type, from) after
// an edit; the tree's own edits (undo of a delete) bring nodes back bare.
func (a *App) decorate() {
	if !a.rs.dirty {
		return
	}
	a.rs.dirty = false
	nodes := a.rs.tree.Nodes()
	e := edit{nodes: nodes, meta: a.rs.meta}
	for i, n := range nodes {
		md := a.rs.meta[n.ID]
		typ := "object"
		switch {
		case md.from != "":
			if s := a.shapeAt(md.from); s != nil {
				typ = s.Types()
				if s.Optional {
					typ += "?"
				}
			} else {
				typ = "missing"
			}
			if len(e.siblings(n.ID)) > 0 || md.built != "" && !md.item {
				typ = "object"
				if e.hasItem(n.ID) {
					typ = "array"
				}
			}
		case e.hasItem(n.ID):
			typ = "array"
		}
		label := n.Label
		switch {
		case n.Parent == "":
			label = ". (root)"
		case md.item:
			label = "[]"
		}
		nodes[i].Noun, nodes[i].Cells = "node", []string{label, typ, md.from}
	}
	_ = a.rs.tree.SetNodes(nodes)
}

// highlight marks, on one tree, the other tree's selected node: the
// shape a restructure node comes from on the original, and on the
// restructure the nodes an original shape went to. It runs every frame
// and sets a tree's nodes only when its marks change.
func (a *App) highlight() {
	var origLit string
	newLit := map[string]bool{}
	switch a.sideOf(a.r.Focus) {
	case rsTreeID:
		if n, ok := a.rs.tree.Selected(); ok {
			if f := a.rs.meta[n.ID].from; f != "" {
				origLit = "o:s:" + f
			}
		}
	case rsOrigID:
		if n, ok := a.rs.orig.Selected(); ok {
			if s, _ := n.Data.(*Shape); s != nil {
				for id, md := range a.rs.meta {
					if md.from == s.Path {
						newLit[id] = true
					}
				}
			}
		}
	}
	mark := func(t *ochazuke.Tree, base func(string) []ochazuke.ToneSpan, lit func(string) bool) {
		nodes := t.Nodes()
		changed := false
		for i, n := range nodes {
			want := base(n.ID)
			if lit(n.ID) {
				want = append(slices.Clone(want), ochazuke.CellTone(0, toneOrigin))
			}
			if !slices.Equal(n.Tones, want) {
				nodes[i].Tones, changed = want, true
			}
		}
		if changed {
			_ = t.SetNodes(nodes)
		}
	}
	mark(a.rs.orig, func(id string) []ochazuke.ToneSpan { return a.rs.origTones[id] }, func(id string) bool { return id == origLit })
	mark(a.rs.tree, func(string) []ochazuke.ToneSpan { return nil }, func(id string) bool { return newLit[id] })
}

// --- verbs ---

// focusedNode is the restructure node on a focus, "" for none.
func (a *App) focusedNode(focus string) string {
	if a.r.Tree(rsTreeID) == nil || !slices.ContainsFunc(a.rs.tree.Nodes(), func(n ochazuke.TreeNode) bool { return n.ID == focus }) {
		return ""
	}
	return focus
}

func subjectIDs(c ochazuke.Ctx) []string {
	var ids []string
	for _, s := range c.Subjects {
		ids = append(ids, s.ID)
	}
	if len(ids) == 0 && c.Instance != nil {
		ids = []string{c.Instance.ID}
	}
	return ids
}

// moved is an edit (indent or dedent) available on a node: its JSON rule
// holds and the mapping still checks (or did not before).
func (a *App) moved(id string, move func(edit, string) (edit, bool)) bool {
	if id == "" {
		return false
	}
	now := a.current()
	after, ok := move(edit{nodes: slices.Clone(now.nodes), meta: maps.Clone(now.meta)}, id)
	return ok && (a.valid(after.nodes, after.meta) || !a.valid(now.nodes, now.meta))
}

func (a *App) registerRestructureVerbs() {
	r := a.r
	r.Define("restructure")
	r.Define("node")
	params := func(key string, p map[string]ochazuke.Parameter) {
		op := r.Operations[r.CanonicalKey(key)]
		op.Parameters = p
		r.Operations[r.CanonicalKey(key)] = op
	}
	r.DefineGuard("node-indent", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		id := a.focusedNode(ev.Focus)
		if id == "" {
			return false
		}
		now := a.current()
		ids := a.indentSubjects(now, []string{id}, ochazuke.StringArg(ev.Args, "with", ""))
		after, n := a.indentAll(edit{nodes: slices.Clone(now.nodes), meta: maps.Clone(now.meta)}, ids, ochazuke.StringArg(ev.Args, "into", "previous"))
		return n > 0 && (a.valid(after.nodes, after.meta) || !a.valid(now.nodes, now.meta))
	})
	r.DefineGuard("node-dedent", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return a.moved(a.focusedNode(ev.Focus), a.dedent)
	})
	// The probe wraps with the key the real wrap gives (the placeholder),
	// so a sibling's name never decides it.
	r.DefineGuard("node-wrappable", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return a.moved(a.focusedNode(ev.Focus), func(e edit, id string) (edit, bool) {
			e, _, ok := a.wrap(e, id, "")
			return e, ok
		})
	})
	r.DefineGuard("node-to-array", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return a.moved(a.focusedNode(ev.Focus), a.toArray)
	})
	r.DefineGuard("node-to-object", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return a.moved(a.focusedNode(ev.Focus), a.toObject)
	})
	// A new key goes on the restructure tree: on a node, or the tree
	// itself while it is empty.
	r.DefineGuard("restructure-tree", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return a.sideOf(ev.Focus) == rsTreeID
	})
	// Renaming needs a node with a parent: the document node has no key
	// and is always there (deleting it neither: node-yankable, put.go).
	r.DefineGuard("node-renamable", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		id := a.focusedNode(ev.Focus)
		return id != "" && !a.rs.meta[id].item && a.current().parent(id) != ""
	})
	r.DefineGuard("shape-sendable", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		inst := r.Instances[ev.Focus]
		if inst == nil {
			return false
		}
		s, _ := inst.Data.(*Shape)
		return s != nil && s.parent != nil
	})
	r.DefineGuard("restructure-single", func(*ochazuke.Runtime, ochazuke.KeyEvent) bool { return a.layout() == "single" })

	// move runs an edit over the verb's subjects, one undo step for all.
	move := func(name string, fn func(c ochazuke.Ctx, e edit) (edit, int)) ochazuke.AppHandler {
		return func(c ochazuke.Ctx) ochazuke.Result {
			e, n := fn(c, a.current())
			if n == 0 {
				return ochazuke.Failed(fmt.Errorf("%s does not apply here", name))
			}
			focus := c.Instance.ID
			if err := a.commit(name, focus, e); err != nil {
				return ochazuke.Failed(err)
			}
			a.refocus(focus)
			return ochazuke.Done(a.Mapping())
		}
	}
	r.Define("node").
		Verb("indent", move("indent", func(c ochazuke.Ctx, e edit) (edit, int) {
			ids := a.indentSubjects(e, subjectIDs(c), ochazuke.StringArg(c.Args, "with", ""))
			return a.indentAll(e, ids, ochazuke.StringArg(c.Args, "into", "previous"))
		}), "Nest the node into the nearest sibling that takes keys (an object or a wrapper): into previous (the default), the one above, as its last key; into next, the one below, as its first. Plain siblings between are skipped. With with (next or previous) the node beside it in the tree is indented too, as vim's >j and >k take two lines").Many().When("node-indent").
		Verb("dedent", move("dedent", func(c ochazuke.Ctx, e edit) (edit, int) {
			n := 0
			for _, id := range subjectIDs(c) {
				var ok bool
				if e, ok = a.dedent(e, id); ok {
					n++
				}
			}
			return e, n
		}), "Move the node out to its parent's level, just before the parent (the parent stays, an empty object once nothing is left in it); out of an array item it collects the values into an array placed just before the array's key (.items[].id becomes .id[])").Many().When("node-dedent").
		Verb("rename", func(c ochazuke.Ctx) ochazuke.Result {
			id := c.Instance.ID
			key, ok := c.Args["key"].(string)
			if !ok {
				// No key: edit the label in place (the outline's line).
				v, err := r.ExecuteAt(c.Context, ochazuke.Request{Noun: "ochazuke.outline", Verb: "edit"})
				if err != nil {
					return ochazuke.Failed(err)
				}
				return ochazuke.Done(v)
			}
			e := a.current()
			e.nodes[e.at(id)].Label = key
			if err := a.commit("rename "+id, id, e); err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Done(a.Mapping())
		}, "Rename the node's key: in place, or to key").When("node-renamable")
	// node.delete, node.yank, node.put and the selection's delete and
	// yank: put.go.
	a.registerPutVerbs()
	// A new key (a wrapper from S, or o and O) opens selected: typing
	// renames it, esc or a motion keeps the placeholder. With key it is
	// named at once (the palette, the CLI).
	created := func(c ochazuke.Ctx, label string, e edit, id, key string) ochazuke.Result {
		if err := a.commit(label, id, e); err != nil {
			return ochazuke.Failed(err)
		}
		a.refocus(id)
		if key == "" {
			if err := a.rs.tree.Edit(id, true); err != nil {
				return ochazuke.Failed(err)
			}
		}
		return ochazuke.Done(a.Mapping())
	}
	toggle := func(name string, fn func(edit, string) (edit, bool)) ochazuke.AppHandler {
		return func(c ochazuke.Ctx) ochazuke.Result {
			id := c.Instance.ID
			e, ok := fn(a.current(), id)
			if !ok || !a.valid(e.nodes, e.meta) && a.valid(a.rs.tree.Nodes(), a.rs.meta) {
				return ochazuke.Failed(fmt.Errorf("%s does not apply here", name))
			}
			if name == "to-object" {
				id = edit{nodes: a.rs.tree.Nodes(), meta: a.rs.meta}.arrayOf(id)
			}
			if err := a.commit(name, id, e); err != nil {
				return ochazuke.Failed(err)
			}
			a.refocus(id)
			return ochazuke.Done(a.Mapping())
		}
	}
	r.Define("node").
		Verb("wrap", func(c ochazuke.Ctx) ochazuke.Result {
			key := ochazuke.StringArg(c.Args, "key", "")
			e, w, ok := a.wrap(a.current(), c.Instance.ID, key)
			if !ok || !a.valid(e.nodes, e.meta) && a.valid(a.rs.tree.Nodes(), a.rs.meta) {
				return ochazuke.Failed(errors.New("wrap does not apply here"))
			}
			return created(c, "wrap", e, w, key)
		}, "Wrap the node in a new object in its place, the new key a placeholder opened selected (typing names it)").When("node-wrappable").
		Verb("array", toggle("to-array", a.toArray), "Turn the key into an array of itself: its value, or its keys, become the array's item; the document holding keys becomes [ {keys} ]").When("node-to-array").
		Verb("object", func(c ochazuke.Ctx) ochazuke.Result {
			e, key, ok := a.toObjectKey(a.current(), c.Instance.ID)
			if key == "" || !ok || !a.valid(e.nodes, e.meta) {
				return toggle("to-object", a.toObject)(c)
			}
			return created(c, "to-object", e, key, "")
		}, "Turn an array (or its item) back into the key its item was, where the mapping still checks; an array document's own array goes under a new key, its placeholder opened selected. Not on an array whose item has many values (.items[]): a key holds one value").When("node-to-object")
	r.Define("restructure").
		Verb("add-key", func(c ochazuke.Ctx) ochazuke.Result {
			key := ochazuke.StringArg(c.Args, "key", "")
			at := a.focusedNode(c.Instance.ID)
			if at == "" {
				if n, ok := a.rs.tree.Selected(); ok {
					at = n.ID
				}
			}
			e, id, err := a.addKey(a.current(), at, ochazuke.StringArg(c.Args, "where", "below"), key)
			if err != nil {
				return ochazuke.Failed(err)
			}
			if !a.valid(e.nodes, e.meta) && a.valid(a.rs.tree.Nodes(), a.rs.meta) {
				return ochazuke.Failed(errors.New("a key does not go here: an array takes only its [] item"))
			}
			if where := ochazuke.StringArg(c.Args, "where", ""); where == "child" && at != "" {
				_ = a.rs.tree.SetCollapsed(at, false)
			}
			return created(c, "add key", e, id, key)
		}, "Add an empty object key below or above the node, or as its last child; the key is a placeholder opened selected (typing names it)").When("restructure-tree")
	params("node.rename", map[string]ochazuke.Parameter{"key": {Type: "string", Description: "the new key; omit to edit it in place"}})
	params("node.indent", map[string]ochazuke.Parameter{
		"into": {Type: "string", Enum: []string{"previous", "next"}, Description: "the sibling to nest into: the nearest above that takes keys (previous, the default) or below (next)"},
		"with": {Type: "string", Enum: []string{"next", "previous"}, Description: "also indent the node below (next) or above (previous) in the tree, as vim's >j and >k"},
	})
	params("node.wrap", map[string]ochazuke.Parameter{"key": {Type: "string", Description: "the wrapper's key; omit for a placeholder opened selected"}})
	params("restructure.add-key", map[string]ochazuke.Parameter{
		"where": {Type: "string", Enum: []string{"below", "above", "child"}},
		"key":   {Type: "string", Description: "the new key; omit for a placeholder opened selected"},
	})
	setHistory(r, ochazuke.HistoryChange, "node.indent", "node.dedent", "node.rename", "node.delete", "node.wrap", "node.array", "node.object", "restructure.add-key")

	sendShapes := func(c ochazuke.Ctx, shapes []*Shape) ochazuke.Result {
		if a.Doc == nil {
			return ochazuke.Failed(errors.New("no document"))
		}
		if len(shapes) == 0 {
			return ochazuke.Failed(errors.New("no shape to send"))
		}
		e, added := a.send(a.current(), shapes)
		if added == 0 {
			c.Info("already in the restructure")
			return ochazuke.Done(a.Mapping())
		}
		if err := a.commit("send", "", e); err != nil {
			return ochazuke.Failed(err)
		}
		say := fmt.Sprintf("sent %d node(s) to the restructure", added)
		if a.r.Page != rsPageID {
			say += " (M opens it)"
		}
		c.Info(say)
		return ochazuke.Done(a.Mapping())
	}
	r.Define("shape").
		Verb("send", func(c ochazuke.Ctx) ochazuke.Result {
			var shapes []*Shape
			subjects := c.Subjects
			if len(subjects) == 0 {
				subjects = []*ochazuke.Instance{c.Instance}
			}
			for _, inst := range subjects {
				if s, _ := inst.Data.(*Shape); s != nil && s.parent != nil {
					shapes = append(shapes, s)
				}
			}
			return sendShapes(c, shapes)
		}, "Send the shape (with the ancestors it needs and its fields) to the restructure").Many().When("shape-sendable")

	r.Define("restructure").
		Verb("open", func(c ochazuke.Ctx) ochazuke.Result {
			if err := r.OpenPage(rsPageID); err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Done(a.Mapping())
		}, "Open the restructure page focused on the original structure, on the shape the viewer has selected (else the first)").
		Verb("close", func(c ochazuke.Ctx) ochazuke.Result {
			return a.doneErr(r.OpenPage(pageID))
		}, "Go back to the viewer; the restructure stays as it is").
		Verb("show", func(c ochazuke.Ctx) ochazuke.Result {
			side := ochazuke.StringArg(c.Args, "side", "")
			if side == "" {
				side = "original"
				if a.sideOf(r.Focus) == rsOrigID {
					side = "restructured"
				}
			}
			t := a.rs.orig
			if side == "restructured" {
				t = a.rs.tree
			}
			a.rs.shown = t.ID()
			return a.doneErr(t.Focus())
		}, "Show and focus the original or the restructured tree (single layout; without side, the other one)").When("restructure-single").
		Verb("add", func(c ochazuke.Ctx) ochazuke.Result {
			path := ochazuke.StringArg(c.Args, "path", "")
			s := a.shapeAt(path)
			if s == nil || s.parent == nil {
				return ochazuke.Failed(fmt.Errorf("no shape %s", path))
			}
			return sendShapes(c, []*Shape{s})
		}, "Add the shape at a structure path, as shape.send does").
		Verb("undo", func(c ochazuke.Ctx) ochazuke.Result {
			return a.outline(c, "undo")
		}, "Undo the last restructure change").
		Verb("redo", func(c ochazuke.Ctx) ochazuke.Result {
			return a.outline(c, "redo")
		}, "Redo the last undone restructure change").
		Verb("write", func(c ochazuke.Ctx) ochazuke.Result {
			if c.Args["force"] == true {
				return a.write(c, writeForce)
			}
			return a.write(c, writeNewFile)
		}, "Write the restructured document to a new file (e.g. NAME.restructured.json); an existing file asks first, the source never. With force (W) an existing file is replaced without asking, and the source too, after one confirm (restructure.overwrite-source)").
		Verb("overwrite", func(c ochazuke.Ctx) ochazuke.Result { return a.write(c, writeReplace) },
			"Replace an existing file with the restructured document (never the source)").Confirm("Replace {{path}} with the restructured document?").
		Verb("overwrite-source", func(c ochazuke.Ctx) ochazuke.Result { return a.write(c, writeSource) },
			"Replace the source document with the restructured one (as o-json restructure --out SOURCE --overwrite does); it always asks first").Confirm("Overwrite the source {{path}} with the restructured document?").
		Verb("jq", func(c ochazuke.Ctx) ochazuke.Result {
			p, err := a.plan()
			if err != nil {
				return ochazuke.Failed(err)
			}
			return a.copyText(c, p.JQ(), "the jq program")
		}, "Copy the jq program that makes the restructured document into the registers and the system clipboard").
		Verb("export-map", func(c ochazuke.Ctx) ochazuke.Result {
			path := ochazuke.StringArg(c.Args, "path", "")
			text, err := a.mappingText()
			if err != nil {
				return ochazuke.Failed(err)
			}
			return a.writeFile(c, path, []byte(text), writeNewFile, "the mapping", "", func(string) { a.markSaved() })
		}, "Write the mapping (the --mapping form, o-json restructure --map reads it) to a new file").
		Verb("mapping", func(c ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(a.Mapping()) }, "The restructure's mapping, {map: [{from, to}]}").Fetch().
		Verb("layout", func(c ochazuke.Ctx) ochazuke.Result {
			l := ochazuke.StringArg(c.Args, "layout", "")
			if l == "" {
				l = map[string]string{"split": "single", "single": "split"}[a.layout()]
			}
			return a.doneErr(r.Settings().Set(settingLayout, l))
		}, "Set the restructure page's layout (split or single; without layout, the other one)")
	path := ochazuke.Parameter{Type: "string", Required: true, Description: "the file to write, e.g. NAME.restructured.json"}
	params("restructure.write", map[string]ochazuke.Parameter{"path": path,
		"force": {Type: "boolean", Description: "replace an existing file without asking; the source asks once (W)"}})
	params("restructure.overwrite", map[string]ochazuke.Parameter{"path": path})
	params("restructure.overwrite-source", map[string]ochazuke.Parameter{"path": path})
	params("restructure.export-map", map[string]ochazuke.Parameter{"path": {Type: "string", Required: true, Description: "the mapping file to write, e.g. NAME.map.json"}})
	params("restructure.add", map[string]ochazuke.Parameter{"path": {Type: "string", Required: true, Description: "a structure path, e.g. .items[].id"}})
	params("restructure.show", map[string]ochazuke.Parameter{"side": {Type: "string", Enum: []string{"original", "restructured"}}})
	params("restructure.layout", map[string]ochazuke.Parameter{"layout": {Type: "string", Enum: []string{"split", "single"}}})
	setHistory(r, ochazuke.HistoryAction, "restructure.open", "restructure.close", "restructure.show")
	setHistory(r, ochazuke.HistoryChange, "shape.send", "restructure.add", "restructure.undo", "restructure.redo",
		"restructure.write", "restructure.overwrite", "restructure.overwrite-source", "restructure.jq", "restructure.export-map", "restructure.layout")
}

func (a *App) doneErr(err error) ochazuke.Result {
	if err != nil {
		return ochazuke.Failed(err)
	}
	return ochazuke.Done(a.Mapping())
}

// outline runs an outline verb (undo, redo) on the restructure tree from
// anywhere on the page.
func (a *App) outline(c ochazuke.Ctx, verb string) ochazuke.Result {
	_, err := a.r.ExecuteAt(c.Context, ochazuke.Request{Noun: "ochazuke.outline", Verb: verb, Target: rsTreeID})
	return a.doneErr(err)
}

// focusOriginal puts the focus on the original tree (shown in a single
// layout), on the shape the viewer's structure has selected, else on the
// original's first shape.
func (a *App) focusOriginal() error {
	a.rs.shown = rsOrigID
	t := a.rs.orig
	nodes := t.Nodes()
	if len(nodes) == 0 {
		return nil
	}
	id := nodes[0].ID
	if n, ok := a.tree.Selected(); ok && slices.ContainsFunc(nodes, func(o ochazuke.TreeNode) bool { return o.ID == "o:"+n.ID }) {
		id = "o:" + n.ID
	}
	if err := t.Select(id); err != nil {
		return err
	}
	return t.Focus()
}

// keyFor is the key a binding row on the focus gives a verb (run without
// arguments), "" when none does.
func (a *App) keyFor(verb string) string {
	key := a.r.CanonicalKey(verb)
	for _, b := range a.r.ListBindings(a.r.Focus) {
		if b.Keys != "" && len(b.Request.Args) == 0 && a.r.CanonicalKey(b.Request.Key()) == key {
			return b.Keys
		}
	}
	return ""
}

// sideOf is the page tree a focus is in, "" for neither.
func (a *App) sideOf(focus string) string {
	for _, id := range a.r.Ancestry(focus) {
		if id == rsOrigID || id == rsTreeID {
			return id
		}
	}
	return ""
}

// refocus puts the focus back on a node after its rows were replaced,
// when the focus was in the restructure tree.
func (a *App) refocus(id string) {
	if id == "" || a.sideOf(a.r.Focus) != rsTreeID && a.r.Instances[a.r.Focus] != nil {
		return
	}
	if err := a.rs.tree.Select(id); err == nil {
		_ = a.r.SetFocus(id)
	}
}

// nextAfter is the node to select once ids are deleted: the next one
// left, else the one before.
func (a *App) nextAfter(e edit, ids []string) string {
	left := e.remove(ids)
	first := len(e.nodes)
	for _, id := range ids {
		if i := e.at(id); i >= 0 {
			first = min(first, i)
		}
	}
	for _, n := range e.nodes[min(first, len(e.nodes)):] {
		if left.at(n.ID) >= 0 {
			return n.ID
		}
	}
	for i := min(first, len(e.nodes)) - 1; i >= 0; i-- {
		if left.at(e.nodes[i].ID) >= 0 {
			return e.nodes[i].ID
		}
	}
	return ""
}

// isSource reports a path that names the document being shown.
func (a *App) isSource(path string) bool {
	if a.Path == "" || a.Path == "-" {
		return false
	}
	if x, err := os.Stat(path); err == nil {
		if y, err := os.Stat(a.Path); err == nil && os.SameFile(x, y) {
			return true
		}
	}
	p, _ := filepath.Abs(path)
	s, _ := filepath.Abs(a.Path)
	return p == s
}

// errExists is a write that found its file there.
var errExists = errors.New("exists")

// writeMode is how a write treats a file that is there.
type writeMode int

const (
	writeNewFile writeMode = iota // w: an existing file asks (restructure.overwrite), the source is refused
	writeReplace                  // restructure.overwrite: an existing file is replaced, the source refused
	writeForce                    // W: an existing file is replaced; the source asks (restructure.overwrite-source)
	writeSource                   // restructure.overwrite-source: any file, the source too (it has confirmed)
)

// write writes the restructured document as mode says.
func (a *App) write(c ochazuke.Ctx, mode writeMode) ochazuke.Result {
	path := ochazuke.StringArg(c.Args, "path", "")
	p, err := a.plan()
	if err != nil {
		return ochazuke.Failed(err)
	}
	if mode == writeForce && strings.TrimSpace(path) != "" && a.isSource(path) {
		// The source: ask once, then restructure.overwrite-source writes it.
		v, err := a.r.Execute(ochazuke.Request{Noun: "restructure", Verb: "overwrite-source", Args: ochazuke.Arguments{"path": path}})
		if err != nil {
			return ochazuke.Failed(err)
		}
		return ochazuke.Done(v)
	}
	return a.writeFile(c, path, []byte(Pretty(p.Apply(a.Doc.Root))+"\n"), mode, "the restructured document", "restructure.overwrite", func(string) { a.markSaved() })
}

// writeFile writes data to path off the loop through writeNew. The source
// only with writeSource; an existing file with writeReplace, writeForce or
// writeSource. With writeNewFile a file that exists runs the confirming
// verb ask with the path (none: the write is refused). done, when set,
// runs on the loop after a write that succeeded.
func (a *App) writeFile(c ochazuke.Ctx, path string, data []byte, mode writeMode, what, ask string, done func(path string)) ochazuke.Result {
	if strings.TrimSpace(path) == "" {
		return ochazuke.Failed(errors.New("path is required"))
	}
	if a.isSource(path) && mode != writeSource {
		return ochazuke.Failed(fmt.Errorf("%s is the source; W (restructure.write {force: true}) overwrites it after a confirm", path))
	}
	overwrite := mode != writeNewFile
	return ochazuke.Pending(func(context.Context) (any, error) {
		if _, err := os.Stat(path); err == nil && !overwrite {
			return nil, errExists
		}
		return path, writeNew(path, data, true)
	}).Then(func(c ochazuke.Ctx, _ any, err error) {
		switch {
		case errors.Is(err, errExists) && ask != "":
			// It is there: ask before replacing it.
			noun, verb, _ := strings.Cut(ask, ".")
			if _, err := a.r.Execute(ochazuke.Request{Noun: noun, Verb: verb, Args: ochazuke.Arguments{"path": path}}); err != nil {
				c.Error(err.Error())
			}
		case errors.Is(err, errExists):
			c.Error(path + " exists; o-json writes a new file")
		case err != nil:
			c.Error(err.Error())
		default:
			if done != nil {
				done(path)
			}
			c.Info("wrote " + what + " to " + path)
		}
	})
}

// --- the page ---

func (a *App) renderRestructure(r *ochazuke.Runtime, width, height int) ochazuke.Block {
	a.decorate()
	a.highlight()
	s := ochazuke.NewStyles(*a.m.Appearance)
	l := r.PaneLayout(rsPageID, ochazuke.Layout{Width: width, Height: height, Styles: s})
	crumb := s.Muted.Render(ansi.Truncate(a.restructureCrumb(), width, "…"))
	bh := max(0, height-1)
	if bh < 6 || width < 16 {
		return ochazuke.Text(crumb)
	}
	side := a.sideOf(r.Focus)
	if side != "" {
		a.rs.shown = side
	}
	treePanel := func(id string, w, h int) ochazuke.Block {
		cw, ch := ochazuke.PanelContentSize(w, h)
		t, title, body := a.rs.orig, "Original", ""
		if id == rsTreeID {
			t = a.rs.tree
			title = fmt.Sprintf("Restructured  %d nodes", len(a.rs.tree.Nodes())-1)
		}
		switch {
		case a.Doc == nil:
			r.Placed(id)
		case id == rsTreeID && len(a.rs.tree.Nodes()) == 1 && ch > 2:
			hint := wrap("empty: s on a shape of the original sends it here", cw)
			body = strings.TrimRight(t.View(cw, 1), "\n") + "\n\n" + hint
		default:
			body = t.View(cw, ch)
		}
		return l.PanelBlock(title, ochazuke.Text(body), w, h, t.List().Focused())
	}
	mapPanel := func(w, h int) ochazuke.Block {
		cw, ch := ochazuke.PanelContentSize(w, h)
		text, err := a.mappingText()
		title := "Mapping"
		if err != nil {
			title, text = "Mapping (does not check)", err.Error()
			if len(a.rs.tree.Nodes()) == 0 {
				title = "Mapping"
			}
		}
		var lines []string
		for _, ln := range strings.Split(strings.TrimRight(wrapLong(text, cw, err != nil), "\n"), "\n") {
			if len(lines) == ch {
				break
			}
			lines = append(lines, ansi.Truncate(ln, cw, "…"))
		}
		return l.PanelBlock(title, ochazuke.Text(strings.Join(lines, "\n")), w, h, false)
	}
	th := max(3, bh*3/5)
	mh := bh - th
	var body string
	if a.layout() == "single" {
		shown := a.rs.shown
		body = treePanel(shown, width, th).Text + "\n" + mapPanel(width, mh).Text
	} else {
		lw := l.Divide(width/2, width-1, ochazuke.PaneMinWidth)
		rw := width - lw - 1
		left := strings.Split(treePanel(rsOrigID, lw, bh).Text, "\n")
		right := strings.Split(treePanel(rsTreeID, rw, th).Text+"\n"+mapPanel(rw, mh).Text, "\n")
		rows := make([]string, bh)
		for i := range rows {
			rows[i] = pad(at(left, i), lw) + " " + at(right, i)
		}
		body = strings.Join(rows, "\n")
	}
	return ochazuke.Text(crumb + "\n" + body)
}

// wrapLong wraps an error to the width; mapping JSON is cut, not wrapped.
func wrapLong(text string, width int, wrapIt bool) string {
	if wrapIt {
		return wrap(text, width)
	}
	return text
}

// restructureCrumb is the line above the panels: the file, the focused
// node's new path and where it comes from, the layout and the key that
// flips it.
func (a *App) restructureCrumb() string {
	if a.Doc == nil {
		return "Restructure  ·  no document"
	}
	parts := []string{"Restructure", a.Doc.Name}
	if id := a.focusedNode(a.r.Focus); id != "" {
		e := a.current()
		part := nodePaths(e.nodes, e.meta)[id]
		switch from := e.meta[id].from; {
		case from != "":
			part += " ← " + from
		case e.parent(id) == "":
			part += " (the document)"
		default:
			part += " (new object)"
		}
		parts = append(parts, part)
	} else if n, ok := a.rs.orig.Selected(); ok && a.sideOf(a.r.Focus) == rsOrigID {
		if s, _ := n.Data.(*Shape); s != nil {
			parts = append(parts, s.Path)
			if note := s.DuplicateNote(); note != "" {
				parts = append(parts, note)
			}
		}
	}
	layout := a.layout() + " layout"
	if key := a.keyFor("restructure.layout"); key != "" {
		layout += fmt.Sprintf(" (%s: %s)", key, map[string]string{"split": "single", "single": "split"}[a.layout()])
	}
	return strings.Join(append(parts, layout), "  ·  ")
}
