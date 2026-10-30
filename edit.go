package main

// EDIT (Design D): the data pane's value edits (r, i, x, d d, o, O, K, p)
// answer only in EDIT, which i enters (r enters it and opens the value
// editor on the value); esc leaves it, and so does leaving the pane: EDIT
// is per pane, as Vim's insert is per window. In TABLE the edit keys say
// how to get there (viewer.needs-edit) instead of changing anything. The
// setting modes.edit-gate off makes the edit keys answer in TABLE as in
// o-json. The detail's edits are not gated.

import (
	"errors"

	"github.com/unixsuperhero/ochazuke"
)

const settingEditGate = "modes.edit-gate"

// editGateDefault is modes.edit-gate's default (the test suite, written
// for o-json's ungated edits, turns it off in TestMain).
var editGateDefault = true

// editGated reports the data pane's value edits wait for EDIT.
func (a *App) editGated() bool {
	return a.r.Settings().Bool(settingEditGate) && !a.editing
}

// enterEdit is i (and r, with open): EDIT on the data pane.
func (a *App) enterEdit(c ochazuke.Ctx, open bool) ochazuke.Result {
	a.editing = true
	_ = a.r.SetMode("edit")
	a.syncModeAccent()
	if open {
		res, err := a.r.Execute(ochazuke.Request{Noun: "value", Verb: "edit", Target: a.r.Focus})
		if err != nil {
			return ochazuke.Failed(err)
		}
		return ochazuke.Done(res)
	}
	c.Info("EDIT: r/i edit · x/d d delete · o/O add · K rename · p put · esc leaves")
	return ochazuke.Done(nil)
}

// leaveEdit is esc in EDIT: back to the pane's declared mode.
func (a *App) leaveEdit() ochazuke.Result {
	a.editing = false
	_ = a.r.SetMode("normal")
	a.syncDeclaredMode()
	return ochazuke.Done(nil)
}

func (a *App) registerEdit() error {
	r := a.r
	if err := declareOwn(r, ochazuke.Setting{
		Name: settingEditGate, Type: ochazuke.SettingBoolean, Default: editGateDefault,
		Description: "The data pane's value edits (r, i, x, d d, o, O, K, p) answer only in EDIT (i enters it, r enters it and edits the value, esc leaves); off, they answer in the table as before",
	}); err != nil {
		return err
	}
	onData := func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool { return a.inData(ev.Focus) }
	r.DefineGuard("edit-enterable", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return onData(r, ev) && a.editGated() && a.dataEditable() && a.editedShown() && !a.queryShowing() && a.lines == nil
	})
	r.DefineGuard("edit-gated", func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		return onData(r, ev) && a.editGated()
	})
	r.DefineGuard("edit-on", func(*ochazuke.Runtime, ochazuke.KeyEvent) bool { return a.editing })
	r.Define("viewer").
		Verb("edit", func(c ochazuke.Ctx) ochazuke.Result {
			return a.enterEdit(c, c.Args["open"] == true)
		}, "Enter EDIT on the data pane (i), where the value keys change the document; with open (r), edit the value under the cursor at once").When("edit-enterable").
		Verb("edit-leave", func(ochazuke.Ctx) ochazuke.Result { return a.leaveEdit() },
			"Leave EDIT (esc): back to the data pane's view mode").When("edit-on").
		Verb("needs-edit", func(c ochazuke.Ctx) ochazuke.Result {
			return ochazuke.Failed(errors.New("this key changes the document: press i first (EDIT), esc when done"))
		}, "Say that a value edit needs EDIT (the edit keys in the table, modes.edit-gate)").When("edit-gated")
	setParams(r, "viewer.edit", map[string]ochazuke.Parameter{"open": {Type: "boolean", Description: "edit the value under the cursor at once (r)"}})
	setHistory(r, ochazuke.HistoryAction, "viewer.edit", "viewer.edit-leave", "viewer.needs-edit")
	return nil
}
