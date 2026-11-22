package main

import (
	"bytes"
	"flag"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConfigDirEndsInNext(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/x")
	if got := configDir(); got != filepath.Join("/x", "o-json-next") {
		t.Fatalf("xdg: %q", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	if got := configDir(); got != "" && filepath.Base(got) != "o-json-next" {
		t.Fatalf("default: %q", got)
	}
}

func TestSplitPathsKeepsQuotedCommas(t *testing.T) {
	got, err := splitPaths(`["x,y"], orders[].id,a["b]"]`)
	want := []string{`["x,y"]`, `orders[].id`, `a["b]"]`}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := splitPaths(`["x,y`); err == nil {
		t.Fatal("unclosed quote accepted")
	}
}

func TestDashLeadingWordsArePositional(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	compact := fs.Bool("compact", false, "")
	pos, err := parseArgs(fs, []string{"-", "-.a", "--compact", "-1"})
	if err != nil || !*compact || !reflect.DeepEqual(pos, []string{"-", "-.a", "-1"}) {
		t.Fatalf("pos=%q compact=%v err=%v", pos, *compact, err)
	}
	fs = flag.NewFlagSet("t", flag.ContinueOnError)
	fs.Bool("compact", false, "")
	pos, _ = parseArgs(fs, []string{"f", "--", "-compact"})
	if !reflect.DeepEqual(pos, []string{"f", "-compact"}) {
		t.Fatalf("after --: %q", pos)
	}
}

func TestFlattenSelectTable(t *testing.T) {
	in := `[{"x,y":1,"note":"a\tb","orders":[{"id":7}]}]`
	var out, errb bytes.Buffer
	code := subcommand("flatten", []string{"-", "--select", `["x,y"],note,orders[].id`, "--format", "table"}, strings.NewReader(in), &out, &errb)
	if code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	got := out.String()
	if !strings.Contains(got, `a\tb`) || strings.Contains(got, "\t") || !strings.Contains(got, "---") {
		t.Fatalf("table:\n%s", got)
	}
}
