package main

// The data pane's line views (FLAT and JSON): each line knows the value
// it stands for, so the line keys (y y, y p, y v, enter, the folds, %)
// read the line's own record instead of parsing its text. A line view is
// rows of the line noun (linesSource); its Data is a *dataLine.

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"ofam.dev/ochazuke"
)

// dataLine is one line of a line view: its text, the value it shows (nil
// for the bracket of a fan-out's wrapper array, or a cut notice), and in
// the JSON view its matching bracket line (-1 for none).
type dataLine struct {
	text    string
	node    *Node
	partner int
	// folded is set on a folded container's one line.
	folded bool
	// shown is the line as drawn when it differs from text (FLAT's padded
	// indexes, flat.go), tones its colours; text stays what the line keys,
	// the filter and export read.
	shown string
	tones []ochazuke.ToneSpan
}

// drawn is the line as the view draws it.
func (l *dataLine) drawn() string {
	if l.shown != "" {
		return l.shown
	}
	return l.text
}

// lineTexts is the views' text, for tests and the text dumps.
func lineTexts(lines []dataLine) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.text
	}
	return out
}

// jsonLines is the values as indented JSON (many values: a JSON array),
// one dataLine per line; a container whose path folded reports is one
// line. It stops at rawLimit bytes with a notice line.
func jsonLines(values []*Node, many bool, folded func(*Node) bool) []dataLine {
	var out []dataLine
	used, cut := 0, false
	add := func(l dataLine) int {
		out = append(out, l)
		used += len(l.text)
		return len(out) - 1
	}
	var emit func(n *Node, ind int, prefix string, comma bool)
	emit = func(n *Node, ind int, prefix string, comma bool) {
		if cut || used > rawLimit {
			cut = true
			return
		}
		pad := strings.Repeat("  ", ind)
		tail := ""
		if comma {
			tail = ","
		}
		if !n.IsContainer() {
			add(dataLine{text: pad + prefix + n.Scalar() + tail, node: n, partner: -1})
			return
		}
		open, cls, what := "{", "}", "key"
		if n.Kind == KArray {
			open, cls, what = "[", "]", "item"
		}
		if len(n.Children) == 0 {
			add(dataLine{text: pad + prefix + open + cls + tail, node: n, partner: -1})
			return
		}
		if folded != nil && folded(n) {
			count := strconv.Itoa(len(n.Children)) + " " + what
			if len(n.Children) != 1 {
				count += "s"
			}
			add(dataLine{text: pad + prefix + open + "… " + count + cls + tail, node: n, partner: -1, folded: true})
			return
		}
		start := add(dataLine{text: pad + prefix + open, node: n, partner: -1})
		for i, c := range n.Children {
			p := ""
			if n.Kind == KObject {
				p = quote(c.Key) + ": "
			}
			emit(c, ind+1, p, i < len(n.Children)-1)
		}
		if cut {
			return
		}
		end := add(dataLine{text: pad + cls + tail, node: n, partner: start})
		out[start].partner = end
	}
	if many {
		start := add(dataLine{text: "[", partner: -1})
		for i, v := range values {
			emit(v, 1, "", i < len(values)-1)
		}
		if !cut {
			end := add(dataLine{text: "]", partner: start})
			out[start].partner = end
		}
	} else if len(values) == 1 {
		emit(values[0], 0, "", false)
	}
	if cut {
		out = append(out, dataLine{text: "… cut at 4 MB: " + appName() + " get prints it all", partner: -1})
	}
	return out
}

// flatLines is one `path = value` line per leaf (an empty object or
// array is a leaf), in document order, cut at rawLimit as JSON is. look
// is how the lines are drawn (flat.go): the text is always the plain
// path.
func flatLines(values []*Node, look flatLook) []dataLine {
	var out []dataLine
	used := 0
	var walk func(*Node)
	walk = func(n *Node) {
		if used >= rawLimit {
			return
		}
		if n.IsContainer() && len(n.Children) > 0 {
			for _, child := range n.Children {
				walk(child)
			}
			return
		}
		value := " = " + Compact(n, cellLimit)
		text := n.Path() + value
		l := dataLine{text: text, node: n, partner: -1}
		if look != (flatLook{}) {
			path, tones := flatPath(n, look)
			if shown := path + value; shown != text {
				l.shown = shown
			}
			l.tones = tones
		}
		out = append(out, l)
		used += len(text)
	}
	for _, n := range values {
		walk(n)
	}
	if used >= rawLimit {
		out = append(out, dataLine{text: "… cut at 4 MB: " + appName() + " get prints it all", partner: -1})
	}
	return out
}

// lineAt is the line a line row instance stands for, nil for another row.
func lineAt(inst *ochazuke.Instance) *dataLine {
	if inst == nil {
		return nil
	}
	l, _ := inst.Data.(*dataLine)
	return l
}

// nodeText is a value as a yank writes it: a plain value's text, an
// object or array as indented JSON.
func nodeText(n *Node) string {
	if n.IsContainer() {
		return Pretty(n)
	}
	return Text(n, 0)
}

// jsonFolded is the JSON view's fold test: the container's document path
// is in the shown path's fold set.
func (a *App) jsonFolded(n *Node) bool {
	if a.shown == nil {
		return false
	}
	return a.folds[a.shown.Path][n.Path()]
}

// foldLine changes the folds of the JSON view: how is toggle, open or
// close on the line's container (a plain value's line: its enclosing
// container, as Vim's za acts on the fold holding the cursor), close-all
// (every container but the shown values) or open-all. The cursor stays on
// the container's line.
func (a *App) foldLine(l *dataLine, how string) error {
	if a.shown == nil || a.viewName != viewJSON {
		return errors.New("folds work in the JSON view (D or tab to it)")
	}
	key := a.shown.Path
	set := a.folds[key]
	if set == nil {
		set = map[string]bool{}
		a.folds[key] = set
	}
	target := (*Node)(nil)
	if l != nil {
		target = l.node
	}
	if target != nil && !target.IsContainer() || target != nil && len(target.Children) == 0 {
		target = target.Parent
	}
	tops := a.shown.Values()
	switch how {
	case "close-all":
		clear(set)
		for _, v := range tops {
			v.walkContainers(func(n *Node) {
				if n != v {
					set[n.Path()] = true
				}
			})
		}
	case "open-all":
		clear(set)
	default:
		if target == nil || slices.Contains(tops, target) && how != "open" {
			return errors.New("nothing to fold here: the shown value is the whole view")
		}
		p := target.Path()
		switch how {
		case "open":
			delete(set, p)
		case "close":
			set[p] = true
		default:
			set[p] = !set[p]
		}
	}
	if err := a.reshow(); err != nil {
		return err
	}
	// Keep the cursor on the line of the value it was on (or its fold).
	for at := target; at != nil; at = at.Parent {
		for i, dl := range a.lines {
			if dl.node == at && (dl.partner < 0 || dl.partner > i) {
				return a.data.to(i)
			}
		}
	}
	return nil
}

// walkContainers calls fn on n and every object or array below it.
func (n *Node) walkContainers(fn func(*Node)) {
	if !n.IsContainer() {
		return
	}
	fn(n)
	for _, c := range n.Children {
		c.walkContainers(fn)
	}
}

// jumpStructure moves the structure cursor to the shape of a document
// path (indices become []), or the nearest enclosing shape the tree
// lists; the data pane follows as it follows every structure move.
func (a *App) jumpStructure(path string) error {
	steps, err := parsePath(path)
	if err != nil {
		return err
	}
	for len(steps) >= 0 {
		want := stepsPath(steps)
		if a.editedShown() {
			e := a.current()
			for id, p := range nodePaths(e.nodes, e.meta) {
				if ps, err := parsePath(p); err == nil && stepsPath(ps) == want {
					return a.rs.tree.Select(id)
				}
			}
		} else if a.Doc != nil {
			if s := a.Doc.Shape.Find(want); s != nil {
				return a.tree.Select(shapeID(s))
			}
		}
		if len(steps) == 0 {
			break
		}
		steps = steps[:len(steps)-1]
	}
	return errors.New("no structure path for " + path)
}

// registerLineVerbs adds the line noun's verbs beyond line.yank: the
// parts a line yanks, the structure jump, the JSON folds and %.
func (a *App) registerLineVerbs() {
	r := a.r
	done := func(err error) ochazuke.Result {
		if err != nil {
			return ochazuke.Failed(err)
		}
		return ochazuke.Done(nil)
	}
	r.DefineGuard("line-has-value", func(r *ochazuke.Runtime, _ ochazuke.KeyEvent) bool {
		l := lineAt(r.Instances[r.Focus])
		return l != nil && l.node != nil
	})
	r.DefineGuard("line-has-partner", func(r *ochazuke.Runtime, _ ochazuke.KeyEvent) bool {
		l := lineAt(r.Instances[r.Focus])
		return l != nil && l.partner >= 0
	})
	r.DefineGuard("line-json", func(*ochazuke.Runtime, ochazuke.KeyEvent) bool { return a.viewName == viewJSON && a.lines != nil })
	r.Define("line").
		Verb("yank-part", func(c ochazuke.Ctx) ochazuke.Result {
			l := lineAt(c.Instance)
			if l == nil {
				return ochazuke.Failed(errors.New("no line here"))
			}
			switch part := ochazuke.StringArg(c.Args, "part", "line"); part {
			case "line":
				return a.copyText(c, l.text, "the line")
			case "path":
				if l.node == nil {
					return ochazuke.Failed(errors.New("this line has no path"))
				}
				return a.copyText(c, l.node.Path(), "the path "+l.node.Path())
			case "value":
				if l.node == nil {
					return ochazuke.Failed(errors.New("this line has no value"))
				}
				return a.copyText(c, nodeText(l.node), "the value of "+l.node.Path())
			default:
				return ochazuke.Failed(errors.New("part is line, path or value"))
			}
		}, "Copy part of the line: the line's text (line), its value's path (path) or its value (value: plain text, or indented JSON for an object or array)").
		Verb("jump", func(c ochazuke.Ctx) ochazuke.Result {
			l := lineAt(c.Instance)
			if l == nil || l.node == nil {
				return ochazuke.Failed(errors.New("this line has no path"))
			}
			if err := a.jumpStructure(l.node.Path()); err != nil {
				return ochazuke.Failed(err)
			}
			c.Info("structure → " + l.node.Path())
			return ochazuke.Done(nil)
		}, "Move the structure cursor to the shape of the line's value (enter); the data pane follows").When("line-has-value").
		Verb("fold", func(c ochazuke.Ctx) ochazuke.Result {
			return done(a.foldLine(lineAt(c.Instance), ochazuke.StringArg(c.Args, "how", "toggle")))
		}, "Fold the JSON view: toggle (z a), open (z o) or close (z c) the value under the line, close-all (z M) or open-all (z R)").When("line-json").
		Verb("bracket", func(c ochazuke.Ctx) ochazuke.Result {
			l := lineAt(c.Instance)
			if l == nil || l.partner < 0 {
				return ochazuke.Failed(errors.New("% jumps between a bracket and its match"))
			}
			return done(a.data.to(l.partner))
		}, "Move to the line of the matching bracket (%)").Motion().When("line-has-partner")
	setParams(r, "line.yank-part", map[string]ochazuke.Parameter{"part": {Type: "string", Enum: []string{"line", "path", "value"}, Description: "what to copy; default line"}})
	setParams(r, "line.fold", map[string]ochazuke.Parameter{"how": {Type: "string", Enum: []string{"toggle", "open", "close", "close-all", "open-all"}, Description: "default toggle"}})
	setHistory(r, ochazuke.HistoryChange, "line.yank-part")
	setRepeatable(r, "line.yank-part")
	setHistory(r, ochazuke.HistoryAction, "line.jump", "line.fold", "line.bracket")
}

// setParams sets a verb's parameters (the palette's and the CLI's form).
func setParams(r *ochazuke.Runtime, key string, p map[string]ochazuke.Parameter) {
	op := r.Operations[r.CanonicalKey(key)]
	op.Parameters = p
	r.Operations[r.CanonicalKey(key)] = op
}
