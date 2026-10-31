package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
)

func TestTreeGuides(t *testing.T) {
	a := open(t, sample, 120, 24)
	for _, tree := range []*ochazuke.Tree{a.tree, a.rs.orig, a.rs.tree} {
		if !tree.Guides {
			t.Fatal("an o-json tree did not opt into guides")
		}
	}
	if err := a.r.Settings().Set(ochazuke.TreeGuidesSetting, "off"); err != nil {
		t.Fatal(err)
	}
	before := ansi.Strip(a.tree.View(60, 20))
	if strings.ContainsAny(before, "├└│") {
		t.Fatal(before)
	}
	if err := a.r.Settings().Set(ochazuke.TreeGuidesSetting, "app"); err != nil {
		t.Fatal(err)
	}
	after := ansi.Strip(a.tree.View(60, 20))
	if !strings.Contains(after, "├─") || !strings.Contains(after, "└─") {
		t.Fatal(after)
	}
	t.Logf("Structure before:\n%s\nStructure after:\n%s", before, after)
}
