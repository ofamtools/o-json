package main

import (
	"fmt"
	"strings"
)

// jqDefs are the helpers a synthesized program may use, in output order.
// Each mirrors one rule of the Go engine so both agree on every input.
var jqDefs = []struct{ name, def string }{
	{"get", `def get(f): first(f?, null);`},
	{"col", `def col($k): first(.[$k]?, null);`},
	{"each", `def each: if type == "array" then .[] else empty end;`},
	{"left", `def left(f; $pad): [f] | if length == 0 then $pad else .[] end;`},
	{"obj", `def obj(f): if type == "object" then f else null end;`},
	{"arr", `def arr(f): if type == "array" then f else null end;`},
	{"need", `def need($t; $op): if type == $t then . else error($op + " needs an " + $t + ", got " + type) end;`},
	{"rows", `def rows(f): if type == "array" then map(if type == "object" then f else . end) elif type == "object" then f else . end;`},
	{"cells", `def cells: .. | select(type == "string" or type == "number" or type == "boolean");`},
	{"fuzzy", `def fuzzy($q): ($q | explode) as $c | (reduce explode[] as $x (0; if . < ($c | length) and $c[.] == $x then . + 1 else . end)) == ($c | length);`},
	{"keyby", `def keyby(f): need("array"; "array-object") | reduce .[] as $e ({}; ($e | get(f)) as $k | if ($k | type | IN("string", "number", "boolean") | not) then error("array-object: an element has no usable key") elif has($k | tostring) then error("array-object: duplicate key " + ($k | tostring)) else .[$k | tostring] = $e end);`},
	{"entries", `def entries($n): need("object"; "object-array") | to_entries | map(if (.value | type) == "object" then (if .value | has($n) then error("object-array: key column " + $n + " collides with a field") else {($n): .key} + .value end) elif $n == "value" then error("object-array: key column value collides with the value") else {($n): .key, value: .value} end);`},
}

type jqGen struct {
	used map[string]bool
	vars int
}

func (g *jqGen) use(names ...string) {
	for _, n := range names {
		g.used[n] = true
	}
	if g.used["keyby"] {
		g.used["get"], g.used["need"] = true, true
	}
	if g.used["entries"] {
		g.used["need"] = true
	}
}

func jqStr(s string) string { return quoteKey(s) }

// jqKey is an object key: bare when it is an identifier.
func jqKey(k string) string {
	if isIdent(k) {
		return k
	}
	return jqStr(k)
}

// jqPath is the jq path expression of p; wildcards are "[]".
func jqPath(p Path) string {
	var b strings.Builder
	for _, s := range p {
		if !s.Array && !s.Indexed && isIdent(s.Key) {
			b.WriteString("." + s.Key)
			continue
		}
		if b.Len() == 0 {
			b.WriteByte('.')
		}
		switch {
		case s.Array:
			b.WriteString("[]")
		case s.Indexed:
			fmt.Fprintf(&b, "[%d]", s.Index)
		default:
			b.WriteString("[" + jqStr(s.Key) + "]")
		}
	}
	if b.Len() == 0 {
		return "."
	}
	return b.String()
}

// SynthesizeJQ writes a jq program equivalent to ApplyPipeline(ops). A step
// that cannot run becomes a jq error with the same message.
func SynthesizeJQ(ops []Operation) string {
	g := &jqGen{used: map[string]bool{}}
	var stages, scoped []string
	var scope Path
	flush := func() {
		if len(scoped) > 0 {
			stages = append(stages, jqPath(scope)+" |= ("+strings.Join(scoped, " | ")+")")
			scoped = nil
		}
	}
	for i, raw := range ops {
		op, err := normalize(raw)
		var st string
		switch {
		case err != nil:
			st = "error(" + jqStr(fmt.Sprintf("step %d (%s): %v", i+1, raw.Kind, err)) + ")"
		case op.Kind == "scope":
			flush()
			scope = op.Path
			if op.Mode == "select" {
				stages = append(stages, g.selectExpr(op.Path))
				scope = nil
			}
			continue
		default:
			st = g.op(op)
		}
		if len(scope) > 0 {
			scoped = append(scoped, st)
		} else {
			stages = append(stages, st)
		}
	}
	flush()
	if len(stages) == 0 {
		return "."
	}
	var defs []string
	for _, d := range jqDefs {
		if g.used[d.name] {
			defs = append(defs, d.def)
		}
	}
	return strings.Join(append(defs, strings.Join(stages, " | ")), " ")
}

func (g *jqGen) selectExpr(p Path) string {
	if !hasWild(p) {
		if len(p) == 0 {
			return "."
		}
		g.use("get")
		return "get(" + jqPath(p) + ")"
	}
	var parts []string
	var keys Path
	for _, s := range p {
		if !s.Array {
			keys = append(keys, s)
			continue
		}
		if len(keys) > 0 {
			parts, keys = append(parts, "get("+jqPath(keys)+")"), nil
		}
		parts = append(parts, "each")
	}
	if len(keys) > 0 {
		parts = append(parts, "get("+jqPath(keys)+")")
	}
	g.use("get", "each")
	return "[" + strings.Join(parts, " | ") + "]"
}

func (g *jqGen) op(op Operation) string {
	col := func(k string) string { g.use("col"); return "col(" + jqStr(k) + ")" }
	switch op.Kind {
	case "project":
		return g.project(buildTree(op.Paths, nil))
	case "flatten":
		root, err := flattenPlan(op)
		if err != nil {
			return "error(" + jqStr(err.Error()) + ")"
		}
		return "[" + g.rows(root, false, op.Inner) + "]"
	case "array-object":
		g.use("keyby")
		return "keyby(" + jqPath(MustPath(op.Field)) + ")"
	case "object-array":
		g.use("entries")
		return "entries(" + jqStr(op.Name) + ")"
	case "wrap-object":
		return "{" + jqKey(op.Name) + ": .}"
	case "wrap-array":
		return "[.]"
	case "object-values":
		g.use("need")
		return `need("object"; "object-values") | [.[]]`
	case "object-keys":
		g.use("need")
		return `need("object"; "object-keys") | keys_unsorted`
	case "group":
		g.use("need")
		by := "need(\"array\"; \"group\") | group_by(" + col(op.Field) + ")"
		if op.Mode == "object" {
			return by + " | reduce .[] as $g ({}; ($g[0] | " + col(op.Field) + " | tostring) as $k | if has($k) then error(\"group: two groups share the key \" + $k) else .[$k] = $g end)"
		}
		return by + " | map({" + jqKey(op.Field) + ": (.[0] | " + col(op.Field) + "), " + jqKey(op.Name) + ": .})"
	case "rename":
		g.use("rows")
		o, n := jqStr(op.Field), jqStr(op.Name)
		return "rows(if has(" + o + ") and has(" + n + ") then error(\"rename: column \" + " + n + " + \" already exists\") else with_entries(if .key == " + o + " then .key = " + n + " else . end) end)"
	case "drop":
		g.use("rows")
		return "rows(del(.[" + jqStr(op.Field) + "]))"
	case "sort":
		g.use("need")
		s := "need(\"array\"; \"sort\") | sort"
		if op.Field != "" {
			s += "_by(" + col(op.Field) + ")"
		}
		if op.Desc {
			s += " | reverse"
		}
		return s
	case "filter":
		g.use("need")
		return "need(\"array\"; \"filter\") | map(select(" + g.predicate(op) + "))"
	}
	return "."
}

func (g *jqGen) predicate(op Operation) string {
	q := jqStr(op.Query)
	target := "."
	if op.Field != "" {
		g.use("col")
		target = "col(" + jqStr(op.Field) + ")"
	}
	var test string
	switch op.Mode {
	case "substring":
		test = "tostring | ascii_downcase | contains(" + jqStr(asciiLower(op.Query)) + ")"
	case "fuzzy":
		g.use("fuzzy")
		test = "tostring | ascii_downcase | fuzzy(" + jqStr(asciiLower(op.Query)) + ")"
	case "regex":
		test = "tostring | test(" + q + ")"
	case "contains":
		test = "tostring | contains(" + q + ")"
	case "gt", "lt":
		cmp := map[string]string{"gt": ">", "lt": "<"}[op.Mode]
		test = "type == \"number\" and . " + cmp + " " + strings.TrimSpace(op.Query)
	case "eq", "ne":
		test = "tostring == " + q
		if _, ok := jsonNumber(op.Query); ok {
			test = "(" + test + " or (type == \"number\" and . == " + strings.TrimSpace(op.Query) + "))"
		}
	}
	not := ""
	if op.Mode == "ne" {
		not = " | not"
	}
	if op.Field != "" && (op.Mode == "eq" || op.Mode == "ne") {
		return "(" + target + " | " + test + ")" + not
	}
	g.use("cells")
	src := "cells"
	if target != "." {
		src = target + " | cells"
	}
	return "any(" + src + "; " + test + ")" + not
}

// project mirrors project(): terminals keep their value, objects keep the
// selected keys, arrays map their elements, anything else becomes null.
func (g *jqGen) project(n *selNode) string {
	if n.term {
		return "."
	}
	var fields, idx []string
	wild := ""
	for _, k := range n.kids {
		switch {
		case k.part.Array:
			wild = "map(" + g.project(k) + ")"
		case k.part.Indexed:
			idx = append(idx, fmt.Sprintf("(.[%d:%d][] | %s)", k.part.Index, k.part.Index+1, g.project(k)))
		case k.term && isIdent(k.part.Key):
			fields = append(fields, k.part.Key)
		default:
			sub := jqPath(Path{k.part})
			if !k.term {
				sub = "(" + sub + " | " + g.project(k) + ")"
			}
			fields = append(fields, jqKey(k.part.Key)+": "+sub)
		}
	}
	if wild == "" && len(idx) > 0 {
		wild = "[" + strings.Join(idx, ", ") + "]"
	}
	obj := "{" + strings.Join(fields, ", ") + "}"
	switch {
	case len(fields) > 0 && wild != "":
		return "if type == \"object\" then " + obj + " elif type == \"array\" then " + wild + " else null end"
	case len(fields) > 0:
		g.use("obj")
		return "obj(" + obj + ")"
	case wild != "":
		g.use("arr")
		return "arr(" + wild + ")"
	}
	return "null"
}

// rows mirrors (*selNode).rows: the row stream of n evaluated at n's value.
// Parts with arrays are bound to variables, first part outermost, and
// joined in column order. Nested is false only at the root.
func (g *jqGen) rows(n *selNode, nested, inner bool) string {
	var binds, segs, lit []string
	flush := func() {
		if len(lit) > 0 {
			segs, lit = append(segs, "{"+strings.Join(lit, ", ")+"}"), nil
		}
	}
	if n.term {
		lit = append(lit, jqKey(n.col)+": .")
	}
	for _, k := range n.kids {
		if !k.hasArray() {
			g.use("get")
			k.terms(Path{k.part}, func(t *selNode, rel Path) {
				lit = append(lit, jqKey(t.col)+": get("+jqPath(rel)+")")
			})
			continue
		}
		flush()
		if len(n.kids) == 1 && !n.term {
			return g.stream(k, nested, inner)
		}
		name := fmt.Sprintf("$r%d", g.vars)
		g.vars++
		binds = append(binds, g.stream(k, nested, inner)+" as "+name)
		segs = append(segs, name)
	}
	flush()
	if len(segs) == 0 {
		segs = []string{"{}"}
	}
	return strings.Join(append(binds, strings.Join(segs, " + ")), " | ")
}

// stream is the row stream of kid k, evaluated at its parent's value. An
// array below the root pads with a null row when it has none (left join).
func (g *jqGen) stream(k *selNode, nested, inner bool) string {
	if k.part.Array {
		g.use("each")
		body := "each | " + g.rows(k, true, inner)
		if nested && !inner {
			g.use("left")
			pad, _ := FormatJSON(k.nullRow(), false)
			return "left(" + body + "; " + string(pad) + ")"
		}
		return "(" + body + ")"
	}
	path := Path{k.part}
	for !k.term && len(k.kids) == 1 && !k.kids[0].part.Array {
		k = k.kids[0]
		path = append(path, k.part)
	}
	g.use("get")
	return "(get(" + jqPath(path) + ") | " + g.rows(k, true, inner) + ")"
}
