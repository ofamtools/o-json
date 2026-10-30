package main

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/unixsuperhero/ochazuke"
)

// Marks and reshape steps on the restructured tree.
//
// space marks a node (node.mark) at any depth, and again unmarks it
// (node.unmark); the marks are o-json's, by node id, so they survive
// collapsing a parent, moving a node between levels and undo (the list's
// own marks are only drawn: syncMarks copies them onto the rows shown).
// The pane's title counts them (marks.count).
//
// marks.pick keeps only the marked nodes, at their paths, with the
// ancestors they need and what is under them: a tree edit, so the result
// stays editable in the same tree. marks.flatten, node.key-by,
// node.values and node.entries are reshape steps (reshape.go): the tree
// so far closes into the mapping's steps, and the tree becomes the copy
// of the step's output, editable as the document's copy is. Each is one
// undo step; u brings the tree it closed back.

const (
	marksID = "marks"

	settingFlattenArrays = "flatten.arrays"
	settingFlattenJoin   = "flatten.join"
	settingFlattenNames  = "flatten.names"
	settingKeyByKeep     = "key-by.keep-field"
	settingKeyByDup      = "key-by.duplicates"
	settingKeyByMissing  = "key-by.missing"
	settingMarksCount    = "marks.count"
)

// closedStage is a tree a reshape step closed: its nodes, origins and
// value edits, the key of the copy it started as (a later tree left a
// copy is written without a map), and the step that closed it.
type closedStage struct {
	nodes   []ochazuke.TreeNode
	meta    map[string]nodeMeta
	patches []Patch
	idKey   string
	op      Stage
}

// stageInput is the current tree's input after a reshape step: the
// step's output shape and the key of its copy.
type stageInput struct {
	shape *Shape
	idKey string
}

type stageState struct {
	stages []closedStage
	in     *stageInput
}

func (a *App) stagesNow() stageState { return stageState{stages: a.rs.stages, in: a.rs.in} }

// inShape is the shape the current tree's origins name: the document's,
// or the last reshape step's output.
func (a *App) inShape() *Shape {
	if a.rs.in != nil {
		return a.rs.in.shape
	}
	return a.Doc.Shape
}

// treeMap is a tree's map, nil when it is still the copy it started as.
func treeMap(nodes []ochazuke.TreeNode, meta map[string]nodeMeta, idKey string) []MapEntry {
	m := mappingOf(nodes, meta).Map
	if idKey != "" && mappingKey(&Mapping{Map: m}) == idKey {
		return nil
	}
	return m
}

// stagedMapping is the mapping with reshape steps: the first tree's map
// and patches, then each step with the map and patches of the tree after
// it, the current one last.
func (a *App) stagedMapping() *Mapping { return a.stagedMappingWith(a.rs.tree.Nodes(), a.rs.meta) }

// stagedMappingWith is stagedMapping with nodes and meta as the current
// tree.
func (a *App) stagedMappingWith(nodes []ochazuke.TreeNode, meta map[string]nodeMeta) *Mapping {
	st := a.rs.stages
	m := mappingOf(st[0].nodes, st[0].meta)
	m.Patches = slices.Clone(st[0].patches)
	for i := 1; i <= len(st); i++ {
		s := st[i-1].op
		if i < len(st) {
			s.Map, s.Patches = treeMap(st[i].nodes, st[i].meta, st[i].idKey), slices.Clone(st[i].patches)
		} else {
			s.Map, s.Patches = treeMap(nodes, meta, a.rs.in.idKey), slices.Clone(a.rs.patches)
		}
		m.Then = append(m.Then, s)
	}
	return m
}

// reshape runs a step on the restructured document as one undo step:
// the tree closes into the steps and becomes the copy of the output.
func (a *App) reshape(c ochazuke.Ctx, st Stage) ochazuke.Result {
	sps, _, err := a.reshapeFrom(a.current(), []Stage{st}, st.Op)
	if err != nil {
		return ochazuke.Failed(err)
	}
	a.refocus(rsRootID)
	c.Info(stageName(sps[0].st) + "; u undoes")
	return ochazuke.Done(a.Mapping())
}

// reshapeFrom closes the tree from (the current one, or an edit of it not
// yet made: node.wrap {as: array} wraps first) into the reshape steps,
// runs steps on its result in order, and makes the copy of the last
// output the tree: one undo step back to the tree as it is now. It
// reports the steps as run and the copy.
func (a *App) reshapeFrom(from edit, steps []Stage, label string) ([]*stagePlan, edit, error) {
	if a.Doc == nil {
		return nil, edit{}, errors.New("no document")
	}
	var in *Node
	m := a.mappingWith(from.nodes, from.meta)
	if mappingKey(m) == mappingKey(a.Mapping()) {
		d := a.restructuredDoc()
		if d.err != nil {
			return nil, edit{}, d.err
		}
		in = d.root
	} else {
		p, err := NewDocPlan(m, a.Doc.Root, a.Doc.Shape)
		d := buildRestructured(a.Doc, p, err, mappingKey(m))
		if d.err != nil {
			return nil, edit{}, d.err
		}
		in = d.root
	}
	stages := slices.Clone(a.rs.stages)
	cur := closedStage{nodes: slices.Clone(from.nodes), meta: maps.Clone(from.meta), patches: slices.Clone(a.rs.patches)}
	if a.rs.in != nil {
		cur.idKey = a.rs.in.idKey
	}
	var sps []*stagePlan
	var copied edit
	var next *stageInput
	for _, st := range steps {
		sp, err := prepareStage(st, in)
		if err != nil {
			return nil, edit{}, err
		}
		out := normalized(sp.apply(in))
		cur.op = sp.st
		stages = append(stages, cur)
		shape := Infer(out)
		copied = a.identityOf(shape)
		next = &stageInput{shape: shape, idKey: mappingKey(&Mapping{Map: mappingOf(copied.nodes, copied.meta).Map})}
		cur = closedStage{nodes: slices.Clone(copied.nodes), meta: maps.Clone(copied.meta), idKey: next.idKey}
		sps, in = append(sps, sp), out
	}
	delete(a.rs.marks, rsRootID) // the document node stays; its mark was for the tree that closed
	if err := a.commitStaged(label, rsRootID, copied, nil, stageState{stages: stages, in: next}, false); err != nil {
		return nil, edit{}, err
	}
	if _, err := a.plan(); err != nil { // never expected: the copy of the output checks
		return nil, edit{}, err
	}
	if err := a.collapseCopy(copied); err != nil {
		return nil, edit{}, err
	}
	return sps, copied, nil
}

// --- marks ---

// markedIDs are the marked nodes the tree has, in tree order.
func (a *App) markedIDs() []string {
	var out []string
	for _, n := range a.rs.tree.Nodes() {
		if a.rs.marks[n.ID] {
			out = append(out, n.ID)
		}
	}
	return out
}

// syncMarks draws the marks on the rows shown: the list's own marks
// follow o-json's (rows a collapse hid lose theirs in the list, never
// here). A V selection draws over them, so it is left alone.
func (a *App) syncMarks() {
	l := a.rs.tree.List()
	if l.Visual() {
		return
	}
	shown := map[string]bool{}
	for _, it := range l.Marked() {
		shown[it.ID] = true
	}
	for _, n := range a.rs.tree.Nodes() {
		if a.rs.marks[n.ID] != shown[n.ID] {
			_ = l.ToggleMark(n.ID) // a row a collapse hides is not in the list: refused, drawn when shown
		}
	}
}

// marksNote is the title's mark count, "" for none or marks.count off.
func (a *App) marksNote() string {
	if !a.r.Settings().Bool(settingMarksCount) {
		return ""
	}
	if n := len(a.markedIDs()); n > 0 {
		return fmt.Sprintf("%d marked", n)
	}
	return ""
}

// targets are the marked nodes, else the verb's subjects (the node, or a
// V selection).
func (a *App) targets(c ochazuke.Ctx) []string {
	if ids := a.markedIDs(); len(ids) > 0 {
		return ids
	}
	ids := slices.DeleteFunc(subjectIDs(c), func(id string) bool { return a.focusedNode(id) == "" })
	if len(ids) == 0 {
		if n, ok := a.rs.tree.Selected(); ok {
			ids = []string{n.ID}
		}
	}
	return ids
}

// nodePathsOf are the nodes' paths in the restructured document.
func (a *App) nodePathsOf(ids []string) []string {
	paths := nodePaths(a.rs.tree.Nodes(), a.rs.meta)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if p, ok := paths[id]; ok && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// pick keeps ids, their ancestors and their subtrees, and nothing else.
func (e edit) pick(ids []string) (edit, int) {
	keep := map[string]bool{}
	for _, id := range ids {
		for at := id; at != ""; at = e.parent(at) {
			keep[at] = true
		}
	}
	marked := map[string]bool{}
	for _, id := range ids {
		marked[id] = true
	}
	var drop []string
	for _, n := range e.nodes {
		if n.Parent == "" || keep[n.ID] || marked[n.Parent] || !keep[n.Parent] {
			continue
		}
		drop = append(drop, n.ID)
	}
	return e.remove(drop), len(drop)
}

// --- the shapes a node holds ---

// outShape is the restructured document's shape at a node's path.
func (a *App) outShape(id string) *Shape {
	d := a.shownDoc()
	if d.err != nil || d.shape == nil {
		return nil
	}
	path, ok := nodePaths(a.rs.tree.Nodes(), a.rs.meta)[id]
	if !ok {
		return nil
	}
	steps, err := parsePath(path)
	if err != nil {
		return nil
	}
	return d.shape.Find(stepsPath(steps))
}

// keyFields are the fields every value of which is a scalar on the
// objects in a node's arrays: what key-by can file them under.
func (a *App) keyFields(id string) []string {
	s := a.outShape(id)
	if s == nil || s.Kinds[KArray] == 0 || s.items == nil {
		return nil
	}
	var out []string
	for _, f := range s.items.Children {
		if f.IsField() && f.Kinds[KObject]+f.Kinds[KArray] == 0 {
			out = append(out, f.Key)
		}
	}
	return out
}

func (a *App) holdsObjects(id string) bool {
	s := a.outShape(id)
	return s != nil && s.Kinds[KObject] > 0
}

// --- settings and verbs ---

func declareReshapeSettings(r *ochazuke.Runtime) error {
	for _, s := range []ochazuke.Setting{
		{Name: settingFlattenArrays, Type: ochazuke.SettingString, Default: arraysJoin, Enum: []string{arraysJoin, arraysJSON, arraysRows},
			Description: "How marks.flatten writes an array among the marked fields: join its items (flatten.join between them), its compact JSON, or one row per item with the other fields repeated (the export's rule, export.arrays)"},
		{Name: settingFlattenJoin, Type: ochazuke.SettingString, Default: ";",
			Description: "The separator between an array's items when marks.flatten joins them (flatten.arrays join)"},
		{Name: settingFlattenNames, Type: ochazuke.SettingString, Default: namesLeaf, Enum: []string{namesLeaf, namesPath},
			Description: "The names marks.flatten gives the fields: leaf, the last key where no other field ends in it (city), else the dotted path; path, always the dotted path (address.city)"},
		{Name: settingKeyByKeep, Type: ochazuke.SettingBoolean, Default: true,
			Description: "node.key-by keeps the field it files each item under inside the item; off takes it out"},
		{Name: settingKeyByDup, Type: ochazuke.SettingString, Default: dupRename, Enum: []string{dupRename, dupLast, dupGroup},
			Description: "node.key-by on items that share a key: rename the later ones (a-2, a-3), keep the last (jq's INDEX), or group them all in an array under the key"},
		{Name: settingKeyByMissing, Type: ochazuke.SettingString, Default: defaultMissing,
			Description: "The key node.key-by puts the items without the field under (in an array; a taken key moves on to KEY-2)"},
		{Name: settingMarksCount, Type: ochazuke.SettingBoolean, Default: true,
			Description: "The structure pane's title counts the marked nodes (\"3 marked\"); off leaves it out"},
	} {
		if err := declareOwn(r, s); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) registerReshapeVerbs() {
	r := a.r
	r.Instance(marksID, ochazuke.Kind(marksID))
	params := func(key string, p map[string]ochazuke.Parameter) {
		op := r.Operations[r.CanonicalKey(key)]
		op.Parameters = p
		r.Operations[r.CanonicalKey(key)] = op
	}
	node := func(ev ochazuke.KeyEvent) string { return a.focusedNode(ev.Focus) }
	r.DefineGuard("node-unmarked", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		id := node(ev)
		return id != "" && !a.rs.marks[id]
	})
	r.DefineGuard("node-marked", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		id := node(ev)
		return id != "" && a.rs.marks[id]
	})
	r.DefineGuard("marks-any", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool { return len(a.markedIDs()) > 0 })
	r.DefineGuard("node-reshapable", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return a.Doc != nil && a.editable() && (len(a.markedIDs()) > 0 || node(ev) != "")
	})
	r.DefineGuard("node-keyable", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		id := node(ev)
		return id != "" && len(a.keyFields(id)) > 0
	})
	r.DefineGuard("node-objects", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		id := node(ev)
		return id != "" && a.holdsObjects(id)
	})
	setMarks := func(on bool) ochazuke.AppHandler {
		return func(c ochazuke.Ctx) ochazuke.Result {
			n := 0
			for _, id := range subjectIDs(c) {
				if a.focusedNode(id) == "" {
					continue
				}
				if on {
					a.rs.marks[id] = true
				} else {
					delete(a.rs.marks, id)
				}
				n++
			}
			if n == 0 {
				return ochazuke.Failed(errors.New("no node to mark here"))
			}
			return ochazuke.Done(a.nodePathsOf(a.markedIDs()))
		}
	}
	flatten := func(c ochazuke.Ctx, ids []string) ochazuke.Result {
		if len(ids) == 0 {
			return ochazuke.Failed(errors.New("nothing to flatten: space marks the fields"))
		}
		s := a.r.Settings()
		return a.reshape(c, Stage{Op: opFlatten, Paths: a.nodePathsOf(ids),
			Arrays: ochazuke.StringArg(c.Args, "arrays", s.String(settingFlattenArrays)),
			Join:   ochazuke.StringArg(c.Args, "join", s.String(settingFlattenJoin)),
			Names:  ochazuke.StringArg(c.Args, "names", s.String(settingFlattenNames))})
	}
	pick := func(c ochazuke.Ctx, ids []string) ochazuke.Result {
		e, n := a.current().pick(ids)
		if n == 0 {
			return ochazuke.Failed(errors.New("nothing to pick away: every node is kept"))
		}
		if err := a.check(e); err != nil {
			return ochazuke.Failed(err)
		}
		focus := ids[0]
		if err := a.commit("pick", focus, e); err != nil {
			return ochazuke.Failed(err)
		}
		a.refocus(focus)
		c.Info(fmt.Sprintf("kept %d marked node(s), dropped %d; u undoes", len(ids), n))
		return ochazuke.Done(a.Mapping())
	}
	at := func(c ochazuke.Ctx) string {
		path := nodePaths(a.rs.tree.Nodes(), a.rs.meta)[c.Instance.ID]
		return path
	}
	flattenParams := map[string]ochazuke.Parameter{
		"arrays": {Type: "string", Enum: []string{arraysJoin, arraysJSON, arraysRows}, Description: "an array among the fields: join, json or rows (default flatten.arrays)"},
		"join":   {Type: "string", Description: "the separator for arrays join (default flatten.join)"},
		"names":  {Type: "string", Enum: []string{namesLeaf, namesPath}, Description: "leaf or path names (default flatten.names)"},
	}
	r.Define("node").
		Verb("mark", setMarks(true), "Mark the node (any depth; space, again unmarks) for marks.flatten and marks.pick; marks survive collapsing, moves and undo").Many().When("node-unmarked").
		Verb("unmark", setMarks(false), "Unmark the node").Many().When("node-marked").
		Verb("flatten", func(c ochazuke.Ctx) ochazuke.Result { return flatten(c, subjectIDs(c)) },
			"Flatten the node (or a V selection) as marks.flatten flattens the marks: a flat array of objects, one per item of the nearest common array").Many().When("node-reshapable").
		Verb("pick", func(c ochazuke.Ctx) ochazuke.Result { return pick(c, subjectIDs(c)) },
			"Keep only the node (or a V selection) as marks.pick keeps the marks").Many().When("node-reshapable").
		Verb("key-by", func(c ochazuke.Ctx) ochazuke.Result {
			field := ochazuke.StringArg(c.Args, "field", "")
			s := a.r.Settings()
			keep := s.Bool(settingKeyByKeep)
			if v, ok := c.Args["keep"].(bool); ok {
				keep = v
			}
			return a.reshape(c, Stage{Op: opKeyBy, Path: at(c), Field: field, Drop: !keep,
				Duplicates: ochazuke.StringArg(c.Args, "duplicates", s.String(settingKeyByDup)),
				Missing:    ochazuke.StringArg(c.Args, "missing", s.String(settingKeyByMissing))})
		}, "Turn the array of objects into an object keyed by field (jq's INDEX(.[]; .FIELD)): the field stays in each value (key-by.keep-field); items sharing a key follow key-by.duplicates (rename, last, group); items without it go under key-by.missing, never dropped").When("node-keyable").
		Verb("key-fields", func(c ochazuke.Ctx) ochazuke.Result {
			// Asked from the arguments form, the focus is the form's field:
			// the node is the restructured tree's selection.
			id := ""
			if c.Instance != nil {
				id = a.focusedNode(c.Instance.ID)
			}
			if n, ok := a.rs.tree.Selected(); id == "" && ok {
				id = n.ID
			}
			return ochazuke.Done(a.keyFields(id))
		}, "The scalar fields of the node's items: the fields node.key-by can key them by").Fetch().
		Verb("values", func(c ochazuke.Ctx) ochazuke.Result {
			return a.reshape(c, Stage{Op: opValues, Path: at(c)})
		}, "Turn the object into the array of its values (jq's [.[]]): the reverse of node.key-by").When("node-objects").
		Verb("entries", func(c ochazuke.Ctx) ochazuke.Result {
			return a.reshape(c, Stage{Op: opEntries, Path: at(c)})
		}, "Turn the object into {key, value} pairs (jq's to_entries)").When("node-objects")
	r.Define(marksID).
		Verb("clear", func(c ochazuke.Ctx) ochazuke.Result {
			n := len(a.markedIDs())
			clear(a.rs.marks)
			c.Info(fmt.Sprintf("cleared %d mark(s)", n))
			return ochazuke.Done([]string{})
		}, "Unmark every node").When("marks-any").
		Verb("list", func(c ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(a.nodePathsOf(a.markedIDs())) },
			"The marked nodes' paths in the restructured document").Fetch().
		Verb("flatten", func(c ochazuke.Ctx) ochazuke.Result { return flatten(c, a.targets(c)) },
			"Flatten the marked fields (else the selected node) into a flat array of objects, one per item of their nearest common array, under flat names (flatten.names); an array among them joins, becomes JSON or makes rows (flatten.arrays). A reshape step: one undo step, in the mapping and the jq").When("node-reshapable").
		Verb("pick", func(c ochazuke.Ctx) ochazuke.Result { return pick(c, a.targets(c)) },
			"Keep only the marked fields (else the selected node), at their paths, with the objects and arrays they are in: the rest of the tree is dropped, as one undo step").When("node-reshapable")
	params("node.flatten", flattenParams)
	params("marks.flatten", flattenParams)
	params("node.key-by", map[string]ochazuke.Parameter{
		"field":      {Type: "string", Required: true, Source: "node.key-fields", Description: "the field each item is filed under (a scalar field of the items)"},
		"keep":       {Type: "boolean", Description: "keep the field in each value (default key-by.keep-field)"},
		"duplicates": {Type: "string", Enum: []string{dupRename, dupLast, dupGroup}, Description: "items sharing a key: rename, last or group (default key-by.duplicates)"},
		"missing":    {Type: "string", Description: "the key for items without the field (default key-by.missing)"},
	})
	setHistory(r, ochazuke.HistoryAction, "node.mark", "node.unmark", "marks.clear")
	setHistory(r, ochazuke.HistoryChange, "node.flatten", "node.pick", "node.key-by", "node.values", "node.entries", "marks.flatten", "marks.pick")
}
