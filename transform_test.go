package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func loadDoc(t *testing.T, name string) any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	v, err := ParseDocument(string(raw))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return v
}

func compact(t *testing.T, v any) string {
	t.Helper()
	b, err := FormatJSON(v, false)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func paths(ss ...string) []Path {
	out := make([]Path, len(ss))
	for i, s := range ss {
		out[i] = MustPath(s)
	}
	return out
}

func TestParseDocument(t *testing.T) {
	v, err := ParseDocument(`{"id": 3, "name": "g", "id": 33}`)
	if err != nil || compact(t, v) != `{"id":33,"name":"g"}` {
		t.Fatalf("duplicate keys: %v %v", compact(t, v), err)
	}
	v, err = ParseDocument("{\"a\":1}\n{\"a\":2}\n")
	if err != nil || compact(t, v) != `[{"a":1},{"a":2}]` {
		t.Fatalf("jsonl: %v %v", v, err)
	}
	if _, err := ParseJSON("{\"a\":1}\n{\"a\":2}"); err == nil {
		t.Fatal("ParseJSON ignored a second value")
	}
	var pe *ParseError
	if _, err := ParseDocument("{\"a\":1}\n  x"); !errors.As(err, &pe) || pe.Line != 2 {
		t.Fatalf("trailing token: %v", err)
	}
	if _, err := ParseDocument("{\"foo\": "); !errors.As(err, &pe) || pe.Line != 1 || pe.Col != 9 {
		t.Fatalf("incomplete: %#v", err)
	}
	if _, err := ParseDocument(" \n "); err != ErrEmpty {
		t.Fatalf("empty: %v", err)
	}
	pretty, _ := FormatJSON(MustParse(`{"a":[],"b":{},"c":[1,{"d":"\u007f"}]}`), true)
	want := "{\n  \"a\": [],\n  \"b\": {},\n  \"c\": [\n    1,\n    {\n      \"d\": \"\\u007f\"\n    }\n  ]\n}"
	if string(pretty) != want {
		t.Fatalf("pretty:\n%s", pretty)
	}
}

func MustParse(s string) any {
	v, err := ParseJSON(s)
	if err != nil {
		panic(err)
	}
	return v
}

func TestPaths(t *testing.T) {
	for in, want := range map[string]string{
		"$": "$", ".": "$", "orders[].id": "orders[].id", "$[].orders[].id": "$[].orders[].id",
		".a.b": "a.b", `meta["a b"].c`: `meta["a b"].c`, `$["x.y"][2]`: `$["x.y"][2]`, `$.items[0]`: "items[0]",
	} {
		p, err := ParsePath(in)
		if err != nil || p.String() != want {
			t.Errorf("ParsePath(%q) = %v %v, want %s", in, p, err, want)
		}
		if q, _ := ParsePath(want); q.String() != want {
			t.Errorf("round trip %s -> %s", want, q)
		}
	}
	for _, bad := range []string{"a[", "a[x]", "a..b", `a["b"`} {
		if _, err := ParsePath(bad); err == nil {
			t.Errorf("ParsePath(%q) accepted", bad)
		}
	}
	doc := MustParse(`{"items":[{"id":1},{"id":2,"owner":{"name":"bo"}}]}`)
	if v, ok := AtPath(doc, MustPath("items[].owner.name")); !ok || v != "bo" {
		t.Fatalf("AtPath wildcard: %v %v", v, ok)
	}
	out, err := ReplaceAt(doc, MustPath("items[].owner.name"), "cy")
	if err != nil || compact(t, out) != `{"items":[{"id":1},{"id":2,"owner":{"name":"cy"}}]}` {
		t.Fatalf("ReplaceAt: %s %v", compact(t, out), err)
	}
	if compact(t, doc) != `{"items":[{"id":1},{"id":2,"owner":{"name":"bo"}}]}` {
		t.Fatal("ReplaceAt modified its input")
	}
	if _, err := ReplaceAt(doc, MustPath("nope.x"), 1); err == nil {
		t.Fatal("ReplaceAt invented an intermediate key")
	}
}

func TestInferSchema(t *testing.T) {
	s := InferSchema(loadDoc(t, "sample.json"))
	email := s.Find(MustPath("items[].owner.email"))
	if email == nil || email.Count != 2 || email.Total != 4 || email.Type != "mixed" || email.Types["null"] != 1 {
		t.Fatalf("email: %+v", email)
	}
	tags := s.Find(MustPath("items[].tags[]"))
	if tags.Count != 4 || tags.Total != 4 || tags.Type != "string" {
		t.Fatalf("tags[]: %+v", tags)
	}
	r := InferSchema(loadDoc(t, "restructure.json"))
	if owner := r.Find(MustPath("items[].owner")); owner.Type != "mixed" || len(owner.Children) != 2 {
		t.Fatalf("owner: %+v", owner)
	}
	if size := r.Find(MustPath("items[].size")); size.Type != "mixed" || size.Count != 3 || size.Total != 4 {
		t.Fatalf("size: %+v", size)
	}
	if empty := InferSchema(MustParse(`{"t":[]}`)).Find(MustPath("t[]")); empty.Type != "empty" || empty.Total != 1 {
		t.Fatalf("empty array: %+v", empty)
	}
}

func rel(t *testing.T, r Relation, err error) string {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(r.Columns, ",") + " " + compact(t, r.Value())
}

func TestFlatten(t *testing.T) {
	walk := loadDoc(t, "walk.json")
	f := paths("$[].id", "$[].orders[].sku", "$[].orders[].lines[].qty")
	r, err := Flatten(walk, f, nil, false, false)
	if got := rel(t, r, err); got != `id,orders.sku,orders.lines.qty [{"id":1,"orders.sku":"a1","orders.lines.qty":2},{"id":2,"orders.sku":"b2","orders.lines.qty":1},{"id":2,"orders.sku":"b3","orders.lines.qty":null}]` {
		t.Fatalf("left join: %s", got)
	}
	r, err = Flatten(walk, f, nil, false, true)
	if len(r.Rows) != 2 || err != nil {
		t.Fatalf("inner join: %v %v", r.Rows, err)
	}
	// orders[].lines[] is deeper than tags[], so it is the grain and the
	// unrelated tags column is omitted (owner rule: deeper wins, a tie asks).
	withTags := append(f, MustPath("$[].tags[]"))
	r, err = Flatten(walk, withTags, nil, false, false)
	if got := rel(t, r, err); got != `id,orders.sku,orders.lines.qty [{"id":1,"orders.sku":"a1","orders.lines.qty":2},{"id":2,"orders.sku":"b2","orders.lines.qty":1},{"id":2,"orders.sku":"b3","orders.lines.qty":null}]` {
		t.Fatalf("deeper grain wins: %s", got)
	}
	if got := SiblingArrays(walk, withTags); got != nil {
		t.Fatalf("SiblingArrays at different depths: %v", got)
	}
	var ge *GrainError
	if _, err = Flatten(walk, paths("$[].orders[].sku", "$[].tags[]"), nil, false, false); !errors.As(err, &ge) || len(ge.Candidates) != 2 || ge.Candidates[0].String() != "$[].orders[]" || ge.Candidates[1].String() != "$[].tags[]" {
		t.Fatalf("tie: %v", err)
	}
	if got := SiblingArrays(walk, f); got != nil {
		t.Fatalf("SiblingArrays without ambiguity: %v", got)
	}
	r, err = Flatten(walk, withTags, MustPath("$[].tags[]"), false, false)
	if got := rel(t, r, err); got != `id,tags [{"id":1,"tags":"eu"},{"id":1,"tags":"beta"},{"id":2,"tags":null}]` {
		t.Fatalf("chosen grain omits the other branch: %s", got)
	}
	r, err = Flatten(walk, withTags, nil, true, false)
	if got := rel(t, r, err); len(r.Rows) != 4 || !strings.HasPrefix(got, `id,orders.sku,orders.lines.qty,tags [{"id":1,"orders.sku":"a1","orders.lines.qty":2,"tags":"eu"},`) {
		t.Fatalf("cross join: %s", got)
	}
	r, err = Flatten(MustParse(`{"a.b":1,"a":{"b":2}}`), nil, nil, false, false)
	if got := rel(t, r, err); got != `$["a.b"],a.b [{"$[\"a.b\"]":1,"a.b":2}]` {
		t.Fatalf("collision: %s", got)
	}
	r, err = Flatten(MustParse(`[]`), paths("$[].id"), nil, false, false)
	if len(r.Rows) != 0 || err != nil {
		t.Fatalf("empty root array keeps no padded row: %v %v", r.Rows, err)
	}
	r, err = Flatten(loadDoc(t, "sample.json"), nil, nil, false, false)
	if err != nil || len(r.Rows) != 5 || r.Columns[0] != "name" || r.Rows[2][4].Value != "bob" {
		t.Fatalf("whole document: %v %v", rel(t, r, err), err)
	}
}

func TestTableAndExport(t *testing.T) {
	r := TableRelation(MustParse(`[{"id":1,"n":"a,b"},{"id":2,"x":[1]},"s"]`))
	var b bytes.Buffer
	if err := ExportCSV(&b, r); err != nil || b.String() != "id,n,x,value\n1,\"a,b\",,\n2,,[1],\n,,,s\n" {
		t.Fatalf("csv: %q %v", b.String(), err)
	}
	b.Reset()
	if err := ExportTSV(&b, TableRelation(MustParse(`{"A1":{"total":50},"B2":{"total":7}}`))); err != nil || b.String() != "key\ttotal\nA1\t50\nB2\t7\n" {
		t.Fatalf("tsv: %q %v", b.String(), err)
	}
	if got := TableRelation(MustParse(`{"a":1,"b":[2]}`)); strings.Join(got.Columns, ",") != "key,value" || len(got.Rows) != 2 {
		t.Fatalf("object table: %+v", got)
	}
}

func TestResolveStep(t *testing.T) {
	doc := loadDoc(t, "reshape.json")
	scope := []Operation{{Kind: "scope", Path: MustPath("people")}}
	op, err := ResolveStep(doc, scope, Operation{Kind: "flatten", Paths: paths("$[]")})
	if err != nil || len(op.Paths) != 5 || op.Paths[0].String() != "$[].id" || op.Paths[4].String() != "$[].tags[]" {
		t.Fatalf("scoped flatten: %v %v", op.Paths, err)
	}
	v, err := ApplyPipeline(doc, append(scope, op))
	if err != nil || !strings.Contains(compact(t, v), `"people":[{"id":"p1","name":"Ann","address.city":"Oslo","address.zip":"0150","tags":"a"},`) {
		t.Fatalf("scoped flatten result: %s %v", compact(t, v), err)
	}
	_, err = ApplyPipeline(doc, []Operation{{Kind: "filter", Mode: "regex", Query: "(a"}})
	if err == nil || !strings.Contains(err.Error(), `invalid regex "(a"`) {
		t.Fatalf("regex error: %v", err)
	}
}

// TestJQParity runs each pipeline through the engine and through real jq
// with the synthesized program; output (or failure) must agree.
func TestJQParity(t *testing.T) {
	jqBin, err := exec.LookPath("jq")
	if err != nil {
		t.Skip("jq not installed")
	}
	sc := func(p string) Operation { return Operation{Kind: "scope", Path: MustPath(p)} }
	resolve := func(file string, op Operation) Operation {
		r, err := ResolveOperation(loadDoc(t, file), op)
		if err != nil {
			t.Fatalf("resolve %s: %v", file, err)
		}
		return r
	}
	walkF := paths("$[].id", "$[].orders[].sku", "$[].orders[].lines[].qty", "$[].tags[]")
	items := sc("items")
	cases := []struct {
		name, file string
		ops        []Operation
		fails      bool
	}{
		{"project", "sample.json", []Operation{{Kind: "project", Paths: paths("name", "items[].owner.name", "items[].tags", "meta[\"source\"]")}}, false},
		{"project index and mixed", "restructure.json", []Operation{{Kind: "project", Paths: paths("items[].owner.name", "items[1]", "version")}}, false},
		{"flatten left", "walk.json", []Operation{{Kind: "flatten", Paths: walkF[:3]}}, false},
		{"flatten inner", "walk.json", []Operation{{Kind: "flatten", Paths: walkF[:3], Inner: true}}, false},
		{"flatten grain", "walk.json", []Operation{{Kind: "flatten", Paths: walkF, Grain: MustPath("$[].tags[]")}}, false},
		{"flatten deeper grain", "walk.json", []Operation{{Kind: "flatten", Paths: paths("$[].name"), Grain: MustPath("$[].orders[].lines[]")}}, false},
		{"flatten cross", "walk.json", []Operation{{Kind: "flatten", Paths: walkF, Cross: true}}, false},
		{"flatten cross inner", "walk.json", []Operation{{Kind: "flatten", Paths: walkF, Cross: true, Inner: true}}, false},
		{"flatten deeper grain wins", "walk.json", []Operation{{Kind: "flatten", Paths: walkF}}, false},
		{"flatten tie", "walk.json", []Operation{{Kind: "flatten", Paths: paths("$[].orders[].sku", "$[].tags[]")}}, true},
		{"flatten whole", "sample.json", []Operation{resolve("sample.json", Operation{Kind: "flatten"})}, false},
		{"flatten nested grain", "nested.json", []Operation{resolve("nested.json", Operation{Kind: "flatten", Grain: MustPath("$[].orders[].items[]")})}, false},
		{"flatten empties", "reshape.json", []Operation{resolve("reshape.json", Operation{Kind: "flatten"})}, false},
		{"flatten odd shapes", "restructure.json", []Operation{{Kind: "flatten", Paths: paths("items[].id", "items[].tags[].t", "items[].owner.name")}}, false},
		{"flatten then group", "walk.json", []Operation{{Kind: "flatten", Paths: walkF[:3]}, {Kind: "group", Field: "id"}}, false},
		{"flatten then group object", "walk.json", []Operation{{Kind: "flatten", Paths: walkF[:3]}, {Kind: "group", Field: "orders.sku", Mode: "object", Name: "lines"}}, false},
		{"array-object scoped", "tutorial-reshape.json", []Operation{sc("records"), {Kind: "array-object", Field: "id"}}, false},
		{"array-object round trip", "tutorial-reshape.json", []Operation{sc("records"), {Kind: "array-object", Field: "id"}, {Kind: "object-array", Name: "code"}}, false},
		{"array-object duplicate", "reshape.json", []Operation{sc("people"), {Kind: "array-object", Field: "id"}}, true},
		{"array-object last duplicate wins in parse", "duplicate-keys.json", []Operation{items, {Kind: "array-object", Field: "id"}}, false},
		{"array-object nested key", "sample.json", []Operation{items, {Kind: "array-object", Field: "owner.name"}}, false},
		{"object-array mixed", "nest.json", []Operation{{Kind: "object-array"}}, false},
		{"object-array collision", "nest.json", []Operation{{Kind: "object-array", Name: "b"}}, true},
		{"object-array on array", "walk.json", []Operation{{Kind: "object-array"}}, true},
		{"sort desc", "sample.json", []Operation{items, {Kind: "sort", Field: "name", Desc: true}}, false},
		{"sort missing stable", "sample.json", []Operation{items, {Kind: "sort", Field: "archived"}}, false},
		{"sort values in each", "sample.json", []Operation{sc("items[].tags"), {Kind: "sort", Desc: true}}, false},
		{"group missing column", "reshape.json", []Operation{sc("people"), {Kind: "group", Field: "id", Name: "members"}}, false},
		{"filter substring global", "sample.json", []Operation{items, {Kind: "filter", Query: "AN"}}, false},
		{"filter fuzzy", "sample.json", []Operation{items, {Kind: "filter", Mode: "fuzzy", Query: "gma"}}, false},
		{"filter regex", "sample.json", []Operation{items, {Kind: "filter", Mode: "regex", Field: "name", Query: "^(be|de)"}}, false},
		{"filter eq number", "sample.json", []Operation{items, {Kind: "filter", Mode: "eq", Field: "id", Query: "2.0"}}, false},
		{"filter ne", "sample.json", []Operation{items, {Kind: "filter", Mode: "ne", Field: "note", Query: "null"}}, false},
		{"filter eq global", "sample.json", []Operation{items, {Kind: "filter", Mode: "eq", Query: "true"}}, false},
		{"filter ne global", "sample.json", []Operation{items, {Kind: "filter", Mode: "ne", Query: "red"}}, false},
		{"filter contains nested cell", "sample.json", []Operation{items, {Kind: "filter", Mode: "contains", Field: "owner", Query: "example"}}, false},
		{"filter gt", "sample.json", []Operation{items, {Kind: "filter", Mode: "gt", Field: "id", Query: "2"}}, false},
		{"filter lt mixed", "restructure.json", []Operation{items, {Kind: "filter", Mode: "lt", Field: "size", Query: "5"}}, false},
		{"filter object", "sample.json", []Operation{{Kind: "filter", Query: "a"}}, true},
		{"rename and drop", "sample.json", []Operation{items, {Kind: "rename", Field: "name", Name: "title"}, {Kind: "drop", Field: "tags"}}, false},
		{"rename collision", "sample.json", []Operation{items, {Kind: "rename", Field: "name", Name: "id"}}, true},
		{"scope select wildcard", "sample.json", []Operation{{Kind: "scope", Mode: "select", Path: MustPath("items[].owner")}, {Kind: "filter", Mode: "eq", Field: "name", Query: "bob"}}, false},
		{"scope select then flatten", "tutorial-selection.json", []Operation{{Kind: "scope", Mode: "select", Path: MustPath("customers")}, {Kind: "flatten", Paths: paths("$[].name.first", "$[].contact.city")}}, false},
		{"scope reset", "sample.json", []Operation{items, {Kind: "filter", Query: "ann"}, sc("$"), {Kind: "project", Paths: paths("items[].id", "version")}}, false},
		{"scope missing key creates", "sample.json", []Operation{sc("extra"), {Kind: "sort"}}, true},
		{"scope wildcard over object values", "nest.json", []Operation{sc("x[]"), {Kind: "filter", Query: "x"}}, true},
		{"bad op", "sample.json", []Operation{{Kind: "nope"}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			goOut, goErr := ApplyPipeline(loadDoc(t, c.file), c.ops)
			prog := SynthesizeJQ(c.ops)
			cmd := exec.Command(jqBin, "-c", prog, filepath.Join("testdata", c.file))
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			jqOut, jqErr := cmd.Output()
			if (goErr != nil) != c.fails || (jqErr != nil) != c.fails {
				t.Fatalf("failure mismatch (want fail %v)\ngo: %v\njq: %v %s\nprog: %s", c.fails, goErr, jqErr, stderr.String(), prog)
			}
			if c.fails {
				return
			}
			if got, want := compact(t, goOut), strings.TrimSpace(string(jqOut)); got != want {
				t.Fatalf("output mismatch\ngo: %s\njq: %s\nprog: %s", got, want, prog)
			}
		})
	}
}

func TestSynthesizeReadable(t *testing.T) {
	if got := SynthesizeJQ(nil); got != "." {
		t.Fatalf("empty pipeline: %s", got)
	}
	got := SynthesizeJQ([]Operation{{Kind: "scope", Path: MustPath("items")}, {Kind: "sort", Field: "name"}})
	if !strings.HasSuffix(got, `.items |= (need("array"; "sort") | sort_by(col("name")))`) || strings.Contains(got, "def get") {
		t.Fatalf("sort: %s", got)
	}
}
