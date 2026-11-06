package main

import (
	"io"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke/namecheck"
	"ofam.dev/ochazuke/nouncheck"
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

// TestVerbReferences holds every tracked Go, JSON and Markdown file to the
// verbs the catalog has: a verb the framework purged (rename, move,
// restore, duplicate, patch; the renamed command verbs) fails here until
// the last reference to it is gone. A dated CHANGELOG section is history.
func TestVerbReferences(t *testing.T) {
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
	namecheck.VerbsCheck(t, namecheck.VerbsConfig{
		Catalog: mergeCatalogs(a.r.VerbCatalog(), sh.Runtime.VerbCatalog()),
		Removed: a.r.RemovedVerbs(),
		Root:    ".",
	})
}

func mergeCatalogs(cs ...namecheck.Catalog) namecheck.Catalog {
	out := namecheck.Catalog{Nouns: map[string]string{}, Known: map[string]string{}}
	for _, c := range cs {
		if out.Generated == nil {
			out.Generated = c.Generated
		}
		for k, v := range c.Nouns {
			out.Nouns[k] = v
		}
		for k, v := range c.Known {
			if _, ok := out.Known[k]; !ok || v == "" {
				out.Known[k] = v
			}
		}
	}
	return out
}

// TestNouns holds every thing noun to the core verbs and a key prop, and
// every command noun off the core verbs' names (Runtime.ValidateNouns).
func TestNouns(t *testing.T) {
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
	rows := append(nouncheck.Rows(a.r), nouncheck.Rows(sh.Runtime)...)
	nouncheck.CheckRows(t, rows, nouncheck.Config{Baseline: "testdata/nouncheck.txt"})
}
