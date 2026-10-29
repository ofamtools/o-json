package main

import (
	"strings"
	"testing"
)

// Each patch case runs the mapping with its patches in Go and through the
// generated jq program (when jq is on PATH); the two must agree, and
// orphans names the patches skipped.
var patchCases = []struct {
	name, doc, patches, want string
	orphans                  []int
}{
	{"set a string", `{"people":[{"name":"ann","email":"a@x"},{"name":"bob"}]}`,
		`[{"op":"set","path":".people[0].email","value":"ann@y"}]`,
		`{"people":[{"name":"ann","email":"ann@y"},{"name":"bob"}]}`, nil},
	{"set a number, a bool, null and an object", `{"a":1,"b":true,"c":"x","d":null}`,
		`[{"op":"set","path":".a","value":42},{"op":"set","path":".b","value":false},{"op":"set","path":".c","value":null},{"op":"set","path":".d","value":{"z":1,"y":[2]}}]`,
		`{"a":42,"b":false,"c":null,"d":{"z":1,"y":[2]}}`, nil},
	{"delete a field and an array item", `{"a":1,"b":[1,2,3]}`,
		`[{"op":"delete","path":".a"},{"op":"delete","path":".b[1]"}]`,
		`{"b":[1,3]}`, nil},
	{"add a field last and after a key", `{"a":1,"b":2}`,
		`[{"op":"add","path":".z","value":"last"},{"op":"add","path":".n","value":0,"after":"a"}]`,
		`{"a":1,"n":0,"b":2,"z":"last"}`, nil},
	{"add an item", `{"xs":[1,3]}`,
		`[{"op":"add","path":".xs[1]","value":2},{"op":"add","path":".xs[3]","value":4}]`,
		`{"xs":[1,2,3,4]}`, nil},
	{"rename one key in one object", `{"people":[{"name":"ann","email":"a"},{"name":"bob","email":"b"}]}`,
		`[{"op":"rename","path":".people[1].email","key":"mail"}]`,
		`{"people":[{"name":"ann","email":"a"},{"name":"bob","mail":"b"}]}`, nil},
	{"odd keys", `{"a b":{"c.d":1}}`,
		`[{"op":"set","path":".[\"a b\"][\"c.d\"]","value":2}]`,
		`{"a b":{"c.d":2}}`, nil},
	{"orphans are skipped", `{"a":1,"xs":[1],"o":{"k":1,"j":2}}`,
		`[{"op":"set","path":".gone","value":1},{"op":"delete","path":".xs[5]"},{"op":"add","path":".a","value":2},{"op":"add","path":".xs[3]","value":9},{"op":"rename","path":".o.k","key":"j"},{"op":"set","path":".a.b","value":1},{"op":"set","path":".xs[0]","value":7}]`,
		`{"a":1,"xs":[7],"o":{"k":1,"j":2}}`, []int{0, 1, 2, 3, 4, 5}},
	{"a later patch sees the earlier ones", `{"a":1}`,
		`[{"op":"rename","path":".a","key":"b"},{"op":"set","path":".b","value":2},{"op":"set","path":".a","value":3}]`,
		`{"b":2}`, []int{2}},
	{"set the document", `{"a":1}`, `[{"op":"set","path":".","value":[1]}]`, `[1]`, nil},
}

func TestPatchesAgreeWithJQ(t *testing.T) {
	for _, c := range patchCases {
		t.Run(c.name, func(t *testing.T) {
			root, err := Build([]byte(c.doc))
			if err != nil {
				t.Fatal(err)
			}
			doc := &Doc{Root: root, Shape: Infer(root)}
			m := identityMapping(t, doc)
			m.Patches = readPatches(t, c.patches)
			p, err := NewPlan(m, doc.Shape)
			if err != nil {
				t.Fatal(err)
			}
			out, orphans := p.ApplyAll(root)
			if got := Compact(out, 0); got != c.want {
				t.Fatalf("go %s\nwant %s", got, c.want)
			}
			if !equalInts(orphans, c.orphans) {
				t.Fatalf("orphans %v, want %v", orphans, c.orphans)
			}
			if Compact(root, 0) != Compact(mustBuild(t, c.doc), 0) {
				t.Fatal("the source changed")
			}
			if got := runJQ(t, p.JQ(), c.doc); got != c.want {
				t.Fatalf("jq %s\nwant %s\nprogram %s", got, c.want, p.JQ())
			}
		})
	}
}

// A patch is checked: a [] or a negative index, an op it does not know,
// a missing value or key is refused with the patch's number.
func TestPatchesAreChecked(t *testing.T) {
	root := mustBuild(t, `{"a":[1]}`)
	doc := &Doc{Root: root, Shape: Infer(root)}
	for patch, want := range map[string]string{
		`{"op":"set","path":".a[]","value":1}`:              "has no []",
		`{"op":"set","path":".a[-1]","value":1}`:            "from the start",
		`{"op":"move","path":".a"}`:                         "set, delete, add or rename",
		`{"op":"set","path":".a"}`:                          `"value" is a JSON value`,
		`{"op":"rename","path":".a"}`:                       `"key" is the new one`,
		`{"op":"delete","path":"."}`:                        "not the document",
		`{"op":"add","path":".a[0]","value":1,"after":"x"}`: `"after" names a key`,
	} {
		m := identityMapping(t, doc)
		m.Patches = readPatches(t, "["+patch+"]")
		if _, err := NewPlan(m, doc.Shape); err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "patch 1") {
			t.Errorf("%s: %v, want %q", patch, err, want)
		}
	}
	if _, err := ReadMapping([]byte(`{"map":[],"patches":[{"op":"set","path":".a","value":1,"nope":2}]}`)); err == nil {
		t.Error("an unknown patch field was read")
	}
}

// --map with patches: the document, --mapping (which keeps them, one per
// line, and reads back the same) and --jq agree with the Go apply.
func TestRestructureMapWithPatches(t *testing.T) {
	spec := `{"map": [{"from": ".items[]", "to": ".rows[]"}, {"from": ".items[].id", "to": ".rows[].id"}, {"from": ".items[].name", "to": ".rows[].name"}],
"patches": [{"op": "set", "path": ".rows[1].name", "value": "BETA"}, {"op": "rename", "path": ".rows[0].id", "key": "key"}, {"op": "delete", "path": ".rows[3]"}, {"op": "set", "path": ".rows[9].id", "value": 0}]}`
	file := writeMap(t, spec)
	run := func(args ...string) string {
		t.Helper()
		var b strings.Builder
		if err := Main(append([]string{"restructure", sample, "--map", file}, args...), nil, &b, &b); err != nil {
			t.Fatalf("restructure %v: %v", args, err)
		}
		return b.String()
	}
	got := run()
	want := `{"rows":[{"key":1,"name":"alpha"},{"id":2,"name":"BETA"},{"id":3,"name":"gamma"}]}`
	if !sameJSON(t, got, want) || !strings.Contains(got, `"key": 1`) {
		t.Fatalf("output %s", got)
	}
	mapping := run("--mapping")
	if !strings.Contains(mapping, "\"patches\": [\n  {\"op\": \"set\", \"path\": \".rows[1].name\", \"value\": \"BETA\"},\n") {
		t.Fatalf("mapping\n%s", mapping)
	}
	again := writeMap(t, mapping)
	var b strings.Builder
	if err := Main([]string{"restructure", sample, "--map", again}, nil, &b, &b); err != nil || b.String() != got {
		t.Fatalf("the --mapping output does not read back: %v\n%s", err, b.String())
	}
	doc, err := ReadDoc(sample, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out := runJQ(t, run("--jq"), Compact(doc.Root, 0)); !sameJSON(t, out, want) {
		t.Fatalf("jq %s", out)
	}
}

func identityMapping(t *testing.T, doc *Doc) *Mapping {
	t.Helper()
	to := "."
	return &Mapping{Map: []MapEntry{{From: ".", To: &to}}}
}

func readPatches(t *testing.T, text string) []Patch {
	t.Helper()
	m, err := ReadMapping([]byte(`{"map": [], "patches": ` + text + `}`))
	if err != nil {
		t.Fatal(err)
	}
	return m.Patches
}

func mustBuild(t *testing.T, text string) *Node {
	t.Helper()
	n, err := Build([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
