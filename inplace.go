package main

import (
	"errors"
	"fmt"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
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
// The restructure page opens on the state as it is: the copy a document
// starts with, or the in-place edits, beside the original. Starting from
// nothing is a verb of its own (restructure.clear, one undo step), never
// a side effect of opening the page, so closing it keeps the viewer's
// selection and the edits made in the viewer.

const (
	settingEditable    = "structure.editable"
	settingUndoAtStart = "undo.at-start"
)

func declareEditSettings(r *ochazuke.Runtime) error {
	return declareOwn(r, ochazuke.Setting{
		Name: settingEditable, Type: ochazuke.SettingBoolean, Default: true,
		Description: "The viewer's structure pane is the restructure editor, a copy of the document's shape (T shows the original); off keeps the viewer read-only and the restructure page (M) alone edits. Takes effect on the next document",
	}, ochazuke.Setting{
		Name: settingUndoAtStart, Type: ochazuke.SettingString, Default: "info", Enum: []string{"info", "silent"},
		Description: "u at the copy a document starts with (the oldest change; nothing is undone): info says \"already at oldest change\" in the plain tone, silent says nothing",
	})
}

// editable reports the structure.editable setting.
func (a *App) editable() bool { return a.r.Settings().Bool(settingEditable) }

// editedShown reports the edited document drawn: in the viewer the
// restructured tree in the structure pane and its data in the data pane
// (viewerEdited); on the restructure page always, its preview being the
// restructured document's data.
func (a *App) editedShown() bool {
	return a.Doc != nil && (a.r.Page == rsPageID || a.viewerEdited())
}

// viewerEdited reports the viewer set to the edited document (not T's
// original), whichever page is open.
func (a *App) viewerEdited() bool { return a.editable() && !a.original && a.Doc != nil }

// identity is the restructure that copies the document: the document
// node and one node per shape, each mapped to itself. A shape is expanded
// only when every value is an object or every value is an array, and it
// lists at most maxFields fields: anything else is one node that copies
// its values, so the output is always the input.
func (a *App) identity() edit {
	if a.Doc == nil {
		return edit{nodes: []ochazuke.TreeNode{{ID: rsRootID, Label: ".", Noun: "node"}}, meta: map[string]nodeMeta{}}
	}
	return a.identityOf(a.Doc.Shape)
}

// identityOf is the copy of a document of this shape (identity).
func (a *App) identityOf(root *Shape) edit {
	e := edit{nodes: []ochazuke.TreeNode{{ID: rsRootID, Label: ".", Noun: "node"}}, meta: map[string]nodeMeta{}}
	if !copyExpands(root) {
		e.meta[rsRootID] = nodeMeta{from: "."}
		return e
	}
	for _, c := range root.Children {
		a.copyShape(&e, c, rsRootID)
	}
	return e
}

// copyExpands reports a shape the copy shows with its children: objects
// (or arrays) with few enough fields.
func copyExpands(s *Shape) bool {
	fields := 0
	for _, c := range s.Children {
		if c.IsField() {
			fields++
		}
	}
	return len(s.Children) > 0 && fields <= maxFields && (objectOnly(s) || s.Kinds[KArray] == s.Count)
}

// copyShape appends s and the subtree the copy shows of it under parent,
// each node mapped from its shape (the identity mapping of s).
func (a *App) copyShape(e *edit, s *Shape, parent string) {
	id := a.newNodeID()
	e.nodes = append(e.nodes, ochazuke.TreeNode{ID: id, Parent: parent, Label: s.Key, Noun: "node"})
	e.meta[id] = nodeMeta{from: s.Path, item: s.Key == "[]"}
	if copyExpands(s) {
		for _, c := range s.Children {
			a.copyShape(e, c, id)
		}
	}
}

// startState makes the restructure state the one a new document starts
// with: a copy of its shape (structure.editable), as one undo step so u
// brings back the edits it replaced; with none replaced it is the floor
// (F6). It is the base unsaved compares with.
func (a *App) startState() error {
	clear(a.rs.marks)
	if !a.editable() {
		a.rs.patches, a.rs.stages, a.rs.in = nil, nil, nil
		a.rs.base = mappingKey(&Mapping{Map: []MapEntry{}})
		return nil
	}
	a.original = false
	// The copy is the floor of the undo stack (u there does nothing,
	// undo.at-start) unless it replaced edits: then u brings them back,
	// as viewer.discard promises (e, R and viewer new over edits).
	floor := a.rs.base == "" || !a.edited()
	e := a.identity()
	if err := a.commitStaged("open", rsRootID, e, nil, stageState{}, floor); err != nil {
		return err
	}
	a.rs.base, a.rs.saved = mappingKey(a.Mapping()), ""
	return a.collapseCopy(e)
}

// collapseCopy collapses a big copy below its second level, as the
// original tree opens.
func (a *App) collapseCopy(e edit) error {
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

// placeTree lends the restructured tree and the data pane to a page:
// they are drawn, focused and keyed there (a focus must sit inside the
// open page). On the restructure page the data pane is the preview of the
// restructured document (restructure.preview).
func (a *App) placeTree(page string) {
	for _, id := range []string{rsTreeID, dataID} {
		if inst := a.r.Instances[id]; inst != nil {
			inst.Parent = page
		}
	}
}

// openedViewer runs when the viewer opens: the restructured tree and the
// data pane come back to it.
func (a *App) openedViewer() {
	a.placeTree(pageID)
	// The page keeps the focus it had; one on the page itself or on the
	// hidden original moves to the structure pane shown.
	if a.editedShown() && (a.r.Focus == pageID || slices.Contains(a.r.Ancestry(a.r.Focus), treeID)) {
		_ = a.rs.tree.Focus()
	}
}

// openedRestructure runs when the restructure page opens: the tree and
// the data pane (the preview, restructure.preview) come to it as they
// are, and the focus goes to the original.
func (a *App) openedRestructure() {
	want := a.viewerShape()
	a.placeTree(rsPageID)
	_ = a.focusOriginal(want)
}

// clearRestructure is restructure.clear: the restructured tree emptied to
// the document node, one undo step (u brings the structure back).
func (a *App) clearRestructure() error {
	if len(a.rs.tree.Nodes()) <= 1 {
		return errors.New("the restructure is already empty")
	}
	root := edit{nodes: []ochazuke.TreeNode{{ID: rsRootID, Label: ".", Noun: "node"}}, meta: map[string]nodeMeta{}}
	return a.commit("clear", rsRootID, root)
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
// parsed again so its values have their own paths. It is always current:
// writes, exports and value edits read it. What is drawn reads shownDoc.
func (a *App) restructuredDoc() *restructured {
	key := mappingKey(a.Mapping())
	if a.ed != nil && a.ed.key == key && a.ed.doc == a.Doc {
		return &a.ed.restructured
	}
	p, err := a.plan()
	start := time.Now()
	out := buildRestructured(a.Doc, p, err, key)
	a.rb.slow = time.Since(start) > rebuildOnLoop
	a.ed = &editedCache{restructured: out, doc: a.Doc}
	if len(a.queued) > 0 { // their patches change the mapping: build again
		a.flushQueued()
		return a.restructuredDoc()
	}
	return &a.ed.restructured
}

type editedCache struct {
	restructured
	doc *Doc
}

// buildRestructured applies a plan to doc and parses the result again:
// the costly part of a rebuild (ApplyAll, Compact, Build, Infer). It reads
// only its arguments, so it runs off the loop as well as on it.
func buildRestructured(doc *Doc, p *Plan, err error, key string) restructured {
	out := restructured{key: key}
	if err != nil {
		out.err = err
		return out
	}
	v, orphans := p.ApplyAll(doc.Root)
	if root, err := Build([]byte(Compact(v, 0))); err != nil {
		out.err = err
	} else {
		out.root, out.shape, out.orphans = root, Infer(root), orphans
	}
	return out
}

// shownDoc is the restructured document to draw: the current one, or,
// while a slow rebuild runs off the loop (rebuilder), the last one built
// for this document, kept on screen until the new one arrives.
func (a *App) shownDoc() *restructured {
	if a.rb.slow && a.ed != nil && a.ed.doc == a.Doc {
		return &a.ed.restructured
	}
	return a.restructuredDoc()
}

// staleShown reports the data pane showing the previous restructured
// document while a slow rebuild of the current one runs off the loop.
func (a *App) staleShown() bool {
	return a.rb.slow && a.Doc != nil && a.ed != nil && a.ed.doc == a.Doc && a.ed.key != mappingKey(a.Mapping())
}

// settleEdited makes the shown restructured document the current one
// before a value edit acts on it (rebuild.value-edit current). While a
// slow rebuild runs off the loop the data pane shows the previous result:
// the rebuild is done on the loop now (the job cancelled), the data pane
// and the detail follow, and the edit acts on the row at the cursor in
// the current document. Only a value edit made in that window waits; a
// structure edit never does.
func (a *App) settleEdited() error {
	if !a.staleShown() {
		return nil
	}
	a.rb.cancel()
	a.restructuredDoc()
	a.rb.slow = true // still a large document: later rebuilds stay off the loop
	if err := a.sync(); err != nil {
		return err
	}
	a.refreshDetail()
	return nil
}

// rebuildOnLoop is the longest rebuild the loop waits for: once one takes
// longer (a large document), the next rebuilds run off the loop.
const rebuildOnLoop = 20 * time.Millisecond

const rebuildID = "restructure-rebuild"

// rebuilder rebuilds the restructured document off the loop once a
// rebuild is slow (F7: about 200 ms on 60k items): one job slot, a newer
// mapping cancelling the older run; a result lands only for the mapping
// and document it was built for.
type rebuilder struct {
	a      *App
	slow   bool   // the last rebuild on the loop took longer than rebuildOnLoop
	wanted string // the mapping the running job builds
	ticket ochazuke.Ticket
}

type rebuilt struct {
	doc *Doc
	restructured
}

// PendingCommand starts a rebuild when the shown document is stale.
func (b *rebuilder) PendingCommand() tea.Cmd {
	a := b.a
	if !b.slow || a.Doc == nil || a.ed == nil || a.ed.doc != a.Doc {
		return nil
	}
	key := mappingKey(a.Mapping())
	if a.ed.key == key || b.wanted == key {
		return nil
	}
	b.cancel()
	b.wanted = key
	p, err := a.plan()
	ticket, ctx, ierr := a.r.Issue(rebuildID, "restructure.rebuild", "rebuild", ochazuke.Ctx{Runtime: a.r})
	if ierr != nil {
		return nil
	}
	b.ticket = ticket
	doc := a.Doc
	return func() tea.Msg {
		if ctx.Err() != nil {
			return ochazuke.Completion{Ticket: ticket, Err: ctx.Err()}
		}
		return ochazuke.Completion{Ticket: ticket, Value: rebuilt{doc: doc, restructured: buildRestructured(doc, p, err, key)}}
	}
}

func (b *rebuilder) cancel() {
	if b.a.r.Accepts(b.ticket) {
		b.a.r.Settle(b.ticket, errors.New("superseded"))
	}
	b.wanted = ""
}

func (b *rebuilder) HandleMessage(msg tea.Msg) bool {
	c, ok := msg.(ochazuke.Completion)
	if !ok || c.Ticket.Owner != rebuildID {
		return false
	}
	if out, ok := c.Value.(rebuilt); ok && c.Ticket == b.ticket {
		b.wanted = ""
		a := b.a
		if out.doc == a.Doc && out.key == mappingKey(a.Mapping()) {
			a.ed = &editedCache{restructured: out.restructured, doc: out.doc}
			a.flushQueued()
		}
	}
	return true
}

func (b *rebuilder) Close() { b.cancel() }

// editedShape is the restructured document's shape at the selected
// node's new path, or why there is none.
func (a *App) editedShape() (*Shape, string) {
	n, ok := a.rs.tree.Selected()
	if !ok {
		return nil, ""
	}
	d := a.shownDoc()
	if d.err != nil {
		return nil, d.err.Error()
	}
	e := a.current()
	path := nodePaths(e.nodes, e.meta)[n.ID]
	// The node's path is written as jq writes it (".[]"), a shape's
	// canonically ("[]" under the document): look the shape up by its steps.
	if steps, err := parsePath(path); err == nil {
		if s := d.shape.Find(stepsPath(steps)); s != nil {
			return s, ""
		}
	}
	return nil, fmt.Sprintf("no values at %s in the restructured document", path)
}

// flip flips the viewer between the original and the edited document,
// keeping the selection on the same shape: a node's origin, or the first
// node an original shape went to; the data pane keeps its row and column. The focus moves along when it was in
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
	if a.shown != nil && a.lines == nil && !a.Raw {
		a.carry = &rowCarry{sel: max(0, a.data.Selected()), col: a.data.list.Column()}
	}
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

// treeNaturalWidth includes the branch guides when measuring the viewer's
// structure pane. Read the setting on each render so its opt-out also resizes.
func (a *App) treeNaturalWidth(tree *ochazuke.Tree, cols [3]int) int {
	indent := 2
	if tree.Guides && a.r.Settings().String(ochazuke.TreeGuidesSetting) != "off" {
		indent = 3
	}
	depth := map[string]int{}
	for _, n := range tree.Nodes() {
		if n.Parent != "" {
			depth[n.ID] = depth[n.Parent] + 1
		}
		if len(n.Cells) < 3 {
			continue
		}
		cols[0] = max(cols[0], indent*depth[n.ID]+2+ansi.StringWidth(n.Cells[0]))
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

// viewLabel names the shown view in the structure pane's title: original
// after T; edited once the restructure (its structure or its value edits)
// differs from the copy the document opened with; unchanged before that,
// so a document just opened never reads as edited. shownName stays the
// side's name (viewer.state's view, restructure.show's side).
func (a *App) viewLabel() string {
	switch {
	case a.original:
		return "original"
	case a.edited():
		return "edited"
	}
	return "unchanged"
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
