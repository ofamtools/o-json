package main

import (
	"io"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/rulecheck"
)

// TestRules runs ochazuke's hiddenkeys over o-json's viewer runtime and the
// catalog runtime (the doc and note lists) and the recorded places in
// testdata/rules/places.json: a hidden default row takes no key that did
// something at any place and mode without it. The other rule checks have
// their own tests here (names, refusals, lexicon, nouns).
func TestRules(t *testing.T) {
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
	places, err := rulecheck.LoadPlaces("testdata/rules/places.json")
	if err != nil {
		t.Fatal(err)
	}
	const own = "has its own test in this package"
	rulecheck.Check(t, rulecheck.Config{
		Runtime:  a.r,
		Runtimes: []*ochazuke.Runtime{sh.Runtime},
		Places:   places,
		Root:     ".",
		Off: map[string]string{
			"deadkeys": own, "names": own, "refusals": own, "lexicon": own, "nouns": own,
		},
	})
}
