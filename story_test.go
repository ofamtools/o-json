package main

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/unixsuperhero/ochazuke/story"
)

// o-json's stories (testdata/stories): ochazuke's story engine drives the
// real viewer. A story's page id picks the session (the story format asks
// for a page): "o-json" is o-json's runtime and model on the nest
// document's restructure page, the focus on the restructured tree's .y;
// "o-json-viewer" is a fresh viewer on the nest document, as it opens;
// "o-json-nested" a fresh viewer on the nested document (an array of
// people with objects and arrays in their cells, hover.go);
// "o-json-empty" the viewer with no document (o-json alone).
// The story's script and expectations run against it as o-story runs its
// own; its settings set o-json's (each one's default is the value); a
// story with a skip ("gap: ...") runs but cannot fail. Stories run in
// parallel: a session shares nothing with another (the race detector
// checks it under -race).
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
			t.Parallel() // each story has its own runtime and model
			var a *App
			switch {
			case s.Page != nil && s.Page.ID == "o-json-viewer":
				a = open(t, nestDoc, 120, 30)
			case s.Page != nil && s.Page.ID == "o-json-nested":
				a = open(t, nestedDoc, 120, 30)
			case s.Page != nil && s.Page.ID == "o-json-empty":
				a = empty(t)
			default:
				a = openNest(t, nestDoc, ".a", ".x", ".y", ".z")
				a.on(t, ".y")
			}
			// A story's settings are o-json's own: each one's default is
			// the value the session runs with.
			for _, st := range s.Settings {
				if err := a.r.Settings().Set(st.Name, st.Default); err != nil {
					t.Fatal(err)
				}
			}
			w, h := s.Size()
			a.m.Width, a.m.Height = w, h
			ss := &story.Session{Story: s, R: a.r, M: a.m}
			d := story.NewDriver(ss, story.Options{Mono: true})
			var failed []string
			check := func(n int) error {
				for _, e := range s.Expect {
					if e.After == n || e.After == 0 && n == len(s.Script) {
						for _, bad := range story.Check(ss, e) {
							failed = append(failed, fmt.Sprintf("after %d: %s\n%s", n, bad, ss.Frame()))
						}
					}
				}
				return nil
			}
			err := d.Play(s.Script, check)
			if s.Skip != "" {
				t.Skipf("%s (err %v, %d failed)", s.Skip, err, len(failed))
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range failed {
				t.Error(f)
			}
		})
	}
}
