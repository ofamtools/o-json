package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// A path names values in a document, jq style: "." is the root, ".key"
// and `["odd key"]` step into an object, "[2]" (or "[-1]", from the end)
// into an array, and "[]" fans out to every item. The leading dot may be
// left off ("items[0].name"), and a leading "$" is accepted. A path with
// "[]" in it can name many values; structure paths (".items[].owner")
// are paths of this kind.

// pathStep is one step of a parsed path.
type pathStep struct {
	key   string
	index int
	kind  byte // 'k' key, 'i' index, 'a' every item
}

// parsePath splits a path into steps.
func parsePath(path string) ([]pathStep, error) {
	s := strings.TrimSpace(path)
	s = strings.TrimPrefix(s, "$")
	if s == "" || s == "." {
		return nil, nil
	}
	if s[0] != '.' && s[0] != '[' {
		s = "." + s
	}
	var steps []pathStep
	for i := 0; i < len(s); {
		switch s[i] {
		case '.':
			i++
			if i < len(s) && s[i] == '[' {
				continue // ".[0]" and ".["key"]"
			}
			j := i
			for j < len(s) && s[j] != '.' && s[j] != '[' {
				j++
			}
			if j == i {
				return nil, fmt.Errorf("path %q: empty key at %d", path, i)
			}
			steps = append(steps, pathStep{kind: 'k', key: s[i:j]})
			i = j
		case '[':
			end := closing(s, i)
			if end < 0 {
				return nil, fmt.Errorf("path %q: unclosed [", path)
			}
			inner := strings.TrimSpace(s[i+1 : end])
			switch {
			case inner == "":
				steps = append(steps, pathStep{kind: 'a'})
			case inner[0] == '"':
				var key string
				if err := json.Unmarshal([]byte(inner), &key); err != nil {
					return nil, fmt.Errorf("path %q: bad key %s", path, inner)
				}
				steps = append(steps, pathStep{kind: 'k', key: key})
			default:
				n, err := strconv.Atoi(inner)
				if err != nil {
					return nil, fmt.Errorf("path %q: [%s] is not an index", path, inner)
				}
				steps = append(steps, pathStep{kind: 'i', index: n})
			}
			i = end + 1
		default:
			return nil, fmt.Errorf("path %q: unexpected %q at %d", path, s[i], i)
		}
	}
	return steps, nil
}

// closing is the index of the "]" closing the "[" at i, skipping a quoted key.
func closing(s string, i int) int {
	quoted := false
	for j := i + 1; j < len(s); j++ {
		switch {
		case quoted && s[j] == '\\':
			j++
		case s[j] == '"':
			quoted = !quoted
		case !quoted && s[j] == ']':
			return j
		}
	}
	return -1
}

// Select is every value the path names, in document order. A missing key
// or index names nothing; Select reports an error only for a bad path.
// fanned reports whether the path has a "[]" step, so its answer is a set
// of values rather than one.
func Select(root *Node, path string) (values []*Node, fanned bool, err error) {
	steps, err := parsePath(path)
	if err != nil {
		return nil, false, err
	}
	at := []*Node{root}
	for _, st := range steps {
		next := make([]*Node, 0, len(at))
		for _, n := range at {
			switch st.kind {
			case 'k':
				if c := n.Field(st.key); c != nil {
					next = append(next, c)
				}
			case 'i':
				if n.Kind != KArray {
					continue
				}
				i := st.index
				if i < 0 {
					i += len(n.Children)
				}
				if i >= 0 && i < len(n.Children) {
					next = append(next, n.Children[i])
				}
			case 'a':
				fanned = true
				if n.IsContainer() {
					next = append(next, n.Children...)
				}
			}
		}
		at = next
	}
	return at, fanned, nil
}

// Get is the one value a path without "[]" names.
func Get(root *Node, path string) (*Node, error) {
	values, fanned, err := Select(root, path)
	switch {
	case err != nil:
		return nil, err
	case fanned:
		return nil, fmt.Errorf("path %q names many values ([] fans out); Select them", path)
	case len(values) == 0:
		return nil, fmt.Errorf("nothing at %s", path)
	}
	return values[0], nil
}
