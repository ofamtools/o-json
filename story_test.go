//go:build monogatari

package main

import (
	"flag"
	"path/filepath"
	"testing"

	"ofam.dev/monogatari/host"
)

var updateStories = flag.Bool("update", false, "rewrite the stories' golden frames")

// o-json's stories (testdata/stories) through monogatari's host: each
// story's fixture (nest, viewer, empty) builds the real viewer, its seed
// names the document, its values set o-json's settings. A story with a
// skip ("gap: ...") runs but cannot fail. Stories run in parallel: a
// session shares nothing with another (the race detector checks it under
// -race).
func TestStories(t *testing.T) {
	cfg := storyConfig("o-json story")
	cfg.Dir, cfg.Update = filepath.Join("testdata", "stories"), *updateStories
	host.Test(t, cfg)
}
