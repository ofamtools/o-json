package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

// Value edits in the data pane: the second layer of the restructure
// (patch.go). In the edited view the data pane's rows are values of the
// restructured document; the value verbs patch one of them at its
// concrete path, as one undo step each on the restructure's undo stack,
// held in memory until w writes (the [+] marker covers them):
//
//	r, i      value.edit    edit the value in place: an inline input, its
//	                        text selected (rename.select), the type
//	                        inferred from the text on accept
//	d d       value.delete  delete the field or item
//	o         value.add     add a field (its key first, a placeholder
//	                        selected) or an item, after this one
//	K         value.rename  rename this key, in this one object
//	p         value.paste   put the register's value over this one
//	y         value.yank    (item.yank, field.yank) copy the value
//	          value.patches the value edits, the orphaned ones marked
//	          value.drop    forget one value edit
//
// A table row's value is the cell under the cursor: a field column's
// cell (a cell the row lacks is added by edit and paste), or on the #
// and (value) columns the row itself. The setting data.editable (default
// on) off keeps the data pane read-only.

const (
	settingDataEditable = "data.editable"
	valueInputID        = "data/value-input"
	// valueShape marks a register holding a value yanked from the data
	// pane, valueShape+":"+its kind, so a put keeps its type.
	valueShape = "o-json.value"
)

func declareValueSettings(r *ochazuke.Runtime) error {
	return r.DeclareSetting(ochazuke.Setting{
		Name: settingDataEditable, Type: ochazuke.SettingBoolean, Default: true,
		Description: "In the viewer's edited view the data pane edits single values (r, d d, o, K, p), as value patches applied after the restructure; off keeps the data pane read-only",
	})
}

func (a *App) dataEditable() bool { return a.r.Settings().Bool(settingDataEditable) }

// valueAt is one value of the data pane: node in its container parent
// (nil for the document), or a table cell the row lacks (node nil, key
// its column).
type valueAt struct {
	node   *Node
	parent *Node
	key    string
}

// path is the value's concrete path in the restructured document.
func (v valueAt) path() string {
	if v.node != nil {
		return v.node.Path()
	}
	p := v.parent.Path()
	if p == "." {
		p = ""
	}
	return p + keyStep(v.key)
}

// inObject reports a value that is a key of an object.
func (v valueAt) inObject() bool { return v.parent != nil && v.parent.Kind == KObject }

// valueOf is the value an instance of the data pane stands for: a row's
// node, a table cell's, or the one scalar the pane shows.
func (a *App) valueOf(inst *ochazuke.Instance) (valueAt, bool) {
	if inst == nil || a.rowsOf(inst) == nil {
		return valueAt{}, false
	}
	if inst.ID == a.data.id || inst.ID == rowsID {
		if inst.ID == a.data.id && a.scalar != nil {
			return valueAt{node: a.scalar, parent: a.scalar.Parent, key: a.scalar.Key}, true
		}
		return valueAt{}, false
	}
	n := nodeAt(inst)
	if n == nil {
		return valueAt{}, false
	}
	if v := a.rowsOf(inst); v == a.data && a.tab != nil && inst.Noun == "item" {
		if col := v.list.Column(); col >= 1 && col <= len(a.tab.Columns) && n.Kind == KObject {
			if c := a.tab.Columns[col-1]; !c.Self {
				if f := n.Field(c.Name); f != nil {
					return valueAt{node: f, parent: n, key: f.Key}, true
				}
				return valueAt{parent: n, key: c.Name}, true
			}
		}
	}
	return valueAt{node: n, parent: n.Parent, key: n.Key}, true
}

// editableValue is the data pane value a verb may patch on a focus: the
// edited view shown, data.editable on, the data pane (not the detail),
// not the raw JSON, no value input open.
func (a *App) editableValue(focus string) (valueAt, bool) {
	inst := a.r.Instances[focus]
	if !a.dataEditable() || !a.editedShown() || a.queryShowing() || a.Raw || a.vedit != nil || a.rowsOf(inst) != a.data {
		return valueAt{}, false
	}
	return a.valueOf(inst)
}

// inferValue is the JSON a typed text stands for: a number, true, false
// or null, an object or array in JSON, a "…" JSON string (to force a
// string: "42"); anything else is the text itself as a string.
func inferValue(text string) (json.RawMessage, Kind) {
	t := strings.TrimSpace(text)
	switch t {
	case "true", "false":
		return json.RawMessage(t), KBool
	case "null":
		return json.RawMessage(t), KNull
	}
	if t != "" && json.Valid([]byte(t)) {
		switch c := t[0]; {
		case c == '-' || c >= '0' && c <= '9':
			return json.RawMessage(t), KNumber
		case c == '"':
			var s string
			_ = json.Unmarshal([]byte(t), &s)
			return json.RawMessage(quote(s)), KString
		case c == '{' || c == '[':
			n, err := Build([]byte(t))
			if err == nil {
				return json.RawMessage(Compact(n, 0)), n.Kind
			}
		}
	}
	return json.RawMessage(quote(text)), KString
}

// editText is the text a value's input opens with: a string as it is
// (quoted when its text would read as another type, so it stays a
// string), another scalar as its literal, a container as compact JSON.
func editText(n *Node) string {
	switch {
	case n == nil:
		return ""
	case n.Kind == KString:
		if raw, _ := inferValue(n.Value); string(raw) == quote(n.Value) {
			return n.Value
		}
		return quote(n.Value)
	case n.IsContainer():
		return Compact(n, 0)
	}
	return n.Value
}

// valueYank is a value as a register holds it: the text a cell shows (a
// string unquoted, a container as indented JSON), its kind in the shape.
func valueYank(n *Node) ochazuke.Yank {
	text := Text(n, 0)
	if n.IsContainer() {
		text = Pretty(n)
	}
	return ochazuke.Yank{Text: text, Shape: valueShape + ":" + string(n.Kind)}
}

// yankedValue is the JSON a register's yank puts: a value yanked here
// keeps its type; other text is inferred as typed text is.
func yankedValue(y ochazuke.Yank) (json.RawMessage, error) {
	switch {
	case y.Shape == nodesShape:
		return nil, errors.New("the register holds restructure nodes (p on the structure pane puts them); y on a value fills it")
	case y.Text == "" && y.Shape == "":
		return nil, errors.New("the register is empty: y on a value fills it")
	}
	if kind, ok := strings.CutPrefix(y.Shape, valueShape+":"); ok {
		if Kind(kind) == KString {
			return json.RawMessage(quote(y.Text)), nil
		}
		if n, err := Build([]byte(y.Text)); err == nil {
			return json.RawMessage(Compact(n, 0)), nil
		}
	}
	raw, _ := inferValue(strings.TrimSuffix(y.Text, "\n"))
	return raw, nil
}

// commitPatches makes patches the value layer, one undo step on the
// restructure's stack (commitWith).
func (a *App) commitPatches(label string, patches []Patch) error {
	return a.commitWith(label, "", a.current(), patches)
}

// addPatch adds one patch as one undo step.
func (a *App) addPatch(p Patch) error {
	return a.commitPatches(p.Op+" "+p.Path, append(slices.Clone(a.rs.patches), p))
}

// setValue patches v to raw: a set, or an add for a cell the row lacks.
func (a *App) setValue(v valueAt, raw json.RawMessage) error {
	op := opSet
	if v.node == nil {
		op = opAdd
	} else if string(raw) == Compact(v.node, 0) {
		return nil // unchanged
	}
	return a.addPatch(Patch{Op: op, Path: v.path(), Value: raw})
}

// freeValueKey is the setting's placeholder (restructure.new-key),
// numbered past the keys obj has.
func (a *App) freeValueKey(obj *Node) string {
	base := strings.TrimSpace(a.r.Settings().String(settingNewKey))
	if base == "" {
		base = "new-key"
	}
	taken := map[string]bool{}
	for _, c := range obj.Children {
		taken[c.Key] = true
	}
	return freeKey(base, taken)
}

// addAfter is the patch adding raw beside v in its container: an item
// right after it in an array, a key right after it in an object.
func addAfter(v valueAt, key string, raw json.RawMessage) Patch {
	pp := v.parent.Path()
	if pp == "." {
		pp = ""
	}
	if v.parent.Kind == KArray {
		return Patch{Op: opAdd, Path: pp + "[" + strconv.Itoa(v.node.Index+1) + "]", Value: raw}
	}
	p := Patch{Op: opAdd, Path: pp + keyStep(key), Value: raw}
	if v.node != nil {
		p.After = v.node.Key
	}
	return p
}

// --- the inline input ---

// valueEdit is the open inline input of the data pane.
type valueEdit struct {
	in     *ochazuke.Input
	what   string // "edit", "key" (a new key's name), "add" (its value), "rename"
	at     valueAt
	key    string // add: the new key (an object) once named
	before string // the text it opened with
	insert bool   // drawn as a new line below the row
}

// openInput opens the inline input on the focused row, text selected
// when sel.
func (a *App) openInput(ve *valueEdit, text string, sel bool) error {
	row := a.r.Focus
	if a.rowsOf(a.r.Instances[row]) != a.data {
		row = a.data.id
	}
	if a.r.Instances[valueInputID] != nil {
		_ = a.r.RemoveInstance(valueInputID)
	}
	in := ochazuke.NewInput(valueInputID, text, false)
	if err := a.r.AddInput(in, ochazuke.Parent(a.data.id), ochazuke.Kind("value-input"), ochazuke.FocusOnClose(row)); err != nil {
		return err
	}
	in.Cursor = len(in.Text)
	ve.in, ve.before = in, text
	a.vedit = ve
	if err := a.r.SetFocus(in.ID); err != nil {
		return err
	}
	if err := a.r.SetMode("insert"); err != nil {
		return err
	}
	if sel && text != "" {
		return in.SelectAll()
	}
	return nil
}

// closeInput closes the inline input, the focus back on its row, and
// is its text.
func (a *App) closeInput() (*valueEdit, string) {
	ve := a.vedit
	a.vedit = nil
	if ve == nil {
		return nil, ""
	}
	text := string(ve.in.Text)
	if a.r.Instances[valueInputID] != nil {
		_ = a.r.RemoveInstance(valueInputID)
	}
	if a.r.Mode.Name != "normal" {
		_ = a.r.SetMode("normal")
	}
	return ve, text
}

// accept closes the input and makes its text the edit: a value (its
// type inferred), a new key's name (then its value opens), or a key.
func (a *App) accept() error {
	ve, text := a.closeInput()
	if ve == nil {
		return errors.New("no value is being edited")
	}
	switch ve.what {
	case "edit":
		if text == ve.before {
			return nil
		}
		raw, _ := inferValue(text)
		return a.setValue(ve.at, raw)
	case "key":
		key := text
		taken := map[string]bool{}
		for _, c := range ve.at.parent.Children {
			taken[c.Key] = true
		}
		if key == "" {
			key = a.freeValueKey(ve.at.parent)
		}
		key = freeKey(key, taken)
		return a.openInput(&valueEdit{what: "add", at: ve.at, key: key, insert: true}, "null", true)
	case "add":
		raw, _ := inferValue(text)
		return a.addPatch(addAfter(ve.at, ve.key, raw))
	case "rename":
		return a.renameValue(ve.at, text)
	}
	return nil
}

// renameValue renames v's key to key, in its object only.
func (a *App) renameValue(v valueAt, key string) error {
	switch {
	case key == v.key:
		return nil
	case key == "":
		return errors.New("a key is not empty")
	case v.parent.Field(key) != nil:
		return fmt.Errorf("this object has a key %q already", key)
	}
	return a.addPatch(Patch{Op: opRename, Path: v.path(), Key: key})
}

// label is the input's prefix: what it edits and, for a value, the type
// its text reads as.
func (ve *valueEdit) label() string {
	text := string(ve.in.Text)
	switch ve.what {
	case "key":
		return "new key: "
	case "rename":
		return "rename " + ve.at.key + " to: "
	}
	_, kind := inferValue(text)
	name := ve.at.key
	switch {
	case ve.what == "add" && ve.key != "":
		name = ve.key
	case ve.what == "add":
		name = "new item"
	case ve.at.node != nil && ve.at.parent != nil && ve.at.parent.Kind == KArray:
		name = "[" + strconv.Itoa(ve.at.node.Index) + "]"
	case ve.at.parent == nil:
		name = "."
	}
	return name + " (" + string(kind) + "): "
}

// inputBlock draws the data pane with the inline input on the selected
// row (or a new line below it), body the pane as drawn.
func (a *App) inputBlock(body string, w int) ochazuke.Block {
	ve := a.vedit
	lines := strings.Split(body, "\n")
	row := 0
	if a.scalar == nil {
		row = a.data.Selected() - a.data.off
		if len(a.data.src.columns) > 0 {
			row++ // the header
		}
	}
	prefix := ve.label()
	pw := ansi.StringWidth(prefix)
	field := ochazuke.InputBlock(ve.in, max(1, w-pw), 1)
	text := prefix + field.Text
	switch {
	case ve.insert:
		row = min(row+1, len(lines))
		lines = slices.Insert(lines, row, text)
		lines = lines[:max(1, len(lines)-1)]
	case row < len(lines):
		lines[row] = text
	default:
		row = len(lines)
		lines = append(lines, text)
	}
	return field.Offset(pw, row).WithText(strings.Join(lines, "\n"))
}

// --- verbs ---

func (a *App) registerValueVerbs() {
	r := a.r
	r.Define("value")
	r.Define("value-input").Pin("insert")
	guard := func(name string, ok func(valueAt) bool) {
		r.DefineGuard(name, func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
			v, found := a.editableValue(ev.Focus)
			return found && ok(v)
		})
	}
	guard("value-editable", func(valueAt) bool { return true })
	guard("value-in-container", func(v valueAt) bool { return v.node != nil && v.parent != nil })
	guard("value-addable", func(v valueAt) bool { return v.parent != nil && (v.node != nil || v.inObject()) })
	guard("value-renamable", func(v valueAt) bool { return v.node != nil && v.inObject() })
	r.DefineGuard("value-yankable", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		v, ok := a.valueOf(r.Instances[ev.Focus])
		return ok && v.node != nil
	})
	r.DefineGuard("value-typing", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return a.vedit != nil && ev.Focus == valueInputID
	})
	params := func(key string, p map[string]ochazuke.Parameter) {
		op := r.Operations[r.CanonicalKey(key)]
		op.Parameters = p
		r.Operations[r.CanonicalKey(key)] = op
	}
	done := func(err error) ochazuke.Result {
		if err != nil {
			return ochazuke.Failed(err)
		}
		return ochazuke.Done(a.patchList())
	}
	target := func(c ochazuke.Ctx) (valueAt, error) {
		v, ok := a.editableValue(c.Instance.ID)
		if !ok {
			return v, errors.New("no value to edit here (the edited view's data pane, data.editable)")
		}
		return v, nil
	}
	r.Define("value").
		Verb("edit", func(c ochazuke.Ctx) ochazuke.Result {
			v, err := target(c)
			if err != nil {
				return ochazuke.Failed(err)
			}
			if text, ok := c.Args["text"].(string); ok {
				raw, _ := inferValue(text)
				return done(a.setValue(v, raw))
			}
			sel := a.r.RenameSelect()
			if b, ok := c.Args["select"].(bool); ok {
				sel = b
			}
			return done(a.openInput(&valueEdit{what: "edit", at: v}, editText(v.node), sel))
		}, "Edit the value in place: an inline input, its text selected (select; omitted, the rename.select setting decides) so typing replaces it. On enter or esc the text is the new value: a number, true, false, null, an object or array in JSON, \"…\" for a string that would read as another type, else the text as a string (the prefix shows the type it reads as). With text, set it at once. A cell the row lacks is added").When("value-editable").
		Verb("delete", func(c ochazuke.Ctx) ochazuke.Result {
			v, err := target(c)
			if err != nil {
				return ochazuke.Failed(err)
			}
			return done(a.addPatch(Patch{Op: opDelete, Path: v.path()}))
		}, "Delete the field or item from its object or array (one value, a value patch)").When("value-in-container").
		Verb("add", func(c ochazuke.Ctx) ochazuke.Result {
			v, err := target(c)
			if err != nil {
				return ochazuke.Failed(err)
			}
			text, hasText := c.Args["text"].(string)
			raw := json.RawMessage("null")
			if hasText {
				raw, _ = inferValue(text)
			}
			if v.parent.Kind == KArray {
				if hasText {
					return done(a.addPatch(addAfter(v, "", raw)))
				}
				return done(a.openInput(&valueEdit{what: "add", at: v, insert: true}, "null", true))
			}
			key, hasKey := c.Args["key"].(string)
			switch {
			case hasKey && hasText:
				if v.parent.Field(key) != nil {
					return ochazuke.Failed(fmt.Errorf("this object has a key %q already", key))
				}
				return done(a.addPatch(addAfter(v, key, raw)))
			case hasKey:
				if v.parent.Field(key) != nil {
					return ochazuke.Failed(fmt.Errorf("this object has a key %q already", key))
				}
				return done(a.openInput(&valueEdit{what: "add", at: v, key: key, insert: true}, "null", true))
			}
			return done(a.openInput(&valueEdit{what: "key", at: v, insert: true}, a.freeValueKey(v.parent), true))
		}, "Add a value after this one: in an array an item, in an object a key (its name first, a placeholder selected: typing names it), then its value (null selected). With key and text, at once").When("value-addable").
		Verb("rename", func(c ochazuke.Ctx) ochazuke.Result {
			v, err := target(c)
			if err != nil {
				return ochazuke.Failed(err)
			}
			if key, ok := c.Args["key"].(string); ok {
				return done(a.renameValue(v, key))
			}
			return done(a.openInput(&valueEdit{what: "rename", at: v}, v.key, a.r.RenameSelect()))
		}, "Rename this key in this one object (the structure's keys stay): an inline input, the key selected; with key, at once").When("value-renamable").
		Verb("paste", func(c ochazuke.Ctx) ochazuke.Result {
			v, err := target(c)
			if err != nil {
				return ochazuke.Failed(err)
			}
			y, err := a.r.ReadYank("")
			a.r.Registers().Active = ""
			if err != nil {
				return ochazuke.Failed(err)
			}
			raw, err := yankedValue(y)
			if err != nil {
				return ochazuke.Failed(err)
			}
			return done(a.setValue(v, raw))
		}, "Put the register's value (\"a p from a) over this one: a value yanked with y keeps its type, other text reads as typed text does").When("value-editable").
		Verb("yank", func(c ochazuke.Ctx) ochazuke.Result { return a.yankValue(c) },
			"Copy the value (a table row's cell under the cursor, or the row on #) into the registers, its type kept for p, and the system clipboard").When("value-yankable").
		Verb("patches", func(c ochazuke.Ctx) ochazuke.Result {
			list := a.patchList()
			c.Info(a.patchSummary())
			return ochazuke.Done(list)
		}, "The value edits, in the order they apply, each with orphaned: true when its path is not in the restructured document (a structure edit removed or renamed it); an orphan is kept and skipped, and applies again once its path is back").Fetch().
		Verb("drop", func(c ochazuke.Ctx) ochazuke.Result {
			i := ochazuke.IntArg(c.Args, "index", 0) - 1
			if i < 0 || i >= len(a.rs.patches) {
				return ochazuke.Failed(fmt.Errorf("index is 1 to %d (value.patches lists them)", len(a.rs.patches)))
			}
			p := a.rs.patches[i]
			return done(a.commitPatches("drop "+p.Path, slices.Delete(slices.Clone(a.rs.patches), i, i+1)))
		}, "Forget one value edit (index, from 1, as value.patches lists them), as one undo step")
	r.Define("value-input").
		Verb("accept", func(c ochazuke.Ctx) ochazuke.Result { return done(a.accept()) },
			"Close the inline input and make its text the edit (enter, esc): the value, the new key's name (its value opens next), or the key").When("value-typing").
		Verb("cancel", func(c ochazuke.Ctx) ochazuke.Result {
			a.closeInput()
			return done(nil)
		}, "Close the inline input without changing anything").When("value-typing")
	params("value.edit", map[string]ochazuke.Parameter{
		"text":   {Type: "string", Description: "the new value as typed (a number, true, false, null, JSON, \"…\" for a string); omit to edit it in place"},
		"select": {Type: "boolean", Description: "open the input with the text selected; omitted, the rename.select setting decides"},
	})
	params("value.add", map[string]ochazuke.Parameter{
		"key":  {Type: "string", Description: "in an object, the new key; omit to type it"},
		"text": {Type: "string", Description: "the value as typed; omit to type it"},
	})
	params("value.rename", map[string]ochazuke.Parameter{"key": {Type: "string", Description: "the new key; omit to edit it in place"}})
	params("value.drop", map[string]ochazuke.Parameter{"index": {Type: "integer", Required: true, Description: "the value edit, from 1, as value.patches lists them"}})
	setHistory(r, ochazuke.HistoryChange, "value.edit", "value.delete", "value.add", "value.rename", "value.paste", "value.yank", "value.drop", "value-input.accept", "value-input.cancel")
	setHistory(r, ochazuke.HistoryFetch, "value.patches")
	setRepeatable(r, "value.yank")
}

// yankValue copies the value on the focus (item.yank, field.yank,
// value.yank).
func (a *App) yankValue(c ochazuke.Ctx) ochazuke.Result {
	v, ok := a.valueOf(c.Instance)
	if !ok || v.node == nil {
		return ochazuke.Failed(errors.New("no value here"))
	}
	return a.copyYank(c, valueYank(v.node), v.node.Path())
}

// patchStatus is one value edit as value.patches lists it.
type patchStatus struct {
	Patch
	Orphaned bool `json:"orphaned,omitempty"`
}

// orphans is the indexes of the value edits the restructured document
// skips.
func (a *App) orphans() []int {
	if a.Doc == nil || len(a.rs.patches) == 0 {
		return nil
	}
	d := a.restructuredDoc()
	if d.err != nil {
		out := make([]int, len(a.rs.patches))
		for i := range out {
			out[i] = i
		}
		return out
	}
	return d.orphans
}

func (a *App) patchList() []patchStatus {
	orphans := a.orphans()
	out := make([]patchStatus, 0, len(a.rs.patches))
	for i, p := range a.rs.patches {
		out = append(out, patchStatus{Patch: p, Orphaned: slices.Contains(orphans, i)})
	}
	return out
}

// patchSummary is the value edits in a line: how many, and the orphans.
func (a *App) patchSummary() string {
	n := len(a.rs.patches)
	if n == 0 {
		return "no value edits"
	}
	say := fmt.Sprintf("%d value edit%s", n, plural(n))
	if o := a.orphans(); len(o) > 0 {
		var paths []string
		for _, i := range o {
			p := a.rs.patches[i]
			paths = append(paths, fmt.Sprintf("%d %s %s", i+1, p.Op, p.Path))
		}
		say += fmt.Sprintf(", %d orphaned (no such path, skipped): %s", len(o), strings.Join(paths, "; "))
	}
	return say
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
