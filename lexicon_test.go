package main

import (
	"io"
	"strings"
	"testing"

	"ofam.dev/ochazuke"

	"ofam.dev/ochazuke/lexiconcheck"
)

// o-json's registered operations are held to the lexicon's naming rules
// (lexicon plan): one word has one part of speech, nouns are singular, a
// namespace is not a noun or action word, a key has at most three parts.
// testdata/lexicon-words.tsv is a copy of ochazuke's word list.
func TestLexicon(t *testing.T) {
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
	for name, rt := range map[string]*ochazuke.Runtime{"viewer": a.r, "shell": sh.Runtime} {
		t.Run(name, func(t *testing.T) {
			lexiconcheck.Check(t, lexiconcheck.Config{
				Operations: &rt.Operations,
				MainNouns:  map[string]string{"json": "doc"},
				Words:      "testdata/lexicon-words.tsv",
				Baseline:   "testdata/lexiconcheck.txt",
			})
		})
	}
}
