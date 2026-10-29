package main

import (
	"strings"
	"testing"
)

// o-json's structure trees get ochazuke's zoom: z + draws only the
// node's subtree under a breadcrumb line of the hidden ancestors, z -
// steps out a level, z 0 shows every level; on the edited tree and on
// the original (T) alike.
func TestStructureTreesZoom(t *testing.T) {
	a := open(t, sample, 120, 30)
	a.on(t, ".items[].owner")
	id := a.r.Focus
	press(t, a, "z", "+")
	if a.rs.tree.Zoomed() != id {
		t.Fatalf("z + zoomed %q, want %q", a.rs.tree.Zoomed(), id)
	}
	frame := view(a)
	if crumbs := a.rs.tree.Breadcrumb(); len(crumbs) == 0 || !strings.Contains(frame, crumbs[len(crumbs)-1]) {
		t.Fatalf("no breadcrumb %v:\n%s", crumbs, frame)
	}
	press(t, a, "z", "-")
	if z := a.rs.tree.Zoomed(); z == id || z == "" {
		t.Fatalf("z - zoom %q", z)
	}
	press(t, a, "z", "0")
	if a.rs.tree.Zoomed() != "" {
		t.Fatal("z 0 kept a zoom")
	}
	// The original structure (T).
	press(t, a, "T")
	selectPath(t, a, ".items[].owner")
	press(t, a, "z", "+")
	if a.tree.Zoomed() == "" || len(a.tree.Breadcrumb()) == 0 {
		t.Fatalf("the original tree did not zoom: %q", a.tree.Zoomed())
	}
	press(t, a, "z", "0")
	if a.tree.Zoomed() != "" {
		t.Fatal("z 0 kept the original's zoom")
	}
}
