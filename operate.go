package main

// The grammar inside SHAPE, TABLE and NESTED (Design D, Design B's
// operators): y d c s ! enter ochazuke's operator_pending mode (the
// editor's pattern, bindings/editor.json), and the next keys name what
// they act on: a motion (j, k with counts: rows of the table) or a JSON
// text object, i (inner) or a (around) and a letter:
//
//	v  the value under the cursor    (av: "key": value)
//	k  its key                        (ak: the whole field)
//	p  its concrete path              (ap: the structure path, with [])
//	o  the enclosing object           (ao: with its key)
//	a  the enclosing array            (aa: with its key)
//	r  the record (the table row)     (ar: the same)
//	c  the column's values            (ac: {"column": [values]})
//	t  the table's array              (at: the same)
//	s  the structure's subtree        (the structure pane only)
//
// One verb per noun (item.operate, field.operate on the data pane;
// node.operate, shape.operate on the structure), one handler (operate):
// it resolves the object to values, then the operator acts on them. Which
// pairs exist is the table operatorPairs (data); a pair that is not there
// says so ("d: no column here"). The shortcut rows (y y, y p, y k, Y, d d,
// x) are rows on the same verb. Counts multiply (2 d 3 j), . repeats the
// last operate at the new focus (Repeatable), " a picks the register.

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"ofam.dev/ochazuke"
)

// operand is what an object or motion resolved to.
type operand struct {
	what  string  // the label the notice uses
	nodes []*Node // the values (one, or several: a column, rows)
	text  string  // a text object (key, path): yanked as text
	key   string  // a wrapping key (av, ak, ao, ac)
	at    valueAt // the value the change verbs act on
	rows  bool    // nodes are rows of one array (a motion, ir)
	col   string  // the column (ic, ac)
	array *Node   // the array the rows or the column are in
}

// operatorPairs is which objects each operator takes, per pane ("data",
// "structure"); a motion counts as the object "motion".
var operatorPairs = map[string]map[string][]string{
	"data": {
		"y": {"value", "key", "path", "object", "array", "row", "column", "table", "motion"},
		"d": {"value", "key", "object", "array", "row", "column", "table", "motion"},
		"c": {"value", "key", "row"},
		"s": {"column", "object", "array", "table"},
		"!": {"value", "path", "object", "array", "row", "column", "table"},
	},
	"structure": {
		"y": {"value", "key", "path", "subtree"},
		"d": {"subtree", "key"},
		"c": {"key"},
		"!": {"value", "path"},
	},
}

var objectLetters = map[string]string{
	"v": "value", "k": "key", "p": "path", "o": "object", "a": "array",
	"r": "row", "c": "column", "t": "table", "s": "subtree",
}

var arrayIndex = regexp.MustCompile(`\[[0-9]+\]`)

// generalPath is a concrete path's structure path: indexes as [].
func generalPath(p string) string { return arrayIndex.ReplaceAllString(p, "[]") }

// operate is the one handler of the operate verbs.
func (a *App) operate(c ochazuke.Ctx, pane string) ochazuke.Result {
	op := ochazuke.StringArg(c.Args, "op", "")
	object := ochazuke.StringArg(c.Args, "object", "")
	if l, ok := objectLetters[object]; ok {
		object = l
	}
	inner := c.Args["inner"] != false
	if m := ochazuke.StringArg(c.Args, "motion", ""); m != "" {
		object = "motion"
	}
	pairs, ok := operatorPairs[pane][op]
	if !ok {
		return ochazuke.Failed(fmt.Errorf("%s: no such operator here (y d c s !)", op))
	}
	if !slices.Contains(pairs, object) {
		return ochazuke.Failed(fmt.Errorf("%s: no %s here", op, objectName(object)))
	}
	if (op == "d" || op == "c") && pane == "data" && a.editGated() {
		c.Info(needsEditNote(op))
		return ochazuke.Done(nil)
	}
	var o operand
	var err error
	if pane == "data" {
		o, err = a.dataOperand(c, object, inner)
	} else {
		o, err = a.structureOperand(c, object, inner)
	}
	if err != nil {
		return ochazuke.Failed(fmt.Errorf("%s: %w", op, err))
	}
	switch op {
	case "y":
		return a.operateYank(c, object, o)
	case "d":
		if pane == "structure" {
			return a.forward(c, "node", "delete")
		}
		return a.operateDelete(c, object, o)
	case "c":
		if pane == "structure" {
			return a.forward(c, "node", "rename")
		}
		if object == "key" {
			return a.forward(c, "value", "rename")
		}
		if object == "row" {
			if len(o.nodes) == 0 {
				return noValues(op, object)
			}
			o.at = valueAt{node: o.nodes[0], parent: o.nodes[0].Parent, key: o.nodes[0].Key}
		}
		if err := a.openInput(&valueEdit{what: "edit", at: o.at}, editText(o.at.node), a.r.RenameSelect()); err != nil {
			return ochazuke.Failed(err)
		}
		return ochazuke.Done(nil)
	case "s":
		return a.operateSort(c, object, o, c.Args["reverse"] == true)
	case "!":
		return a.operateJQ(object, inner, o)
	}
	return ochazuke.Failed(fmt.Errorf("%s: no such operator", op))
}

// noValues is an operator's answer when its object resolved to no values
// (the # column's "column" is the table's order, not values): a message,
// nothing changed. Every operator that reads o.nodes or o.array checks
// first (TestOperatorOnNoValuesIsAMessage).
func noValues(op, object string) ochazuke.Result {
	name := object
	if object == "motion" {
		name = "rows"
	}
	return ochazuke.Failed(fmt.Errorf("%s: no %s here", op, name))
}

func objectName(object string) string {
	if object == "motion" {
		return "motion"
	}
	return object + " object"
}

// forward runs a verb of noun on the focus (the structure's own edits,
// the value verbs), as its key would.
func (a *App) forward(c ochazuke.Ctx, noun, verb string) ochazuke.Result {
	res, err := a.r.Execute(ochazuke.Request{Noun: noun, Verb: verb, Target: c.Instance.ID})
	if err != nil {
		return ochazuke.Failed(err)
	}
	return ochazuke.Done(res)
}

// dataOperand resolves an object on the data pane (the cell under the
// cursor, its row, its table).
func (a *App) dataOperand(c ochazuke.Ctx, object string, inner bool) (operand, error) {
	v, ok := a.valueOf(c.Instance)
	if !ok {
		return operand{}, errors.New("no value here")
	}
	row := nodeAt(c.Instance)
	if hv, ok := a.hoverValueAt(); ok && a.rowsOf(c.Instance) == a.data {
		v, row = hv, hv.parent
	}
	array := func(from *Node) *Node {
		for n := from; n != nil; n = n.Parent {
			if n.Kind == KArray {
				return n
			}
		}
		return nil
	}
	o := operand{at: v}
	switch object {
	case "value":
		if v.node == nil {
			return o, fmt.Errorf("no value at %s", v.path())
		}
		o.what, o.nodes = v.path(), []*Node{v.node}
		if !inner && v.inObject() {
			o.key = v.key
		}
	case "key":
		if !v.inObject() && !inner && v.node != nil {
			// ak on an array item is the item (d d deletes it).
			o.what, o.nodes = v.path(), []*Node{v.node}
			return o, nil
		}
		if !v.inObject() {
			return o, errors.New("no key here (an array item)")
		}
		o.what, o.text = "the key "+v.key, v.key
		if !inner && v.node != nil {
			o.text, o.nodes, o.key = "", []*Node{v.node}, v.key
		}
	case "path":
		o.what, o.text = "the path", v.path()
		if !inner {
			o.text = generalPath(v.path())
		}
	case "object":
		n := v.parent
		if v.node != nil && v.node.Kind == KObject {
			n = v.node
		}
		for n != nil && n.Kind != KObject {
			n = n.Parent
		}
		if n == nil {
			return o, errors.New("no object here")
		}
		o.what, o.nodes = n.Path(), []*Node{n}
		if !inner && n.Parent != nil && n.Parent.Kind == KObject {
			o.key = n.Key
		}
	case "array", "table":
		n := array(v.parent)
		if object == "array" && v.node != nil && v.node.Kind == KArray {
			n = v.node
		}
		if n == nil {
			return o, errors.New("no array here")
		}
		o.what, o.nodes = n.Path(), []*Node{n}
		if !inner && object == "array" && n.Parent != nil && n.Parent.Kind == KObject {
			o.key = n.Key
		}
	case "row":
		if row == nil {
			return o, errors.New("no row here")
		}
		o.what, o.nodes, o.rows, o.array = "the row "+row.Path(), []*Node{row}, true, row.Parent
	case "column":
		if row != nil && v.node == row && a.tab != nil {
			o.what = "the table's order" // the # column: s clears the sort
			return o, nil
		}
		if row == nil || row.Parent == nil || row.Parent.Kind != KArray || !v.inObject() {
			return o, errors.New("no column here")
		}
		arr := row.Parent
		for _, r := range arr.Children {
			if f := r.Field(v.key); f != nil {
				o.nodes = append(o.nodes, f)
			}
		}
		o.what, o.col, o.array = fmt.Sprintf("the column %s (%d values)", v.key, len(o.nodes)), v.key, arr
		if !inner {
			o.key = v.key
		}
	case "motion":
		if row == nil || row.Parent == nil || row.Parent.Kind != KArray {
			return o, errors.New("no rows here")
		}
		arr, steps := row.Parent, max(1, ochazuke.IntArg(c.Args, "steps", 1))
		lo, hi := row.Index, row.Index+steps
		if ochazuke.StringArg(c.Args, "motion", "down") == "up" {
			lo, hi = row.Index-steps, row.Index
		}
		lo, hi = max(lo, 0), min(hi, len(arr.Children)-1)
		o.nodes, o.rows, o.array = arr.Children[lo:hi+1], true, arr
		o.what = fmt.Sprintf("%d row%s of %s", len(o.nodes), plural(len(o.nodes)), arr.Path())
	}
	return o, nil
}

// structureOperand resolves an object on the structure pane: the shape
// under the cursor (the data pane shows it).
func (a *App) structureOperand(c ochazuke.Ctx, object string, inner bool) (operand, error) {
	s := a.shown
	if a.lit != nil {
		s = a.lit
	}
	if s == nil {
		return operand{}, errors.New("no shape here")
	}
	path := s.Path
	o := operand{}
	switch object {
	case "value":
		d := a.shownDoc()
		if d.err != nil {
			return o, d.err
		}
		values, fanned, err := Select(d.root, path)
		if err != nil {
			return o, err
		}
		o.what, o.nodes = path, values
		if fanned || len(values) != 1 {
			o.nodes = []*Node{arrayOf(values)}
		}
	case "key":
		key := lastKey(path)
		if key == "" {
			return o, errors.New("no key here")
		}
		o.what, o.text = "the key "+key, key
	case "path":
		o.what, o.text = "the path", path
	case "subtree":
		o.what = "the subtree"
	}
	return o, nil
}

// lastKey is the key a structure path ends in, "" for an index or [].
func lastKey(path string) string {
	steps, err := parsePath(path)
	if err != nil || len(steps) == 0 || steps[len(steps)-1].kind != 'k' {
		return ""
	}
	return steps[len(steps)-1].key
}

// arrayOf is values as one JSON array node.
func arrayOf(values []*Node) *Node {
	items := make([]string, len(values))
	for i, n := range values {
		items[i] = Compact(n, 0)
	}
	n, err := Build([]byte("[" + strings.Join(items, ",") + "]"))
	if err != nil {
		return &Node{Kind: KArray}
	}
	return n
}

// operateYank copies the operand: text as text, one value with its
// type, several as one JSON array; a wrapping key makes an object.
func (a *App) operateYank(c ochazuke.Ctx, object string, o operand) ochazuke.Result {
	switch {
	case o.text != "":
		return a.copyText(c, o.text, o.what)
	case o.what == "the subtree":
		return a.forward(c, "node", "yank")
	}
	if len(o.nodes) == 0 {
		return noValues("y", object)
	}
	n := o.nodes[0]
	if len(o.nodes) != 1 || object == "motion" || object == "column" {
		n = arrayOf(o.nodes)
	}
	if o.key != "" {
		k, _ := json.Marshal(o.key)
		if obj, err := Build([]byte("{" + string(k) + ":" + Compact(n, 0) + "}")); err == nil {
			n = obj
		}
	}
	format := ochazuke.StringArg(c.Args, "format", "json")
	if format != "json" {
		f, err := formatNamed(format)
		if err != nil {
			return ochazuke.Failed(err)
		}
		rows := n
		if n.Kind != KArray {
			rows = &Node{Kind: KArray, Children: []*Node{n}}
		}
		data, err := exportBytes(rows, f, a.exportOptions(".", nil))
		if err != nil {
			return ochazuke.Failed(err)
		}
		return a.copyText(c, string(data), o.what+" as "+f.name)
	}
	return a.copyYank(c, valueYank(n), o.what)
}

// operateDelete deletes the operand as value patches, one undo step.
func (a *App) operateDelete(c ochazuke.Ctx, object string, o operand) ochazuke.Result {
	var patches []Patch
	switch object {
	case "value", "key":
		if o.at.node == nil || o.at.parent == nil {
			return ochazuke.Failed(errors.New("d: nothing to delete here"))
		}
		patches = []Patch{{Op: opDelete, Path: o.at.path()}}
	case "row", "motion":
		for i := len(o.nodes) - 1; i >= 0; i-- { // from the last, so the indexes hold
			patches = append(patches, Patch{Op: opDelete, Path: o.nodes[i].Path()})
		}
	case "column", "object", "array", "table":
		for _, n := range o.nodes {
			if n.Parent == nil {
				return ochazuke.Failed(errors.New("d: the document stays"))
			}
			patches = append(patches, Patch{Op: opDelete, Path: n.Path()})
		}
	}
	if len(patches) == 0 {
		return noValues("d", object)
	}
	if !a.editedShown() || !a.dataEditable() {
		return ochazuke.Failed(errors.New("d: values change on the edited view (T), with data.editable on"))
	}
	if err := a.commitPatches("delete "+o.what, append(slices.Clone(a.rs.patches), patches...)); err != nil {
		return ochazuke.Failed(err)
	}
	c.Info(fmt.Sprintf("deleted %s · u undoes", o.what))
	return ochazuke.Done(nil)
}

// operateJQ opens the search line in its jq type with the operand's path
// typed (the column: its values; an array: its items); around (a), the
// structure path.
func (a *App) operateJQ(object string, inner bool, o operand) ochazuke.Result {
	var q string
	switch object {
	case "path":
		q = o.text
	case "value":
		q = o.at.path()
		if len(o.nodes) > 0 && o.at.node == nil {
			q = o.what
		}
	case "column":
		if o.array == nil || o.col == "" {
			return noValues("!", object)
		}
		q = arrayItems(o.array.Path()) + keyStep(o.col)
	case "array", "table":
		if len(o.nodes) == 0 {
			return noValues("!", object)
		}
		q = arrayItems(o.nodes[0].Path())
	default:
		if len(o.nodes) == 0 {
			return noValues("!", object)
		}
		q = o.nodes[0].Path()
	}
	if !inner && object != "path" {
		q = generalPath(q)
	}
	a.sr.typ = "jq"
	if err := a.setQuery(q); err != nil {
		return ochazuke.Failed(err)
	}
	if err := a.focusQuery(); err != nil {
		return ochazuke.Failed(err)
	}
	return ochazuke.Done(nil)
}

// arrayItems is the jq path of an array's items.
func arrayItems(path string) string {
	if path == "." {
		return ".[]"
	}
	return path + "[]"
}

// tableSort is the data pane's sort: the table at path shown by its
// column col's values (a view, not an edit: the document keeps its
// order; :w writes the document, an export the rows as shown).
type tableSort struct {
	path, col string
	reverse   bool
}

// operateSort is s: the column sorts the table (view state; s on the
// same column again reverses it); an object or array is sorted as a value
// patch (keys by name, items by value), one undo step.
func (a *App) operateSort(c ochazuke.Ctx, object string, o operand, reverse bool) ochazuke.Result {
	switch {
	case object == "column" && o.col == "":
		if o.what != "the table's order" {
			return ochazuke.Failed(errors.New("s: no column here"))
		}
		a.sort = nil
		a.reshowData()
		c.Info("sort cleared: the document's order")
		return ochazuke.Done(nil)
	case object == "column":
		if a.tab == nil {
			return ochazuke.Failed(errors.New("s: sort a table"))
		}
		path := a.tab.Path
		if a.sort != nil && a.sort.path == path && a.sort.col == o.col && !reverse && c.Args["again"] != false {
			reverse = !a.sort.reverse
		}
		a.sort = &tableSort{path: path, col: o.col, reverse: reverse}
		a.reshowData()
		dir := "↑"
		if reverse {
			dir = "↓"
		}
		c.Info(fmt.Sprintf("sorted by %s %s (a view: the document keeps its order) · s on # clears", o.col, dir))
		return ochazuke.Done(nil)
	}
	if len(o.nodes) == 0 {
		return noValues("s", object)
	}
	n := o.nodes[0]
	if !a.editedShown() || !a.dataEditable() {
		return ochazuke.Failed(errors.New("s: values change on the edited view (T), with data.editable on"))
	}
	if a.editGated() {
		c.Info(needsEditNote("s"))
		return ochazuke.Done(nil)
	}
	kids := slices.Clone(n.Children)
	if n.Kind == KObject {
		sort.SliceStable(kids, func(i, j int) bool { return kids[i].Key < kids[j].Key })
	} else {
		sort.SliceStable(kids, func(i, j int) bool { return lessValue(kids[i], kids[j]) })
	}
	if reverse {
		slices.Reverse(kids)
	}
	var b strings.Builder
	open, end := "[", "]"
	if n.Kind == KObject {
		open, end = "{", "}"
	}
	b.WriteString(open)
	for i, k := range kids {
		if i > 0 {
			b.WriteString(",")
		}
		if n.Kind == KObject {
			key, _ := json.Marshal(k.Key)
			b.Write(key)
			b.WriteString(":")
		}
		b.WriteString(Compact(k, 0))
	}
	b.WriteString(end)
	if err := a.commitPatches("sort "+n.Path(), append(slices.Clone(a.rs.patches), Patch{Op: opSet, Path: n.Path(), Value: json.RawMessage(b.String())})); err != nil {
		return ochazuke.Failed(err)
	}
	c.Info("sorted " + n.Path() + " · u undoes")
	return ochazuke.Done(nil)
}

// lessValue orders values: numbers by value, then strings, then the rest
// by their JSON; a missing value last.
func lessValue(x, y *Node) bool {
	if x == nil || y == nil {
		return x != nil && y == nil
	}
	rank := func(n *Node) int {
		switch n.Kind {
		case KNumber:
			return 0
		case KString:
			return 1
		case KBool:
			return 2
		case KNull:
			return 4
		}
		return 3
	}
	if rx, ry := rank(x), rank(y); rx != ry {
		return rx < ry
	}
	if x.Kind == KNumber {
		fx, ex := strconv.ParseFloat(Text(x, 0), 64)
		fy, ey := strconv.ParseFloat(Text(y, 0), 64)
		if ex == nil && ey == nil {
			return fx < fy
		}
	}
	return Compact(x, 0) < Compact(y, 0)
}

// sortedRows is t's rows in the table sort's order, when it sorts t.
func (a *App) sortedRows(t *Table) []*Node {
	if a.sort == nil || t.Path != a.sort.path {
		return t.Rows
	}
	rows := slices.Clone(t.Rows)
	col := a.sort.col
	sort.SliceStable(rows, func(i, j int) bool {
		x, y := rows[i].Field(col), rows[j].Field(col)
		if a.sort.reverse && x != nil && y != nil {
			return lessValue(y, x)
		}
		return lessValue(x, y) // a missing value last either way
	})
	return rows
}

// reshowData draws the data pane's table again (a sort changed).
func (a *App) reshowData() {
	if a.shown != nil {
		_ = a.show(a.shown)
	}
}

func (a *App) registerOperate() {
	r := a.r
	desc := "Operate on a JSON text object or a motion (Vim's operators): op y yank, d delete, c change, s sort, ! jq; object v value, k key, p path, o object, a array, r row, c column, t table, s subtree (inner, or around with inner false), or motion down/up with steps rows; the register from \" a"
	for _, noun := range []string{"item", "field"} {
		r.Define(noun).Verb("operate", func(c ochazuke.Ctx) ochazuke.Result { return a.operate(c, "data") }, desc)
	}
	for _, noun := range []string{"node", "shape"} {
		r.Define(noun).Verb("operate", func(c ochazuke.Ctx) ochazuke.Result { return a.operate(c, "structure") }, desc)
	}
	params := map[string]ochazuke.Parameter{
		"op":       {Type: "string", Required: true, Enum: []string{"y", "d", "c", "s", "!"}, Description: "the operator"},
		"object":   {Type: "string", Description: "the text object: value, key, path, object, array, row, column, table, subtree (or its letter v k p o a r c t s)"},
		"inner":    {Type: "boolean", Description: "i (inner, the default) or a (around: with its key, the structure path)"},
		"motion":   {Type: "string", Enum: []string{"down", "up"}, Description: "a motion instead of an object: rows down or up"},
		"steps":    {Type: "integer", Description: "rows the motion covers"},
		"reverse":  {Type: "boolean", Description: "s: descending"},
		"again":    {Type: "boolean", Description: "s on the sorted column reverses it (the default); false sorts ascending again"},
		"register": {Type: "string", Description: "y: the register"},
		"format":   {Type: "string", Enum: append([]string{"json"}, exportFormatNames()...), Description: "y: json keeps the value and its type (default); csv, tsv, jsonl or md export the selected objects as rows using export.arrays, export.join and export.null; key and path yanks remain plain text"},
	}
	keys := []string{"item.operate", "field.operate", "node.operate", "shape.operate"}
	for _, k := range keys {
		setParams(r, k, params)
	}
	setHistory(r, ochazuke.HistoryChange, keys...)
	setRepeatable(r, keys...)
	for _, al := range modeAliases {
		if err := r.AddActionAlias(al); err != nil {
			panic(err) // o-jj's own rows: a clash is a bug here
		}
	}
}
