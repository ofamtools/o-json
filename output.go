package main

import (
	"bytes"
	"io"
	"os"
)

// Where output goes. Every export and save is a function of an io.Writer
// (restructureTo, exportRequest.writeTo, writeAtomic's write), so the place
// is a parameter of the call and never part of the work: a file, standard
// output, or the verb's own answer. The library's shared output parameter
// can take writeOut's place without touching what is written.

// writeOut runs write into the place out names: a new file (an existing
// one, the source among them, only with overwrite; always atomically), or
// stdout when out is "" or "-".
func writeOut(out string, overwrite bool, stdout io.Writer, write func(io.Writer) error) error {
	if out == "" || out == "-" {
		return write(stdout)
	}
	return existsHint(out, writeAtomic(out, overwrite, write))
}

// verbOut is writeOut for a verb, which has no stream of its own: with no
// out the text is the verb's answer, and a written file answers {out,
// bytes}.
func verbOut(out string, overwrite bool, write func(io.Writer) error) (any, error) {
	var kept bytes.Buffer
	if out == "" || out == "-" {
		if err := write(&kept); err != nil {
			return nil, err
		}
		return kept.String(), nil
	}
	n := 0
	err := writeOut(out, overwrite, nil, func(w io.Writer) error {
		if err := write(&kept); err != nil {
			return err
		}
		n = kept.Len()
		_, err := w.Write(kept.Bytes())
		return err
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"out": out, "bytes": n}, nil
}

// readMappingFile reads a mapping file (--map SPEC.json).
func readMappingFile(path string) (*Mapping, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, plainReadErr(path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, plainReadErr(path, err)
	}
	return ReadMapping(data)
}
