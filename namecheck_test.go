package main

import (
	"io"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke/namecheck"
)

// TestNoNewNameBranches holds the non-test Go to namecheck: no new
// comparison, case label or lookup table keyed by a noun, verb, mode,
// motion or parameter name the runtime declares, and no function grows
// or passes complexity 25. The accepted uses are testdata/namecheck.txt,
// which only shrinks.
func TestNoNewNameBranches(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir+"/config")
	t.Setenv("XDG_STATE_HOME", dir+"/state")
	t.Setenv("XDG_DATA_HOME", dir+"/data")
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	sh := newShell(strings.NewReader(""), io.Discard, io.Discard)
	namecheck.Check(t, namecheck.Config{
		Vocab:    mergeVocab(a.r.NameVocab(), sh.Runtime.NameVocab()),
		Baseline: "testdata/namecheck.txt",
		Dirs:     []string{"."},
	})
}

func mergeVocab(vs ...map[string][]string) map[string][]string {
	out := map[string][]string{}
	for _, v := range vs {
		for name, cats := range v {
			for _, c := range cats {
				if !slices.Contains(out[name], c) {
					out[name] = append(out[name], c)
				}
			}
		}
	}
	return out
}
