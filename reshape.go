package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Reshape steps: the restructures a shape mapping cannot say, because
// they make rows or keys out of values. A mapping's "then" lists them in
// order; each reads the document the steps before it made (the map and
// its patches first), and carries its own map and patches, which read
// the step's output as the first map reads the source:
//
//	{"op": "flatten", "paths": [P...], "arrays": "join"|"json"|"rows", "join": SEP, "names": "leaf"|"path"}
//	    a flat array of objects, one per item of the paths' nearest common
//	    array (the document itself when they share none), each holding the
//	    marked fields under flat names: the dotted path from the item
//	    (address.city), or with names leaf the last key where no other
//	    column ends in it. An array among them joins (join, SEP between the
//	    items), becomes its compact JSON (json), or gives one row per item
//	    with the other columns repeated (rows; several arrays give every
//	    combination), the rule the export follows (export.arrays).
//	{"op": "key-by", "path": P, "field": F, "drop": bool, "duplicates": "rename"|"last"|"group", "missing": K}
//	    the array at P as an object keyed by each item's F (jq's
//	    INDEX(.[]; .F)); drop takes F out of each value. A key two items
//	    share is renamed (k-2, k-3: the second and later items keep their
//	    own key), the last item's (jq's INDEX), or every item's in an array
//	    (group). Items without F (not an object, no F, or F an object or
//	    an array) go under the key K (default _missing), in an array, never
//	    dropped.
//	{"op": "values", "path": P}     the object at P as the array of its values ([.[]])
//	{"op": "entries", "path": P}    the object at P as {key, value} pairs (to_entries)
//
// A path is a structure path in the step's input (keys and []); a value
// at P that is not the kind the op reads stays as it is. The Go apply
// (applyStage) and the jq program (stageJQ) agree on any document.

// The reshape ops.
const (
	opFlatten = "flatten"
	opKeyBy   = "key-by"
	opValues  = "values"
	opEntries = "entries"
)

// The flatten name modes and the key-by duplicate modes.
const (
	namesLeaf      = "leaf"
	namesPath      = "path"
	dupRename      = "rename"
	dupLast        = "last"
	dupGroup       = "group"
	defaultMissing = "_missing"
)

// Stage is one reshape step and the map and patches that read its output.
type Stage struct {
	Op         string     `json:"op"`
	Paths      []string   `json:"paths,omitempty"`
	Path       string     `json:"path,omitempty"`
	Field      string     `json:"field,omitempty"`
	Arrays     string     `json:"arrays,omitempty"`
	Join       string     `json:"join,omitempty"`
	Names      string     `json:"names,omitempty"`
	Drop       bool       `json:"drop,omitempty"`
	Duplicates string     `json:"duplicates,omitempty"`
	Missing    string     `json:"missing,omitempty"`
	Map        []MapEntry `json:"map,omitempty"`
	Patches    []Patch    `json:"patches,omitempty"`
}

// defaults fills what a stage leaves out and checks the rest.
func (s Stage) defaults() (Stage, error) {
	switch s.Op {
	case opFlatten:
		if len(s.Paths) == 0 {
			return s, errors.New(`flatten: "paths" names the fields to keep`)
		}
		if s.Arrays == "" {
			s.Arrays = arraysJoin
		}
		if !slices.Contains([]string{arraysJoin, arraysJSON, arraysRows}, s.Arrays) {
			return s, fmt.Errorf("flatten: arrays %q (join, json or rows)", s.Arrays)
		}
		if s.Names == "" {
			s.Names = namesLeaf
		}
		if s.Names != namesLeaf && s.Names != namesPath {
			return s, fmt.Errorf("flatten: names %q (leaf or path)", s.Names)
		}
		if s.Arrays == arraysJoin && s.Join == "" {
			s.Join = ";"
		}
		for _, p := range s.Paths {
			if _, err := mappingPath(p); err != nil {
				return s, fmt.Errorf("flatten: %w", err)
			}
		}
	case opKeyBy:
		if s.Field == "" {
			return s, errors.New(`key-by: "field" names the key each item is filed under`)
		}
		if s.Duplicates == "" {
			s.Duplicates = dupRename
		}
		if !slices.Contains([]string{dupRename, dupLast, dupGroup}, s.Duplicates) {
			return s, fmt.Errorf("key-by: duplicates %q (rename, last or group)", s.Duplicates)
		}
		if s.Missing == "" {
			s.Missing = defaultMissing
		}
		fallthrough
	case opValues, opEntries:
		if s.Path == "" {
			s.Path = "."
		}
		if _, err := mappingPath(s.Path); err != nil {
			return s, fmt.Errorf("%s: %w", s.Op, err)
		}
	default:
		return s, fmt.Errorf("op %q: a reshape step is flatten, key-by, values or entries", s.Op)
	}
	return s, nil
}

// stagePlan is a stage made ready against its input: the defaults filled
// in, flatten's common array, tries and renames worked out from the data,
// and the plan of its map over the step's output (nil: a copy).
type stagePlan struct {
	st      Stage
	common  []pathStep // flatten: the items' path
	trie    *trie      // flatten: the fields under an item
	renames map[string]string
	plan    *Plan // the stage's map; nil copies the output
}

// prepareStage readies a stage on its input document (input's shape).
func prepareStage(s Stage, in *Node) (*stagePlan, error) {
	s, err := s.defaults()
	if err != nil {
		return nil, err
	}
	sp := &stagePlan{st: s}
	if s.Op == opFlatten {
		shape := Infer(in)
		var paths [][]pathStep
		for _, p := range s.Paths {
			steps, _ := mappingPath(p)
			paths = append(paths, steps)
		}
		// A marked array the others lie in (or the only mark) stands for
		// its items: their rows are the array's.
		lcp := commonPrefix(paths)
		if sh := shape.Find(stepsPath(lcp)); sh != nil && sh.Count > 0 && sh.Kinds[KArray] == sh.Count {
			for i, p := range paths {
				if len(p) == len(lcp) {
					paths[i] = append(slices.Clone(p), pathStep{kind: 'a'})
				}
			}
		}
		sp.common = commonItems(paths)
		sp.trie = &trie{}
		for _, p := range paths {
			sp.trie.add(p[len(sp.common):])
		}
		sp.renames = sp.leafRenames(in)
	}
	return sp, nil
}

// commonItems is the paths' nearest common array item: their longest
// common prefix, cut back to its last [] (nil when there is none).
func commonItems(paths [][]pathStep) []pathStep {
	prefix := commonPrefix(paths)
	for len(prefix) > 0 && prefix[len(prefix)-1].kind != 'a' {
		prefix = prefix[:len(prefix)-1]
	}
	return prefix
}

// commonPrefix is the paths' longest common prefix.
func commonPrefix(paths [][]pathStep) []pathStep {
	prefix := slices.Clone(paths[0])
	for _, p := range paths[1:] {
		n := 0
		for n < len(prefix) && n < len(p) && prefix[n] == p[n] {
			n++
		}
		prefix = prefix[:n]
	}
	return prefix
}

// trie is the marked paths below an item: end where a path ends (the
// value is kept whole), keys and items for the steps below.
type trie struct {
	end   bool
	keys  []string
	sub   map[string]*trie
	items *trie
}

func (t *trie) add(steps []pathStep) {
	if len(steps) == 0 {
		t.end = true
		return
	}
	st := steps[0]
	if st.kind == 'a' {
		if t.items == nil {
			t.items = &trie{}
		}
		t.items.add(steps[1:])
		return
	}
	if t.sub == nil {
		t.sub = map[string]*trie{}
	}
	c := t.sub[st.key]
	if c == nil {
		c = &trie{}
		t.sub[st.key] = c
		t.keys = append(t.keys, st.key)
	}
	c.add(steps[1:])
}

// prune is n with only the marked paths, in n's own order, and whether
// anything is left: a key on an object that has it, every item of an
// array that keeps something; an object or array left empty, or a value
// of the wrong kind, is absent.
func prune(n *Node, t *trie) (*Node, bool) {
	if t.end {
		return n, true
	}
	switch {
	case n.Kind == KObject && len(t.keys) > 0:
		out := &Node{Kind: KObject, Key: n.Key}
		for _, c := range n.Children {
			if sub := t.sub[c.Key]; sub != nil {
				if v, ok := prune(c, sub); ok {
					out.Children = append(out.Children, rekey(v, c.Key))
				}
			}
		}
		return out, len(out.Children) > 0
	case n.Kind == KArray && t.items != nil:
		out := &Node{Kind: KArray, Key: n.Key}
		for _, c := range n.Children {
			if v, ok := prune(c, t.items); ok {
				out.Children = append(out.Children, rekey(v, ""))
			}
		}
		return out, len(out.Children) > 0
	}
	return nil, false
}

// pruneJQ is prune as a jq filter (empty for absent).
func (t *trie) jq() string {
	if t.end {
		return "."
	}
	var obj, arr string
	if len(t.keys) > 0 {
		var arms []string
		for i, k := range t.keys {
			kw := "elif"
			if i == 0 {
				kw = "if"
			}
			arms = append(arms, kw+" .key == "+quote(k)+" then ((.value | "+t.sub[k].jq()+") as $v | {key: .key, value: $v})")
		}
		obj = "to_entries | map(" + strings.Join(arms, " ") + " else empty end) | if length == 0 then empty else from_entries end"
	}
	if t.items != nil {
		arr = "map(" + t.items.jq() + ") | if length == 0 then empty else . end"
	}
	switch {
	case obj != "" && arr != "":
		return `if type == "object" then (` + obj + `) elif type == "array" then (` + arr + `) else empty end`
	case obj != "":
		return `if type == "object" then (` + obj + `) else empty end`
	}
	return `if type == "array" then (` + arr + `) else empty end`
}

// column is one flat field of a row: its dotted name, the key it ends in
// (for names leaf) and its value.
type column struct {
	name, leaf string
	v          *Node
}

type flatRow []column

// set is the row with c added, or put in place of a column of its name
// (a later key wins and keeps the first one's position, as jq's + does).
func (r flatRow) set(c column) flatRow {
	for i := range r {
		if r[i].name == c.name {
			r[i] = c
			return r
		}
	}
	return append(r, c)
}

func joinRows(a, b []flatRow) []flatRow {
	out := make([]flatRow, 0, len(a)*len(b))
	for _, x := range a {
		for _, y := range b {
			row := slices.Clone(x)
			for _, c := range y {
				row = row.set(c)
			}
			out = append(out, row)
		}
	}
	return out
}

// flatRows is a value's rows (rows mode makes one per array item), the
// export's flatten keeping JSON values: prefix is the dotted path so far,
// leaf the key it ends in.
func (sp *stagePlan) flatRows(n *Node, prefix, leaf string) []flatRow {
	name := columnName(prefix)
	if leaf == "" {
		leaf = name
	}
	switch n.Kind {
	case KObject:
		if len(n.Children) == 0 {
			return []flatRow{{{name, leaf, &Node{Kind: KObject}}}}
		}
		out := []flatRow{{}}
		for _, c := range n.Children {
			out = joinRows(out, sp.flatRows(c, dottedKey(prefix, c.Key), c.Key))
		}
		return out
	case KArray:
		switch sp.st.Arrays {
		case arraysRows:
			if len(n.Children) == 0 {
				return []flatRow{{}}
			}
			var out []flatRow
			for _, c := range n.Children {
				out = append(out, sp.flatRows(c, prefix, leaf)...)
			}
			return out
		}
		return []flatRow{sp.collapse(n, prefix, leaf)}
	}
	return []flatRow{{{name, leaf, n}}}
}

// collapse is an array as one row (arrays join or json): each item's
// columns, then every column's values across the items joined (join, the
// separator between them) or as a JSON array (json). An empty array is
// one column under its own name: "" or "[]".
func (sp *stagePlan) collapse(n *Node, prefix, leaf string) flatRow {
	text := func(vals []*Node) string {
		if sp.st.Arrays == arraysJSON {
			return Compact(&Node{Kind: KArray, Children: vals}, 0)
		}
		parts := make([]string, len(vals))
		for i, v := range vals {
			switch {
			case v.Kind == KNull:
			case v.IsContainer():
				parts[i] = Compact(v, 0)
			default:
				parts[i] = v.Value
			}
		}
		return strings.Join(parts, sp.st.Join)
	}
	if len(n.Children) == 0 {
		return flatRow{{columnName(prefix), leaf, &Node{Kind: KString, Value: text(nil)}}}
	}
	var cols []column
	vals := map[string][]*Node{}
	for _, c := range n.Children {
		for _, row := range sp.flatRows(c, prefix, leaf) {
			for _, col := range row {
				if _, ok := vals[col.name]; !ok {
					cols = append(cols, col)
				}
				vals[col.name] = append(vals[col.name], col.v)
			}
		}
	}
	out := make(flatRow, len(cols))
	for i, c := range cols {
		out[i] = column{c.name, c.leaf, &Node{Kind: KString, Value: text(vals[c.name])}}
	}
	return out
}

// items are the values flatten makes rows of.
func (sp *stagePlan) items(in *Node) []*Node {
	if len(sp.common) == 0 {
		return []*Node{in}
	}
	return stream(in, sp.common)
}

// rows is every row flatten makes, before renaming.
func (sp *stagePlan) rows(in *Node) []flatRow {
	var out []flatRow
	for _, it := range sp.items(in) {
		v, ok := prune(it, sp.trie)
		if !ok {
			out = append(out, flatRow{})
			continue
		}
		out = append(out, sp.flatRows(v, "", "")...)
	}
	return out
}

// leafRenames is, for names leaf, each column's new name: the key it
// ends in, where no other column ends in the same key.
func (sp *stagePlan) leafRenames(in *Node) map[string]string {
	if sp.st.Names != namesLeaf {
		return nil
	}
	leafOf := map[string]string{}
	var order []string
	for _, row := range sp.rows(in) {
		for _, c := range row {
			if _, ok := leafOf[c.name]; !ok {
				leafOf[c.name] = c.leaf
				order = append(order, c.name)
			}
		}
	}
	count := map[string]int{}
	for _, name := range order {
		count[leafOf[name]]++
	}
	out := map[string]string{}
	for _, name := range order {
		if l := leafOf[name]; count[l] == 1 && l != name {
			out[name] = l
		}
	}
	return out
}

// applyStage is the step's output on its input.
func (sp *stagePlan) apply(in *Node) *Node {
	s := sp.st
	switch s.Op {
	case opFlatten:
		out := &Node{Kind: KArray}
		for _, row := range sp.rows(in) {
			obj := &Node{Kind: KObject}
			for _, c := range row {
				name := c.name
				if r, ok := sp.renames[name]; ok {
					name = r
				}
				obj.Children = append(obj.Children, rekey(c.v, name))
			}
			out.Children = append(out.Children, obj)
		}
		return out
	}
	steps, _ := mappingPath(s.Path)
	return updateAt(in, steps, func(n *Node) *Node {
		switch {
		case s.Op == opKeyBy && n.Kind == KArray:
			return keyBy(n, s)
		case s.Op == opValues && n.Kind == KObject:
			out := &Node{Kind: KArray}
			for _, c := range n.Children {
				out.Children = append(out.Children, rekey(c, ""))
			}
			return out
		case s.Op == opEntries && n.Kind == KObject:
			out := &Node{Kind: KArray}
			for _, c := range n.Children {
				out.Children = append(out.Children, &Node{Kind: KObject, Children: []*Node{
					{Key: "key", Kind: KString, Value: c.Key}, rekey(c, "value")}})
			}
			return out
		}
		return n
	})
}

// updateAt is n with f applied to every value at steps (keys and []):
// a key on an object that has it, every item of an array; the rest stays.
func updateAt(n *Node, steps []pathStep, f func(*Node) *Node) *Node {
	if len(steps) == 0 {
		return rekey(f(n), n.Key)
	}
	st := steps[0]
	if i := childAt(n, st); st.kind == 'k' && i >= 0 {
		c := cloneNode(n)
		c.Children[i] = updateAt(n.Children[i], steps[1:], f)
		return c
	}
	if st.kind == 'a' && n.Kind == KArray {
		c := cloneNode(n)
		for i, item := range n.Children {
			c.Children[i] = updateAt(item, steps[1:], f)
		}
		return c
	}
	return n
}

// keyText is a scalar's key as jq's tostring writes it, and whether the
// value can be a key (objects and arrays cannot).
func keyText(v *Node) (string, bool) {
	if v == nil || v.IsContainer() {
		return "", false
	}
	return v.Value, true
}

// keyBy files an array's items under their field's value.
func keyBy(arr *Node, s Stage) *Node {
	out := &Node{Kind: KObject}
	at := map[string]int{}
	var missing []*Node
	for _, it := range arr.Children {
		k, ok := keyText(it.Field(s.Field))
		if !ok {
			missing = append(missing, rekey(it, ""))
			continue
		}
		v := it
		if s.Drop {
			v = cloneNode(it)
			v.Children = slices.DeleteFunc(v.Children, func(c *Node) bool { return c.Key == s.Field })
		}
		i, seen := at[k]
		switch {
		case s.Duplicates == dupGroup && seen:
			g := cloneNode(out.Children[i])
			g.Children = append(g.Children, rekey(v, ""))
			out.Children[i] = g
		case s.Duplicates == dupGroup:
			at[k] = len(out.Children)
			out.Children = append(out.Children, &Node{Kind: KArray, Key: k, Children: []*Node{rekey(v, "")}})
		case s.Duplicates == dupLast && seen:
			out.Children[i] = rekey(v, k)
		default:
			if seen {
				k = freeKey(k, takenKeys(at))
			}
			at[k] = len(out.Children)
			out.Children = append(out.Children, rekey(v, k))
		}
	}
	if len(missing) > 0 {
		k := freeKey(s.Missing, takenKeys(at))
		out.Children = append(out.Children, &Node{Kind: KArray, Key: k, Children: missing})
	}
	return out
}

func takenKeys(at map[string]int) map[string]bool {
	out := make(map[string]bool, len(at))
	for k := range at {
		out[k] = true
	}
	return out
}

// jqFree is freeKey in jq: $k, or $k-2, $k-3, ... past the object's keys.
const jqFree = `def _free($k; $o): if ($o | has($k)) | not then $k else first(range(2; infinite) | "\($k)-\(.)" | select(. as $c | $o | has($c) | not)) end;`

// jqFlattenDefs are flatten's helpers: the export's _frags keeping JSON
// values, with the arrays rule in the middle.
func jqFlattenDefs(s Stage) string {
	var array string
	if s.Arrays == arraysRows {
		array = `if length == 0 then {} else .[] | _frags($p) end`
	} else {
		empty, text := `""`, `map(if type == "string" then . elif type == "null" then "" elif type == "object" or type == "array" then tojson else tostring end) | join(`+quote(s.Join)+`)`
		if s.Arrays == arraysJSON {
			empty, text = `"[]"`, `tojson`
		}
		array = `if length == 0 then {(_name($p)): ` + empty + `} else ([.[] | _frags($p)] as $rs | ` +
			`reduce ($rs[] | to_entries[] | .key) as $k ([]; if any(.[]; . == $k) then . else . + [$k] end) | ` +
			`reduce .[] as $k ({}; . + {($k): ([$rs[] | select(has($k)) | .[$k]] | ` + text + `)})) end`
	}
	return `def _key($p; $k): if $p == "" then $k else $p + "." + $k end; ` +
		`def _name($p): if $p == "" then "value" else $p end; ` +
		`def _frags($p): def _fields($es): if ($es | length) == 0 then {} else ($es[0].value | _frags(_key($p; $es[0].key))) as $a | _fields($es[1:]) as $b | $a + $b end; ` +
		`if type == "object" then (if length == 0 then {(_name($p)): {}} else _fields(to_entries) end) ` +
		`elif type == "array" then (` + array + `) else {(_name($p)): .} end;`
}

// jqItems is the stream of values at steps, as stream reads them.
func jqItems(steps []pathStep) string {
	if len(steps) == 0 {
		return "."
	}
	var parts []string
	for _, st := range steps {
		if st.kind == 'a' {
			parts = append(parts, `select(type == "array") | .[]`)
			continue
		}
		parts = append(parts, `select(type == "object" and has(`+quote(st.key)+`)) | `+dotted(keyStep(st.key)))
	}
	return strings.Join(parts, " | ")
}

// jqUpdate is updateAt in jq: f at every value at steps, the rest as it is.
func jqUpdate(steps []pathStep, f string) string {
	if len(steps) == 0 {
		return f
	}
	st, rest := steps[0], jqUpdate(steps[1:], f)
	if st.kind == 'a' {
		return `if type == "array" then map(` + rest + `) else . end`
	}
	return `if type == "object" and has(` + quote(st.key) + `) then ` + dotted(keyStep(st.key)) + ` |= (` + rest + `) else . end`
}

// opJQ is the step as a jq program.
func (sp *stagePlan) opJQ() string {
	s := sp.st
	switch s.Op {
	case opFlatten:
		prog := jqFlattenDefs(s) + " [" + jqItems(sp.common) + " | ([" + sp.trie.jq() + "] | if length == 0 then {} else .[0] | _frags(\"\") end)]"
		if len(sp.renames) > 0 {
			names := make([]string, 0, len(sp.renames))
			for from, to := range sp.renames {
				names = append(names, quote(from)+": "+quote(to))
			}
			slices.Sort(names)
			prog += " | {" + strings.Join(names, ", ") + "} as $ren | map(with_entries(.key |= ($ren[.] // .)))"
		}
		return prog
	}
	steps, _ := mappingPath(s.Path)
	var f string
	switch s.Op {
	case opValues:
		f = `if type == "object" then [.[]] else . end`
	case opEntries:
		f = `if type == "object" then to_entries else . end`
	case opKeyBy:
		field := quote(s.Field)
		v := "$e"
		if s.Drop {
			v = "($e | del(.[" + field + "]))"
		}
		var file string
		switch s.Duplicates {
		case dupLast:
			file = ".o[$k] = $v"
		case dupGroup:
			file = ".o[$k] += [$v]"
		default:
			file = ".o |= (_free($k; .) as $kk | . + {($kk): $v})"
		}
		f = `if type == "array" then (reduce .[] as $e ({o: {}, m: []}; ` +
			`if ($e | type) == "object" and ($e | has(` + field + `)) and (($e[` + field + `] | type) as $t | $t != "object" and $t != "array") ` +
			`then ($e[` + field + `] | tostring) as $k | ` + v + ` as $v | ` + file + ` else .m += [$e] end) ` +
			`| if (.m | length) > 0 then (.o as $o | .o + {(_free(` + quote(s.Missing) + `; $o)): .m}) else .o end) else . end`
		return jqFree + " " + jqUpdate(steps, f)
	}
	return jqUpdate(steps, f)
}

// mapJQ is the stage's map and patches as jq, "" when it copies.
func (sp *stagePlan) mapJQ() string {
	if sp.plan != nil {
		return strings.TrimSuffix(sp.plan.JQ(), "\n")
	}
	steps, uses := patchesJQ(sp.st.Patches)
	if steps == "" {
		return ""
	}
	prog := "." + steps
	if uses {
		prog = jqHasPath + " " + prog
	}
	return prog
}

// stageNames is a stage's one-line description, for notices.
func stageName(s Stage) string {
	switch s.Op {
	case opFlatten:
		return fmt.Sprintf("flatten %d field(s), arrays %s", len(s.Paths), s.Arrays)
	case opKeyBy:
		return fmt.Sprintf("key %s by %s", s.Path, s.Field)
	}
	return s.Op + " " + s.Path
}

// formatStage writes a stage on one line, its map and patches below.
func formatStage(s Stage, str func(string) string) string {
	var fields []string
	add := func(k, v string) { fields = append(fields, quote(k)+": "+v) }
	add("op", str(s.Op))
	if len(s.Paths) > 0 {
		paths := make([]string, len(s.Paths))
		for i, p := range s.Paths {
			paths[i] = str(p)
		}
		add("paths", "["+strings.Join(paths, ", ")+"]")
	}
	for _, kv := range [][2]string{{"path", s.Path}, {"field", s.Field}, {"arrays", s.Arrays}, {"join", s.Join}, {"names", s.Names}} {
		if kv[1] != "" || kv[0] == "join" && s.Op == opFlatten && s.Arrays == arraysJoin {
			add(kv[0], str(kv[1]))
		}
	}
	if s.Drop {
		add("drop", "true")
	}
	for _, kv := range [][2]string{{"duplicates", s.Duplicates}, {"missing", s.Missing}} {
		if kv[1] != "" {
			add(kv[0], str(kv[1]))
		}
	}
	var b strings.Builder
	b.WriteString("  {" + strings.Join(fields, ", "))
	if len(s.Map) > 0 {
		b.WriteString(`, "map": [` + "\n")
		for i, e := range s.Map {
			b.WriteString("    " + formatEntry(e, str))
			if i < len(s.Map)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString("  ]")
	}
	if len(s.Patches) > 0 {
		b.WriteString(`, "patches": [` + "\n")
		for i, p := range s.Patches {
			b.WriteString("    " + formatPatch(p, str))
			if i < len(s.Patches)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString("  ]")
	}
	b.WriteString("}")
	return b.String()
}
