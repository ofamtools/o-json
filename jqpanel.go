package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/unixsuperhero/ochazuke"
)

// The jq panel: the restructure's jq program (Plan.JQ, what restructure.jq
// copies), or its mapping (the --mapping form, restructure.mapping), kept
// current as the structure, the value edits and the reshape steps change.
// g j (jq-panel.toggle) opens it, or focuses it when it is open; in it tab (jq-panel.view) switches
// between the program and the mapping, esc (jq-panel.leave) gives the
// focus back and q (jq-panel.close) closes it. Setting jq.panel: split
// draws it under the panes at a fixed height (jq.panel-height, so it
// never grows with the program), modal opens it over the page, off takes
// the key away.
//
// The text is a read-only ochazuke input (ochazuke.ReadOnly), one line for
// a one-line program and several for the mapping, so its motions, visual
// mode and y{motion} (into the registers) work as in any input and an
// edit is a silent no-op; y over a visual selection and Y (jq-panel.yank)
// copy into the registers (and the system clipboard, as the clipboard setting says).

const (
	jqPanelID      = "jq-panel"
	jqPanelTextID  = "jq-panel/text"
	jqPanelModalID = "jq-panel-modal"

	settingJQPanel       = "jq.panel"
	settingJQPanelHeight = "jq.panel-height"

	jqPanelProgram = "jq"
	jqPanelMapping = "mapping"
)

type jqPanel struct {
	open  bool
	view  string // jq or mapping
	in    *ochazuke.Input
	back  string // the focus to give back
	where string // split or modal, as it was opened
	key   string // the mapping (mappingKey) and view the text was made for
	text  string
}

func declareJQPanelSettings(r *ochazuke.Runtime) error {
	return declareOwn(r, ochazuke.Setting{
		Name: settingJQPanel, Type: ochazuke.SettingString, Default: "split", Enum: []string{"off", "split", "modal"},
		Description: "Where g j (jq-panel.toggle) shows the live jq program and mapping: split under the panes (the default: it stays while you edit the structure beside it), modal over the page, off not at all",
	}, ochazuke.Setting{
		Name: settingJQPanelHeight, Type: ochazuke.SettingInteger, Default: 8,
		Description: "The split jq panel's height in rows, border included: fixed, so the panes never move as the program grows",
	})
}

// panelText is what the panel shows: the program or the mapping, or why
// there is none. It is made again only when the mapping changes.
func (a *App) panelText() string {
	key := a.jqp.view
	if a.Doc != nil {
		key += mappingKey(a.Mapping())
	}
	if key == a.jqp.key && a.jqp.in != nil {
		return a.jqp.text
	}
	text := ""
	switch {
	case a.Doc == nil:
		text = "no document"
	case a.jqp.view == jqPanelMapping:
		if t, err := a.mappingText(); err != nil {
			text = "the mapping does not check: " + err.Error()
		} else {
			text = strings.TrimRight(t, "\n")
		}
	default:
		if p, err := a.plan(); err != nil {
			text = "no program: " + err.Error()
		} else {
			text = strings.TrimRight(p.JQ(), "\n")
		}
	}
	a.jqp.key, a.jqp.text = key, text
	return text
}

// openJQPanel opens the panel where jq.panel says.
func (a *App) openJQPanel(view string) error {
	where := a.r.Settings().String(settingJQPanel)
	if where == "off" {
		return errors.New("the jq panel is off (jq.panel)")
	}
	if view != "" {
		a.jqp.view = view
	}
	if a.jqp.open {
		// Focused from elsewhere: esc gives the focus back to where it
		// came from this time, not where the panel was first opened.
		if a.r.Focus != jqPanelTextID && a.r.Instances[a.r.Focus] != nil {
			a.jqp.back = a.r.Focus
		}
		return a.r.SetFocus(jqPanelTextID)
	}
	a.jqp.back, a.jqp.where, a.jqp.key = a.r.Focus, where, ""
	text := a.panelText()
	a.jqp.in = ochazuke.NewInput(jqPanelTextID, text, strings.Contains(text, "\n"))
	if where == "modal" {
		inst, err := a.r.OpenModal(jqPanelModalID, ochazuke.Payload{Prompt: a.panelTitle()}, ochazuke.WithInput(a.jqp.in, ochazuke.Kind("jq-panel-text"), ochazuke.ReadOnly()))
		if err != nil {
			return err
		}
		inst.Component = &jqPanelModal{a: a}
	} else {
		if err := a.r.AddInput(a.jqp.in, ochazuke.Parent(a.r.Page), ochazuke.Kind("jq-panel-text"), ochazuke.ReadOnly()); err != nil {
			return err
		}
		if err := a.r.SetFocus(jqPanelTextID); err != nil {
			return err
		}
	}
	a.jqp.open = true
	return nil
}

// closeJQPanel closes it and gives the focus back.
func (a *App) closeJQPanel() error {
	if !a.jqp.open {
		return nil
	}
	a.jqp.open = false
	if a.jqp.where == "modal" {
		if a.r.Instances[jqPanelModalID] != nil {
			if err := a.r.Pop(); err != nil {
				return err
			}
		}
	} else {
		_ = a.r.RemoveInstance(jqPanelTextID)
		a.giveBack()
	}
	a.jqp.in = nil
	return nil
}

func (a *App) giveBack() {
	if a.r.Instances[a.jqp.back] != nil {
		_ = a.r.SetFocus(a.jqp.back)
	}
}

// syncJQPanel keeps an open panel current: the text for the view and the
// mapping, the input on the page that is open, an input of the right
// kind (one line or several).
func (a *App) syncJQPanel() {
	if !a.jqp.open || a.jqp.in == nil {
		return
	}
	if a.jqp.where == "modal" && a.r.Instances[jqPanelModalID] == nil {
		a.jqp.open, a.jqp.in = false, nil // closed another way (esc of the modal)
		return
	}
	text := a.panelText()
	if multi := strings.Contains(text, "\n"); multi != a.jqp.in.Multiline && a.jqp.where != "modal" {
		focused := a.r.Focus == jqPanelTextID
		_ = a.r.RemoveInstance(jqPanelTextID)
		a.jqp.in = ochazuke.NewInput(jqPanelTextID, text, multi)
		if err := a.r.AddInput(a.jqp.in, ochazuke.Parent(a.r.Page), ochazuke.Kind("jq-panel-text"), ochazuke.ReadOnly()); err != nil {
			a.jqp.open, a.jqp.in = false, nil
			return
		}
		if focused {
			_ = a.r.SetFocus(jqPanelTextID)
		}
		return
	}
	if inst := a.r.Instances[jqPanelTextID]; inst != nil && a.jqp.where != "modal" && inst.Parent != a.r.Page {
		inst.Parent = a.r.Page
	}
	if in := a.jqp.in; string(in.Text) != text {
		// The mapping changed: the viewer shows the new text, a fresh
		// history, the cursor kept where it fits (Input.SetText). The
		// modal keeps its input, so it takes several lines when the text
		// does (SetText keeps one line of a one-line input).
		in.Multiline = in.Multiline || strings.Contains(text, "\n")
		in.SetText(text)
	}
}

// panelTitle names the view and the keys that switch and close it.
func (a *App) panelTitle() string {
	title := "jq program"
	other := "mapping"
	if a.jqp.view == jqPanelMapping {
		title, other = "Mapping", "jq"
	}
	var hints []string
	if k := a.keyFor("jq-panel.view"); k != "" {
		hints = append(hints, k+" "+other)
	}
	if k := a.keyFor("jq-panel.yank"); k != "" {
		hints = append(hints, k+" copy")
	}
	if len(hints) > 0 {
		title += "  " + strings.Join(hints, " · ")
	}
	return title
}

// splitHeight is the rows the split panel takes from the page.
func (a *App) splitHeight(height int) int {
	if !a.jqp.open || a.jqp.where != "split" {
		return 0
	}
	h := a.r.Settings().Int(settingJQPanelHeight)
	return min(max(3, h), height/2)
}

// jqPanelBlock draws the split panel at width x height.
func (a *App) jqPanelBlock(l ochazuke.Layout, width, height int) ochazuke.Block {
	a.syncJQPanel()
	if a.jqp.in == nil {
		return ochazuke.Text("")
	}
	a.r.Placed(jqPanelTextID)
	cw, ch := ochazuke.PanelContentSize(width, height)
	focused := a.r.Focus == jqPanelTextID
	body := ochazuke.InputBlock(a.jqp.in, cw, ch)
	if !focused {
		body.Cursor = nil
	}
	return l.PanelBlock(a.panelTitle(), body, width, height, focused)
}

// jqPanelModal is the modal form: the same input at a fixed size.
type jqPanelModal struct{ a *App }

func (m *jqPanelModal) ModalTitle() string { return m.a.panelTitle() }

func (m *jqPanelModal) ModalBlock(s ochazuke.Styles, width, height int) ochazuke.Block {
	a := m.a
	a.syncJQPanel()
	if a.jqp.in == nil {
		return ochazuke.Text("")
	}
	h := min(max(3, a.r.Settings().Int(settingJQPanelHeight)), max(1, height))
	w := min(width, 100)
	b := ochazuke.InputBlock(a.jqp.in, w, h)
	lines := strings.Split(b.Text, "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	b.Text = strings.Join(lines, "\n")
	return b
}

func (a *App) registerJQPanel() {
	r := a.r
	a.jqp.view = jqPanelProgram
	r.Instance(jqPanelID, ochazuke.Kind(jqPanelID))
	r.Define("jq-panel-text")
	r.DefineGuard("jq-panel-on", func(*ochazuke.Runtime, ochazuke.KeyEvent) bool {
		return r.Settings().String(settingJQPanel) != "off"
	})
	r.DefineGuard("jq-panel-open", func(*ochazuke.Runtime, ochazuke.KeyEvent) bool { return a.jqp.open })
	r.Define(jqPanelID).
		Verb("toggle", func(c ochazuke.Ctx) ochazuke.Result {
			if a.jqp.open && a.r.Focus == jqPanelTextID {
				return a.doneErr(a.closeJQPanel())
			}
			return a.doneErr(a.openJQPanel(ochazuke.StringArg(c.Args, "view", "")))
		}, "Open the live jq program of the restructure (or, with view mapping, its mapping) where jq.panel says; on an open panel, focus it, and in it close it").When("jq-panel-on").
		Verb("open", func(c ochazuke.Ctx) ochazuke.Result {
			return a.doneErr(a.openJQPanel(ochazuke.StringArg(c.Args, "view", "")))
		}, "Open the jq panel (or focus it), on view jq or mapping").When("jq-panel-on").
		Verb("close", func(c ochazuke.Ctx) ochazuke.Result { return a.doneErr(a.closeJQPanel()) }, "Close the jq panel").When("jq-panel-open").
		Verb("leave", func(c ochazuke.Ctx) ochazuke.Result {
			if a.jqp.where == "modal" {
				return a.doneErr(a.closeJQPanel())
			}
			a.giveBack()
			return ochazuke.Done(nil)
		}, "Give the focus back to the pane it came from; the split panel stays, the modal closes").When("jq-panel-open").
		Verb("view", func(c ochazuke.Ctx) ochazuke.Result {
			v := ochazuke.StringArg(c.Args, "view", "")
			if v == "" {
				v = map[string]string{jqPanelProgram: jqPanelMapping, jqPanelMapping: jqPanelProgram}[a.jqp.view]
			}
			if v != jqPanelProgram && v != jqPanelMapping {
				return ochazuke.Failed(fmt.Errorf("view %q (jq or mapping)", v))
			}
			a.jqp.view = v
			a.syncJQPanel()
			if inst := r.Instances[jqPanelModalID]; inst != nil {
				inst.Payload.Prompt = a.panelTitle()
			}
			return ochazuke.Done(v)
		}, "Show the jq program or the mapping in the jq panel (without view, the other one)").When("jq-panel-open").
		Verb("text", func(c ochazuke.Ctx) ochazuke.Result {
			return ochazuke.Done(a.panelText())
		}, "The text the jq panel shows (the program or the mapping)").Fetch().
		Verb("yank", func(c ochazuke.Ctx) ochazuke.Result {
			text := a.panelText()
			what := "the jq program"
			if a.jqp.view == jqPanelMapping {
				what = "the mapping"
			}
			if in := a.jqp.in; in != nil {
				if start, end, ok := in.Selection(); ok && end > start {
					text, what = string(in.Text[start:min(end, len(in.Text))]), "the selection"
					in.SetMode("normal")
					_ = r.SetMode("normal")
				}
			}
			return a.copyText(c, text, what)
		}, "Copy the jq panel's selection, or all of it, into the registers (and the system clipboard, as the clipboard setting says)").When("jq-panel-open")
	params := func(key string, p map[string]ochazuke.Parameter) {
		op := r.Operations[r.CanonicalKey(key)]
		op.Parameters = p
		r.Operations[r.CanonicalKey(key)] = op
	}
	view := ochazuke.Parameter{Type: "string", Enum: []string{jqPanelProgram, jqPanelMapping}, Description: "jq (the program) or mapping"}
	params("jq-panel.toggle", map[string]ochazuke.Parameter{"view": view})
	params("jq-panel.open", map[string]ochazuke.Parameter{"view": view})
	params("jq-panel.view", map[string]ochazuke.Parameter{"view": view})
	setHistory(r, ochazuke.HistoryAction, "jq-panel.toggle", "jq-panel.open", "jq-panel.close", "jq-panel.leave", "jq-panel.view")
	setHistory(r, ochazuke.HistoryChange, "jq-panel.yank")
}
