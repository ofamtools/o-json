package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
)

// The value editor (value.editor modal, the default): r / i, K and o / O
// open a compact modal over the table, which stays drawn as it is behind
// it, instead of an input drawn over the row (value.editor inline). It is
// ochazuke's prompt modal (OpenModal with its inputs under the body, the
// body drawing itself as a ModalBody), with the record editor's look:
//
//	╭──────────────────────────────────────────────────────╮
//	│ .items[1].name                                       │  the value's path
//	│ was    beta                                   string │  the old value, dimmed
//	│ value  omega▏                                [string]│  the input, its type
//	│                                                      │
//	│ enter set · esc cancel                               │  the keys (bindings)
//	╰──────────────────────────────────────────────────────╯
//
// The badge is the type the text will be set as, read as it is typed
// (inferValue); what the text will do that a reader may not expect (a key
// the object has, numbered: "renames to the key "id-2"") is a note line
// in the modal, and enter always sets. o / O in an object ask two fields,
// the key then the value (enter or tab moves on, shift+tab back); an
// object or array value is a several-line input, where enter is a new
// line, esc normal mode, and enter there sets. enter makes the one change
// (one undo step: u undoes it), esc cancels with nothing changed; the
// focus goes back to the row or sub-table line it came from.

const (
	settingValueEditor = "value.editor"
	// settingValueLeave is what esc in normal mode does in the value
	// editor and the inline input: set (the default) makes the text the
	// edit, as a record editor keeps each change (u undoes it); cancel
	// closes it unchanged, as a prompt does.
	settingValueLeave = "value.leave"
	// settingValueStableHeight keeps a row in the value editor for each
	// note it can show, so a note appearing as the text is typed never
	// moves the box.
	settingValueStableHeight = "value.stable-height"
	leaveSet                 = "set"
	leaveCancel              = "cancel"
	editorModal              = "modal"
	editorInline             = "inline"
	// valueEditorID is the modal (its body's id and noun); its inputs are
	// value-field (one line) and value-text (several lines).
	valueEditorID = "value-editor"
	valueFieldID  = valueEditorID + "/value"
	valueKeyID    = valueEditorID + "/key"
	// valueModalWidth is the body's width before a long value widens it
	// (up to valueModalWide), both inside the modal bounds.
	valueModalWidth = 56
	valueModalWide  = 76
)

func declareValueEditorSetting(r *ochazuke.Runtime) error {
	return declareOwn(r, ochazuke.Setting{
		Name: settingValueEditor, Type: ochazuke.SettingString, Default: editorModal, Enum: []string{editorModal, editorInline},
		Description: "How r / i, K and o / O edit a value in the data pane and the detail: modal opens a compact editor over the table (the value's path, its type as typed, the old value); inline draws the input over the row. In both enter sets, esc goes to normal mode with the text kept, and a second esc leaves as value.leave says",
	}, ochazuke.Setting{
		Name: settingValueLeave, Type: ochazuke.SettingString, Default: leaveSet, Enum: []string{leaveSet, leaveCancel},
		Description: "What esc in normal mode does in the value editor and the inline input (the first esc goes from insert to normal, the text kept): set makes the text the edit, one undo step (u undoes it), as a record editor keeps each change; cancel closes it with nothing changed, as a prompt does. q in normal mode always cancels",
	}, ochazuke.Setting{
		Name: settingValueStableHeight, Type: ochazuke.SettingBoolean, Default: true,
		Description: "The value editor keeps a row for each note it can show (JSON that does not parse, a key the object has), so a note appearing as you type never moves the box or its keys; off, the box fits the notes it shows",
	})
}

// leaveSets reports value.leave set.
func (a *App) leaveSets() bool {
	return a.r.Settings().String(settingValueLeave) != leaveCancel
}

// modalEditor reports value.editor modal.
func (a *App) modalEditor() bool {
	return a.r.Settings().String(settingValueEditor) != editorInline
}

// editField is one field of the value editor: a key or a value.
type editField struct {
	key   bool // a key (no type badge)
	in    *ochazuke.Input
	was   string // the text before, dimmed above the field
	had   bool   // there was a text before (an edit, a rename)
	kind  Kind   // the old value's type (a value field with was)
	fresh bool   // still the text it opened with (a new key's placeholder, null)
}

// valueModal is the open value editor: what it edits (ve) and its fields.
type valueModal struct {
	a      *App
	ve     *valueEdit
	title  string
	fields []*editField
}

// openModal opens the value editor for ve, its first field holding text
// (selected when sel).
func (a *App) openModal(ve *valueEdit, text string, sel bool) error {
	view := a.rowsOf(a.r.Instances[a.r.Focus])
	if view == nil {
		view = a.data
	}
	if ve.view == nil {
		ve.view = view
	}
	ve.sub = ve.view == a.data && a.hoverIn()
	vm := &valueModal{a: a, ve: ve, title: ve.title()}
	switch ve.what {
	case "edit":
		f := &editField{in: ochazuke.NewInput(valueFieldID, text, false), was: text}
		if n := ve.at.node; n != nil {
			f.kind, f.had = n.Kind, true
			if n.IsContainer() {
				f.in = ochazuke.NewInput(valueFieldID, Pretty(n), true)
				f.was = oneLine(Compact(n, 0))
			}
		}
		vm.fields = []*editField{f}
	case "rename":
		vm.fields = []*editField{{key: true, in: ochazuke.NewInput(valueKeyID, text, false), was: text, had: true}}
	case "key":
		// o / O in an object: the key, then the value, in one modal.
		ve.what = "add"
		vm.fields = []*editField{
			{key: true, in: ochazuke.NewInput(valueKeyID, text, false), fresh: true},
			{in: ochazuke.NewInput(valueFieldID, "null", false), fresh: true},
		}
	default: // add: an item, or a value under a key given
		vm.fields = []*editField{{in: ochazuke.NewInput(valueFieldID, text, false), fresh: true}}
	}
	ve.modal = vm
	first := vm.fields[0]
	ve.in, ve.before = first.in, string(first.in.Text)
	for _, f := range vm.fields {
		f.in.Cursor = len(f.in.Text)
	}
	inst, err := a.r.OpenModal(valueEditorID, ochazuke.Payload{Prompt: vm.title}, ochazuke.WithInput(first.in, ochazuke.Kind(first.noun())))
	if err != nil {
		return err
	}
	inst.Component = vm
	for _, f := range vm.fields[1:] {
		if err := a.r.AddInput(f.in, ochazuke.Parent(valueEditorID), ochazuke.Kind(f.noun())); err != nil {
			_ = a.r.Pop()
			return err
		}
	}
	a.vedit = ve
	if err := a.r.SetMode("insert"); err != nil {
		return err
	}
	if sel && text != "" {
		return first.in.SelectAll()
	}
	return nil
}

// noun is the field's input noun: value-text for several lines.
func (f *editField) noun() string {
	if f.in.Multiline {
		return "value-text"
	}
	return "value-field"
}

// title is the modal's title: the value's path, and for a key or a new
// value what is done there.
func (ve *valueEdit) title() string {
	at := ve.at
	switch ve.what {
	case "rename":
		return at.path() + " · rename key"
	case "key", "add":
		where := "after"
		if ve.above {
			where = "before"
		}
		name := at.key
		if at.node != nil && at.parent != nil && at.parent.Kind == KArray {
			name = "[" + strconv.Itoa(at.node.Index) + "]"
		}
		if at.node == nil {
			return at.parent.Path() + " · add"
		}
		return at.parent.Path() + " · add " + where + " " + name
	}
	return at.path()
}

// focused is the field holding the focus, its index; nil off the modal.
func (vm *valueModal) focused() (*editField, int) {
	for i, f := range vm.fields {
		if f.in.ID == vm.a.r.Focus {
			return f, i
		}
	}
	return nil, -1
}

// move focuses the field delta away, in insert mode, a text still as it
// opened selected so typing replaces it.
func (vm *valueModal) move(delta int) error {
	_, i := vm.focused()
	j := i + delta
	if i < 0 || j < 0 || j >= len(vm.fields) {
		return errors.New("no field there")
	}
	f := vm.fields[j]
	if err := vm.a.r.SetFocus(f.in.ID); err != nil {
		return err
	}
	vm.ve.in = f.in
	if err := vm.a.r.SetMode("insert"); err != nil {
		return err
	}
	if f.fresh && string(f.in.Text) == vm.opened(j) && len(f.in.Text) > 0 {
		return f.in.SelectAll()
	}
	return nil
}

// opened is the text field i opened with.
func (vm *valueModal) opened(i int) string {
	if i == 0 {
		return vm.ve.before
	}
	return "null"
}

// texts is the key and the value as typed ("" for a field the modal has
// not).
func (vm *valueModal) texts() (key, value string) {
	for _, f := range vm.fields {
		if f.key {
			key = string(f.in.Text)
		} else {
			value = string(f.in.Text)
		}
	}
	return key, value
}

// notes are what the text will do that a reader may not expect: a new
// key that is taken or empty (another is chosen), JSON that does not
// parse (set as a string).
func (vm *valueModal) notes() []string {
	ve := vm.ve
	key, value := vm.texts()
	var out []string
	if ve.what == "add" && ve.at.inObject() && len(vm.fields) == 2 {
		if k := vm.a.newValueKey(ve, key); k != key {
			out = append(out, fmt.Sprintf("adds the key %q", k))
		}
	}
	if ve.what == "rename" {
		if k := vm.a.renameKey(ve.at, key); k != key {
			out = append(out, fmt.Sprintf("renames to the key %q", k))
		}
	}
	t := strings.TrimSpace(value)
	if t != "" && strings.ContainsRune("{[\"", rune(t[0])) && !json.Valid([]byte(t)) {
		out = append(out, "not valid JSON: sets it as a string")
	}
	return out
}

// noteRows is how many notes this editor can show (notes says which):
// the rows value.stable-height keeps for them.
func (vm *valueModal) noteRows() int {
	n := 0
	if vm.ve.what == "add" && vm.ve.at.inObject() && len(vm.fields) == 2 || vm.ve.what == "rename" {
		n++ // the key it will take
	}
	for _, f := range vm.fields {
		if !f.key {
			n++ // not valid JSON
		}
	}
	return n
}

// newValueKey is the key an add names: the typed one, the placeholder for an
// empty one, numbered past the object's keys.
func (a *App) newValueKey(ve *valueEdit, key string) string {
	taken := map[string]bool{}
	for _, c := range ve.at.parent.Children {
		taken[c.Key] = true
	}
	if key == "" {
		key = a.freeValueKey(ve.at.parent)
	}
	return freeKey(key, taken)
}

// setModal makes the editor's text the edit and closes it.
func (a *App) setModal() error {
	ve := a.vedit
	if ve == nil || ve.modal == nil {
		return errors.New("no value is being edited")
	}
	vm := ve.modal
	key, text := vm.texts()
	a.vedit = nil
	if a.r.Instances[valueEditorID] != nil {
		if err := a.r.Pop(); err != nil {
			return err
		}
	}
	switch ve.what {
	case "edit":
		if text == ve.before {
			return nil
		}
		raw, _ := inferValue(text)
		return a.onValue(ve.at, func(v valueAt) error { return a.setValue(v, raw) })
	case "rename":
		return a.onValue(ve.at, func(v valueAt) error { return a.renameValue(v, key) })
	case "add":
		raw, _ := inferValue(text)
		k, above := ve.key, ve.above
		if len(vm.fields) == 2 {
			k = a.newValueKey(ve, key)
		}
		return a.onValue(ve.at, func(v valueAt) error { return a.addPatch(addBeside(v, k, raw, above)) })
	}
	return nil
}

// cancelModal closes the editor, nothing changed.
func (a *App) cancelModal() error {
	a.vedit = nil
	if a.r.Instances[valueEditorID] != nil {
		return a.r.Pop()
	}
	return nil
}

// ModalTitle is the value's path.
func (vm *valueModal) ModalTitle() string { return vm.title }

// ModalBlock draws the fields: per field the old text dimmed with its
// type, then a label, the input and the type the text reads as; then
// the notes.
func (vm *valueModal) ModalBlock(s ochazuke.Styles, width, height int) ochazuke.Block {
	const gutter = 7 // "value  "
	// The badge column fits the widest type as the theme's badge draws
	// it (its own padding included); tags are right-aligned in it.
	badge := func(kind Kind) string { return s.Badge.Render(string(kind)) }
	badgeW := ansi.StringWidth(badge(KBool)) + 2
	w := min(width, valueModalWidth)
	for _, f := range vm.fields {
		if long := gutter + ansi.StringWidth(f.was) + badgeW + 2; long > w {
			w = min(width, max(w, min(long, valueModalWide)))
		}
	}
	inW := max(1, w-gutter-badgeW-1)
	var lines []string
	var cursor *ochazuke.Block
	label := func(text string, on bool) string {
		style := s.Muted
		if on {
			style = s.Accent
		}
		return style.Render(text) + strings.Repeat(" ", max(0, gutter-ansi.StringWidth(text)))
	}
	right := func(left string, tag string) string {
		gap := max(1, w-ansi.StringWidth(left)-ansi.StringWidth(tag))
		return left + strings.Repeat(" ", gap) + tag
	}
	// muted is a dimmed tag as wide as the badge would draw it.
	muted := func(kind Kind) string {
		pad := max(0, ansi.StringWidth(badge(kind))-len(kind))
		return strings.Repeat(" ", pad/2) + s.Muted.Render(string(kind)) + strings.Repeat(" ", pad-pad/2)
	}
	focus, _ := vm.focused()
	for _, f := range vm.fields {
		if f.had {
			tag := ""
			if !f.key && f.kind != "" {
				tag = muted(f.kind)
			}
			was := s.Muted.Render(ansi.Truncate(f.was, inW, "…"))
			lines = append(lines, right(label("was", false)+was, tag))
		}
		name := "value"
		if f.key {
			name = "key"
		}
		h := 1
		if f.in.Multiline {
			h = min(max(3, strings.Count(string(f.in.Text), "\n")+1), max(3, height-len(vm.fields)*2-3))
		}
		block := ochazuke.InputBlock(f.in, inW, h)
		rows := strings.Split(block.Text, "\n")
		for len(rows) < h {
			rows = append(rows, "")
		}
		tag := ""
		if !f.key {
			_, kind := inferValue(string(f.in.Text))
			tag = badge(kind)
		}
		if f == focus && block.Cursor != nil {
			c := block.Offset(gutter, len(lines))
			cursor = &c
		}
		for i, row := range rows {
			lead := strings.Repeat(" ", gutter)
			if i == 0 {
				lead = label(name, f == focus)
			}
			line := lead + pad(row, inW)
			if i == 0 && tag != "" {
				line = right(line, tag)
			}
			lines = append(lines, line)
		}
	}
	// The notes: under the fields after a blank line, or with
	// value.stable-height right under them in rows kept for every note
	// the editor can show, so one appearing never moves the box.
	notes := vm.notes()
	stable := vm.a.r.Settings().Bool(settingValueStableHeight)
	if len(notes) > 0 && !stable {
		lines = append(lines, "")
	}
	for _, n := range notes {
		lines = append(lines, s.Muted.Render(ansi.Truncate("· "+n, w, "…")))
	}
	if stable {
		for range vm.noteRows() - len(notes) {
			lines = append(lines, "")
		}
	}
	lines = append(lines, "") // air above the keys
	lines[0] = pad(lines[0], w)
	out := ochazuke.Text(strings.Join(lines, "\n"))
	if cursor != nil {
		out = cursor.WithText(out.Text)
	}
	return out
}

// registerValueEditor defines the modal, its inputs and its verbs.
func (a *App) registerValueEditor() {
	r := a.r
	r.Define("value-field")
	r.DefineGuard("value-leave-sets", func(*ochazuke.Runtime, ochazuke.KeyEvent) bool { return a.leaveSets() })
	r.Define("value-text")
	field := func(name string, ok func(vm *valueModal, i int) bool) {
		r.DefineGuard(name, func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
			if a.vedit == nil || a.vedit.modal == nil {
				return false
			}
			for i, f := range a.vedit.modal.fields {
				if f.in.ID == ev.Focus {
					return ok(a.vedit.modal, i)
				}
			}
			return false
		})
	}
	field("value-editor-open", func(*valueModal, int) bool { return true })
	field("value-editor-last", func(vm *valueModal, i int) bool { return i == len(vm.fields)-1 })
	field("value-editor-more", func(vm *valueModal, i int) bool { return i < len(vm.fields)-1 })
	field("value-editor-back", func(_ *valueModal, i int) bool { return i > 0 })
	done := func(err error) ochazuke.Result {
		if err != nil {
			return ochazuke.Failed(err)
		}
		return ochazuke.Done(a.patchList())
	}
	r.Define(valueEditorID).
		Verb("set", func(c ochazuke.Ctx) ochazuke.Result { return done(a.setModal()) },
			"Close the value editor and make its text the edit, one undo step: the value (its type as the badge shows), the key renamed to, or the key and value added (a key the object has numbered, as the editor's note says)").When("value-editor-last").
		Verb("next", func(c ochazuke.Ctx) ochazuke.Result { return done(a.vedit.modal.move(1)) },
			"Focus the value editor's next field (o in an object: the key, then the value)").When("value-editor-more").
		Verb("previous", func(c ochazuke.Ctx) ochazuke.Result { return done(a.vedit.modal.move(-1)) },
			"Focus the value editor's previous field").When("value-editor-back").
		Verb("cancel", func(c ochazuke.Ctx) ochazuke.Result { return done(a.cancelModal()) },
			"Close the value editor without changing anything").When("value-editor-open").
		Verb("leave", func(c ochazuke.Ctx) ochazuke.Result {
			if a.leaveSets() {
				return done(a.setModal())
			}
			return done(a.cancelModal())
		}, "Leave the value editor as value.leave says: set (the default) makes its text the edit from any field, cancel closes it with nothing changed (esc in normal mode)").When("value-editor-open")
	setHistory(r, ochazuke.HistoryChange, valueEditorID+".set", valueEditorID+".leave")
	setHistory(r, ochazuke.HistoryAction, valueEditorID+".next", valueEditorID+".previous", valueEditorID+".cancel")
}
