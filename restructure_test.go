package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// plan parses a document and a mapping and plans the mapping on the
// document as jq reads it.
func plan(t *testing.T, doc, mapping string) (*Plan, *Node) {
	t.Helper()
	root, err := Build([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	m, err := ReadMapping([]byte(mapping))
	if err != nil {
		t.Fatal(err)
	}
	src := Dedupe(root)
	p, err := NewPlan(m, Infer(src))
	if err != nil {
		t.Fatal(err)
	}
	return p, src
}

// runJQ runs a program through the jq binary, skipping without one.
func runJQ(t *testing.T, program, doc string) string {
	t.Helper()
	jq, err := exec.LookPath("jq")
	if err != nil {
		t.Skip("jq is not on PATH")
	}
	cmd := exec.Command(jq, "-c", program)
	cmd.Stdin = strings.NewReader(doc)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil || errb.Len() > 0 {
		t.Fatalf("jq failed: %v %s\nprogram %s", err, errb.String(), program)
	}
	return strings.TrimSpace(out.String())
}

// Each case is applied in Go and, where jq is on PATH, through the
// generated program; the two must agree and jq must not fail.
var restructureCases = []struct {
	name, doc, mapping, want string
}{
	{
		name:    "optional fields map only where present",
		doc:     `{"items":[{"id":1,"note":"a","owner":{"name":"ann","email":"e"}},{"id":2},{"id":3,"owner":{"name":"cy"}}]}`,
		mapping: `{"map":[{"from":".items[]","to":".rows[]"},{"from":".items[].id","to":".rows[].id"},{"from":".items[].note","to":".rows[].note"},{"from":".items[].owner.email","to":".rows[].email"}]}`,
		want:    `{"rows":[{"id":1,"note":"a","email":"e"},{"id":2},{"id":3}]}`,
	},
	{
		name:    "a mixed-type field and mixed items",
		doc:     `[{"v":{"x":1}},{"v":"s"},{"v":null},"plain",7,{"w":1}]`,
		mapping: `{"map":[{"from":"[]","to":"[]"},{"from":"[].v.x","to":"[].x"},{"from":"[].v","to":"[].v"}]}`,
		want:    `[{"x":1,"v":{"x":1}},{"v":"s"},{"v":null},{},{},{}]`,
	},
	{
		name:    "a nested array, collected and kept per item",
		doc:     `{"items":[{"tags":[{"t":"a"},{"t":"b"}]},{"tags":[]},{},{"tags":"none"},{"tags":{"t":"obj"}}]}`,
		mapping: `{"map":[{"from":".items[].tags[].t","to":".all[]"},{"from":".items[]","to":".items[]"},{"from":".items[].tags[]","to":".items[].tags[]"}]}`,
		want:    `{"all":["a","b"],"items":[{"tags":[{"t":"a"},{"t":"b"}]},{"tags":[]},{"tags":[]},{"tags":[]},{"tags":[]}]}`,
	},
	{
		name:    "a duplicate key takes the last value, as jq does",
		doc:     `[{"id":1,"id":11},{"id":2},{"name":"x"}]`,
		mapping: `{"map":[{"from":"[]","to":".ids[]"},{"from":"[].id","to":".ids[].id"},{"from":"[].id","to":".flat[]"}]}`,
		want:    `{"ids":[{"id":11},{"id":2},{}],"flat":[11,2]}`,
	},
	{
		name:    "a wrapper, a rename and a drop; copies keep the source's key order",
		doc:     `{"a":{"z":1,"y":2},"b":3,"odd key":4}`,
		mapping: `{"map":[{"to":".wrap"},{"from":".b","to":".wrap.bee"},{"from":".a","to":".wrap.a"},{"from":"[\"odd key\"]","to":".odd"},{"from":".a.z","to":null}]}`,
		want:    `{"wrap":{"bee":3,"a":{"z":1,"y":2}},"odd":4}`,
	},
	{
		name:    "a missing optional parent with a present child elsewhere",
		doc:     `{"rows":[{"m":{"k":{"v":1}}},{"m":{}},{"m":{"k":null}},{}]}`,
		mapping: `{"map":[{"from":".rows[]","to":"[]"},{"from":".rows[].m.k.v","to":"[].v"},{"from":".rows[].m.k","to":"[].k"}]}`,
		want:    `[{"v":1,"k":{"v":1}},{},{"k":null},{}]`,
	},
}

func TestRestructureCasesAgreeWithJQ(t *testing.T) {
	for _, c := range restructureCases {
		t.Run(c.name, func(t *testing.T) {
			p, src := plan(t, c.doc, c.mapping)
			got := Compact(p.Apply(src), 0)
			if got != c.want {
				t.Fatalf("go\n%s\nwant\n%s\njq program %s", got, c.want, p.JQ())
			}
			if out := runJQ(t, p.JQ(), c.doc); out != got {
				t.Errorf("jq\n%s\ngo\n%s\nprogram %s", out, got, p.JQ())
			}
		})
	}
}

// The fixture covers every case at once (optional, mixed, nested,
// duplicate); its document and jq program are goldens.
func TestRestructureFixture(t *testing.T) {
	doc, _ := os.ReadFile("testdata/restructure.json")
	for _, as := range []string{"document", "jq"} {
		var out bytes.Buffer
		args := []string{"restructure", "testdata/restructure.json", "--map", "testdata/restructure.map.json"}
		if as == "jq" {
			args = append(args, "--jq")
		}
		if err := Main(args, nil, &out, &out); err != nil {
			t.Fatal(err)
		}
		golden := map[string]string{"document": "testdata/restructure.out.json", "jq": "testdata/restructure.jq"}[as]
		if os.Getenv("UPDATE_GOLDEN") != "" {
			_ = os.WriteFile(golden, out.Bytes(), 0o644)
		}
		want, _ := os.ReadFile(golden)
		if out.String() != string(want) {
			t.Errorf("%s\n%s\nwant\n%s", as, out.String(), want)
		}
	}
	program, _ := os.ReadFile("testdata/restructure.jq")
	result, _ := os.ReadFile("testdata/restructure.out.json")
	root, _ := Build(result)
	if got := runJQ(t, string(program), string(doc)); got != Compact(root, 0) {
		t.Errorf("jq\n%s\ngo\n%s", got, Compact(root, 0))
	}
}

func TestMappingErrors(t *testing.T) {
	doc := `{"items":[{"id":1,"tags":["a"]}],"n":1}`
	for _, c := range []struct{ mapping, want string }{
		{`{"map":[{"from":".items[].id","to":".ids"}]}`, "names many values for one key"},
		{`{"map":[{"from":".items[]","to":".r[]"},{"from":".n","to":".r[].n"}]}`, "is outside .items[], the item it is in"},
		{`{"map":[{"to":".r[]"}]}`, "an array item needs a from"},
		{`{"map":[{"from":".nope","to":".x"}]}`, "the document has no such path"},
		{`{"map":[{"from":".items[0]","to":".x"}]}`, "no index"},
		{`{"map":[{"from":".n","to":".x"},{"from":".n","to":".x"}]}`, "an earlier entry maps it"},
		{`{"map":[{"from":".n","to":".x.a"},{"from":".items[]","to":".x[]"}]}`, "it is an object, not an array"},
		{`{"map":[{"from":".n"}]}`, `needs "to"`},
		{`{"map":[{"from":".n","to":".x","as":1}]}`, `unknown field "as"`},
		{`{"map":[{"from":".n","to":null}]}`, "maps nothing"},
		{`{"entries":[]}`, "a mapping is"},
	} {
		m, err := ReadMapping([]byte(c.mapping))
		if err == nil {
			root, _ := Build([]byte(doc))
			_, err = NewPlan(m, Infer(Dedupe(root)))
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want %q", c.mapping, err, c.want)
		}
	}
}

// --mapping prints the spec with every shape it leaves out as a drop:
// the shapes under a copied value and the ancestors of a mapped one are
// kept, an explicit drop is not repeated.
func TestCompleteMapping(t *testing.T) {
	p, _ := plan(t, `{"items":[{"id":1,"owner":{"name":"a","email":"e"},"tags":["x"]}],"meta":{"n":1},"v":2}`,
		`{"map":[{"from":".items[]","to":".r[]"},{"from":".items[].owner.name","to":".r[].owner"},{"from":".meta","to":".meta"},{"from":".v","to":null}]}`)
	var got []string
	for _, e := range p.Complete().Map {
		to := "null"
		if e.To != nil {
			to = *e.To
		}
		got = append(got, e.From+">"+to)
	}
	want := ".items[]>.r[] .items[].owner.name>.r[].owner .meta>.meta .v>null .items[].id>null .items[].owner.email>null .items[].tags>null"
	if strings.Join(got, " ") != want {
		t.Errorf("complete\n%s\nwant\n%s", strings.Join(got, " "), want)
	}
}

// restructure writes a new file with --out and never replaces an
// existing one (the source among them) without --overwrite.
func TestRestructureNeverWritesTheSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "doc.json")
	spec := filepath.Join(dir, "map.json")
	data, _ := os.ReadFile("testdata/restructure.json")
	specData, _ := os.ReadFile("testdata/restructure.map.json")
	_ = os.WriteFile(src, data, 0o600)
	_ = os.WriteFile(spec, specData, 0o644)
	run := func(args ...string) (string, error) {
		var out, errb bytes.Buffer
		err := Main(append([]string{"restructure", src, "--map", spec}, args...), nil, &out, &errb)
		return out.String(), err
	}
	printed, err := run()
	if err != nil || !strings.HasPrefix(printed, "{\n") {
		t.Fatalf("stdout %q %v", printed, err)
	}
	dest := filepath.Join(dir, "doc.restructured.json")
	if _, err := run("--out", dest); err != nil {
		t.Fatal(err)
	}
	if written, _ := os.ReadFile(dest); string(written) != printed {
		t.Errorf("written %q", written)
	}
	if _, err := run("--out", dest); err == nil || !strings.Contains(err.Error(), "exists; --overwrite replaces it") {
		t.Errorf("second write: %v", err)
	}
	if _, err := run("--out", src); err == nil {
		t.Error("wrote the source without --overwrite")
	}
	if after, _ := os.ReadFile(src); string(after) != string(data) {
		t.Fatal("the source changed")
	}
	if _, err := run("--overwrite"); err == nil {
		t.Error("--overwrite without --out")
	}
	// Explicitly, the source can be replaced; it keeps its mode.
	if _, err := run("--out", src, "--overwrite", "--jq"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(src)
	info, _ := os.Stat(src)
	if !strings.HasPrefix(string(after), `{"v": .version`) || info.Mode().Perm() != 0o600 {
		t.Errorf("overwritten source %q mode %v", after, info.Mode())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Errorf("%d files in the directory (a temporary file left behind?)", len(entries))
	}
}
