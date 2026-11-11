package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// doc query, remap and export answer what the top-level query,
// restructure and export commands print, and write an out file as they do.
func TestDocVerbsAnswerWhatTheCommandsPrint(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "map.json")
	if err := os.WriteFile(spec, []byte(`{"map":[{"from":".items[]","to":".rows[]"},{"from":".items[].name","to":".rows[].n"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var names []string
	if err := json.Unmarshal([]byte(run(t, "", "doc", "query", sample, ".items[] | .name")), &names); err != nil || len(names) == 0 || names[0] != "alpha" {
		t.Fatalf("doc query: %v %v", names, err)
	}
	if err := json.Unmarshal([]byte(run(t, sampleText(t), "doc", "query", "-", ".items[]", "|", ".name")), &names); err != nil || names[0] != "alpha" {
		t.Fatalf("doc query from stdin, the query as the rest of the line: %v %v", names, err)
	}
	// The remapped document is the same JSON, keys in order.
	want := run(t, "", "restructure", sample, "--map", spec)
	var got, wanted bytes.Buffer
	if err := json.Indent(&got, []byte(run(t, "", "doc", "remap", sample, spec)), "", "  "); err != nil {
		t.Fatal(err)
	}
	if err := json.Indent(&wanted, []byte(want), "", "  "); err != nil || got.String() != wanted.String() {
		t.Fatalf("doc remap:\n%s\nrestructure:\n%s (%v)", got.String(), wanted.String(), err)
	}
	var program string
	if err := json.Unmarshal([]byte(run(t, "", "doc", "remap", sample, spec, "--as", "jq")), &program); err != nil || program != run(t, "", "restructure", sample, "--map", spec, "--jq") {
		t.Fatalf("doc remap as jq: %q %v", program, err)
	}
	var text string
	if err := json.Unmarshal([]byte(run(t, "", "doc", "export", sample, "--of", ".items", "--as", "tsv", "--mapping", spec, "--of", ".rows")), &text); err != nil || text != run(t, "", "export", sample, "--map", spec, "--of", ".rows", "--format", "tsv") {
		t.Fatalf("doc export: %q %v", text, err)
	}
	// An out file is written once; a file that is there only with overwrite.
	out := filepath.Join(dir, "rows.csv")
	var wrote struct {
		Out   string `json:"out"`
		Bytes int    `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(run(t, "", "doc", "export", sample, "--of", ".items", "--out", out)), &wrote); err != nil || wrote.Out != out {
		t.Fatalf("doc export --out: %+v %v", wrote, err)
	}
	data, err := os.ReadFile(out)
	if err != nil || len(data) != wrote.Bytes || string(data) != run(t, "", "export", sample, "--of", ".items") {
		t.Fatalf("the out file: %q %v", data, err)
	}
	var stdout, stderr bytes.Buffer
	if err := Main([]string{"doc", "export", sample, "--of", ".items", "--out", out}, strings.NewReader(""), &stdout, &stderr); err == nil && !strings.Contains(stderr.String(), "exists") {
		t.Fatalf("a second write replaced the file: %s %s", stdout.String(), stderr.String())
	}
	run(t, "", "doc", "export", sample, "--of", ".items", "--as", "md", "--out", out, "--overwrite")
	if data, _ := os.ReadFile(out); !strings.HasPrefix(string(data), "| id |") {
		t.Fatalf("overwrite: %q", data)
	}
}
