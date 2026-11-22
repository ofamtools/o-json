package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/ochazuke/widgets/pathpicker"
)

// TestSmoke replays the scripted session through the real keymap, engine,
// shell and renderer; every requested behavior has a check there. The given
// fixture holds the relational oracle: 20 records flatten (left join) to 40
// rows at the orders[].items[] grain, without the shallower tags[].
func TestSmoke(t *testing.T) {
	var out strings.Builder
	if err := runSmoke(&out, "testdata/nested.json"); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	s := out.String()
	if !strings.Contains(s, "ok   F flattens the given file to 40 rows") {
		t.Fatalf("nested.json is not 40 rows:\n%s", s)
	}
	header := s[strings.Index(s, "with header ")+len("with header "):]
	header = header[:strings.IndexByte(header, '\n')]
	if !strings.Contains(header, "orders.items.sku") || strings.Contains(header, "tags") {
		t.Fatalf("csv header %q", header)
	}
	if !strings.Contains(s, "csv   row 1: 1,Ada A,") {
		t.Fatalf("first csv row:\n%s", s)
	}
}

// TestReadOnlyNeverWrites drives every save and edit path a read-only file
// offers and checks the file and its folder stay as they were.
func TestReadOnlyNeverWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.json")
	raw := `{"a":[{"b":1},{"b":2}]}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := NewBuffer(path, raw, false)
	if err != nil {
		t.Fatal(err)
	}
	a := newApp(Config{FS: pathpicker.OSFS{}, Cwd: dir}, b)
	d := newDriver(a.shell(), 120, 30)
	d.keys("4", "tab", "i")
	d.typeText("zz")
	d.keys("esc", "esc", "ctrl+s")
	if !strings.Contains(a.status, "READ-ONLY") {
		t.Errorf("ctrl+s status %q", a.status)
	}
	d.keys(":")
	d.typeText("w " + path)
	d.keys("enter", "esc", "tab", "g", "g", "F", "ctrl+s", "esc", "esc")
	got, _ := os.ReadFile(path)
	ents, _ := os.ReadDir(dir)
	if string(got) != raw || b.RawText != raw || b.Dirty || len(ents) != 1 {
		t.Fatalf("read-only wrote: file %q raw %q dirty %v entries %d (status %q)", got, b.RawText, b.Dirty, len(ents), a.status)
	}
}

// TestScanJSON maps every value of a text, JSONL included, to its path and
// byte range, so pretty, KV and raw views agree on the focus.
func TestScanJSON(t *testing.T) {
	src := `{"a": [1, {"b c": true}], "d": "x"}`
	got := map[string]string{}
	for _, sp := range scanJSON(src) {
		got[sp.path.String()] = src[sp.start:sp.end]
	}
	for path, want := range map[string]string{"$": src, "a[1]": `{"b c": true}`, "d": `"x"`} {
		if got[path] != want {
			t.Errorf("%s = %q, want %q (all: %v)", path, got[path], want, got)
		}
	}
	lines := scanJSON("{\"k\": 1}\n{\"k\": 2}\n")
	if n := len(lines); n != 4 || lines[3].path.String() != "$[1].k" {
		t.Errorf("JSONL spans = %d, last %v", n, lines[n-1].path)
	}
}

func TestParseStatus(t *testing.T) {
	for raw, want := range map[string]string{
		`{"foo": `:            "incomplete JSON: line 1, col 9",
		"{\n  \"a\": 1,\n  x": "invalid JSON: line 3, col 3",
	} {
		if got := parseStatus(raw, nil); got != want {
			t.Errorf("parseStatus(%q) = %q, want %q", raw, got, want)
		}
	}
}
