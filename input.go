package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/x/term"
)

// Every command reads one document: a file, or stdin as "-" (or with no
// file word at all when stdin is not a terminal).

// Doc is a parsed document and its structure.
type Doc struct {
	Name  string // the file, or "stdin"
	Size  int
	Root  *Node
	Shape *Shape
}

// ReadDoc reads and parses a file ("-" is stdin) and infers its shape.
func ReadDoc(path string, stdin io.Reader) (*Doc, error) {
	var (
		data []byte
		err  error
		name = path
	)
	if path == "-" {
		name = "stdin"
		if stdin == nil {
			return nil, errors.New("no stdin to read")
		}
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	root, err := Build(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return &Doc{Name: name, Size: len(data), Root: root, Shape: Infer(root)}, nil
}

// splitFile takes the file from the front of words: "-", an existing
// file, else stdin when it is not a terminal (the words stay).
func splitFile(words []string, stdin io.Reader) (file string, rest []string, err error) {
	if len(words) > 0 && (words[0] == "-" || isFile(words[0])) {
		return words[0], words[1:], nil
	}
	if !isTerminal(stdin) {
		return "-", words, nil
	}
	if len(words) > 0 {
		return "", nil, fmt.Errorf("no file %s (- reads stdin)", words[0])
	}
	return "", nil, errors.New("a file is required (- reads stdin)")
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// isTerminal reports stdin attached to a terminal; anything that is not
// a file (a test's reader) is not one.
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && term.IsTerminal(f.Fd())
}
