package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"

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

// stdinName names a document read from standard input.
const stdinName = "stdin"

// ReadDoc reads and parses the document a source names (a file, or "-"
// for stdin) and infers its shape.
func ReadDoc(source string, stdin io.Reader) (*Doc, error) {
	name, r, err := openSource(source, stdin)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return ReadFrom(name, r)
}

// ReadFrom parses the document r holds. name says where it came from (a
// file's path, or "stdin"); the document keeps it, so it can be shown and
// read again. Every open goes through it: a source is a name and a reader.
func ReadFrom(name string, r io.Reader) (*Doc, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, plainReadErr(name, err)
	}
	root, err := Build(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return &Doc{Name: name, Size: len(data), Root: root, Shape: Infer(root)}, nil
}

// openSource opens what a source word names and says its name: "-" is
// standard input ("stdin"), anything else a file (its path).
func openSource(source string, stdin io.Reader) (string, io.ReadCloser, error) {
	if source != "-" {
		f, err := os.Open(source)
		if err != nil {
			return source, nil, plainReadErr(source, err)
		}
		return source, f, nil
	}
	switch in := stdin.(type) {
	case nil:
		return stdinName, nil, errors.New("no stdin to read")
	case *keptInput:
		r, err := in.open()
		return stdinName, r, err
	}
	return stdinName, io.NopCloser(stdin), nil
}

// keptInput is standard input read once and kept: a stream is spent by its
// first read, so the viewer keeps what it held and the document named
// "stdin" can be read again (viewer.load, doc.open -).
type keptInput struct {
	r    io.Reader
	once sync.Once
	data []byte
	err  error
}

// keepInput wraps stdin for a program that may read its document again.
func keepInput(stdin io.Reader) io.Reader {
	if stdin == nil {
		return nil
	}
	return &keptInput{r: stdin}
}

func (k *keptInput) Read(p []byte) (int, error) { return k.r.Read(p) }

func (k *keptInput) open() (io.ReadCloser, error) {
	k.once.Do(func() { k.data, k.err = io.ReadAll(k.r) })
	if k.err != nil {
		return nil, plainReadErr(stdinName, k.err)
	}
	return io.NopCloser(bytes.NewReader(k.data)), nil
}

// plainReadErr says a failed read in plain words: the file's name and
// what went wrong, without the system call ("open x: no such file or
// directory" is "x not found"). The cause stays underneath (errors.Is).
func plainReadErr(name string, err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return &readError{name + " not found", err}
	case errors.Is(err, fs.ErrPermission):
		return &readError{name + ": permission denied", err}
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return &readError{fmt.Sprintf("%s: %v", name, pe.Err), err}
	}
	return err
}

type readError struct {
	msg   string
	cause error
}

func (e *readError) Error() string { return e.msg }
func (e *readError) Unwrap() error { return e.cause }

// unwritten reports a failed read of a source that holds no document yet:
// a path that names no file, or nothing but white space (an empty file,
// an empty standard input). The viewer opens those as new documents and
// never refuses them; a text command has nothing to print and fails.
func unwritten(err error) bool {
	var pe *ParseError
	return errors.Is(err, fs.ErrNotExist) || errors.As(err, &pe) && pe.Msg == emptyInput
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

// pathWord reports a word that reads as a path and not as a name (a
// command word, a document identity): it holds a dot or a slash. ./NAME
// makes a path of any name.
func pathWord(word string) bool { return strings.ContainsAny(word, "./") }

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// isTerminal reports stdin attached to a terminal; anything that is not
// a file (a test's reader) is not one.
func isTerminal(r io.Reader) bool {
	if k, ok := r.(*keptInput); ok {
		r = k.r
	}
	f, ok := r.(*os.File)
	return ok && term.IsTerminal(f.Fd())
}
