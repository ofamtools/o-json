package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/unixsuperhero/ochazuke"
)

// The function picker: ctrl+f (in the query line, or in query mode on
// the structure pane) opens a chooser of jq's builtins, each with its
// arity (gojq's builtins/0, so the list follows the engine). A function
// is written at the anchor with an empty slot (□, an empty chip) per
// argument, select(□), sub(□; □); in the line the first slot is selected,
// so typing fills it, and tab goes to the next. A slot holds anything,
// another call included: ctrl+f on a selected slot replaces it. The query
// does not run while a slot is empty.

const jqFuncChooser = "jq-functions"

// funcText is a function as the query writes it: its name and a slot per
// argument.
func funcText(f jqFunc) string {
	if f.Arity == 0 {
		return f.Name
	}
	return f.Name + "(" + strings.Repeat(string(jqSlot)+"; ", f.Arity-1) + string(jqSlot) + ")"
}

// openFunctions opens the picker; the chosen function goes in at the
// anchor (the line's select range, else its cursor or the slot there).
func (a *App) openFunctions() error {
	var rows []ochazuke.ChooserRow
	for _, f := range jqBuiltins() {
		args := ""
		if f.Arity > 0 {
			args = "(" + strings.TrimSuffix(strings.Repeat("f; ", f.Arity), "; ") + ")"
		}
		rows = append(rows, ochazuke.ChooserRow{ID: f.String(), Label: f.Name + args, Data: f, Cells: []string{f.Name + args, fmt.Sprintf("%d argument%s", f.Arity, plural(f.Arity))}})
	}
	if len(rows) == 0 {
		return errors.New("no jq builtins")
	}
	_, err := a.r.OpenChooser(ochazuke.ChooserSpec{
		Kind: jqFuncChooser, Title: "jq function", Rows: rows,
		// A name matches from its start or from a word inside it
		// (down finds ascii_downcase), so sub is not behind gsub.
		Filter: func(q string, row ochazuke.ChooserRow) bool {
			q, l := strings.ToLower(strings.TrimSpace(q)), strings.ToLower(row.Label)
			return strings.HasPrefix(l, q) || strings.Contains(l, "_"+q)
		},
		Choose: func(row ochazuke.ChooserRow) ochazuke.Result {
			f, _ := row.Data.(jqFunc)
			return a.queryDone(a.insertFunction(f))
		},
	})
	return err
}

// insertFunction writes f at the anchor with its slots, the first one
// selected in the focused line (typing fills it).
func (a *App) insertFunction(f jqFunc) error {
	in := a.q.in
	lo, hi := insertAt(in)
	if s, e, ok := in.SelectedText(); ok {
		lo, hi = s, e
	}
	text := funcText(f)
	if lo == hi && a.r.Focus != jqFieldID && jqContextAt(in.Text, lo).sepNeeded() {
		text = " | " + text
	}
	if err := in.Replace(lo, hi, text); err != nil {
		return err
	}
	in.Cursor = lo + len([]rune(text))
	if f.Arity == 0 {
		return nil
	}
	slot := lo + len([]rune(text[:strings.IndexRune(text, jqSlot)]))
	if a.r.Focus == jqFieldID {
		return in.SelectText(slot, slot+1)
	}
	in.Cursor = slot
	return nil
}

// nextSlot selects the next empty slot after the cursor (from the start
// past the last).
func (a *App) nextSlot() error {
	in := a.q.in
	from := in.Cursor
	if s, e, ok := in.SelectedText(); ok && e > s {
		from = e
	}
	i := slices.Index(in.Text[min(from, len(in.Text)):], jqSlot)
	if i >= 0 {
		i += from
	} else if i = slices.Index(in.Text, jqSlot); i < 0 {
		return errors.New("no empty slot")
	}
	if a.r.Focus == jqFieldID {
		return in.SelectText(i, i+1)
	}
	in.Cursor = i
	return nil
}

func (a *App) registerQueryFunctions() {
	r := a.r
	hasSlot := func() bool { return slices.Contains(a.q.in.Text, jqSlot) }
	r.DefineGuard("query-slot", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return ev.Focus == jqFieldID && hasSlot()
	})
	r.DefineGuard("query-functions", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return a.queryOn() && a.Doc != nil && (ev.Focus == jqFieldID || a.queryModeOn())
	})
	r.Define("jq").
		Verb("function", func(c ochazuke.Ctx) ochazuke.Result {
			if name := ochazuke.StringArg(c.Args, "name", ""); name != "" {
				for _, f := range jqBuiltins() {
					if f.String() == name || f.Name == name {
						return a.queryDone(a.insertFunction(f))
					}
				}
				return ochazuke.Failed(fmt.Errorf("no jq builtin %q", name))
			}
			return a.queryDone(a.openFunctions())
		}, "Pick a jq builtin and write it at the anchor, an empty slot per argument (the first selected: typing fills it, tab goes to the next); with name (sub or sub/2), that one (ctrl+f)").When("query-functions").
		Verb("next-slot", func(c ochazuke.Ctx) ochazuke.Result {
			return a.queryDone(a.nextSlot())
		}, "Select the query's next empty argument slot (tab)").When("query-slot")
	op := r.Operations[r.CanonicalKey("jq.function")]
	op.Parameters = map[string]ochazuke.Parameter{"name": {Type: "string", Description: "a builtin, name or name/arity; omit to pick one"}}
	r.Operations[r.CanonicalKey("jq.function")] = op
	setHistory(r, ochazuke.HistoryChange, "jq.function")
	setHistory(r, ochazuke.HistoryAction, "jq.next-slot")
}
