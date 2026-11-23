package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/ochazuke/tutorial"
)

func TestTutorialSpec(t *testing.T) {
	spec, err := tutorial.LoadSpec(defaultTutorialSpec)
	if err != nil {
		t.Fatalf("LoadSpec(%q): %v", defaultTutorialSpec, err)
	}
	if spec.Tool != "o-json" {
		t.Fatalf("spec.Tool = %q, want o-json", spec.Tool)
	}
	if len(spec.Sections) < 7 {
		t.Fatalf("expected at least 7 sections, got %d", len(spec.Sections))
	}

	res, err := runTutorialSpec(spec, ".")
	if err != nil {
		t.Fatalf("runTutorialSpec: %v", err)
	}

	bindings := defaultBindings()

	htmlDoc := tutorial.RenderHTML(res, tutorial.HTMLOptions{
		VideoPath:  "o-json.mp4",
		IncludeTOC: true,
	})
	for _, want := range []string{
		"<!DOCTYPE html>",
		spec.Title,
		"o-json.mp4",
		"Explore Deep Nested JSON from the Structure Pane",
		"Multi-Depth Mark &amp; Flatten with Leaf Names",
		"Hoist, Splat, Nest &amp; Convert Tree Shapes",
		"Side-by-Side Split Transfer",
		"Interactive jq Builder &amp; Live Preview",
		"pre class=\"term\"",
	} {
		if !strings.Contains(htmlDoc, want) {
			t.Errorf("RenderHTML missing %q", want)
		}
	}

	tape := tutorial.RenderTape(spec, bindings, tutorial.TapeOptions{OutputMP4: "o-json.mp4"})
	for _, want := range []string{
		"Output o-json.mp4",
		`Type "o-json docs/tutorials/fixtures/orders.json"`,
		"# Section: Explore Deep Nested JSON from the Structure Pane",
		"# Section: Interactive jq Builder & Live Preview",
	} {
		if !strings.Contains(tape, want) {
			t.Errorf("RenderTape missing %q", want)
		}
	}

	srt := tutorial.RenderSRT(spec)
	if !strings.Contains(srt, "00:00:00,000 -->") {
		t.Errorf("RenderSRT missing initial cue:\n%s", srt)
	}
	if !strings.Contains(srt, "Open orders.json") {
		t.Errorf("RenderSRT missing first caption:\n%s", srt)
	}

	// Verify the CLI subcommand `o-json tutorial build` writes the full bundle cleanly.
	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"tutorial", "build", "--spec", defaultTutorialSpec, "--out", outDir}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("o-json tutorial build exited %d: stderr=%s", code, stderr.String())
	}
	for _, name := range []string{"index.html", "o-json.tape", "o-json.srt", "tutorial-data.json"} {
		info, err := os.Stat(filepath.Join(outDir, name))
		if err != nil {
			t.Fatalf("expected bundle artifact %s: %v", name, err)
		}
		if info.Size() == 0 {
			t.Errorf("bundle artifact %s is empty", name)
		}
	}
}
