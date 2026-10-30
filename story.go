//go:build monogatari

// The o-json tester (make story-build): o-json's stories
// (testdata/stories) on monogatari's host, mounted as `o-json story`.
// Only a build with -tags monogatari has it; story_off.go is the normal
// build's empty mount, so the shipped binary links no monogatari.

package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/unixsuperhero/monogatari"
	"github.com/unixsuperhero/monogatari/host"
	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/keytest"
	"github.com/unixsuperhero/oden"
)

//go:embed testdata/stories
var storyFiles embed.FS

// storyDocs are the documents a story's seed can name ("doc": "nested.json").
//
//go:embed testdata/*.json
var storyDocs embed.FS

// storyCommands is the tester's story command (host.Command).
func storyCommands() []oden.Command {
	return []oden.Command{host.Command(storyConfig(progName() + " story"))}
}

// storyConfig is o-json's host: one app, o-json, over the embedded stories.
func storyConfig(prog string) host.Config {
	stories, err := fs.Sub(storyFiles, "testdata/stories")
	if err != nil {
		panic(err) // the embed always has testdata/stories
	}
	return host.Config{Prog: prog, Apps: []monogatari.App{storyApp()}, Stories: stories}
}

// storySeed is the app data a story hands its fixture (its seed field).
type storySeed struct {
	// Doc is the document the viewer opens: a file of o-json's testdata
	// ("nest.json", the default; "nested.json").
	Doc string `json:"doc"`
	// Send and On are the nest fixture's restructure page: the shapes sent
	// to the restructured tree (default .a .x .y .z) and the node selected
	// there (default .y).
	Send []string `json:"send"`
	On   string   `json:"on"`
	// Files are testdata documents the empty and viewer fixtures copy into the temp
	// dir and records in the recent list, opening none: the path
	// chooser's first choices, for a story that opens one (doc.open).
	Files []string `json:"files"`
}

// storyApp is o-json for stories: the real viewer, its document written
// to a temp dir (so a write lands there, never in the repo), its state
// directory a temp dir, and no clipboard.
func storyApp() monogatari.App {
	return monogatari.App{
		Name:    "o-json",
		Summary: "the o-json viewer on a testdata document (seed doc), or with none",
		Fixtures: []monogatari.Fixture{
			{Name: "nest", Summary: "the restructure page on the seed's document (nest.json): its send shapes (.a .x .y .z) sent, the focus on the restructured tree's on node (.y)", Build: buildNest},
			{Name: "viewer", Summary: "a fresh viewer on the seed's document (nest.json), as it opens; the seed's files are in its temp dir and recent list", Build: buildViewer},
			{Name: "empty", Summary: "the viewer with no document, as o-json with no file opens; the seed's files are in its temp dir and recent list, none open", Build: buildEmpty},
		},
	}
}

// storySession is one fixture's app and the temp dir it owns.
type storySession struct {
	a      *App
	dir    string
	doc    string
	staged map[string]bool // the seed's files: not writes
}

func (s *storySession) built() monogatari.Built {
	return monogatari.Built{Model: s.a.m, Program: drawEach{s.a.m}, Close: func() { os.RemoveAll(s.dir) }, Writes: s.writes}
}

// drawEach draws a frame after every message, as Bubble Tea's event loop
// does (tea.go: update, then render): the viewer's data pane follows the
// tree in render (sync), so a story that sent keys without drawing
// between them acted on rows a real program had already replaced.
type drawEach struct{ m *ochazuke.Model }

func (d drawEach) Init() tea.Cmd { return d.m.Init() }

func (d drawEach) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := d.m.Update(msg)
	d.m.View()
	return d, cmd
}

func (d drawEach) View() tea.View { return d.m.View() }

// writes is every file in the temp dir but the document: what the app
// wrote (viewer.write, restructure.write, export).
func (s *storySession) writes() []string {
	var out []string
	filepath.WalkDir(s.dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && path != s.doc && !s.staged[path] {
			rel, _ := filepath.Rel(s.dir, path)
			out = append(out, rel)
		}
		return nil
	})
	slices.Sort(out)
	return out
}

// newStorySession is a sandboxed viewer (the story's values set before
// anything loads) sized to the story.
func newStorySession(env monogatari.Env) (s *storySession, err error) {
	dir, err := os.MkdirTemp("", "o-json-story-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
		}
	}()
	a, err := New()
	if err != nil {
		return nil, err
	}
	if err := env.Sandbox(a.m); err != nil {
		return nil, err
	}
	// A story with no state of its own still never reaches the user's
	// state directory (the recent list).
	if a.r.Config().StateDir == "" {
		state := filepath.Join(dir, "state")
		if err := os.Mkdir(state, 0o755); err != nil {
			return nil, err
		}
		a.r.Config().StateDir = state
	}
	a.m.Width, a.m.Height = env.Width, env.Height
	a.m.View()
	return &storySession{a: a, dir: dir}, nil
}

func seedOf(env monogatari.Env) (storySeed, error) {
	var seed storySeed
	if len(env.Seed) > 0 {
		if err := json.Unmarshal(env.Seed, &seed); err != nil {
			return seed, fmt.Errorf("seed: %w", err)
		}
	}
	if seed.Doc == "" {
		seed.Doc = "nest.json"
	}
	return seed, nil
}

// load writes the seed's document to the temp dir and opens it; the
// title shows its name as the story gave it.
func (s *storySession) load(name string) error {
	data, err := storyDocs.ReadFile("testdata/" + name)
	if err != nil {
		return fmt.Errorf("seed doc %q is not a testdata document", name)
	}
	s.doc = filepath.Join(s.dir, name)
	if err := os.WriteFile(s.doc, data, 0o644); err != nil {
		return err
	}
	doc, err := ReadDoc(s.doc, nil)
	if err != nil {
		return err
	}
	doc.Name = name
	if err := s.a.setDoc(s.doc, doc); err != nil {
		return err
	}
	s.a.m.View() // the first frame measures the windows
	return nil
}

func buildEmpty(env monogatari.Env) (monogatari.Built, error) {
	seed, err := seedOf(env)
	if err != nil {
		return monogatari.Built{}, err
	}
	s, err := newStorySession(env)
	if err != nil {
		return monogatari.Built{}, err
	}
	if err := s.stage(seed.Files); err != nil {
		s.built().Close()
		return monogatari.Built{}, err
	}
	return s.built(), nil
}

// stage copies testdata documents into the temp dir and records them in
// the recent list (the first named newest), so the path chooser offers
// them; it opens none.
func (s *storySession) stage(names []string) error {
	state, size := s.a.stateDir(), s.a.recentSize()
	for i := len(names) - 1; i >= 0; i-- {
		data, err := storyDocs.ReadFile("testdata/" + names[i])
		if err != nil {
			return fmt.Errorf("seed file %q is not a testdata document", names[i])
		}
		p := filepath.Join(s.dir, names[i])
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return err
		}
		if s.staged == nil {
			s.staged = map[string]bool{}
		}
		s.staged[p] = true
		if err := recordRecent(state, p, size); err != nil {
			return err
		}
	}
	s.a.LoadRecent()
	return nil
}

func buildViewer(env monogatari.Env) (monogatari.Built, error) {
	seed, err := seedOf(env)
	if err != nil {
		return monogatari.Built{}, err
	}
	s, err := newStorySession(env)
	if err != nil {
		return monogatari.Built{}, err
	}
	if err := s.stage(seed.Files); err != nil {
		s.built().Close()
		return monogatari.Built{}, err
	}
	if err := s.load(seed.Doc); err != nil {
		s.built().Close()
		return monogatari.Built{}, err
	}
	return s.built(), nil
}

func buildNest(env monogatari.Env) (monogatari.Built, error) {
	seed, err := seedOf(env)
	if err != nil {
		return monogatari.Built{}, err
	}
	if seed.Send == nil {
		seed.Send = []string{".a", ".x", ".y", ".z"}
	}
	if seed.On == "" {
		seed.On = ".y"
	}
	s, err := newStorySession(env)
	if err != nil {
		return monogatari.Built{}, err
	}
	if err := s.nest(seed); err != nil {
		s.built().Close()
		return monogatari.Built{}, err
	}
	return s.built(), nil
}

// nest opens the restructure page as the keys would: M, s on each shape
// to send, tab to the restructured tree, the on node selected.
func (s *storySession) nest(seed storySeed) error {
	if err := s.load(seed.Doc); err != nil {
		return err
	}
	a := s.a
	if err := s.press("M"); err != nil {
		return err
	}
	for _, p := range seed.Send {
		if err := s.selectIn(a.rs.orig, func(n ochazuke.TreeNode) bool { return n.ID == "o:s:"+p }); err != nil {
			return fmt.Errorf("seed send %s: %w", p, err)
		}
		if err := s.press("s"); err != nil {
			return err
		}
	}
	if err := s.press("tab"); err != nil {
		return err
	}
	on := func(n ochazuke.TreeNode) bool { return a.rs.meta[n.ID].from == seed.On && !a.rs.meta[n.ID].item }
	if err := s.selectIn(a.rs.tree, on); err != nil {
		return fmt.Errorf("seed on %s: %w", seed.On, err)
	}
	return nil
}

// press sends keys, failing on the first error the model shows, and
// draws one frame after the last.
func (s *storySession) press(keys ...string) error {
	for _, key := range keys {
		keytest.Press(s.a.m, key)
		if s.a.m.Status != "" {
			return fmt.Errorf("key %q: %s", key, s.a.m.Status)
		}
	}
	s.a.m.View()
	return nil
}

// selectIn moves a tree's selection to a node with k and j.
func (s *storySession) selectIn(tree *ochazuke.Tree, match func(ochazuke.TreeNode) bool) error {
	if err := tree.Focus(); err != nil {
		return err
	}
	for _, key := range []string{"k", "j"} {
		for range 40 {
			if n, ok := tree.Selected(); ok && match(n) {
				s.a.m.View()
				return nil
			}
			keytest.Press(s.a.m, key)
			if s.a.m.Status != "" {
				return fmt.Errorf("key %q: %s", key, s.a.m.Status)
			}
		}
	}
	return errors.New("not in " + tree.ID())
}
