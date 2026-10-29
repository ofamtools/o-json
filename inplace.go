package main

import (
	"errors"
	"fmt"
	"slices"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

// Editing the structure in place. The viewer's structure pane is the
// restructure editor: the restructured tree of the restructure page
// (rsTreeID), the one restructure state, drawn in the viewer as the
// structure pane and on the restructure page beside the original. The
// tree instance is lent to the page that is open (placeTree), so its keys,
// its undo stack and its selection are the same on both.
//
// A document opens with the state a copy of its shape (identity): every
// shape a node mapped to itself, so the output is the input. Edits stay in
// memory until w writes (the write rules of the restructure page); u and
// ctrl+r undo and redo. The data pane follows the restructured document
// (restructuredDoc), so it shifts as the structure does; T
// (restructure.show) flips the structure pane and the data pane between
// the original and the edited document. The setting structure.editable
// (default on) off keeps the viewer read-only, as it was: the original
// tree only, and the restructure page alone edits.
//
// The restructure page starts empty (the owner's side-by-side style):
// opened on a state with no edits, it empties it as one undo step;
// opened on in-place edits, it shows them. Back in the viewer with
// nothing done there, the copy comes back (that step undone).

const settingEditable = "structure.editable"

func declareEditSettings(r *ochazuke.Runtime) error {
	return r.DeclareSetting(ochazuke.Setting{
		Name: settingEditable, Type: ochazuke.SettingBoolean, Default: true,
		Description: "The viewer's structure pane is the restructure editor, a copy of the document's shape (T shows the original); off keeps the viewer read-only and the restructure page (M) alone edits. Takes effect on the next document",
	})
}

// editable reports the structure.editable setting.
func (a *App) editable() bool { return a.r.Settings().Bool(settingEditable) }

// editedShown reports the viewer showing the edited document: the
// restructured tree in the structure pane and its data in the data pane.
func (a *App) editedShown() bool { return a.editable() && !a.original && a.Doc != nil }

// identity is the restructure that copies the document: the document
// node and one node per shape, each mapped to itself. A shape is expanded
// only when every value is an object or every value is an array, and it
// lists at most maxFields fields: anything else is one node that copies
// its values, so the output is always the input.
func (a *App) identity() edit {
	e := edit{nodes: []ochazuke.TreeNode{{ID: rsRootID, Label: ".", Noun: "node"}}, meta: map[string]nodeMeta{}}
	if a.Doc == nil {
		return e
	}
	expands := func(s *Shape) bool {
		fields := 0
		for _, c := range s.Children {
			if c.IsField() {
				fields++
			}
		}
		return len(s.Children) > 0 && fields <= maxFields && (objectOnly(s) || s.Kinds[KArray] == s.Count)
	}
	root := a.Doc.Shape
	if !expands(root) {
		e.meta[rsRootID] = nodeMeta{from: "."}
		return e
	}
	var add func(s *Shape, parent string)
	add = func(s *Shape, parent string) {
		id := a.newNodeID()
		e.nodes = append(e.nodes, ochazuke.TreeNode{ID: id, Parent: parent, Label: s.Key, Noun: "node"})
		e.meta[id] = nodeMeta{from: s.Path, item: s.Key == "[]"}
		if expands(s) {
			for _, c := range s.Children {
				add(c, id)
			}
		}
	}
	for _, c := range root.Children {
		add(c, rsRootID)
	}
	return e
}

// startState makes the restructure state the one a new document starts
// with: a copy of its shape (structure.editable), as one undo step so u
// brings back what it replaced. It is the base unsaved compares with.
func (a *App) startState() error {
	a.rs.startedEmpty = false
	if !a.editable() {
		a.rs.patches = nil
		a.rs.base = mappingKey(&Mapping{Map: []MapEntry{}})
		return nil
	}
	a.original = false
	e := a.identity()
	if err := a.commitWith("open", rsRootID, e, nil); err != nil {
		return err
	}
	a.rs.base, a.rs.saved = mappingKey(a.Mapping()), ""
	// A big structure opens collapsed below the second level, as the
	// original tree does.
	if len(e.nodes) > bigStructure {
		depth := map[string]int{}
		parents := map[string]bool{}
		for _, n := range e.nodes {
			parents[n.Parent] = true
		}
		for _, n := range e.nodes {
			if n.Parent != "" {
				depth[n.ID] = depth[n.Parent] + 1
			}
			if depth[n.ID] >= 2 && parents[n.ID] {
				if err := a.rs.tree.SetCollapsed(n.ID, true); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// edited reports a restructure state other than the one the document
// started with.
func (a *App) edited() bool { return mappingKey(a.Mapping()) != a.rs.base }

// placeTree lends the restructured tree to a page: it is drawn, focused
// and keyed there (a focus must sit inside the open page).
func (a *App) placeTree(page string) {
	if inst := a.r.Instances[rsTreeID]; inst != nil {
		inst.Parent = page
	}
}

// openedViewer runs when the viewer opens: the restructured tree comes
// back to it, and a restructure page that started empty and was left
// empty gives the copy back (its start undone).
func (a *App) openedViewer() {
	a.placeTree(pageID)
	if a.rs.startedEmpty && len(a.Mapping().Map) == 0 {
		_, _ = a.r.Execute(ochazuke.Request{Noun: "ochazuke.outline", Verb: "undo", Target: rsTreeID})
	}
	a.rs.startedEmpty = false
	// The page keeps the focus it had; one on the page itself or on the
	// hidden original moves to the structure pane shown.
	if a.editedShown() && (a.r.Focus == pageID || slices.Contains(a.r.Ancestry(a.r.Focus), treeID)) {
		_ = a.rs.tree.Focus()
	}
}

// openedRestructure runs when the restructure page opens: the tree comes
// to it, empty when the state has no edits (structure.editable), and the
// focus goes to the original.
func (a *App) openedRestructure() {
	want := a.viewerShape()
	a.placeTree(rsPageID)
	if a.editable() && a.Doc != nil && !a.edited() && len(a.rs.tree.Nodes()) > 1 {
		root := edit{nodes: []ochazuke.TreeNode{{ID: rsRootID, Label: ".", Noun: "node"}}, meta: map[string]nodeMeta{}}
		if a.commit("start empty", rsRootID, root) == nil {
			a.rs.startedEmpty = true
		}
	}
	_ = a.focusOriginal(want)
}

// restructured is the restructured document the edited view shows,
// built again when the mapping changes.
type restructured struct {
	key     string
	root    *Node
	shape   *Shape
	err     error
	orphans []int // the value edits skipped (patch.go)
}

// restructuredDoc is the document the state makes, as w writes it,
// parsed again so its values have their own paths.
func (a *App) restructuredDoc() *restructured {
	key := mappingKey(a.Mapping())
	if a.ed != nil && a.ed.key == key && a.ed.doc == a.Doc {
		return &a.ed.restructured
	}
	out := restructured{key: key}
	if p, err := a.plan(); err != nil {
		out.err = err
	} else {
		v, orphans := p.ApplyAll(a.Doc.Root)
		if root, err := Build([]byte(Compact(v, 0))); err != nil {
			out.err = err
		} else {
			out.root, out.shape, out.orphans = root, Infer(root), orphans
		}
	}
	a.ed = &editedCache{restructured: out, doc: a.Doc}
	return &a.ed.restructured
}

type editedCache struct {
	restructured
	doc *Doc
}

// editedShape is the restructured document's shape at the selected
// node's new path, or why there is none.
func (a *App) editedShape() (*Shape, string) {
	n, ok := a.rs.tree.Selected()
	if !ok {
		return nil, ""
	}
	d := a.restructuredDoc()
	if d.err != nil {
		return nil, d.err.Error()
	}
	e := a.current()
	path := nodePaths(e.nodes, e.meta)[n.ID]
	if s := d.shape.Find(path); s != nil {
		return s, ""
	}
	return nil, fmt.Sprintf("no values at %s in the restructured document", path)
}

// flip flips the viewer between the original and the edited document,
// keeping the selection on the same shape: a node's origin, or the first
// node an original shape went to. The focus moves along when it was in
// the structure pane.
func (a *App) flip(side string) error {
	if !a.editable() || a.Doc == nil {
		return errors.New("the viewer is read-only (structure.editable is off)")
	}
	inTree := a.sideOf(a.r.Focus) == rsTreeID || slices.Contains(a.r.Ancestry(a.r.Focus), treeID)
	if side == "" {
		side = "original"
		if a.original {
			side = "restructured"
		}
	}
	toOriginal := side == "original"
	if toOriginal {
		if n, ok := a.rs.tree.Selected(); ok {
			if s := a.shapeAt(a.rs.meta[n.ID].from); s != nil {
				_ = a.tree.Select(shapeID(s))
			}
		}
	} else if s, _ := a.selectedOriginal(); s != nil {
		for _, n := range a.rs.tree.Nodes() {
			if a.rs.meta[n.ID].from == s.Path {
				_ = a.rs.tree.Select(n.ID)
				break
			}
		}
	}
	a.original = toOriginal
	a.shown, a.note = nil, ""
	if inTree {
		t := a.rs.tree
		if toOriginal {
			t = a.tree
		}
		if err := t.Focus(); err != nil {
			return err
		}
	}
	return a.sync()
}

// selectedOriginal is the original structure tree's selected shape.
func (a *App) selectedOriginal() (*Shape, bool) {
	node, ok := a.tree.Selected()
	if !ok {
		return nil, false
	}
	s, _ := node.Data.(*Shape)
	return s, s != nil
}

// editedWidth is the restructured tree's natural width in the viewer:
// indent and marker, then its three cells.
func (a *App) editedWidth() int {
	depth := map[string]int{}
	cols := [3]int{4, 4, 4}
	for _, n := range a.rs.tree.Nodes() {
		if n.Parent != "" {
			depth[n.ID] = depth[n.Parent] + 1
		}
		if len(n.Cells) < 3 {
			continue
		}
		cols[0] = max(cols[0], 2*depth[n.ID]+2+ansi.StringWidth(n.Cells[0]))
		cols[1] = max(cols[1], len(n.Cells[1]))
		cols[2] = max(cols[2], len(n.Cells[2]))
	}
	return cols[0] + cols[1] + cols[2] + 4
}

// shownName is the view the viewer shows, for the header.
func (a *App) shownName() string {
	if a.original {
		return "original"
	}
	return "edited"
}

// dirtyMark is vim's [+]: restructure edits no write saved.
func (a *App) dirtyMark() string {
	if a.unsaved() {
		return " [+]"
	}
	return ""
}

// origin is the original shape a shown shape stands for: itself, or in
// the edited view the shape the selected node comes from (so a repeated
// key's note follows it); s when there is none.
func (a *App) origin(s *Shape) *Shape {
	if !a.editedShown() {
		return s
	}
	if n, ok := a.rs.tree.Selected(); ok {
		if o := a.shapeAt(a.rs.meta[n.ID].from); o != nil {
			return o
		}
	}
	return s
}
