package main

import (
	"path/filepath"
	"testing"

	"github.com/unixsuperhero/ochazuke/story"
)

// o-json's stories (testdata/stories): ochazuke's story engine drives the
// real viewer. A story's page is a placeholder (the story format asks for
// one); the session is o-json's runtime and model on the nest document,
// the focus on the restructured tree's .y, and the story's script and
// expectations run against it as o-story runs its own.
func TestStories(t *testing.T) {
	stories, err := story.LoadDir(filepath.Join("testdata", "stories"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stories) == 0 {
		t.Fatal("no stories")
	}
	for _, s := range stories {
		t.Run(s.Area+"/"+story.Slug(s.Name), func(t *testing.T) {
			a := openNest(t, nestDoc, ".a", ".x", ".y", ".z")
			a.on(t, ".y")
			w, h := s.Size()
			a.m.Width, a.m.Height = w, h
			ss := &story.Session{Story: s, R: a.r, M: a.m}
			d := story.NewDriver(ss, story.Options{Mono: true})
			check := func(n int) error {
				for _, e := range s.Expect {
					if e.After == n || e.After == 0 && n == len(s.Script) {
						for _, bad := range story.Check(ss, e) {
							t.Errorf("after %d: %s\n%s", n, bad, ss.Frame())
						}
					}
				}
				return nil
			}
			if err := d.Play(s.Script, check); err != nil {
				t.Fatal(err)
			}
		})
	}
}
