package main

import (
	"slices"

	"github.com/unixsuperhero/ochazuke"
)

// What the data pane shows for the structure tree's selection.
//
// A collection (a shape whose values include an object or an array) is
// shown: its table, its fields, its JSON. A shape of plain values (no
// object, no array among them) is not shown on its own under
// structure.scalar-follow highlight (the default): the data pane keeps
// the collection that holds it (the one it already shows, else the
// nearest one up) and highlights the shape there, its column in a table
// (the cursor's column moves to it, so the view scrolls to show it; the
// row stays) or its field row in a fields view (the cursor goes to it).
// show is the behaviour before: the values alone, a table of them or one
// value. The document itself, a plain value, has no collection to keep
// and is shown either way.

const settingScalarFollow = "structure.scalar-follow"

// toneFollow marks, in the data pane, the column or field row of the
// plain-value shape the structure tree's cursor is on: the same tone as
// the restructure page's origin highlight (toneOrigin), the accent.
const toneFollow = toneOrigin

func declareFollowSettings(r *ochazuke.Runtime) error {
	return declareOwn(r, ochazuke.Setting{
		Name: settingScalarFollow, Type: ochazuke.SettingString, Default: "highlight", Enum: []string{"highlight", "show"},
		Description: "The structure cursor on a path of plain values (no object or array): highlight keeps the data pane on the table or fields that hold it and highlights its column (scrolled into view) or field row; show shows its values alone, as before",
	})
}

// isCollection reports a shape whose values include an object or an array.
func isCollection(s *Shape) bool { return s.Kinds[KObject] > 0 || s.Kinds[KArray] > 0 }

// dataTarget is the shape the data pane shows for the selected shape s,
// and the shape it highlights there (nil for none).
func (a *App) dataTarget(s *Shape) (shown, lit *Shape) {
	if s == nil || isCollection(s) || a.r.Settings().String(settingScalarFollow) == "show" {
		return s, nil
	}
	up := s.Parent()
	if up == nil {
		return s, nil
	}
	// The pane already shows the holder: the shape up itself, or the one
	// array whose items up is (the same rows as a table).
	if cur := a.shown; cur != nil && (cur == up || up.Key == "[]" && up.Parent() == cur && a.tab != nil) {
		return cur, s
	}
	return up, s
}

// display points the data pane at what the selected shape s calls for
// (dataTarget): shown again when it changed or force says so, and the
// highlight put where s is.
func (a *App) display(s *Shape, force bool) error {
	target, lit := a.dataTarget(s)
	if target == nil {
		return nil
	}
	if force || target != a.shown {
		if err := a.show(target); err != nil {
			return err
		}
	} else if lit == a.lit {
		return nil
	}
	return a.light(lit)
}

// light highlights lit in the data pane: its column in a table (the
// cursor's column moved to it, the row kept) or its field row in a fields
// view (the cursor moved to it); nil highlights nothing. The cursor moves
// when the highlighted path changes, not when the same one is lit again.
func (a *App) light(lit *Shape) error {
	a.lit = lit
	col, row := -1, -1
	if lit != nil {
		switch {
		case a.tab != nil:
			if j := slices.IndexFunc(a.tab.Columns, func(c TableColumn) bool {
				if lit.Key == "[]" {
					return c.Self
				}
				return !c.Self && c.Name == lit.Key
			}); j >= 0 {
				col = j + 1 // after the "#" column
			}
		case a.fields != nil:
			row = slices.IndexFunc(a.fields.Children, func(c *Node) bool { return c.Key == lit.Key })
		}
	}
	a.data.litCol, a.data.litRow = col, row
	// The cursor moves only when the tree's cursor went to another path:
	// the same path shown again (a value edit, an undo, a rebuild) keeps
	// it where the data pane's own keys put it.
	moved := lit != nil && lit.Path != a.litPath
	a.litPath = ""
	if lit != nil {
		a.litPath = lit.Path
	}
	if moved && col >= 0 {
		a.data.list.SetColumn(col)
	}
	if moved && row >= 0 {
		return a.data.to(row)
	}
	return a.data.refresh()
}
