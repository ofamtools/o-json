package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONCStripAndParse(t *testing.T) {
	raw := `{
  // line comment with "quotes"
  "url": "https://example.com//keep/*not-a-comment*/",
  /* multi-line
     block comment */
  "items": [
    1,
    2, // trailing comma in array
  ],
  "ok": true, // trailing comma in object
}`
	v, isJSONL, err := ParseFormat(raw, "jsonc")
	if err != nil || isJSONL {
		t.Fatalf("ParseFormat(jsonc) err=%v jsonl=%v", err, isJSONL)
	}
	got := compact(t, v)
	want := `{"url":"https://example.com//keep/*not-a-comment*/","items":[1,2],"ok":true}`
	if got != want {
		t.Fatalf("jsonc got:\n%s\nwant:\n%s", got, want)
	}
}

func TestCSVAndTSVRoundTrip(t *testing.T) {
	csvIn := "id,name,city\n1,\"Ada, Lovelace\",London\n2,Bob,Oslo\n"
	v, _, err := ParseFormat(csvIn, "csv")
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	wantJSON := `[{"id":"1","name":"Ada, Lovelace","city":"London"},{"id":"2","name":"Bob","city":"Oslo"}]`
	if got := compact(t, v); got != wantJSON {
		t.Fatalf("csv -> json = %s, want %s", got, wantJSON)
	}
	outCSV, err := EncodeFormat(v, "csv", true)
	if err != nil || string(outCSV) != csvIn {
		t.Fatalf("csv round-trip = %q, err=%v", string(outCSV), err)
	}

	tsvIn := "id\tname\n1\tAda\n2\tBob\n"
	tv, _, err := ParseFormat(tsvIn, "tsv")
	if err != nil {
		t.Fatalf("parse tsv: %v", err)
	}
	outTSV, err := EncodeFormat(tv, "tsv", true)
	if err != nil || string(outTSV) != tsvIn {
		t.Fatalf("tsv round-trip = %q, err=%v", string(outTSV), err)
	}
}

func TestYAMLRoundTrip(t *testing.T) {
	src := MustParse(`{
  "service": "o-json",
  "enabled": true,
  "retries": 3,
  "empty": null,
  "ratio": 1.5,
  "note": "hello: world",
  "tags": ["alpha", "true", "42"],
  "nested": {
    "owner": "Ada",
    "items": [
      {"sku": "A1", "qty": 2},
      {"sku": "B2", "qty": 5}
    ]
  }
}`)
	y, err := EncodeFormat(src, "yaml", true)
	if err != nil {
		t.Fatalf("EncodeYAML: %v", err)
	}
	parsed, _, err := ParseFormat(string(y), "yaml")
	if err != nil {
		t.Fatalf("ParseYAML: %v\nyaml:\n%s", err, string(y))
	}
	if got, want := compact(t, parsed), compact(t, src); got != want {
		t.Fatalf("yaml round-trip mismatch:\ngot:  %s\nwant: %s\nyaml:\n%s", got, want, string(y))
	}

	// Block scalars, comments, and flow collections.
	yamlDoc := `# header comment
---
name: demo
inline: [1, "two", {ok: true}]
body: |-
  line one
  line two
`
	v, _, err := ParseFormat(yamlDoc, "yaml")
	if err != nil {
		t.Fatalf("ParseYAML block/flow: %v", err)
	}
	want := `{"name":"demo","inline":[1,"two",{"ok":true}],"body":"line one\nline two"}`
	if got := compact(t, v); got != want {
		t.Fatalf("yaml block/flow = %s, want %s", got, want)
	}
}

func TestTOMLRoundTrip(t *testing.T) {
	src := MustParse(`{
  "title": "Demo",
  "count": 42,
  "active": true,
  "tags": ["a", "b"],
  "server": {
    "host": "127.0.0.1",
    "port": 8080
  },
  "items": [
    {"name": "first", "qty": 1},
    {"name": "second", "qty": 2}
  ]
}`)
	tomlBytes, err := EncodeFormat(src, "toml", true)
	if err != nil {
		t.Fatalf("EncodeTOML: %v", err)
	}
	parsed, _, err := ParseFormat(string(tomlBytes), "toml")
	if err != nil {
		t.Fatalf("ParseTOML: %v\ntoml:\n%s", err, string(tomlBytes))
	}
	if got, want := compact(t, parsed), compact(t, src); got != want {
		t.Fatalf("toml round-trip mismatch:\ngot:  %s\nwant: %s\ntoml:\n%s", got, want, string(tomlBytes))
	}
}

func TestMarkdownTableAndConvertCLI(t *testing.T) {
	in := `[{"id":1,"name":"Ada|Lovelace"},{"id":2,"name":"Bob"}]`
	var out, errb bytes.Buffer
	code := run([]string{"convert", "-", "--from", "json", "--to", "md"}, strings.NewReader(in), &out, &errb)
	if code != 0 || errb.Len() > 0 {
		t.Fatalf("convert json->md code=%d err=%q", code, errb.String())
	}
	want := "| id | name |\n| --- | --- |\n| 1 | Ada\\|Lovelace |\n| 2 | Bob |\n"
	if out.String() != want {
		t.Fatalf("md table =\n%q\nwant:\n%q", out.String(), want)
	}

	// JSONC -> YAML -> JSON via CLI.
	out.Reset()
	errb.Reset()
	code = run([]string{"convert", "-", "--from", "jsonc", "--to", "yaml"}, strings.NewReader(`{/*c*/ "a": 1, "b": "hi",}`), &out, &errb)
	if code != 0 {
		t.Fatalf("convert jsonc->yaml code=%d err=%q", code, errb.String())
	}
	yamlOut := out.String()
	out.Reset()
	code = run([]string{"convert", "-", "--from", "yaml", "--to", "json", "--compact"}, strings.NewReader(yamlOut), &out, &errb)
	if code != 0 || strings.TrimSpace(out.String()) != `{"a":1,"b":"hi"}` {
		t.Fatalf("convert yaml->json code=%d out=%q err=%q", code, out.String(), errb.String())
	}

	// JSONL round-trip via convert CLI.
	out.Reset()
	code = run([]string{"convert", "-", "--from", "jsonl", "--to", "jsonl"}, strings.NewReader("{\"x\":1}\n{\"x\":2}\n"), &out, &errb)
	if code != 0 || out.String() != "{\"x\":1}\n{\"x\":2}\n" {
		t.Fatalf("convert jsonl->jsonl code=%d out=%q err=%q", code, out.String(), errb.String())
	}

	// Explicit --format on Buffer preserves auto JSON/JSONL default when omitted.
	bYAML, err := NewBufferWithFormat("cfg.yaml", "a: 1\nb: true\n", "yaml", false)
	if err != nil || compact(t, bYAML.Value) != `{"a":1,"b":true}` {
		t.Fatalf("NewBufferWithFormat yaml: err=%v val=%v", err, bYAML.Value)
	}
	// Save As format picker exposes yaml, toml, md.
	dir := t.TempDir()
	for _, ext := range []string{"yaml", "toml", "md"} {
		outPath := filepath.Join(dir, "out."+ext)
		if err := bYAML.Save(outPath, ext); err != nil {
			t.Fatalf("Save %s: %v", ext, err)
		}
		if data, err := os.ReadFile(outPath); err != nil || len(data) == 0 {
			t.Fatalf("ReadFile %s: err=%v len=%d", outPath, err, len(data))
		}
	}
}
