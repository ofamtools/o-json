package main

// Window keys in Vim's form: ctrl+w h / l move to the pane on that side
// and stop at the edge (viewer.pane), ctrl+w o shows only one pane
// (viewer.only) and ctrl+w o again or ctrl+w = brings the other back.
// Which pane ctrl+w o keeps is data: each mode's binding rows give
// viewer.only its pane (SHAPE: structure; TABLE, NESTED, FLAT, JSON:
// data; JQ, SPLIT and the rest: the focused one), so the footer's hint
// names it. Moving the focus to the hidden pane (ctrl+a o, a command)
// brings the layout back, as Vim's window keys would find no window there.

import (
	"errors"

	"github.com/unixsuperhero/ochazuke"
)

const (
	paneStructure = "structure"
	paneData      = "data"
)

// paneOnly is the viewer pane ctrl+w o keeps ("" for both). A focus on
// the hidden pane restores both.
func (a *App) paneOnly(structureFocused, dataFocused bool) string {
	if a.only == paneStructure && dataFocused || a.only == paneData && structureFocused {
		a.only = ""
	}
	return a.only
}

// sideOnly is the restructure page's side ctrl+w o keeps (its tree's
// instance, "" for both); a focus on the other side restores both.
func (a *App) sideOnly() string {
	if a.rsOnly != "" {
		if side := a.sideOf(a.r.Focus); side != "" && side != a.rsOnly {
			a.rsOnly = ""
		}
	}
	return a.rsOnly
}

// focusStructure focuses the structure pane the viewer shows: the edited
// tree, or the original.
func (a *App) focusStructure() error {
	if a.editedShown() {
		return a.rs.tree.Focus()
	}
	return a.tree.Focus()
}

// structureFocused reports the focus in the viewer's structure pane.
func (a *App) structureFocused() bool {
	return a.tree.List().Focused() || a.rs.tree.List().Focused() || a.r.Focus == treeID || a.r.Focus == rsTreeID
}

// onlyPane is viewer.only: pane is structure, data, focused or none.
// With one pane already alone, it restores both (ctrl+w o again).
func (a *App) onlyPane(c ochazuke.Ctx, pane string) error {
	if a.r.Page == rsPageID {
		if a.rsOnly != "" || pane == "none" {
			a.rsOnly = ""
			c.Info("both sides shown")
			return nil
		}
		side := a.sideOf(a.r.Focus)
		if side == "" {
			return errors.New("focus a side first")
		}
		a.rsOnly = side
		c.Info("this side only · ctrl+w o again or ctrl+w = restores")
		return nil
	}
	if a.only != "" || pane == "none" {
		a.only = ""
		c.Info("both panes shown")
		return nil
	}
	if pane == "focused" || pane == "" {
		pane = paneStructure
		if a.inData(a.r.Focus) {
			pane = paneData
		}
	}
	switch pane {
	case paneStructure:
		if !a.structureFocused() {
			if err := a.focusStructure(); err != nil {
				return err
			}
		}
	case paneData:
		if !a.inData(a.r.Focus) {
			if err := a.data.list.Focus(); err != nil {
				return err
			}
		}
	default:
		return errors.New("pane is structure, data, focused or none")
	}
	a.only = pane
	c.Info(pane + " pane only · ctrl+w o again or ctrl+w = restores")
	return nil
}

// movePane is viewer.pane: to is left or right; at the edge it stays.
func (a *App) movePane(to string) error {
	if a.r.Page == rsPageID {
		if to == "left" {
			return a.rs.orig.Focus()
		}
		return a.rs.tree.Focus()
	}
	if to == "left" {
		if a.structureFocused() {
			return nil
		}
		return a.focusStructure()
	}
	if a.inData(a.r.Focus) {
		return nil
	}
	return a.data.list.Focus()
}

func (a *App) registerPaneVerbs() {
	r := a.r
	done := func(err error) ochazuke.Result {
		if err != nil {
			return ochazuke.Failed(err)
		}
		return ochazuke.Done(nil)
	}
	r.DefineGuard("pane-alone", func(*ochazuke.Runtime, ochazuke.KeyEvent) bool {
		if a.r.Page == rsPageID {
			return a.rsOnly != ""
		}
		return a.only != ""
	})
	r.Define("viewer").
		Verb("only", func(c ochazuke.Ctx) ochazuke.Result {
			return done(a.onlyPane(c, ochazuke.StringArg(c.Args, "pane", "focused")))
		}, "Show only one pane (ctrl+w o): structure, data or the focused one, as the mode's row says; again, or with pane none, both come back").
		Verb("restore", func(c ochazuke.Ctx) ochazuke.Result {
			return done(a.onlyPane(c, "none"))
		}, "Show both panes again after ctrl+w o (ctrl+w =)").When("pane-alone").
		Verb("pane", func(c ochazuke.Ctx) ochazuke.Result {
			return done(a.movePane(ochazuke.StringArg(c.Args, "to", "right")))
		}, "Move the focus to the pane on the left or right (ctrl+w h, ctrl+w l); at the edge it stays, as in Vim")
	setParams(r, "viewer.only", map[string]ochazuke.Parameter{"pane": {Type: "string", Enum: []string{paneStructure, paneData, "focused", "none"}, Description: "default focused"}})
	setParams(r, "viewer.pane", map[string]ochazuke.Parameter{"to": {Type: "string", Enum: []string{"left", "right"}, Description: "default right"}})
	setHistory(r, ochazuke.HistoryAction, "viewer.only", "viewer.restore", "viewer.pane")
}
