package main

import (
	"slices"

	"github.com/unixsuperhero/ochazuke"
)

// Stable columns: a table's columns are ordered by how many rows have
// them, the optional ones last (NewTable). A value edit can change those
// counts (K renames a key in one row, o adds one), and a table built
// again from scratch would move its columns under the cursor: a renamed
// key's column would jump among the optional ones. So while the same
// path is shown again (a value edit, an undo) the table keeps the order
// it had: the columns still there stay where they were, and a new one
// goes beside the column it came from (a rename's old key, the key an add
// was placed after or before), else after the column before it in the
// new order. After a rename or an add on a table row the cursor goes to
// the new key's column, so it stays on the edited cell.
//
// The setting table.stable-columns (default on) off orders the columns
// afresh each time.

const settingStableColumns = "table.stable-columns"

func declareColumnSettings(r *ochazuke.Runtime) error {
	return declareOwn(r, ochazuke.Setting{
		Name: settingStableColumns, Type: ochazuke.SettingBoolean, Default: true,
		Description: "A value edit keeps the data table's column order: a renamed key's column stays where the old one was and a new key's column goes beside the one it was added next to, and the cursor follows the edited key; off orders the columns by how many rows have them (optional last) after every edit",
	})
}

// columnAnchor is where a new column goes: beside key, before it or
// after it.
type columnAnchor struct {
	key    string
	before bool
}

// colKey names a column: its key, or "" for the (value) column.
func colKey(c TableColumn) string {
	if c.Self {
		return ""
	}
	return "." + c.Name
}

// stableColumns is next in prev's order: the columns of prev still in
// next where they were, each new one beside its anchor (by key) or after
// the column before it in next.
func stableColumns(prev, next []TableColumn, anchors map[string]columnAnchor) []TableColumn {
	order := make([]string, 0, len(prev)+len(next))
	for _, c := range prev {
		order = append(order, colKey(c))
	}
	byKey := map[string]TableColumn{}
	for _, c := range next {
		byKey[colKey(c)] = c
	}
	known := func(k string) int { return slices.Index(order, k) }
	for i, c := range next {
		k := colKey(c)
		if known(k) >= 0 {
			continue
		}
		at := -1 // insert after this index of order
		if an, ok := anchors[c.Name]; ok && !c.Self && known("."+an.key) >= 0 {
			at = known("." + an.key)
			if an.before {
				at--
			}
		} else {
			for j := i - 1; j >= 0; j-- {
				if p := known(colKey(next[j])); p >= 0 {
					at = p
					break
				}
			}
		}
		order = slices.Insert(order, at+1, k)
	}
	out := make([]TableColumn, 0, len(next))
	for _, k := range order {
		if c, ok := byKey[k]; ok {
			out = append(out, c)
		}
	}
	return out
}

// columnAnchors are the new keys the value edits made and where each
// came from: a rename's old key, an add's After or Before.
func (a *App) columnAnchors() map[string]columnAnchor {
	out := map[string]columnAnchor{}
	for _, p := range a.rs.patches {
		steps, err := parsePath(p.Path)
		if err != nil || len(steps) == 0 || steps[len(steps)-1].kind != 'k' {
			continue
		}
		last := steps[len(steps)-1].key
		switch {
		case p.Op == opRename:
			out[p.Key] = columnAnchor{key: last}
		case p.Op == opAdd && p.After != "":
			out[last] = columnAnchor{key: p.After}
		case p.Op == opAdd && p.Before != "":
			out[last] = columnAnchor{key: p.Before, before: true}
		}
	}
	return out
}

// followPatch notes the key a rename or an add on a table row made, so
// the next show puts the cursor on its column (stable columns on, not in
// a hover sub-table, whose walk keeps its own cursor).
func (a *App) followPatch(p Patch) {
	if a.tab == nil || a.hoverIn() || a.rowsOf(a.r.Instances[a.r.Focus]) != a.data || !a.r.Settings().Bool(settingStableColumns) {
		return
	}
	steps, err := parsePath(p.Path)
	if err != nil || len(steps) < 2 || steps[len(steps)-1].kind != 'k' {
		return
	}
	switch p.Op {
	case opRename:
		a.followKey = p.Key
	case opAdd:
		a.followKey = steps[len(steps)-1].key
	}
}
