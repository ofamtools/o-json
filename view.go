package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/layout"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/pathpicker"
	"ofam.dev/ochazuke/widgets/table"
)

// Frame and node IDs.
const (
	pageID     = "main"
	nodeTree   = "tree"
	nodePrompt = "prompt"
	modalPick  = "pick"
	modalFiles = "files"
	modalHelp  = "help"
	treeRatio  = 0.3
)

// The four data views.
const (
	viewTable  = 1
	viewNested = 2
	viewPretty = 3
	viewRaw    = 4
)

var viewNames = [...]string{"", "Table", "Nested", "Pretty", "Raw"}
var viewScopes = [...]string{"", "table", "nested", "pretty", "raw"}

func winNode(i int) string { return "win" + strconv.Itoa(i) }

// --- paths ---------------------------------------------------------------------

// The canonical index and wildcard steps come from ParsePath, so the app
// builds paths exactly the way the engine reads them.
var idxPart, wildPart = PathPart{Array: true, Indexed: true}, PathPart{Array: true}

func init() {
	if p, err := ParsePath("$[0]"); err == nil && len(p) == 1 {
		idxPart = p[0]
	}
	if p, err := ParsePath("$[]"); err == nil && len(p) == 1 {
		wildPart = p[0]
	}
}

func pathIdx(i int) PathPart     { p := idxPart; p.Index = i; return p }
func pathKey(k string) PathPart  { return PathPart{Key: k} }
func isWildPart(p PathPart) bool { return p.Array && !p.Indexed }
func isKeyPart(p PathPart) bool  { return !p.Array && !p.Indexed }
func pathJoin(p Path, q ...PathPart) Path {
	out := make(Path, 0, len(p)+len(q))
	return append(append(out, p...), q...)
}

// partMatch reports whether pattern step a matches concrete step b; a
// wildcard matches any index.
func partMatch(a, b PathPart) bool {
	switch {
	case isWildPart(a):
		return b.Array || b.Indexed
	case a.Indexed || b.Indexed:
		return a.Indexed && b.Indexed && a.Index == b.Index
	}
	return a.Key == b.Key && a.Array == b.Array
}

func pathHasPrefix(p, prefix Path) bool {
	if len(prefix) > len(p) {
		return false
	}
	for i := range prefix {
		if !partMatch(prefix[i], p[i]) {
			return false
		}
	}
	return true
}

// wildPath turns every index into a wildcard: the schema path of a value.
func wildPath(p Path) Path {
	out := slices.Clone(p)
	for i := range out {
		if out[i].Indexed {
			out[i] = wildPart
		}
	}
	return out
}

// resolvePath returns the longest concrete prefix of p that exists in v,
// with each wildcard resolved to the first element that has the rest.
func resolvePath(v any, p Path) Path {
	out := Path{}
	for i, pp := range p {
		if isWildPart(pp) {
			arr, _ := v.([]any)
			if len(arr) == 0 {
				return out
			}
			pick := 0
			for j, e := range arr {
				if _, ok := AtPath(e, p[i+1:]); ok {
					pick = j
					break
				}
			}
			out, v = append(out, pathIdx(pick)), arr[pick]
			continue
		}
		nv, ok := AtPath(v, Path{pp})
		if !ok {
			return out
		}
		out, v = append(out, pp), nv
	}
	return out
}

// columnName joins the key steps of p with dots, the flattened column name.
func columnName(p Path) string {
	var parts []string
	for _, pp := range p {
		if isKeyPart(pp) {
			parts = append(parts, pp.Key)
		}
	}
	return strings.Join(parts, ".")
}

// --- values ----------------------------------------------------------------------

func childCount(v any) int {
	switch t := v.(type) {
	case Object:
		return len(t)
	case []any:
		return len(t)
	}
	return 0
}

func eachChild(v any, fn func(part PathPart, name string, child any)) {
	switch t := v.(type) {
	case Object:
		for _, f := range t {
			fn(pathKey(f.Key), f.Key, f.Value)
		}
	case []any:
		for i, e := range t {
			fn(pathIdx(i), "["+strconv.Itoa(i)+"]", e)
		}
	}
}

// valueText is a compact one-line rendering of any value.
func valueText(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	}
	b, err := FormatJSON(v, false)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func valueTone(v any) string {
	switch v.(type) {
	case string:
		return "success"
	case json.Number:
		return "warning"
	case bool, nil:
		return "info"
	}
	return "text"
}

func nodeSummary(v any) string {
	switch t := v.(type) {
	case Object:
		return fmt.Sprintf(" {%d}", len(t))
	case []any:
		return fmt.Sprintf(" [%d]", len(t))
	case string:
		return ": " + strconv.Quote(clipText(t, 80))
	}
	return ": " + valueText(v)
}

func clipText(s string, w int) string {
	if r := []rune(s); w > 1 && len(r) > w {
		return string(r[:w-1]) + "…"
	}
	return s
}

func singleLine(s string) string {
	return strings.NewReplacer("\r\n", "⏎", "\n", "⏎", "\r", "⏎", "\t", " ").Replace(s)
}

func textLine(t, tone string) ui.ViewNode {
	return ui.ViewNode{Kind: ui.KindText, Tone: tone, Props: map[string]any{"text": t}}
}

// --- JSON spans --------------------------------------------------------------------

// jspan is one value of a JSON text: its path and byte range.
type jspan struct {
	path       Path
	start, end int
}

// scanJSON lists every complete value of s in document order. It is best
// effort and always ends: at the first syntax error it stops, keeping the
// values that completed before it and dropping the unfinished ones around
// it, so every span is a whole value. Several top-level values (JSONL) are
// indexed.
func scanJSON(s string) []jspan {
	var out []jspan
	i := 0
	ws := func() {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
			i++
		}
	}
	str := func() (string, bool) {
		start := i
		for i++; i < len(s) && s[i] != '"'; i++ {
			if s[i] == '\\' {
				i++
			}
		}
		if i >= len(s) {
			i = len(s)
			return "", false
		}
		i++
		var k string
		return k, json.Unmarshal([]byte(s[start:i]), &k) == nil
	}
	var val func(p Path) bool
	val = func(p Path) bool {
		ws()
		if i >= len(s) {
			return false
		}
		n := len(out)
		out = append(out, jspan{path: p, start: i})
		ok := true
		switch c := s[i]; c {
		case '{', '[':
			closer := byte('}')
			if c == '[' {
				closer = ']'
			}
			i++
			ws()
			for k := 0; ok && i < len(s) && s[i] != closer; k++ {
				if k > 0 {
					// Members are separated by exactly one comma.
					if s[i] != ',' {
						ok = false
						break
					}
					i++
					ws()
				}
				part := pathIdx(k)
				if c == '{' {
					var key string
					if i >= len(s) || s[i] != '"' {
						ok = false
						break
					}
					if key, ok = str(); !ok {
						break
					}
					part = pathKey(key)
					if ws(); i >= len(s) || s[i] != ':' {
						ok = false
						break
					}
					i++
				}
				ok = val(pathJoin(p, part))
				ws()
			}
			if ok && i < len(s) {
				i++
			} else {
				ok = false
			}
		case '"':
			_, ok = str()
		default:
			start := i
			for i < len(s) && !strings.ContainsRune(",]}[{:\" \t\r\n", rune(s[i])) {
				i++
			}
			ok = i > start
		}
		if !ok {
			// Drop this unfinished value; its finished children stay.
			out = slices.Delete(out, n, n+1)
			return false
		}
		out[n].end = i
		return true
	}
	tops := 0
	for k := 0; ; k++ {
		if ws(); i >= len(s) || !val(Path{pathIdx(k)}) {
			break
		}
		tops++
	}
	if tops <= 1 {
		// One document, finished or not: paths start at its root.
		out = slices.DeleteFunc(out, func(sp jspan) bool { return sp.path[0].Index != 0 })
		for j := range out {
			out[j].path = out[j].path[1:]
		}
	}
	return out
}

func lineStarts(s string) []int {
	out := []int{0}
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, i+1)
		}
	}
	return out
}

func lineOf(starts []int, off int) int { return max(sort.SearchInts(starts, off+1)-1, 0) }

// --- pretty / KV document ------------------------------------------------------------

// lspan is a value's line range in a rendered document.
type lspan struct {
	path   Path
	lo, hi int
}

type textDoc struct {
	lines    []string
	spans    []lspan
	lineSpan []int          // innermost span starting (or, for closing lines, ending) on each line
	byPath   map[string]int // path string -> span index
}

func newTextDoc(lines []string, spans []lspan) *textDoc {
	d := &textDoc{lines: lines, spans: spans, lineSpan: make([]int, len(lines)), byPath: map[string]int{}}
	for i := range d.lineSpan {
		d.lineSpan[i] = -1
	}
	for i, sp := range spans {
		if sp.lo < len(lines) {
			d.lineSpan[sp.lo] = i
		}
		if _, dup := d.byPath[sp.path.String()]; !dup {
			d.byPath[sp.path.String()] = i
		}
	}
	for i, sp := range spans {
		if sp.hi < len(lines) && d.lineSpan[sp.hi] < 0 {
			d.lineSpan[sp.hi] = i
		}
	}
	return d
}

// prettyDoc renders v as indented JSON and maps every value to its lines.
func prettyDoc(v any) *textDoc {
	b, err := FormatJSON(v, true)
	if err != nil {
		return newTextDoc([]string{"error: " + err.Error()}, nil)
	}
	s := string(b)
	starts := lineStarts(s)
	var spans []lspan
	for _, sp := range scanJSON(s) {
		spans = append(spans, lspan{sp.path, lineOf(starts, sp.start), lineOf(starts, max(sp.end-1, sp.start))})
	}
	return newTextDoc(strings.Split(strings.TrimRight(s, "\n"), "\n"), spans)
}

// kvDoc renders v as one "path: value" line per leaf.
func kvDoc(v any) *textDoc {
	var lines []string
	var spans []lspan
	var walk func(v any, p Path)
	walk = func(v any, p Path) {
		i := len(spans)
		spans = append(spans, lspan{path: p, lo: len(lines)})
		if childCount(v) == 0 {
			name := p.String()
			lines = append(lines, name+": "+singleLine(valueText(v)))
		} else {
			eachChild(v, func(part PathPart, _ string, c any) { walk(c, pathJoin(p, part)) })
		}
		spans[i].hi = max(len(lines)-1, spans[i].lo)
	}
	walk(v, Path{})
	return newTextDoc(lines, spans)
}

// focusLines returns the focused value's line range in d.
func (d *textDoc) focusLines(focus Path) (int, int, bool) {
	for p := focus; ; p = p[:len(p)-1] {
		if i, ok := d.byPath[p.String()]; ok {
			return d.spans[i].lo, d.spans[i].hi, true
		}
		if len(p) == 0 {
			return 0, 0, false
		}
	}
}

func (d *textDoc) linePath(line int) (Path, bool) {
	for l := min(line, len(d.lineSpan)-1); l >= 0; l-- {
		if i := d.lineSpan[l]; i >= 0 {
			return d.spans[i].path, true
		}
	}
	return nil, false
}

// jsonTones colors one line of pretty JSON: keys, strings, numbers, literals.
func jsonTones(line string, base int) []ui.ToneSpan {
	var out []ui.ToneSpan
	rs := []rune(line)
	for i := 0; i < len(rs); {
		switch r := rs[i]; {
		case r == '"':
			j := i + 1
			for ; j < len(rs) && rs[j] != '"'; j++ {
				if rs[j] == '\\' {
					j++
				}
			}
			j = min(j+1, len(rs))
			tone := "success"
			if k := strings.TrimLeft(string(rs[j:]), " "); strings.HasPrefix(k, ":") {
				tone = "key"
			}
			out = append(out, ui.ToneSpan{From: base + i, To: base + j, Tone: tone})
			i = j
		case r == '-' || r >= '0' && r <= '9':
			j := i
			for j < len(rs) && strings.ContainsRune("-+.eE0123456789", rs[j]) {
				j++
			}
			out = append(out, ui.ToneSpan{From: base + i, To: base + j, Tone: "warning"})
			i = j
		case r == 't' || r == 'f' || r == 'n':
			j := i
			for j < len(rs) && rs[j] >= 'a' && rs[j] <= 'z' {
				j++
			}
			out = append(out, ui.ToneSpan{From: base + i, To: base + j, Tone: "info"})
			i = j
		default:
			i++
		}
	}
	return out
}

// --- geometry ---------------------------------------------------------------------

type geom struct {
	w, bodyH, treeW int
	winW            [2]int
}

func (a *App) geometry() geom {
	w, h := a.w, a.h
	if w <= 0 || h <= 0 {
		w, h = 120, 32
	}
	body := h - 3
	if a.prompt != nil {
		body--
		if a.prompt.kind == "jq" {
			body--
		}
	}
	g := geom{w: w, bodyH: max(body, 4), treeW: int(float64(w) * treeRatio)}
	data := w - g.treeW
	g.winW[0] = data
	if len(a.wins) > 1 {
		g.winW[0] = int(float64(data) * 0.5)
		g.winW[1] = data - g.winW[0]
	}
	return g
}

// rows is the inner height of a bordered body panel.
func (g geom) rows() int { return max(g.bodyH-2, 1) }

// --- view tree ----------------------------------------------------------------------

func (a *App) view(s core.State) ui.ViewNode {
	if s.Width > 0 && s.Height > 0 {
		a.w, a.h = s.Width, s.Height
	}
	g := a.geometry()
	var wins []ui.ViewNode
	for i := range a.wins {
		wins = append(wins, a.windowNode(i, g))
	}
	right := wins[0]
	if len(wins) > 1 {
		right = ui.ViewNode{ID: "wins", Kind: ui.KindSplit, Ratio: 0.5, Props: map[string]any{"axis": "horizontal"}, Children: wins}
	}
	kids := []ui.ViewNode{a.titleLine(g), {ID: "body", Kind: ui.KindSplit, Ratio: treeRatio, Props: map[string]any{"axis": "horizontal"},
		Children: []ui.ViewNode{a.treePanel(g), right}}}
	if p := a.prompt; p != nil {
		n := p.ed.View(nodePrompt)
		n.Title, n.Scope = p.label, "prompt"
		kids = append(kids, n)
		if p.kind == "jq" {
			kids = append(kids, a.chipLine())
		}
	}
	kids = append(kids, a.infoLine(s, g), a.statusLine(g))
	root := ui.ViewNode{ID: "root", Kind: ui.KindVStack, Children: []ui.ViewNode{{ID: "page", Kind: ui.KindVStack, Scope: "page", Children: kids}}}
	for _, m := range s.Modals {
		root.Children = append(root.Children, a.modalView(s, m))
	}
	return root
}

func (a *App) titleLine(g geom) ui.ViewNode {
	b := a.cur()
	name := b.Name
	if name == "" {
		name = "[No Name]"
	}
	mode, tone := " READ-ONLY ", "badge"
	if b.Editable {
		mode, tone = " EDIT ", "badge.warning"
	}
	line := fmt.Sprintf(" o-json · %s", name)
	if b.Dirty {
		line += " [+]"
	}
	line += fmt.Sprintf(" · buffer %d of %d · ", a.bufPos(b), len(a.ring.List()))
	from := utf8.RuneCountInString(line)
	line += mode
	n := textLine(clipText(line, g.w), "title")
	n.Props["spans"] = []ui.ToneSpan{{From: from, To: from + utf8.RuneCountInString(mode), Tone: tone}}
	return n
}

func (a *App) bufPos(b *Buffer) int {
	for i, x := range a.ring.List() {
		if x.ID == b.ID {
			return i + 1
		}
	}
	return 0
}

func (a *App) treePanel(g geom) ui.ViewNode {
	b := a.cur()
	u := a.bufUI(b)
	rows := a.structRows(b)
	sel := map[string]bool{}
	for _, p := range u.sel {
		sel[p.String()] = true
	}
	var nodes []any
	if root := a.schema(b); root != nil {
		nodes = []any{a.schemaNode(root, 0, u, sel)}
	}
	title := "Structure"
	if len(u.sel) > 0 {
		title += fmt.Sprintf(" · %d selected", len(u.sel))
	}
	tree := ui.ViewNode{ID: nodeTree, Kind: ui.KindTree, Focusable: true, Scope: "tree",
		Props: map[string]any{"nodes": nodes, "selected": min(u.cursor, max(len(rows)-1, 0))}}
	return ui.ViewNode{Kind: ui.KindPanel, Border: true, Title: title, Box: fillBox(), Children: []ui.ViewNode{tree}}
}

func fillBox() layout.Constraints { return layout.Constraints{Flex: 1} }

func (a *App) schemaNode(s *Schema, d int, u *bufUI, sel map[string]bool) map[string]any {
	name := s.Name
	if d == 0 {
		name = "$"
	}
	label := name + "  " + s.Type
	n := map[string]any{"label": label}
	if s.Total > 0 && s.Count < s.Total {
		n["badge"] = fmt.Sprintf("%d%%", s.Count*100/s.Total)
	} else if s.Type == "array" {
		n["badge"] = fmt.Sprintf("×%d", s.Count)
	}
	if sel[s.Path.String()] {
		n["label"], n["tone"] = "● "+label, "match"
	}
	if len(s.Children) > 0 {
		open := u.schemaOpen(s, d)
		n["expanded"] = open
		kids := []any{map[string]any{"label": ""}}
		if open {
			kids = kids[:0]
			for _, c := range s.Children {
				kids = append(kids, a.schemaNode(c, d+1, u, sel))
			}
		}
		n["children"] = kids
	}
	return n
}

// windowNode is one data window: a bordered panel holding the active view.
func (a *App) windowNode(i int, g geom) ui.ViewNode {
	w := a.wins[i]
	b := a.buf(w.buf)
	id := winNode(i)
	inner := max(g.winW[min(i, 1)]-2, 1)
	title := fmt.Sprintf("%d %s · %s", w.view, viewNames[w.view], bufName(b))
	var body ui.ViewNode
	switch w.view {
	case viewTable:
		var m table.Model
		m, title = a.tableModel(w, b, inner, g.rows())
		body = m.View(id)
	case viewNested:
		if focus := a.bufUI(b).focus; w.seen != focus.String() {
			for n := range focus {
				w.open[focus[:n].String()] = true
			}
			w.seen = focus.String()
		}
		rows, nodes := a.nestedRows(w, a.display(b))
		body = ui.ViewNode{ID: id, Kind: ui.KindTree, Props: map[string]any{"nodes": nodes, "selected": a.nestedCursor(w, b, rows)}}
		if w.anchor >= 0 {
			title += fmt.Sprintf(" · VISUAL %d rows (h folds, l unfolds)", absInt(a.nestedCursor(w, b, rows)-w.anchor)+1)
		}
	case viewPretty:
		body = a.prettyNode(w, b, id, inner, g.rows(), i == a.win && a.pane == 1)
		if w.kv {
			title = fmt.Sprintf("%d KV · %s", w.view, bufName(b))
		}
	case viewRaw:
		scope := "whole document"
		if w.sub {
			scope = "subtree " + w.edPath
		}
		format := "as typed"
		if w.format != "" {
			format = w.format
		}
		title = fmt.Sprintf("4 Raw · %s · %s · %s", bufName(b), scope, format)
		body = textLine("no text", "muted")
		if w.ed != nil {
			body = w.ed.View(id)
			if len(w.rawFolds) > 0 {
				folded, cur := foldRawView(w.ed.Value(), w.ed.Cursor(), w.rawFolds)
				body.Props["value"] = folded
				body.Props["cursor"] = cur
				body.CacheKey = 0 // folded display overrides raw cache key
				title += fmt.Sprintf(" · %d folded", len(w.rawFolds))
			}
		}
		body.ID, body.Box = id, fillBox()
	}
	body.Focusable, body.Scope = true, viewScopes[w.view]
	return ui.ViewNode{Kind: ui.KindPanel, Border: true, Title: title, Box: fillBox(), Children: []ui.ViewNode{body}}
}

func bufName(b *Buffer) string {
	if b.Name == "" {
		return "[No Name]"
	}
	return b.Name
}

func absInt(n int) int { return max(n, -n) }

// nestedRows walks the displayed value: the visible rows in order, and the
// ui tree nodes (a collapsed container gets one placeholder child so its
// marker shows).
func (a *App) nestedRows(w *window, v any) ([]Path, []any) {
	var rows []Path
	var walk func(v any, p Path, name string, d int) map[string]any
	walk = func(v any, p Path, name string, d int) map[string]any {
		rows = append(rows, p)
		n := map[string]any{"label": name + nodeSummary(v), "tone": valueTone(v)}
		if childCount(v) > 0 {
			open := w.nestedOpen(p.String(), d)
			kids := []any{map[string]any{"label": ""}}
			if open {
				kids = kids[:0]
				eachChild(v, func(part PathPart, cn string, c any) { kids = append(kids, walk(c, pathJoin(p, part), cn, d+1)) })
			}
			n["expanded"], n["children"], n["tone"] = open, kids, "text"
		}
		return n
	}
	return rows, []any{walk(v, Path{}, "$", 0)}
}

// nestedCursor is the row of the deepest visible ancestor of the focus.
func (a *App) nestedCursor(w *window, b *Buffer, rows []Path) int {
	focus := a.bufUI(b).focus
	best, bestLen := 0, -1
	for i, p := range rows {
		if len(p) > bestLen && pathHasPrefix(focus, p) {
			best, bestLen = i, len(p)
		}
	}
	return best
}

func (a *App) prettyNode(w *window, b *Buffer, id string, width, h int, focused bool) ui.ViewNode {
	d := a.textDoc(b, w.kv)
	focus := a.bufUI(b).focus
	lo, hi, ok := d.focusLines(focus)
	if w.seen != focus.String() {
		w.line, w.seen = lo, focus.String()
	}
	cur := min(max(w.line, 0), max(len(d.lines)-1, 0))
	if !ok {
		lo, hi = cur, cur
	}
	w.off = min(max(w.off, cur-h+1), cur)
	off := min(max(w.off, 0), max(len(d.lines)-h, 0))
	var sb strings.Builder
	var spans []ui.ToneSpan
	pos := 0
	bg := "selected.inactive"
	if focused {
		bg = "selected"
	}
	for l := off; l < min(len(d.lines), off+h); l++ {
		line := clipText(d.lines[l], width)
		n := utf8.RuneCountInString(line)
		switch k := strings.Index(line, ": "); {
		case l >= lo && l <= hi:
			spans = append(spans, ui.ToneSpan{From: pos, To: pos + max(n, 1), Tone: "text", Bg: bg})
		case !w.kv:
			spans = append(spans, jsonTones(line, pos)...)
		case k > 0:
			spans = append(spans, ui.ToneSpan{From: pos, To: pos + utf8.RuneCountInString(line[:k]), Tone: "key"})
		}
		sb.WriteString(line + "\n")
		pos += n + 1
	}
	n := textLine(strings.TrimSuffix(sb.String(), "\n"), "text")
	n.ID, n.Props["spans"] = id, spans
	return n
}

// --- table -------------------------------------------------------------------------

// tableRoot is the deepest array along the focus path, else the root.
func tableRoot(v any, focus Path) Path {
	for n := len(focus); n > 0; n-- {
		if x, ok := AtPath(v, focus[:n]); ok {
			if _, arr := x.([]any); arr {
				return focus[:n]
			}
		}
	}
	return Path{}
}

// tablePos maps the focus to a row and column of the relation at root.
func tablePos(v any, root, focus Path, cols []string) (row, col int) {
	rest := focus[min(len(root), len(focus)):]
	if x, _ := AtPath(v, root); x != nil {
		if _, arr := x.([]any); arr && len(rest) > 0 && rest[0].Indexed {
			row, rest = rest[0].Index, rest[1:]
		}
	}
	col = -1
	for k := len(rest); k > 0 && col < 0; k-- {
		col = slices.Index(cols, columnName(rest[:k]))
	}
	return row, col
}

// cellPath is the focus path of a table cell: a literal column key first
// (flattened relations), else the column name read as a path.
func cellPath(v any, root Path, row int, col string) Path {
	base := root
	if x, _ := AtPath(v, root); x != nil {
		if _, arr := x.([]any); arr {
			base = pathJoin(root, pathIdx(row))
		}
	}
	if col == "" {
		return base
	}
	if p := pathJoin(base, pathKey(col)); func() bool { _, ok := AtPath(v, p); return ok }() {
		return p
	}
	if q, err := ParsePath(col); err == nil {
		return pathJoin(base, q...)
	}
	return pathJoin(base, pathKey(col))
}

func objectField(o Object, key string) any {
	for _, f := range o {
		if f.Key == key {
			return f.Value
		}
	}
	return nil
}

// relation returns the table relation for window w, cached on the display.
func (a *App) relation(w *window, b *Buffer) (Relation, Path) {
	v := a.display(b)
	root := tableRoot(v, a.bufUI(b).focus)
	key := a.bufUI(b).dispKey + "|" + root.String()
	if w.relKey != key {
		x, _ := AtPath(v, root)
		w.rel, w.relKey = TableRelation(x), key
	}
	return w.rel, root
}

// tableModel builds the table widget for window w: the column window that
// keeps the active column visible, with that column at its full width.
func (a *App) tableModel(w *window, b *Buffer, width, h int) (table.Model, string) {
	rel, root := a.relation(w, b)
	row, col := tablePos(a.display(b), root, a.bufUI(b).focus, rel.Columns)
	col = max(col, 0)
	n := len(rel.Columns)
	widths := make([]int, n)
	for j, c := range rel.Columns {
		widths[j] = utf8.RuneCountInString(c) + 1
		for _, r := range rel.Rows[:min(len(rel.Rows), 500)] {
			widths[j] = max(widths[j], utf8.RuneCountInString(singleLine(valueText(objectField(r, c)))))
		}
		if j != col {
			widths[j] = min(widths[j], 24)
		}
	}
	if col < n {
		widths[col] = min(widths[col], width)
	}
	off, end := colWindow(widths, col, w.colOff, width)
	w.colOff = off
	m := table.Model{SelectedIdx: row, Offset: w.tOff, PageSize: max(h-2, 1), MatchMode: table.MatchSubstring}
	for j := off; j < end; j++ {
		t := rel.Columns[j]
		c := table.Column{Key: "c" + strconv.Itoa(j), Title: t}
		if j == col {
			c.Title, c.Width = "▸"+t, widths[j]
		}
		m.Columns = append(m.Columns, c)
	}
	for i, r := range rel.Rows {
		cells := make(map[string]any, end-off)
		for j := off; j < end; j++ {
			cells["c"+strconv.Itoa(j)] = singleLine(valueText(objectField(r, rel.Columns[j])))
		}
		m.Rows = append(m.Rows, table.Row{ID: strconv.Itoa(i), Cells: cells})
	}
	m.Focused()
	w.tOff = m.Offset
	title := fmt.Sprintf("1 Table · %s · %s · %d rows × %d cols", bufName(b), root.String(), len(rel.Rows), n)
	if off > 0 || end < n {
		title += fmt.Sprintf(" · cols %d-%d", off+1, end)
	}
	return m, title
}

// colWindow picks the visible columns [off, end) that fit width and include
// col, scrolling from from as little as possible.
func colWindow(widths []int, col, from, width int) (int, int) {
	n := len(widths)
	if n == 0 {
		return 0, 0
	}
	span := func(a, b int) int {
		s := 2 * (b - a - 1)
		for k := a; k < b; k++ {
			s += widths[k]
		}
		return s
	}
	off := min(max(from, 0), col)
	for off < col && span(off, col+1) > width {
		off++
	}
	end := col + 1
	for end < n && span(off, end+1) <= width {
		end++
	}
	for end == n && off > 0 && span(off-1, end) <= width {
		off--
	}
	return off, end
}

// --- lines ----------------------------------------------------------------------------

func (a *App) chipLine() ui.ViewNode {
	var parts []string
	for _, c := range jqChips {
		parts = append(parts, c.key+" + "+c.name)
	}
	state, tone := "", "muted"
	switch {
	case a.jq.running:
		state = "running… · "
	case a.jq.err != "":
		state, tone = a.jq.err+" · ", "warning"
	case a.jq.ready:
		state = "ready · enter opens the result · "
	}
	return textLine(" "+state+"chips: "+strings.Join(parts, " · ")+" · esc closes", tone)
}

func (a *App) infoLine(s core.State, g geom) ui.ViewNode {
	b := a.cur()
	u := a.bufUI(b)
	parts := []string{"focus " + u.focus.String()}
	if n := len(b.Pipeline); n > 0 {
		var kinds []string
		for _, op := range b.Pipeline {
			kinds = append(kinds, op.Kind)
		}
		parts = append(parts, "pipeline "+strings.Join(kinds, " › "))
	}
	if a.inner {
		parts = append(parts, "inner join")
	}
	parts = append(parts, "match "+a.match)
	tone := "muted"
	if b.ParseErr != nil {
		parts = append(parts, parseStatus(b.RawText, b.ParseErr))
		tone = "warning"
	}
	if in := s.Input; in.Count != nil || len(in.Prefix) > 0 {
		p := strings.Join(in.Prefix, " ")
		if in.Count != nil {
			p = strconv.Itoa(*in.Count) + " " + p
		}
		parts = append(parts, "pending "+strings.TrimSpace(p))
	}
	return textLine(clipText(" "+strings.Join(parts, " · "), g.w), tone)
}

// parseStatus says where raw stops being valid JSON, for example
// "incomplete JSON: line 1, col 9" while a value is still being typed.
func parseStatus(raw string, perr error) string {
	off, what := len(raw), "incomplete JSON"
	d := json.NewDecoder(strings.NewReader(raw))
	for {
		var x any
		err := d.Decode(&x)
		if err == nil {
			continue
		}
		var se *json.SyntaxError
		if errors.As(err, &se) {
			off, what = min(max(int(se.Offset)-1, 0), len(raw)), "invalid JSON"
		} else if errors.Is(err, io.EOF) && perr != nil {
			return "invalid JSON: " + singleLine(perr.Error())
		}
		break
	}
	starts := lineStarts(raw)
	l := lineOf(starts, off)
	col := utf8.RuneCountInString(raw[starts[l]:off]) + 1
	return fmt.Sprintf("%s: line %d, col %d", what, l+1, col)
}

func (a *App) statusLine(g geom) ui.ViewNode {
	if a.status != "" {
		return textLine(clipText(" "+singleLine(a.status), g.w), a.tone)
	}
	hint := " ? help · : commands · 1-4 views · tab pane · space select · F flatten · / filter · J jq · u undo · ctrl+e edit · q quit"
	return textLine(clipText(hint, g.w), "muted")
}

// --- modals -----------------------------------------------------------------------------

func (a *App) modalView(s core.State, f core.Frame) ui.ViewNode {
	switch m := f.Data.(type) {
	case *pickModal:
		return ui.ViewNode{ID: "pick.overlay", Kind: ui.KindOverlay, Scope: "pick", Title: m.m.Title + " · " + string(m.m.MatchMode),
			Props:    map[string]any{"widthPct": 70, "heightPct": 70},
			Children: []ui.ViewNode{m.m.View(modalPick), textLine("enter runs · esc closes · tab match mode · ↑↓ move", "muted")}}
	case *pathpicker.Model:
		return ui.ViewNode{ID: "files.overlay", Kind: ui.KindOverlay, Scope: "files", Title: "Choose a " + string(m.Mode) + " · " + string(m.MatchMode),
			Props:    map[string]any{"widthPct": 90, "heightPct": 80},
			Children: []ui.ViewNode{m.View(modalFiles), textLine("enter opens · ctrl+s uses this folder · ← parent · ctrl+t dotfiles · esc closes", "muted")}}
	case *helpModal:
		n := a.help.HelpView(modalHelp, m.q)
		n.Props["selected"], n.Scope = m.off, "help"
		return ui.ViewNode{ID: "help.overlay", Kind: ui.KindOverlay, Scope: "help", Title: "Help · keys active where you were",
			Props:    map[string]any{"widthPct": 85, "heightPct": 85},
			Children: []ui.ViewNode{n, textLine("j/k scroll · esc closes · config: bindings.json, themes.json, settings.json", "muted")}}
	}
	return ui.ViewNode{ID: f.ID + ".overlay", Kind: ui.KindOverlay, Children: []ui.ViewNode{textLine(f.ID, "muted")}}
}
