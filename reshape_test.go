package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docPlan plans a mapping with reshape steps on a document.
func docPlan(t *testing.T, doc, mapping string) (*Plan, *Node) {
	t.Helper()
	root, err := Build([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	m, err := ReadMapping([]byte(mapping))
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewDocPlan(m, root, Infer(root))
	if err != nil {
		t.Fatal(err)
	}
	return p, root
}

// runGoJQ runs a program through gojq on a document: its one output as
// compact JSON (gojq writes an object's keys sorted).
func runGoJQ(t *testing.T, program string, root *Node) string {
	t.Helper()
	code, err := compileJQ(program)
	if err != nil {
		t.Fatalf("gojq: %v\nprogram %s", err, program)
	}
	iter := code.Run(jqValue(root))
	var outs []string
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := v.(error); ok {
			t.Fatalf("gojq failed: %v\nprogram %s", err, program)
		}
		n, err := nodeOf(v)
		if err != nil {
			t.Fatal(err)
		}
		outs = append(outs, Compact(n, 0))
	}
	if len(outs) != 1 {
		t.Fatalf("gojq gave %d outputs: %v\nprogram %s", len(outs), outs, program)
	}
	return outs[0]
}

const reshapeDoc = `{"title":"t","items":[` +
	`{"id":"a","name":"ann","address":{"city":"Oslo","zip":"0150"},"tags":["x","y"],"orders":[{"sku":"s1","qty":1},{"sku":"s2","qty":2}]},` +
	`{"id":"b","name":"bo","address":{"city":"Rome"},"tags":[],"orders":[]},` +
	`{"id":"a","name":"cy","tags":["z"],"orders":[{"sku":"s3"}]},` +
	`{"name":"dee","address":"none"},` +
	`"stray"]}`

var reshapeCases = []struct {
	name, doc, mapping, want string
}{
	{
		name:    "lift: an array's items become numbered keys at its place",
		doc:     `{"a":1,"t":["x",{"b":2}],"z":3}`,
		mapping: `{"map":[{"from":".","to":"."}],"then":[{"op":"lift","path":".t","prefix":"k"}]}`,
		want:    `{"a":1,"k-1":"x","k-2":{"b":2},"z":3}`,
	},
	{
		name:    "lift under items: a key there that an item takes goes, the lifted value wins; not an array stays",
		doc:     `{"items":[{"t":[1,2],"k-2":"old"},{"t":"s"},{"u":1},[]]}`,
		mapping: `{"map":[{"from":".","to":"."}],"then":[{"op":"lift","path":".items[].t","prefix":"k"}]}`,
		want:    `{"items":[{"k-1":1,"k-2":2},{"t":"s"},{"u":1},[]]}`,
	},
	{
		name:    "lift: the prefix defaults to new-key; an empty array leaves nothing",
		doc:     `{"t":["x"],"e":[],"q":"\"odd"}`,
		mapping: `{"map":[{"from":".","to":"."}],"then":[{"op":"lift","path":".t"},{"op":"lift","path":".e"}]}`,
		want:    `{"new-key-1":"x","q":"\"odd"}`,
	},
	{
		name:    "flatten: dotted fields become leaf names where unique",
		doc:     reshapeDoc,
		mapping: `{"map":[{"from":".","to":"."}],"then":[{"op":"flatten","paths":[".items[].name",".items[].address.city"],"arrays":"join"}]}`,
		want:    `[{"name":"ann","city":"Oslo"},{"name":"bo","city":"Rome"},{"name":"cy"},{"name":"dee"},{}]`,
	},
	{
		name:    "flatten names path keeps the dotted names",
		doc:     reshapeDoc,
		mapping: `{"map":[{"from":".","to":"."}],"then":[{"op":"flatten","paths":[".items[].name",".items[].address.city"],"names":"path"}]}`,
		want:    `[{"name":"ann","address.city":"Oslo"},{"name":"bo","address.city":"Rome"},{"name":"cy"},{"name":"dee"},{}]`,
	},
	{
		name:    "flatten a nested array, join",
		doc:     reshapeDoc,
		mapping: `{"map":[{"from":".","to":"."}],"then":[{"op":"flatten","paths":[".items[].name",".items[].tags",".items[].orders[].sku"],"arrays":"join","join":"|"}]}`,
		want:    `[{"name":"ann","tags":"x|y","sku":"s1|s2"},{"name":"bo","tags":""},{"name":"cy","tags":"z","sku":"s3"},{"name":"dee"},{}]`,
	},
	{
		name:    "flatten a nested array, json",
		doc:     reshapeDoc,
		mapping: `{"map":[{"from":".","to":"."}],"then":[{"op":"flatten","paths":[".items[].name",".items[].tags"],"arrays":"json"}]}`,
		want:    `[{"name":"ann","tags":"[\"x\",\"y\"]"},{"name":"bo","tags":"[]"},{"name":"cy","tags":"[\"z\"]"},{"name":"dee"},{}]`,
	},
	{
		name:    "flatten a nested array, rows (orders' fields stay together)",
		doc:     reshapeDoc,
		mapping: `{"map":[{"from":".","to":"."}],"then":[{"op":"flatten","paths":[".items[].id",".items[].orders[].sku",".items[].orders[].qty"],"arrays":"rows"}]}`,
		want:    `[{"id":"a","sku":"s1","qty":1},{"id":"a","sku":"s2","qty":2},{"id":"b"},{"id":"a","sku":"s3"},{},{}]`,
	},
	{
		name:    "flatten rows over two arrays gives every combination",
		doc:     `{"items":[{"n":1,"a":["x","y"],"b":[true,false]}]}`,
		mapping: `{"map":[{"from":".","to":"."}],"then":[{"op":"flatten","paths":[".items[].n",".items[].a",".items[].b"],"arrays":"rows"}]}`,
		want:    `[{"n":1,"a":"x","b":true},{"n":1,"a":"x","b":false},{"n":1,"a":"y","b":true},{"n":1,"a":"y","b":false}]`,
	},
	{
		name:    "flatten without a common array makes one row",
		doc:     reshapeDoc,
		mapping: `{"map":[{"from":".","to":"."}],"then":[{"op":"flatten","paths":[".title",".items[].name"],"arrays":"join"}]}`,
		want:    `[{"title":"t","name":"ann;bo;cy;dee"}]`,
	},
	{
		name:    "flatten a marked array reads its items",
		doc:     `{"rows":[{"a":{"b":1},"c":2},{"a":{"b":3}}]}`,
		mapping: `{"map":[{"from":".","to":"."}],"then":[{"op":"flatten","paths":[".rows"]}]}`,
		want:    `[{"b":1,"c":2},{"b":3}]`,
	},
	{
		name:    "a repeated leaf keeps both dotted names",
		doc:     `{"rows":[{"a":{"id":1},"b":{"id":2},"n":"x"}]}`,
		mapping: `{"map":[{"from":".","to":"."}],"then":[{"op":"flatten","paths":[".rows[].a.id",".rows[].b.id",".rows[].n"]}]}`,
		want:    `[{"a.id":1,"b.id":2,"n":"x"}]`,
	},
	{
		name:    "key-by rename keeps every item",
		doc:     reshapeDoc,
		mapping: `{"map":[{"from":".","to":"."}],"then":[{"op":"key-by","path":".items","field":"id"}]}`,
		want: `{"title":"t","items":{"a":{"id":"a","name":"ann","address":{"city":"Oslo","zip":"0150"},"tags":["x","y"],"orders":[{"sku":"s1","qty":1},{"sku":"s2","qty":2}]},` +
			`"b":{"id":"b","name":"bo","address":{"city":"Rome"},"tags":[],"orders":[]},` +
			`"a-2":{"id":"a","name":"cy","tags":["z"],"orders":[{"sku":"s3"}]},` +
			`"_missing":[{"name":"dee","address":"none"},"stray"]}}`,
	},
	{
		name:    "key-by last, drop the field",
		doc:     `[{"id":1,"v":"a"},{"id":2,"v":"b"},{"id":1,"v":"c"},{"v":"d"}]`,
		mapping: `{"map":[{"from":".","to":"."}],"then":[{"op":"key-by","field":"id","duplicates":"last","drop":true}]}`,
		want:    `{"1":{"v":"c"},"2":{"v":"b"},"_missing":[{"v":"d"}]}`,
	},
	{
		name:    "key-by group, a named missing key",
		doc:     `[{"id":true,"v":"a"},{"id":null,"v":"b"},{"id":true,"v":"c"},{"id":{"x":1},"v":"d"}]`,
		mapping: `{"map":[{"from":".","to":"."}],"then":[{"op":"key-by","field":"id","duplicates":"group","missing":"other"}]}`,
		want:    `{"true":[{"id":true,"v":"a"},{"id":true,"v":"c"}],"null":[{"id":null,"v":"b"}],"other":[{"id":{"x":1},"v":"d"}]}`,
	},
	{
		name:    "key-by: a taken missing key and a taken renamed key move on",
		doc:     `[{"k":"a"},{"k":"a"},{"k":"a-2"},{"k":"_missing"},{}]`,
		mapping: `{"map":[{"from":".","to":"."}],"then":[{"op":"key-by","field":"k"}]}`,
		want:    `{"a":{"k":"a"},"a-2":{"k":"a"},"a-2-2":{"k":"a-2"},"_missing":{"k":"_missing"},"_missing-2":[{}]}`,
	},
	{
		name:    "key-by fans out under []",
		doc:     `{"groups":[{"items":[{"id":"x"}]},{"items":"none"},{"other":1}]}`,
		mapping: `{"map":[{"from":".","to":"."}],"then":[{"op":"key-by","path":".groups[].items","field":"id"}]}`,
		want:    `{"groups":[{"items":{"x":{"id":"x"}}},{"items":"none"},{"other":1}]}`,
	},
	{
		name:    "values and entries reverse key-by",
		doc:     `{"by":{"a":{"n":1},"b":{"n":2}},"s":"x"}`,
		mapping: `{"map":[{"from":".","to":"."}],"then":[{"op":"values","path":".by"}]}`,
		want:    `{"by":[{"n":1},{"n":2}],"s":"x"}`,
	},
	{
		name:    "entries",
		doc:     `{"by":{"a":{"n":1},"b":2}}`,
		mapping: `{"map":[{"from":".","to":"."}],"then":[{"op":"entries","path":".by"}]}`,
		want:    `{"by":[{"key":"a","value":{"n":1}},{"key":"b","value":2}]}`,
	},
	{
		name:    "values on a value that is not an object leaves it",
		doc:     `{"by":[1,2]}`,
		mapping: `{"map":[{"from":".","to":"."}],"then":[{"op":"values","path":".by"}]}`,
		want:    `{"by":[1,2]}`,
	},
	{
		name: "a step's map and patches read its output; steps chain",
		doc:  reshapeDoc,
		mapping: `{"map":[{"from":".items[]","to":".[]"},{"from":".items[].id","to":".[].id"},{"from":".items[].name","to":".[].who"}],"patches":[{"op":"set","path":".[1].who","value":"BO"}],` +
			`"then":[{"op":"key-by","field":"id","map":[{"from":".a","to":".first"},{"from":".b.who","to":".second"}],"patches":[{"op":"set","path":".second","value":"bob"}]},` +
			`{"op":"entries"}]}`,
		want: `[{"key":"first","value":{"id":"a","who":"ann"}},{"key":"second","value":"bob"}]`,
	},
}

func TestReshapeCasesAgreeWithJQ(t *testing.T) {
	for _, tc := range reshapeCases {
		t.Run(tc.name, func(t *testing.T) {
			p, root := docPlan(t, tc.doc, tc.mapping)
			got := Compact(p.Apply(root), 0)
			if got != tc.want {
				t.Fatalf("Go:\n got %s\nwant %s", got, tc.want)
			}
			// A second document of the same plan runs the steps again.
			again, _ := Build([]byte(tc.doc))
			if g := Compact(p.Apply(again), 0); g != got {
				t.Fatalf("Apply on a fresh parse: %s", g)
			}
			prog := p.JQ()
			if g := runGoJQ(t, prog, root); !sameJSON(t, g, tc.want) {
				t.Fatalf("gojq:\n got %s\nwant %s\nprogram %s", g, tc.want, prog)
			}
			if g := runJQ(t, prog, tc.doc); g != tc.want {
				t.Fatalf("jq:\n got %s\nwant %s\nprogram %s", g, tc.want, prog)
			}
		})
	}
}

func TestReshapeMappingRoundTrip(t *testing.T) {
	for _, tc := range reshapeCases {
		m, err := ReadMapping([]byte(tc.mapping))
		if err != nil {
			t.Fatal(err)
		}
		text, err := FormatMapping(m)
		if err != nil {
			t.Fatal(err)
		}
		back, err := ReadMapping(text)
		if err != nil {
			t.Fatalf("%s: %v\n%s", tc.name, err, text)
		}
		if mappingKey(back) != mappingKey(m) {
			t.Fatalf("%s: round trip\n%s\n%s", tc.name, mappingKey(m), mappingKey(back))
		}
	}
}

func TestReshapeRefusesBadSteps(t *testing.T) {
	root, _ := Build([]byte(`[{"a":1}]`))
	for mapping, want := range map[string]string{
		`{"map":[{"from":".","to":"."}],"then":[{"op":"spin"}]}`:                                     `op "spin"`,
		`{"map":[{"from":".","to":"."}],"then":[{"op":"flatten"}]}`:                                  `"paths"`,
		`{"map":[{"from":".","to":"."}],"then":[{"op":"key-by"}]}`:                                   `"field"`,
		`{"map":[{"from":".","to":"."}],"then":[{"op":"key-by","field":"a","duplicates":"many"}]}`:   `duplicates "many"`,
		`{"map":[{"from":".","to":"."}],"then":[{"op":"flatten","paths":[".[].a"],"arrays":"csv"}]}`: `arrays "csv"`,
		`{"map":[{"from":".","to":"."}],"then":[{"op":"values","map":[{"from":".nope","to":"."}]}]}`: "no such path",
		`{"map":[{"from":".","to":"."}],"then":[{"op":"lift","path":"."}]}`:                          `"path" names an object's key`,
	} {
		m, err := ReadMapping([]byte(mapping))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := NewDocPlan(m, root, Infer(root)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", mapping, err, want)
		}
	}
	m, _ := ReadMapping([]byte(`{"map":[{"from":".","to":"."}],"then":[{"op":"values"}]}`))
	if _, err := NewPlan(m, Infer(root)); err == nil {
		t.Error("NewPlan planned reshape steps without the document")
	}
}

// o-json restructure --map runs a mapping with steps; --jq prints the
// program, which gojq runs to the same document; --mapping reads back.
func TestRestructureCommandRunsSteps(t *testing.T) {
	file := filepath.Join(t.TempDir(), "doc.json")
	if err := os.WriteFile(file, []byte(reshapeDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range reshapeCases {
		spec := writeMap(t, tc.mapping)
		run := func(args ...string) string {
			var b strings.Builder
			if err := Main(append([]string{"restructure", file, "--map", spec}, args...), nil, &b, &b); err != nil {
				t.Fatalf("%s %v: %v", tc.name, args, err)
			}
			return b.String()
		}
		if tc.doc != reshapeDoc {
			continue
		}
		doc, _ := Build([]byte(run()))
		if got := Compact(doc, 0); got != tc.want {
			t.Errorf("%s: restructure\n got %s\nwant %s", tc.name, got, tc.want)
		}
		root, _ := Build([]byte(reshapeDoc))
		if got := runGoJQ(t, run("--jq"), root); !sameJSON(t, got, tc.want) {
			t.Errorf("%s: --jq through gojq %s", tc.name, got)
		}
		if _, err := ReadMapping([]byte(run("--mapping"))); err != nil {
			t.Errorf("%s: --mapping does not read back: %v", tc.name, err)
		}
	}
}
