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

func rqDoc(t *testing.T, name string) any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	v, err := ParseDocument(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func rqVal(t *testing.T, raw string) any {
	t.Helper()
	v, err := ParseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func rqPaths(ss ...string) []Path {
	out := make([]Path, len(ss))
	for i, s := range ss {
		out[i] = MustPath(s)
	}
	return out
}

func rqJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := FormatJSON(v, false)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestFlattenGrainDepth covers the automatic grain by nesting depth, the
// tie that still asks, and the left join padding below the root.
func TestFlattenGrainDepth(t *testing.T) {
	nested := rqDoc(t, "nested.json")
	r, err := Flatten(nested, nil, nil, false, false)
	if err != nil || len(r.Rows) != 40 || strings.Contains(strings.Join(r.Columns, ","), "tags") {
		t.Fatalf("whole input picks orders.items over tags: %v %v %v", len(r.Rows), r.Columns, err)
	}
	if got := SiblingArrays(nested, nil); got != nil {
		t.Fatalf("deeper array is no tie: %v", got)
	}
	r, err = Flatten(nested, rqPaths("id", "orders[].id", "orders[].items[].sku"), nil, false, false)
	if err != nil || len(r.Rows) != 40 || rqJSON(t, r.Rows[0]) != `{"id":1,"orders.id":null,"orders.items.sku":null}` ||
		rqJSON(t, r.Rows[1]) != `{"id":2,"orders.id":"o2-0","orders.items.sku":"whisk"}` ||
		rqJSON(t, r.Rows[2]) != `{"id":2,"orders.id":"o2-0","orders.items.sku":"kettle"}` {
		t.Fatalf("relative oracle: %v %v", r.Rows[:3], err)
	}
	r, err = Flatten(nested, rqPaths("$[].orders[].items[].sku"), nil, false, false)
	if err != nil || len(r.Rows) != 40 || r.Rows[0][0].Value != nil {
		t.Fatalf("deep field only keeps the empty parent: %v %v", len(r.Rows), err)
	}
	r, err = Flatten(rqVal(t, `{"orders":[]}`), rqPaths("orders[].id"), nil, false, false)
	if err != nil || rqJSON(t, r.Value()) != `[{"orders.id":null}]` {
		t.Fatalf("root object keeps one row: %v %v", r.Rows, err)
	}
	walk := rqDoc(t, "walk.json")
	tie := rqPaths("$[].orders[].sku", "$[].tags[]")
	var ge *GrainError
	if _, err = Flatten(walk, tie, nil, false, false); !errors.As(err, &ge) || len(ge.Candidates) != 2 {
		t.Fatalf("tie at the deepest depth asks: %v", err)
	}
	if got := SiblingArrays(walk, rqPaths("orders[].sku", "tags[]")); len(got) != 2 || got[1].String() != "$[].tags[]" {
		t.Fatalf("relative tie: %v", got)
	}
}

// TestFlattenRootRelative covers relative fields and grain on root arrays.
func TestFlattenRootRelative(t *testing.T) {
	walk := rqDoc(t, "walk.json")
	op, err := ResolveOperation(walk, Operation{Kind: "flatten", Paths: rqPaths("id", `["name"]`, "[1].id", "tags[]"), Grain: MustPath("tags[]")})
	var got []string
	for _, p := range op.Paths {
		got = append(got, p.String())
	}
	if err != nil || strings.Join(got, " ") != "$[].id $[].name $[1].id $[].tags[]" || op.Grain.String() != "$[].tags[]" {
		t.Fatalf("resolved: %v %s %v", got, op.Grain, err)
	}
	r, err := Flatten(rqVal(t, `[[{"id":1},{"id":2}],[]]`), rqPaths("id"), nil, false, false)
	if err != nil || rqJSON(t, r.Value()) != `[{"id":1},{"id":2},{"id":null}]` {
		t.Fatalf("array of arrays: %v %v", r.Rows, err)
	}
	op, err = ResolveStep(rqDoc(t, "reshape.json"), []Operation{{Kind: "scope", Path: MustPath("people")}}, Operation{Kind: "flatten", Paths: rqPaths("id", "tags[]")})
	if err != nil || len(op.Paths) != 2 || op.Paths[0].String() != "$[].id" || op.Paths[1].String() != "$[].tags[]" {
		t.Fatalf("scoped relative: %v %v", op.Paths, err)
	}
}

// TestFlattenJoinParity runs resolved flattens through the engine and real jq.
func TestFlattenJoinParity(t *testing.T) {
	jqBin, err := exec.LookPath("jq")
	if err != nil {
		t.Skip("jq not installed")
	}
	nested := rqPaths("id", "orders[].id", "orders[].items[].sku", "tags[]")
	cases := []struct {
		name, input string
		op          Operation
		fails       bool
	}{
		{"auto depth whole", "nested.json", Operation{Kind: "flatten"}, false},
		{"deep only", "nested.json", Operation{Kind: "flatten", Paths: rqPaths("orders[].items[].sku")}, false},
		{"auto depth relative", "nested.json", Operation{Kind: "flatten", Paths: nested}, false},
		{"inner", "nested.json", Operation{Kind: "flatten", Paths: nested, Inner: true}, false},
		{"cross", "nested.json", Operation{Kind: "flatten", Paths: nested, Cross: true}, false},
		{"cross inner", "nested.json", Operation{Kind: "flatten", Paths: nested, Cross: true, Inner: true}, false},
		{"relative grain", "nested.json", Operation{Kind: "flatten", Paths: nested, Grain: MustPath("tags[]")}, false},
		{"root object", `{"orders":null,"x":{"n":1}}`, Operation{Kind: "flatten", Paths: rqPaths("orders[].id", "x.n")}, false},
		{"empty root array", `[]`, Operation{Kind: "flatten", Paths: rqPaths("$[].orders[].id")}, false},
		{"tie", "walk.json", Operation{Kind: "flatten", Paths: rqPaths("$[].orders[].sku", "$[].tags[]")}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var v any
			var stdin []byte
			if strings.HasSuffix(c.input, ".json") {
				v = rqDoc(t, c.input)
				stdin, _ = os.ReadFile(filepath.Join("testdata", c.input))
			} else {
				v, stdin = rqVal(t, c.input), []byte(c.input)
			}
			op, err := ResolveOperation(v, c.op)
			if err != nil {
				if !c.fails {
					t.Fatalf("resolve: %v", err)
				}
				op = c.op
			}
			goOut, goErr := ApplyPipeline(v, []Operation{op})
			prog := SynthesizeJQ([]Operation{op})
			cmd := exec.Command(jqBin, "-c", "--", prog)
			cmd.Stdin = bytes.NewReader(stdin)
			jqOut, jqErr := cmd.Output()
			if (goErr != nil) != c.fails || (jqErr != nil) != c.fails {
				t.Fatalf("failure mismatch (want %v): go %v jq %v\nprog: %s", c.fails, goErr, jqErr, prog)
			}
			if c.fails {
				return
			}
			if got, want := rqJSON(t, goOut), strings.TrimSpace(string(jqOut)); got != want {
				t.Fatalf("output mismatch\ngo: %s\njq: %s\nprog: %s", got, want, prog)
			}
		})
	}
}
