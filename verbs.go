package main

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/unixsuperhero/ochazuke"
)

// The nouns and verbs. Keys are bound in bindings.go; a verb without a
// key is reached through the palette (:). Nothing here writes a file; the
// document verbs (open.go) write only viewer.write's new file.
//
//	viewer   the page: open, load (reload), new, paste, write, files, recent, search, next/previous match, type filter, raw, clear, state
//	shape    a structure tree row: yank (the path)
//	rows     a windowed table (data panel, detail): move, column (along a hover sub-table, hover.go)
//	item     a data table row: open (the detail), yank (the row as JSON)
//	field    a data panel field: open, yank
//	entry    a detail row: open (drill in), up, yank
//	line     a raw JSON line: yank
//	value    a data pane value: edit, delete, add, rename, paste, yank, patches, drop (values.go)

// typeCycle is the order t steps the type filter through, then off.
var typeCycle = []Kind{KString, KNumber, KBool, KNull, KObject, KArray}

// registerNouns declares the nouns the tree and the panels are built of,
// before their instances exist.
func (a *App) registerNouns() {
	r := a.r
	for _, noun := range []string{"viewer", "shape", "item", "field", "entry", "line", detailID} {
		r.Define(noun)
	}
	r.Define("rows").Focusable()
}

func (a *App) registerVerbs() {
	r := a.r
	// The filters work on the original structure tree: the viewer shows it
	// (T), or the structure is not editable.
	r.DefineGuard("viewer-original", func(*ochazuke.Runtime, ochazuke.KeyEvent) bool { return !a.editedShown() })
	params := func(key string, p map[string]ochazuke.Parameter) {
		op := r.Operations[r.CanonicalKey(key)]
		op.Parameters = p
		r.Operations[r.CanonicalKey(key)] = op
	}
	done := func(err error) ochazuke.Result {
		if err != nil {
			return ochazuke.Failed(err)
		}
		return ochazuke.Done(a.state())
	}

	r.Define("viewer").
		Verb("load", func(c ochazuke.Ctx) ochazuke.Result {
			p := ochazuke.StringArg(c.Args, "path", a.Path)
			if p == "" {
				return ochazuke.Failed(errors.New("no file to load: e (viewer.open) opens one"))
			}
			return a.readAsking(p)
		}, "Read the current file again off the loop and show it (R); with path, that file, as viewer.open does (- is stdin). With restructure edits no write saved, it asks first (viewer.discard)").When("viewer-file").
		Verb("search", func(c ochazuke.Ctx) ochazuke.Result {
			if q, ok := c.Args["query"].(string); ok {
				return done(a.search(q))
			}
			return done(a.prompt("Search paths", a.Query, a.search))
		}, "Keep the structure paths whose key or path contains the query (with their ancestors) and jump to the first; empty clears it").When("viewer-original").
		Verb("next", func(ochazuke.Ctx) ochazuke.Result { return done(a.jump(1)) }, "Jump to the next structure path the filters match").Motion().When("viewer-original").
		Verb("previous", func(ochazuke.Ctx) ochazuke.Result { return done(a.jump(-1)) }, "Jump to the previous structure path the filters match").Motion().When("viewer-original").
		Verb("type", func(c ochazuke.Ctx) ochazuke.Result {
			if k, ok := c.Args["kind"].(string); ok {
				return done(a.filterKind(Kind(k)))
			}
			next := typeCycle[0]
			if i := slices.Index(typeCycle, a.Kind); i >= 0 {
				next = ""
				if i+1 < len(typeCycle) {
					next = typeCycle[i+1]
				}
			}
			return done(a.filterKind(next))
		}, "Keep the structure paths holding values of a type; without kind, step string, number, bool, null, object, array, then off").When("viewer-original").
		Verb("raw", func(ochazuke.Ctx) ochazuke.Result {
			a.Raw = !a.Raw
			return done(a.reshow())
		}, "Show the selected path's values as indented JSON instead of a table or fields (toggle)").
		Verb("clear", func(ochazuke.Ctx) ochazuke.Result {
			a.Query, a.Kind = "", ""
			return done(a.refilter())
		}, "Clear the search and the type filter").When("viewer-original").
		Verb("state", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(a.state()) }, "The viewer's file, selected path, filters and row").Fetch()
	params("viewer.load", map[string]ochazuke.Parameter{"path": {Type: "string", Description: "the JSON file; - reads stdin"}})
	params("viewer.search", map[string]ochazuke.Parameter{"query": {Type: "string", Description: "omit to be asked in a prompt"}})
	params("viewer.type", map[string]ochazuke.Parameter{"kind": {Type: "string", Enum: []string{"string", "number", "bool", "null", "object", "array", ""}, Description: "omit to step to the next type"}})
	// load reads the file and shows it: a fetch, as the framework's reloads are.
	setHistory(r, ochazuke.HistoryFetch, "viewer.load")
	a.registerOpenVerbs(params)
	setHistory(r, ochazuke.HistoryAction, "viewer.search", "viewer.next", "viewer.previous", "viewer.type", "viewer.raw", "viewer.clear")

	r.Define("shape").
		Verb("yank", func(c ochazuke.Ctx) ochazuke.Result {
			subjects := c.Subjects
			if len(subjects) == 0 {
				subjects = []*ochazuke.Instance{c.Instance}
			}
			var paths []string
			for _, inst := range subjects {
				if s, _ := inst.Data.(*Shape); s != nil {
					paths = append(paths, s.Path)
				}
			}
			if len(paths) == 0 {
				return ochazuke.Failed(errors.New("not a path"))
			}
			return a.copyText(c, strings.Join(paths, "\n"), strings.Join(paths, ", "))
		}, "Copy the structure path (one per line over a V selection) into the registers and the system clipboard; p / P on the restructured tree put those shapes there").Many()

	// rows.move is the windowed tables' motion: the list holds only the
	// rows on screen, so list.move would stop at the screen's edge.
	r.Define("rows").
		Verb("move", func(c ochazuke.Ctx) ochazuke.Result {
			v := a.rowsOf(c.Instance)
			if v == nil {
				return ochazuke.Failed(errors.New("no table here"))
			}
			steps := max(1, ochazuke.IntArg(c.Args, "steps", 1))
			motion := ochazuke.StringArg(c.Args, "motion", "")
			// On the data pane with table.nested hover, down and up and
			// the half and page motions walk the drawn lines, the open
			// sub-tables' among them (hover.go); top and bottom go to a
			// row and leave any sub-table.
			walk := v == a.data && a.hoverOn()
			if walk && (motion == "top" || motion == "bottom") {
				a.hover.path = nil
			}
			var err error
			switch motion {
			case "down":
				if walk {
					err = a.hoverDown(steps)
				} else {
					err = v.move(steps)
				}
			case "up":
				if walk {
					err = a.hoverUp(steps)
				} else {
					err = v.move(-steps)
				}
			case "half-down":
				if walk {
					err = a.hoverDown(steps * v.page(true))
				} else {
					err = v.move(steps * v.page(true))
				}
			case "half-up":
				if walk {
					err = a.hoverUp(steps * v.page(true))
				} else {
					err = v.move(-steps * v.page(true))
				}
			case "page-down":
				if walk {
					err = a.hoverDown(steps * v.page(false))
				} else {
					err = v.move(steps * v.page(false))
				}
			case "page-up":
				if walk {
					err = a.hoverUp(steps * v.page(false))
				} else {
					err = v.move(-steps * v.page(false))
				}
			case "top":
				err = v.to(ochazuke.IntArg(c.Args, "line", 1) - 1)
			case "bottom":
				line := ochazuke.IntArg(c.Args, "line", 0)
				if line > 0 {
					err = v.to(line - 1)
				} else {
					err = v.to(v.src.n - 1)
				}
			default:
				err = errors.New("motion is one of down, up, half-down, half-up, page-down, page-up, top, bottom")
			}
			if err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Done(v.sel)
		}, "Move the selected row: down, up, half-down, half-up, page-down, page-up, top, bottom").Motion()
	params("rows.move", map[string]ochazuke.Parameter{
		"motion": {Type: "string", Required: true, Enum: []string{"down", "up", "half-down", "half-up", "page-down", "page-up", "top", "bottom"}},
		"steps":  {Type: "integer", Description: "repeat count (default 1)"},
		"line":   {Type: "integer", Description: "1-based row for top and bottom"},
	})
	setHistory(r, ochazuke.HistoryAction, "rows.move")

	yankNode := func(c ochazuke.Ctx) ochazuke.Result {
		n := nodeAt(c.Instance)
		if n == nil {
			return ochazuke.Failed(errors.New("no value here"))
		}
		text := Text(n, 0)
		if n.IsContainer() {
			text = Pretty(n)
		}
		return a.copyText(c, text, n.Path())
	}
	open := func(c ochazuke.Ctx) ochazuke.Result {
		n := nodeAt(c.Instance)
		// In a sub-table (table.nested hover): the value under the
		// cursor, or the line's value holding a scalar.
		if h := a.hoverValue(c.Instance); h != nil {
			n = h
			if !n.IsContainer() {
				n = n.Parent
			}
		}
		return done(a.openDetail(n))
	}
	yankValue := func(c ochazuke.Ctx) ochazuke.Result { return a.yankValue(c) }
	r.Define("item").
		Verb("open", open, "Open the row's item in the detail as labeled fields").
		Verb("yank", yankValue, "Copy the value under the cursor into the registers and the system clipboard: the cell on a field column, the row's item as JSON on # (value.yank); p puts it with its type")
	r.Define("field").
		Verb("open", open, "Open the field's object or array in the detail").
		Verb("yank", yankValue, "Copy the field's value (JSON for an object or array) into the registers and the system clipboard (value.yank); p puts it with its type")
	r.Define("entry").
		Verb("open", func(c ochazuke.Ctx) ochazuke.Result { return done(a.drill(nodeAt(c.Instance))) }, "Step into the entry's object or array").
		Verb("up", func(ochazuke.Ctx) ochazuke.Result { return done(a.up()) }, "Step back out to the containing value; at the top, close the detail").
		Verb("yank", yankNode, "Copy the entry's value (JSON for an object or array) into the registers and the system clipboard")
	r.Define("line").
		Verb("yank", func(c ochazuke.Ctx) ochazuke.Result {
			line, _ := c.Instance.Data.(string)
			return a.copyText(c, line, "the line")
		}, "Copy the JSON line into the registers and the system clipboard")
	setHistory(r, ochazuke.HistoryAction, "item.open", "field.open", "entry.open", "entry.up")
	// A yank writes the registers and the clipboard, as input.yank does.
	setHistory(r, ochazuke.HistoryChange, "shape.yank", "item.yank", "field.yank", "entry.yank", "line.yank")
	// . yanks again at the focus: each yank reads the focused row, and the
	// repeat keeps only the register.
	setRepeatable(r, "shape.yank", "item.yank", "field.yank", "entry.yank", "line.yank")
}

// rowsOf is the windowed table an instance belongs to (the list itself or
// one of its rows).
func (a *App) rowsOf(inst *ochazuke.Instance) *rowsView {
	for _, v := range []*rowsView{a.data, a.detail} {
		if v != nil && inst != nil && (inst.ID == v.id || inst.Parent == v.id) {
			return v
		}
	}
	return nil
}

// copyText stores text in the registers (the request's register, else
// the active or unnamed one) and copies it to the system clipboard off the
// loop (from o-csv). The status line names what was copied; a clipboard
// failure is a notice, since the registers still hold the text.
func (a *App) copyText(c ochazuke.Ctx, text, what string) ochazuke.Result {
	return a.copyYank(c, ochazuke.Yank{Text: text}, what)
}

// copyYank is copyText for a yank with a shape (a value keeps its type).
func (a *App) copyYank(c ochazuke.Ctx, y ochazuke.Yank, what string) ochazuke.Result {
	register := ochazuke.StringArg(c.Args, "register", "")
	text := y.Text
	if err := a.r.StoreYank(y, register, false); err != nil {
		return ochazuke.Failed(err)
	}
	said := "copied " + what
	if register != "" {
		said += " into register " + register
	}
	clip := a.Clipboard
	if clip == nil {
		c.Info(said)
		return ochazuke.Done(y)
	}
	return ochazuke.Pending(func(context.Context) (any, error) { return y, clip(text) }).
		Then(func(c ochazuke.Ctx, _ any, err error) {
			if err != nil {
				c.Notify("clipboard: " + err.Error() + " (the registers have " + what + ")")
				return
			}
			c.Info(said)
		})
}

// search sets the query, filters the tree and jumps to the first match.
func (a *App) search(q string) error {
	a.Query = q
	if err := a.refilter(); err != nil {
		return err
	}
	if q == "" {
		return nil
	}
	return a.jumpFrom(-1, 1)
}

// filterKind sets the type filter and jumps to the first match.
func (a *App) filterKind(k Kind) error {
	if k != "" && !slices.Contains(typeCycle, k) {
		return errors.New("kind is one of string, number, bool, null, object, array")
	}
	a.Kind = k
	if err := a.refilter(); err != nil {
		return err
	}
	if k == "" {
		return nil
	}
	return a.jumpFrom(-1, 1)
}

// refilter rebuilds the tree for the filters, keeping the selection when
// it survives.
func (a *App) refilter() error {
	if a.Doc == nil {
		return nil
	}
	if err := a.rebuild(); err != nil {
		return err
	}
	return a.sync()
}

// reshow redraws the data panel for the selected shape.
func (a *App) reshow() error {
	if s := a.selectedShape(); s != nil {
		return a.show(s)
	}
	return nil
}

// matchList is the shapes the filters match, in tree order.
func (a *App) matchList() []*Shape {
	var out []*Shape
	if a.Doc == nil || a.Query == "" && a.Kind == "" {
		return nil
	}
	a.Doc.Shape.Walk(func(s *Shape, _ int) {
		if a.matches(s) && s.parent != nil {
			out = append(out, s)
		}
	})
	return out
}

// jump selects the next (dir 1) or previous (-1) match after the selected
// shape, wrapping.
func (a *App) jump(dir int) error {
	matches := a.matchList()
	if len(matches) == 0 {
		return errors.New("no search or type filter: / searches, t filters by type")
	}
	cur := slices.Index(matches, a.selectedShape())
	return a.jumpFrom(cur, dir)
}

func (a *App) jumpFrom(cur, dir int) error {
	matches := a.matchList()
	if len(matches) == 0 {
		return errors.New("no path matches")
	}
	i := cur + dir
	if cur < 0 && dir < 0 {
		i = len(matches) - 1
	}
	i = (i%len(matches) + len(matches)) % len(matches)
	if err := a.tree.Select(shapeID(matches[i])); err != nil {
		return err
	}
	if err := a.tree.Focus(); err != nil {
		return err
	}
	return a.sync()
}

// setHistory declares the history kind (Operation.HistoryKind) of each
// named verb, so none is left to the default: NounBuilder.Fetch marks a
// read where it is declared, and the builder has no method for a change or
// an action. An unknown key panics, so a renamed verb is renamed here too.
func setHistory(r *ochazuke.Runtime, kind string, keys ...string) {
	for _, key := range keys {
		op, ok := r.Operations[r.CanonicalKey(key)]
		if !ok {
			panic("setHistory: no operation " + key)
		}
		op.HistoryKind = kind
		r.Operations[r.CanonicalKey(key)] = op
	}
}

// setRepeatable marks each named verb Operation.Repeatable, so dot-repeat
// (history.repeat, .) runs it again at the current focus. The builder has
// no method for it either; an unknown key panics, as in setHistory.
func setRepeatable(r *ochazuke.Runtime, keys ...string) {
	for _, key := range keys {
		op, ok := r.Operations[r.CanonicalKey(key)]
		if !ok {
			panic("setRepeatable: no operation " + key)
		}
		op.Repeatable = true
		r.Operations[r.CanonicalKey(key)] = op
	}
}
