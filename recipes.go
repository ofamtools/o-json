package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Recipe is a declarative, saveable and replayable transformation pipeline.
type Recipe struct {
	Name        string      `json:"name,omitempty"`
	Description string      `json:"description,omitempty"`
	From        string      `json:"from,omitempty"`
	To          string      `json:"to,omitempty"`
	Operations  []Operation `json:"operations"`
}

// LoadRecipe reads a JSON or JSONC recipe file from path.
func LoadRecipe(path string) (Recipe, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Recipe{}, err
	}
	clean := StripJSONC(string(data))
	var r Recipe
	if err := json.Unmarshal([]byte(clean), &r); err != nil {
		return Recipe{}, fmt.Errorf("recipe %s: %w", path, err)
	}
	return r, nil
}

// SaveRecipe writes r as indented JSON atomically to path.
func SaveRecipe(path string, r Recipe) error {
	if strings.TrimSpace(path) == "" {
		return ErrNoPath
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return atomicWrite(path, data)
}

// RunRecipe parses raw, resolves and applies r.Operations deterministically,
// and encodes the result in the requested output format.
func RunRecipe(raw string, r Recipe, fromOverride, toOverride string, pretty bool) ([]byte, error) {
	inFmt := r.From
	if fromOverride != "" {
		inFmt = fromOverride
	}
	v, _, err := ParseFormat(raw, inFmt)
	if err != nil {
		return nil, err
	}
	resolved := make([]Operation, 0, len(r.Operations))
	for i, op := range r.Operations {
		if op.Kind == "scope" {
			resolved = append(resolved, op)
			continue
		}
		step, err := ResolveStep(v, resolved, op)
		if err != nil {
			return nil, fmt.Errorf("recipe step %d (%s): %w", i+1, op.Kind, err)
		}
		resolved = append(resolved, step)
	}
	out, err := ApplyPipeline(v, resolved)
	if err != nil {
		return nil, err
	}
	outFmt := r.To
	if toOverride != "" {
		outFmt = toOverride
	}
	if outFmt == "" {
		outFmt = "json"
	}
	return EncodeFormat(out, outFmt, pretty)
}

// runRecipeCmd implements `o-json run FILE|- --recipe PATH [--from FMT] [--to FMT] [--compact]`.
func runRecipeCmd(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	usage := "usage: " + subUsage["run"]
	fs := flag.NewFlagSet("o-json run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, usage); fs.PrintDefaults() }
	recipePath := fs.String("recipe", "", "path to a JSON recipe file")
	from := fs.String("from", "", "override input format: json, jsonl, jsonc, yaml, toml, csv, tsv")
	to := fs.String("to", "", "override output format: json, jsonl, jsonc, yaml, toml, csv, tsv, md")
	compact := fs.Bool("compact", false, "compact JSON output")
	pos, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintf(stderr, "o-json run: expected 1 argument, got %d\n%s\n", len(pos), usage)
		return 2
	}
	if *recipePath == "" {
		fmt.Fprintf(stderr, "o-json run: --recipe PATH is required\n%s\n", usage)
		return 2
	}
	fail := func(code int, err error) int {
		fmt.Fprintf(stderr, "o-json run: %v\n", err)
		return code
	}
	r, err := LoadRecipe(*recipePath)
	if err != nil {
		return fail(1, err)
	}
	raw, err := readSource(pos[0], stdin)
	if err != nil {
		return fail(1, err)
	}
	inFmt := *from
	if inFmt == "" && r.From == "" && pos[0] != "-" {
		inFmt = InferInputFormat(pos[0])
	}
	data, err := RunRecipe(raw, r, inFmt, *to, !*compact)
	if err != nil {
		return fail(1, err)
	}
	if _, err := stdout.Write(data); err != nil {
		return fail(1, err)
	}
	return 0
}
