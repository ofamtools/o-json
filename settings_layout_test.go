package main

import (
	"slices"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// Every setting o-json declares is filed in one of settingSections, every
// name the table holds is declared, and the Settings page's o-json tab
// lists those sections, in that order, under its title.
func TestEverySettingHasASection(t *testing.T) {
	a := open(t, sample, 120, 30)
	s := a.r.Settings()
	var own []string
	for _, one := range s.Declared() {
		if s.OwnerOf(one.Name) == ochazuke.OwnerApp {
			own = append(own, one.Name)
		}
	}
	var listed, ids []string
	for _, sec := range settingSections {
		ids = append(ids, sec.ID)
		for _, g := range sec.groups {
			listed = append(listed, g.settings...)
		}
	}
	for _, name := range own {
		if !slices.Contains(listed, name) {
			t.Errorf("%s is in no section of settingSections", name)
		}
	}
	for i, name := range listed {
		if !slices.Contains(own, name) {
			t.Errorf("settingSections names %s, which o-json does not declare", name)
		}
		if slices.Contains(listed[:i], name) {
			t.Errorf("settingSections names %s twice", name)
		}
	}
	sections, err := s.Sections(ochazuke.OwnerApp)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, sec := range sections {
		got = append(got, sec.ID)
	}
	if !slices.Equal(got, ids) {
		t.Errorf("the o-json tab's sections %q, want %q", got, ids)
	}
	if i := slices.IndexFunc(s.Owners(), func(o ochazuke.SettingOwnerInfo) bool { return o.ID == ochazuke.OwnerApp }); i < 0 || s.Owners()[i].Title != "o-json" {
		t.Errorf("owners %+v: want the app tab titled o-json", s.Owners())
	}
	if s.OwnerOf(ochazuke.ModalBackdropHighlightsSetting) != ochazuke.OwnerFramework {
		t.Errorf("modal-highlights moved to %q: o-json only changes its default", s.OwnerOf(ochazuke.ModalBackdropHighlightsSetting))
	}
}
