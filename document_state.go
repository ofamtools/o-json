package main

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"syscall"

	"ofam.dev/ochazuke"
)

// documentState keeps document identities, annotations and relations outside the
// source files. Every transaction reloads under a process-shared advisory lock.
// Tests with configuration disabled keep their state entirely in memory.
type documentState struct {
	dir    func() string
	mu     sync.Mutex
	memory documentRecords
}

type documentRecords struct {
	Docs      map[string]ochazuke.Thing `json:"docs"`
	Notes     map[string]ochazuke.Thing `json:"notes"`
	Relations []ochazuke.Relation       `json:"relations"`
}

func (s *documentState) transaction(write bool, fn func(*documentRecords) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := &s.memory
	dir := s.dir()
	if dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		lock, err := os.OpenFile(filepath.Join(dir, "documents.lock"), os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return err
		}
		defer lock.Close()
		if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
			return err
		}
		defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		state = &documentRecords{}
		data, err := os.ReadFile(filepath.Join(dir, "documents.json"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if len(data) > 0 {
			if err := json.Unmarshal(data, state); err != nil {
				return err
			}
		}
	}
	if state.Docs == nil {
		state.Docs = map[string]ochazuke.Thing{}
	}
	if state.Notes == nil {
		state.Notes = map[string]ochazuke.Thing{}
	}
	if err := fn(state); err != nil {
		return err
	}
	if write && dir != "" {
		data, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			return err
		}
		return writeNew(filepath.Join(dir, "documents.json"), append(data, '\n'), true)
	}
	return nil
}

func (s *documentState) Relations(q ochazuke.RelationQuery) ([]ochazuke.Relation, error) {
	out := []ochazuke.Relation{}
	err := s.transaction(false, func(state *documentRecords) error {
		for _, x := range state.Relations {
			if q.Match(x) {
				out = append(out, x)
			}
		}
		slices.SortStableFunc(out, func(a, b ochazuke.Relation) int { return a.Position - b.Position })
		return nil
	})
	return out, err
}
func (s *documentState) Put(x ochazuke.Relation) error {
	return s.transaction(true, func(state *documentRecords) error {
		i := slices.IndexFunc(state.Relations, func(old ochazuke.Relation) bool { return old.ID() == x.ID() })
		if i < 0 {
			state.Relations = append(state.Relations, x)
		} else {
			state.Relations[i] = x
		}
		return nil
	})
}
func (s *documentState) Remove(x ochazuke.Relation) error {
	return s.transaction(true, func(state *documentRecords) error {
		state.Relations = slices.DeleteFunc(state.Relations, func(old ochazuke.Relation) bool { return old.ID() == x.ID() })
		return nil
	})
}

// The note table delegates query semantics to the framework's reference table.
// Notes are small; copying their metadata is independent of JSON document size.
type documentNotes struct {
	state   *documentState
	runtime *ochazuke.Runtime
}

func (t documentNotes) Get(key string) (out ochazuke.Thing, found bool, err error) {
	err = t.state.transaction(false, func(s *documentRecords) error { out, found = s.Notes[key]; return nil })
	return
}
func (t documentNotes) Put(key string, value ochazuke.Thing) error {
	return t.state.transaction(true, func(s *documentRecords) error { s.Notes[key] = value; return nil })
}
func (t documentNotes) Delete(key string) error {
	return t.state.transaction(true, func(s *documentRecords) error { delete(s.Notes, key); return nil })
}
func (t documentNotes) Version() (string, error) { return "", nil }
func (t documentNotes) Query(q ochazuke.DocQuery) (out ochazuke.ListPage, err error) {
	err = t.state.transaction(false, func(s *documentRecords) error {
		table := ochazuke.NewMemDocTable(t.runtime, "note")
		for _, key := range slices.Sorted(maps.Keys(s.Notes)) {
			if err := table.Put(key, s.Notes[key]); err != nil {
				return err
			}
		}
		var err error
		out, err = table.Query(q)
		return err
	})
	return
}
