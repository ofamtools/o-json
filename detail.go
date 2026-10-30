package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/unixsuperhero/ochazuke"
)

// The detail: enter on a table row (or on a field holding an object or an
// array) opens that value in a modal as labeled rows, field (or [index]),
// type and value. enter on a row holding a container drills into it, and
// h or backspace steps back out; esc (the modal noun) or q closes it. The
// rows are a windowed table (rows.go) of the entry noun, so a huge array
// opens as fast as a small one.

// registerDetail declares the modal and its view; its keys are in bind.
func (a *App) registerDetail() {
	a.m.ModalViews[detailID] = ochazuke.ModalView{Title: "Detail", Render: a.detailView}
}

// openDetail opens the modal on a container.
func (a *App) openDetail(n *Node) error {
	if n == nil || !n.IsContainer() {
		return errors.New("only an object or an array opens in the detail")
	}
	if _, err := a.r.OpenModal(detailID, ochazuke.Payload{Prompt: n.Path()}); err != nil {
		return err
	}
	if a.r.Instances[rowsID] == nil || a.detail == nil {
		v, err := newRowsView(a.r, rowsID, detailID)
		if err != nil {
			_ = a.r.Pop()
			return err
		}
		a.detail = v
	}
	a.trail = []*Node{n}
	if err := a.showDetail(0); err != nil {
		_ = a.r.Pop()
		return err
	}
	return a.detail.list.Focus()
}

// showDetail shows the innermost node of the trail with row sel selected.
func (a *App) showDetail(sel int) error {
	n := a.trail[len(a.trail)-1]
	view := a.m.ModalViews[detailID]
	view.Title = fmt.Sprintf("%s  %s", n.Path(), n.TypeLabel())
	a.m.ModalViews[detailID] = view
	return a.detail.set(fieldsSource(n, "entry"), sel)
}

// drill steps into the selected entry when it holds a container.
func (a *App) drill(n *Node) error {
	if n == nil || !n.IsContainer() {
		return errors.New("not an object or an array")
	}
	a.trail = append(a.trail, n)
	return a.showDetail(0)
}

// up steps back out to the container the trail came from, its entry
// selected; at the top it closes the modal.
func (a *App) up() error {
	if len(a.trail) <= 1 {
		return a.r.Pop()
	}
	from := a.trail[len(a.trail)-1]
	a.trail = a.trail[:len(a.trail)-1]
	return a.showDetail(from.Index)
}

func (a *App) detailView(r *ochazuke.Runtime, _ *ochazuke.Instance, w, h int) ochazuke.Block {
	if a.detail == nil || r.Instances[rowsID] == nil {
		return ochazuke.Text("")
	}
	if a.vedit != nil && a.vedit.modal == nil && a.vedit.view == a.detail && r.Focus != valueInputID {
		// The focus left the detail's inline input: its text is the edit.
		if err := a.accept(); err != nil {
			a.LoadErr = err
		}
	}
	a.refreshDetail()
	if a.detail.src.n == 0 {
		r.Placed(rowsID)
		return ochazuke.Text("(empty)")
	}
	body := a.detail.view(w, h)
	if a.vedit != nil && a.vedit.modal == nil && a.vedit.view == a.detail && r.Instances[valueInputID] != nil {
		return a.inputBlock(body, w)
	}
	return ochazuke.Text(body)
}

// detailEditable reports a detail whose rows are the edited document's
// (opened from the edited view): the value verbs patch them as they do
// the data pane's.
func (a *App) detailEditable() bool {
	if len(a.trail) == 0 || !a.editedShown() {
		return false
	}
	d := a.shownDoc()
	return d.err == nil && rootOf(a.trail[0]) == d.root
}

// rootOf is the document node n is in.
func rootOf(n *Node) *Node {
	for n != nil && n.Parent != nil {
		n = n.Parent
	}
	return n
}

// refreshDetail follows a value edit: the edited document is built
// again, so the trail is found again by path in the new one (a level
// that went is dropped), the selected row kept.
func (a *App) refreshDetail() {
	if len(a.trail) == 0 || !a.editedShown() {
		return
	}
	d := a.shownDoc()
	if d.err != nil || d.root == nil || rootOf(a.trail[0]) == d.root || rootOf(a.trail[0]) == a.Doc.Root {
		return
	}
	var trail []*Node
	for _, n := range a.trail {
		m, err := Get(d.root, n.Path())
		if err != nil || !m.IsContainer() {
			break
		}
		trail = append(trail, m)
	}
	if len(trail) == 0 {
		_ = a.r.Pop() // the value it showed is gone
		a.trail = nil
		return
	}
	a.trail = trail
	_ = a.showDetail(max(0, a.detail.Selected()))
}

// settingDetailBackdropCursor keeps the data pane's cursor drawn beneath
// the detail.
const settingDetailBackdropCursor = "detail.backdrop-cursor"

func declareDetailSettings(r *ochazuke.Runtime) error {
	return r.DeclareSetting(ochazuke.Setting{
		Name: settingDetailBackdropCursor, Type: ochazuke.SettingBoolean, Default: false,
		Description: "Beneath the detail the data pane keeps its cursor bar (dimmed with the page); off draws that row plain there, as the detail shows the row and the bar would only peek out beside it",
	})
}

// beneathDetail is the data pane's body as drawn beneath the detail: the
// cursor's marks (reverse video, the current cell's underline) taken out
// unless detail.backdrop-cursor (the detail shows that row; its bar would
// peek out beside the modal).
//
// A workaround: ochazuke keeps a page's highlight under a modal (Model
// beneath) and its backdrop dims colours but not reverse video, so a bar
// the modal does not cover reads as a stray highlight. A per-modal option
// in ochazuke would replace this.
func (a *App) beneathDetail(b ochazuke.Block) ochazuke.Block {
	if a.r.Instances[detailID] == nil || a.r.Settings().Bool(settingDetailBackdropCursor) {
		return b
	}
	return b.WithText(unreverse(b.Text))
}

// unreverse drops reverse video and underline (SGR 7, 27, 4, 24: the
// cursor's bar and its cell) from text's escape sequences, colour parameters (38/48/58 with 5;N or 2;R;G;B) kept whole.
func unreverse(text string) string {
	var out strings.Builder
	for {
		i := strings.Index(text, "\x1b[")
		if i < 0 {
			out.WriteString(text)
			return out.String()
		}
		j := i + 2
		for j < len(text) && (text[j] >= '0' && text[j] <= '9' || text[j] == ';' || text[j] == ':') {
			j++
		}
		if j >= len(text) || text[j] != 'm' {
			out.WriteString(text[:j])
			text = text[j:]
			continue
		}
		out.WriteString(text[:i])
		params := strings.Split(text[i+2:j], ";")
		var kept []string
		for k := 0; k < len(params); k++ {
			p := params[k]
			switch {
			case p == "7" || p == "27" || p == "4" || p == "24":
				continue
			case (p == "38" || p == "48" || p == "58") && k+1 < len(params):
				n := 2
				if params[k+1] == "2" {
					n = 4
				}
				end := min(len(params), k+1+n)
				kept = append(kept, params[k:end]...)
				k = end - 1
				continue
			}
			kept = append(kept, p)
		}
		if len(kept) > 0 || len(params) == 1 && params[0] == "" {
			out.WriteString("\x1b[" + strings.Join(kept, ";") + "m")
		}
		text = text[j+1:]
	}
}
