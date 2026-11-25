package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestructurePrimitives(t *testing.T) {
	t.Run("coalesce", func(t *testing.T) {
		doc := MustParse(`[
			{"id":1,"message":{"content":"from msg"}},
			{"id":2,"content":"from content","text":"ignored"},
			{"id":3,"content":"","text":"from text"},
			{"id":4,"content":null}
		]`)
		out, err := ApplyPipeline(doc, []Operation{{
			Kind:   "coalesce",
			Fields: []string{"message.content", "content", "text"},
			Name:   "body",
		}})
		if err != nil {
			t.Fatal(err)
		}
		for i, want := range []string{"from msg", "from content", "from text", "null"} {
			v, _ := AtPath(out, Path{pathIdx(i), pathKey("body")})
			if valueText(v) != want {
				t.Fatalf("row %d body = %q, want %q", i, valueText(v), want)
			}
		}
	})

	t.Run("cast", func(t *testing.T) {
		doc := MustParse(`[
			{"n":"42","i":"3.9","b":"yes","s":100},
			{"n":7,"i":8,"b":0,"s":true},
			{"n":null,"i":"","b":"false","s":null}
		]`)
		out, err := ApplyPipeline(doc, []Operation{
			{Kind: "cast", Field: "n", Mode: "number"},
			{Kind: "cast", Field: "i", Mode: "int"},
			{Kind: "cast", Field: "b", Mode: "bool"},
			{Kind: "cast", Field: "s", Mode: "string"},
		})
		if err != nil {
			t.Fatal(err)
		}
		want := `[{"n":42,"i":3,"b":true,"s":"100"},{"n":7,"i":8,"b":false,"s":"true"},{"n":null,"i":null,"b":false,"s":null}]`
		if got := compact(t, out); got != want {
			t.Fatalf("cast got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("dedupe", func(t *testing.T) {
		doc := MustParse(`[{"id":1,"v":"a"},{"id":2,"v":"b"},{"id":1,"v":"dup"},{"id":3,"v":"c"}]`)
		out, err := ApplyPipeline(doc, []Operation{{Kind: "dedupe", Field: "id"}})
		if err != nil {
			t.Fatal(err)
		}
		want := `[{"id":1,"v":"a"},{"id":2,"v":"b"},{"id":3,"v":"c"}]`
		if got := compact(t, out); got != want {
			t.Fatalf("dedupe by field = %s, want %s", got, want)
		}
		fullOut, err := ApplyPipeline(MustParse(`[1,2,1,{"a":1,"b":2},{"b":2,"a":1}]`), []Operation{{Kind: "dedupe"}})
		if err != nil {
			t.Fatal(err)
		}
		if got := compact(t, fullOut); got != `[1,2,{"a":1,"b":2}]` {
			t.Fatalf("dedupe full value = %s", got)
		}
	})

	t.Run("unwind", func(t *testing.T) {
		doc := MustParse(`[
			{"id":1,"tags":["go","tui"]},
			{"id":2,"tags":[]},
			{"id":3}
		]`)
		dropped, err := ApplyPipeline(doc, []Operation{{Kind: "unwind", Field: "tags"}})
		if err != nil {
			t.Fatal(err)
		}
		wantDrop := `[{"id":1,"tags":"go"},{"id":1,"tags":"tui"}]`
		if got := compact(t, dropped); got != wantDrop {
			t.Fatalf("unwind drop empty = %s, want %s", got, wantDrop)
		}
		kept, err := ApplyPipeline(doc, []Operation{{Kind: "unwind", Field: "tags", PreserveEmpty: true}})
		if err != nil {
			t.Fatal(err)
		}
		wantKeep := `[{"id":1,"tags":"go"},{"id":1,"tags":"tui"},{"id":2,"tags":null},{"id":3,"tags":null}]`
		if got := compact(t, kept); got != wantKeep {
			t.Fatalf("unwind preserveEmpty = %s, want %s", got, wantKeep)
		}
	})

	t.Run("redact", func(t *testing.T) {
		doc := MustParse(`{
			"user": "ada",
			"api_key": "secret-123",
			"nested": {"password": "pw", "note": "contact ada@example.com or Bearer abcdef123456"},
			"public": "ok"
		}`)
		out, err := ApplyPipeline(doc, []Operation{{Kind: "redact"}})
		if err != nil {
			t.Fatal(err)
		}
		got := compact(t, out)
		if strings.Contains(got, "secret-123") || strings.Contains(got, `"pw"`) || strings.Contains(got, "ada@example.com") || strings.Contains(got, "abcdef123456") {
			t.Fatalf("redact leaked secret: %s", got)
		}
		if !strings.Contains(got, `"public":"ok"`) || !strings.Contains(got, `"[REDACTED]"`) {
			t.Fatalf("redact unexpected output: %s", got)
		}

		// Custom fields & replacement.
		custom, err := ApplyPipeline(MustParse(`{"user":"ada","ssn":"123"}`), []Operation{{
			Kind:   "redact",
			Fields: []string{"user"},
			Name:   "***",
		}})
		if err != nil || compact(t, custom) != `{"user":"***","ssn":"123"}` {
			t.Fatalf("custom redact = %s, err=%v", compact(t, custom), err)
		}
	})

	t.Run("pivot_and_unpivot", func(t *testing.T) {
		wide := MustParse(`[
			{"region":"us","q1":10,"q2":20},
			{"region":"eu","q1":30,"q2":40}
		]`)
		tall, err := ApplyPipeline(wide, []Operation{{
			Kind:   "unpivot",
			Fields: []string{"q1", "q2"},
			Field:  "quarter",
			Name:   "sales",
		}})
		if err != nil {
			t.Fatal(err)
		}
		wantTall := `[{"region":"us","quarter":"q1","sales":10},{"region":"us","quarter":"q2","sales":20},{"region":"eu","quarter":"q1","sales":30},{"region":"eu","quarter":"q2","sales":40}]`
		if got := compact(t, tall); got != wantTall {
			t.Fatalf("unpivot = %s, want %s", got, wantTall)
		}
		back, err := ApplyPipeline(tall, []Operation{{
			Kind:   "pivot",
			Fields: []string{"region"},
			Field:  "quarter",
			Name:   "sales",
		}})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := compact(t, back), compact(t, wide); got != want {
			t.Fatalf("pivot round-trip = %s, want %s", got, want)
		}
	})
}

func TestRecipesCLIAndTUI(t *testing.T) {
	dir := t.TempDir()
	recipePath := filepath.Join(dir, "clean.recipe.json")
	r := Recipe{
		Name:        "clean-events",
		Description: "normalize, cast, dedupe, redact",
		From:        "jsonl",
		To:          "json",
		Operations: []Operation{
			{Kind: "coalesce", Fields: []string{"msg", "text"}, Name: "body"},
			{Kind: "cast", Field: "count", Mode: "int"},
			{Kind: "dedupe", Field: "id"},
			{Kind: "redact", Fields: []string{"token"}},
		},
	}
	if err := SaveRecipe(recipePath, r); err != nil {
		t.Fatalf("SaveRecipe: %v", err)
	}
	loaded, err := LoadRecipe(recipePath)
	if err != nil || len(loaded.Operations) != 4 {
		t.Fatalf("LoadRecipe: %+v err=%v", loaded, err)
	}

	input := strings.Join([]string{
		`{"id":1,"msg":"hello","count":"5","token":"tok_abc"}`,
		`{"id":1,"msg":"duplicate","count":"9","token":"tok_dup"}`,
		`{"id":2,"text":"world","count":"10","token":"tok_xyz"}`,
	}, "\n")

	var out, errb bytes.Buffer
	code := run([]string{"run", "-", "--recipe", recipePath, "--compact"}, strings.NewReader(input), &out, &errb)
	if code != 0 || errb.Len() > 0 {
		t.Fatalf("o-json run code=%d err=%q", code, errb.String())
	}
	want := `[{"id":1,"msg":"hello","count":5,"token":"[REDACTED]","body":"hello"},{"id":2,"text":"world","count":10,"token":"[REDACTED]","body":"world"}]`
	if got := strings.TrimSpace(out.String()); got != want {
		t.Fatalf("o-json run output:\n%s\nwant:\n%s", got, want)
	}

	// TUI :recipe save PATH and :recipe load PATH round-trip.
	b, _ := NewBuffer("data.json", `[{"id":1,"secret":"x"},{"id":2,"secret":"y"}]`, false)
	a := newApp(Config{Cwd: dir}, b)
	d := newDriver(a.shell(), 120, 32)
	if err := b.Apply(Operation{Kind: "drop", Field: "secret"}); err != nil {
		t.Fatal(err)
	}
	tuiRecipe := filepath.Join(dir, "tui.recipe.json")
	d.keys(":")
	d.typeText("recipe save " + tuiRecipe)
	d.keys("enter")
	saved, err := LoadRecipe(tuiRecipe)
	if err != nil || len(saved.Operations) != 1 || saved.Operations[0].Kind != "drop" {
		t.Fatalf("TUI :recipe save failed: %+v err=%v status=%q", saved, err, a.status)
	}
}
