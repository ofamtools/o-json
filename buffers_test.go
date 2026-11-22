package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBufferRawUndoKeepsLastValid(t *testing.T) {
	b := NewScratch()
	if !b.Editable || b.Name != "[No Name]" || b.Dirty {
		t.Fatalf("scratch = %+v", b)
	}
	b.SetRaw(`{"a": 1}`)
	b.BreakUndo()
	b.SetRaw(`{"a": 1, "b": `)
	b.SetRaw(`{"a": 1, "b": [`)
	if b.ParseErr == nil || b.Structure == nil || !strings.HasPrefix(b.Status(), "incomplete JSON: line 1, col ") {
		t.Fatalf("half-typed: err=%v structure=%v status=%q", b.ParseErr, b.Structure, b.Status())
	}
	if err := b.Save(filepath.Join(t.TempDir(), "x.json"), ""); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("save of incomplete text: %v", err)
	}
	if !b.UndoStep() || b.RawText != `{"a": 1}` || b.ParseErr != nil {
		t.Fatalf("one undo should drop the whole typing run, got %q", b.RawText)
	}
	if !b.RedoStep() || b.RawText != `{"a": 1, "b": [` {
		t.Fatalf("redo = %q", b.RawText)
	}
}

func TestBufferPipelineUndo(t *testing.T) {
	b, err := NewBuffer("", `[{"a":1,"b":2},{"a":3,"b":4}]`, false)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := ParsePath("$[].a")
	if err := b.Apply(Operation{Kind: "project", Paths: []Path{p}}); err != nil {
		t.Fatal(err)
	}
	v, err := b.Preview()
	got, _ := FormatJSON(v, false)
	if err != nil || string(got) != `[{"a":1},{"a":3}]` {
		t.Fatalf("preview = %s, %v", got, err)
	}
	if !b.UndoStep() || len(b.Pipeline) != 0 || b.UndoStep() {
		t.Fatalf("undo should remove the one step, pipeline = %v", b.Pipeline)
	}
	if !b.RedoStep() || len(b.Pipeline) != 1 {
		t.Fatal("redo should restore the step")
	}
}

func TestSaveGuardsAndAdoption(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "orig.json")
	os.WriteFile(orig, []byte(`{"a":1}`), 0o600)
	link := filepath.Join(dir, "link.json")
	os.Symlink(orig, link)
	b, err := OpenBuffer(orig, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"", orig, link, filepath.Join(dir, ".", "orig.json")} {
		if err := b.Save(p, "pretty"); !errors.Is(err, ErrReadOnly) {
			t.Fatalf("read-only save to %q: %v", p, err)
		}
	}
	copyPath := filepath.Join(dir, "copy.json")
	if err := b.Save(copyPath, ""); err != nil || b.Path != orig {
		t.Fatalf("read-only save elsewhere: %v, path %q", err, b.Path)
	}
	b.Editable = true
	b.SetRaw(`{"a":  2}`)
	if !b.Dirty {
		t.Fatal("edit should dirty the buffer")
	}
	if err := b.Save(link, ""); err != nil || b.Dirty {
		t.Fatalf("editable save: %v dirty=%v", err, b.Dirty)
	}
	data, _ := os.ReadFile(orig)
	st, _ := os.Stat(orig)
	if string(data) != "{\"a\":  2}\n" || st.Mode().Perm() != 0o600 {
		t.Fatalf("saved %q mode %v", data, st.Mode())
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("save replaced the symlink instead of writing through it")
	}
	if NewScratch().Save("", "") != ErrNoPath {
		t.Fatal("scratch save needs a path")
	}
}

func TestSaveFormats(t *testing.T) {
	dir := t.TempDir()
	b, _ := NewBuffer("", "{\"id\":1}\n{\"id\":2}\n", true)
	if !b.JSONL {
		t.Fatal("two lines of values are JSONL")
	}
	for name, want := range map[string]string{
		"a.jsonl": "{\"id\":1}\n{\"id\":2}\n",
		"a.csv":   "id\n1\n2\n",
	} {
		if err := b.Save(filepath.Join(dir, name), ""); err != nil {
			t.Fatal(name, err)
		}
		if data, _ := os.ReadFile(filepath.Join(dir, name)); string(data) != want {
			t.Fatalf("%s = %q", name, data)
		}
	}
	if got := strings.Join(b.FormatOffers(), ","); got != "pretty,minified,jsonl,csv,tsv" {
		t.Fatalf("offers = %s", got)
	}
}

func TestBufferRing(t *testing.T) {
	var r BufferRing
	a, b, c := NewScratch(), NewScratch(), NewScratch()
	r.Add(a)
	r.Add(b)
	r.Add(c)
	if r.Current() != c || a.ID == b.ID {
		t.Fatal("Add should switch to the new buffer with a new ID")
	}
	r.Alternate()
	if r.Current() != b {
		t.Fatal("alternate should return to b")
	}
	r.Cycle(2)
	if r.Current() != a || !r.Switch(c.ID) || r.Switch(c.ID) || r.Switch(99) {
		t.Fatal("cycle wraps, switch refuses the current and unknown IDs")
	}
}

func TestJQ(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not on PATH")
	}
	ctx := context.Background()
	res, err := RunJQResults(ctx, `{"a":[1,2]}`, ".a[]")
	if err != nil || len(res) != 2 {
		t.Fatalf("several results: %v %v", res, err)
	}
	if res, err := RunJQResults(ctx, `{}`, "empty"); err != nil || len(res) != 0 {
		t.Fatalf("no output: %v %v", res, err)
	}
	if v, err := RunJQ(ctx, `{}`, ".a"); err != nil || v != nil {
		t.Fatalf("null result: %v %v", v, err)
	}
	if _, err := RunJQResults(ctx, `{}`, ".a |"); err == nil || strings.Contains(err.Error(), "^") || !strings.HasPrefix(err.Error(), "jq: ") {
		t.Fatalf("compile error = %v", err)
	}
	cctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := RunJQResults(cctx, `0`, "last(range(1e15))"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled run = %v", err)
	}
	e := JQEffect("query", `[1]`, ".[0]")
	v, err := e.Run(ctx)
	if r := v.(JQResult); err != nil || e.Slot != "query" || e.Command != JQResultCommand || r.Expression != ".[0]" || len(r.Results) != 1 {
		t.Fatalf("effect = %+v %v", v, err)
	}
}

func TestCLI(t *testing.T) {
	cases := []struct {
		args       []string
		stdin      string
		code       int
		out, inErr string
	}{
		{[]string{"flatten", "-", "--field", "orders[].id"}, `{"orders":[{"id":1},{"id":2}]}`, 0, "orders.id\n1\n2\n", ""},
		{[]string{"flatten", "-", "--field", "a[].x", "--field", "b[].y"}, `{"a":[{"x":1}],"b":[{"y":2}]}`, 2, "", "--grain"},
		{[]string{"structure", "--format", "json", "-"}, `{"a":1}`, 0, `"type": "object"`, ""},
		{[]string{"structure", "-", "extra"}, `{}`, 2, "", "expected 1"},
		{[]string{"query", "-", ".[]", "--compact"}, "[1,2]", 0, "1\n2\n", ""},
	}
	for _, c := range cases {
		if c.args[0] == "query" {
			if _, err := exec.LookPath("jq"); err != nil {
				continue
			}
		}
		var out, errb bytes.Buffer
		code := run(c.args, strings.NewReader(c.stdin), &out, &errb)
		if code != c.code || !strings.Contains(out.String(), c.out) || !strings.Contains(errb.String(), c.inErr) || c.code == 0 && errb.Len() > 0 {
			t.Errorf("%v: code %d out %q err %q", c.args, code, out.String(), errb.String())
		}
	}
}
