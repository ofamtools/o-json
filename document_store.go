package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"ofam.dev/ochazuke"
)

type documentStore struct {
	state   *documentState
	current func() string
	trash   func(context.Context, string) error
	move    func(string, string) error
}

func fileProp(name, description string) ochazuke.PropSpec {
	return ochazuke.PropSpec{Name: name, Parameter: ochazuke.Parameter{Type: ochazuke.PropPath, Path: "file", Description: description}}
}

func documentProps() []ochazuke.PropSpec {
	path := fileProp("file", "JSON source file; changing it explicitly moves the file without replacing a destination")
	path.Human, path.Required, path.New = true, true, true
	return []ochazuke.PropSpec{
		{Name: "id", Key: true, Parameter: ochazuke.Parameter{Type: "string", Description: "Stable document identity (a file path is also accepted)"}},
		path,
		{Name: "name", Role: ochazuke.RoleLabel, Parameter: ochazuke.Parameter{Type: "string", Description: "File name; changing it explicitly renames the file in its folder"}},
		{Name: "content", Multiline: true, Parameter: ochazuke.Parameter{Type: "string", Description: "JSON text; an explicit change writes the source atomically"}},
		{Name: "archived", Role: ochazuke.RoleArchive, Default: false, Parameter: ochazuke.Parameter{Type: "boolean", Description: "Hide this document from the registry without moving its file"}},
		fact("missing", "boolean", "Whether the file is not there: a new document not written yet, or a file that is gone"),
		fact("size", "integer", "Source size in bytes"),
		fact("modified", ochazuke.PropTime, "Source modification time"),
	}
}

// Both runtime entry points use this declaration and the same persisted state.
func registerDocuments(r *ochazuke.Runtime, current func() string, moved func(string, string)) *documentStore {
	state := &documentState{dir: func() string { return r.Config().StateDirectory() }}
	d := &documentStore{state: state, current: current, trash: trashDocument, move: func(from, to string) error { return moveDocumentFile(from, to, os.Link) }}
	r.SetRelationStore(state)
	r.Define("doc").Props(documentProps()...).Store(remoteDocumentStore("doc", ochazuke.Store{
		Kind: ochazuke.KindCollection, Get: d.get, List: d.list, Create: d.create, Patch: d.patch, Delete: d.delete,
		Undo: true, AcceptsKey: true,
		Confirm: map[string]string{"delete": "Move {{label}} to the system Trash? The file leaves its folder; restore it using the system Trash."},
	}, func(t ochazuke.Thing) {
		if moved != nil {
			if old, ok := t["previous_file"].(string); ok {
				moved(old, t["file"].(string))
			}
		}
	}))
	r.Define("note").Props(
		ochazuke.PropSpec{Name: "id", Key: true},
		ochazuke.PropSpec{Name: "text", Role: ochazuke.RoleLabel, Multiline: true, Parameter: ochazuke.Parameter{Type: "string", Required: true}},
		ochazuke.PropSpec{Name: "about", Role: ochazuke.RoleAbout, Parameter: ochazuke.Parameter{Type: "string", Noun: ochazuke.AnyNoun, Free: true}},
		ochazuke.PropSpec{Name: "archived", Role: ochazuke.RoleArchive, Default: false, Parameter: ochazuke.Parameter{Type: "boolean"}},
	).Store(remoteDocumentStore("note", ochazuke.DocStore(documentNotes{state: state, runtime: r}, "note")))
	return d
}

func remoteDocumentStore(noun string, store ochazuke.Store, after ...func(ochazuke.Thing)) ochazuke.Store {
	call := func(c ochazuke.Ctx, op, key string, fn func() (any, error)) (any, error) {
		return c.Await(ochazuke.StoreCall{Store: noun, Op: op, Key: key}, func() ochazuke.Result {
			return ochazuke.Pending(func(context.Context) (any, error) { return fn() })
		}, 0)
	}
	raw := store
	store.Remote = true
	store.Get = func(c ochazuke.Ctx, key string) (ochazuke.Thing, error) {
		v, err := call(c, "get", key, func() (any, error) { return raw.Get(c, key) })
		if err != nil {
			return nil, err
		}
		return v.(ochazuke.Thing), nil
	}
	store.List = func(c ochazuke.Ctx, q ochazuke.ListQuery) ([]ochazuke.Thing, error) {
		v, err := call(c, "list", "", func() (any, error) { return raw.List(c, q) })
		if err != nil {
			return nil, err
		}
		return v.([]ochazuke.Thing), nil
	}
	store.Query = nil // list_query.go applies the complete query to the bounded registry.
	store.Create = func(c ochazuke.Ctx, t ochazuke.Thing) (ochazuke.Thing, error) {
		v, err := call(c, "create", "", func() (any, error) { return raw.Create(c, t) })
		if err != nil {
			return nil, err
		}
		return v.(ochazuke.Thing), nil
	}
	store.Patch = func(c ochazuke.Ctx, key string, t ochazuke.Thing) (ochazuke.Thing, error) {
		v, err := call(c, "set", key, func() (any, error) { return raw.Patch(c, key, t) })
		if err != nil {
			return nil, err
		}
		if len(after) > 0 {
			after[0](v.(ochazuke.Thing))
		}
		return v.(ochazuke.Thing), nil
	}
	store.Delete = func(c ochazuke.Ctx, key string) error {
		_, err := call(c, "delete", key, func() (any, error) { return nil, raw.Delete(c, key) })
		return err
	}
	// A file-backed note table does not have a cheap process-local token.
	store.ChangeToken = nil
	return store
}

// Opening needs only the identity's file, not a second full source read or an
// intermediate record result for the UI to present over the native viewer.
func (d *documentStore) filePath(key string) (string, error) {
	file, err := filepath.Abs(key)
	if err != nil {
		return "", err
	}
	err = d.state.transaction(false, func(s *documentRecords) error {
		if t := s.document(key, file); t != nil {
			file = t["file"].(string)
		}
		return nil
	})
	return file, err
}

func (d *documentStore) get(_ ochazuke.Ctx, key string) (out ochazuke.Thing, err error) {
	err = d.state.transaction(true, func(s *documentRecords) error {
		out, err = d.getLocked(s, key)
		return err
	})
	return out, err
}

func (d *documentStore) getLocked(s *documentRecords, key string) (ochazuke.Thing, error) {
	if key == "" || key == "-" {
		return nil, fmt.Errorf("a document file or identity is required")
	}
	abs, err := filepath.Abs(key)
	if err != nil {
		return nil, err
	}
	out := s.document(key, abs)
	fresh := out == nil
	if fresh {
		out = ochazuke.Thing{"file": abs}
	}
	out, err = d.read(out)
	if err != nil {
		return nil, err
	}
	if fresh && out["missing"] == true {
		// A path that names no file yet is a new, unsaved document: it
		// answers by its path and is registered once it is written. A
		// word that is no path (pathWord) is an identity nothing has.
		if !pathWord(key) {
			return nil, fmt.Errorf("no document %s (a new file is named by a path: ./%s)", key, key)
		}
		out["id"], out["name"], out["archived"] = abs, filepath.Base(abs), false
		return out, nil
	}
	if fresh {
		metadata, err := s.rememberFile(abs, "")
		if err != nil {
			return nil, err
		}
		maps.Copy(out, metadata)
	}
	return out, nil
}

func (s *documentRecords) document(key, abs string) ochazuke.Thing {
	if t, ok := s.Docs[key]; ok {
		return maps.Clone(t)
	}
	for _, t := range s.Docs {
		if t["file"] == abs {
			return maps.Clone(t)
		}
	}
	return nil
}

func (s *documentRecords) rememberFile(abs, id string) (ochazuke.Thing, error) {
	if out := s.document("", abs); out != nil {
		return out, nil
	}
	if id == "" {
		var identity [12]byte
		if _, err := rand.Read(identity[:]); err != nil {
			return nil, err
		}
		id = fmt.Sprintf("j%x", identity)
	}
	out := ochazuke.Thing{"id": id, "file": abs, "name": filepath.Base(abs), "archived": false}
	s.Docs[id], s.changed = maps.Clone(out), true
	return out, nil
}

// Metadata imports do not parse every recent document's content.
func (d *documentStore) rememberFile(path string) (out ochazuke.Thing, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	err = d.state.transaction(true, func(s *documentRecords) error {
		out, err = s.rememberFile(abs, "")
		return err
	})
	return out, err
}
func (d *documentStore) read(t ochazuke.Thing) (ochazuke.Thing, error) {
	file := fmt.Sprint(t["file"])
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		// Not written yet, or gone: the record still answers, as list
		// marks it, with nothing in it. A read never refuses a name.
		t["content"], t["size"], t["missing"] = "", 0, true
		delete(t, "modified")
		return t, nil
	}
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(file)
	if err != nil {
		return nil, err
	}
	t["content"], t["size"], t["modified"] = string(data), len(data), info.ModTime().UTC().Format(time.RFC3339)
	delete(t, "missing")
	return t, nil
}
func (d *documentStore) list(c ochazuke.Ctx, _ ochazuke.ListQuery) ([]ochazuke.Thing, error) {
	// Include the viewer's current file and the existing recent-file registry.
	paths := readRecent(d.state.dir(), 200)
	for _, p := range paths {
		if _, err := d.rememberFile(p); err != nil {
			return nil, err
		}
	}
	out := []ochazuke.Thing{}
	err := d.state.transaction(false, func(s *documentRecords) error {
		for _, key := range slices.Sorted(maps.Keys(s.Docs)) {
			t := maps.Clone(s.Docs[key])
			info, err := os.Stat(fmt.Sprint(t["file"]))
			if errors.Is(err, os.ErrNotExist) {
				t["missing"] = true
			} else if err != nil {
				return err
			} else {
				t["size"] = info.Size()
				t["modified"] = info.ModTime().UTC().Format(time.RFC3339)
			}
			out = append(out, t)
		}
		return nil
	})
	return out, err
}
func (d *documentStore) create(c ochazuke.Ctx, values ochazuke.Thing) (ochazuke.Thing, error) {
	file, _ := values["file"].(string)
	if file == "" {
		return nil, &ochazuke.MissingParameterError{Param: "file", Reason: "choose a new JSON file"}
	}
	file, err := filepath.Abs(file)
	if err != nil {
		return nil, err
	}
	duplicate := ochazuke.StringArg(c.Args, "from", "") != ""
	content, ok := values["content"].(string)
	if !ok || content == "" {
		content = "{}\n"
	}
	data := []byte(content)
	if _, err := Build(data); err != nil {
		return nil, err
	}
	var t ochazuke.Thing
	err = d.state.transaction(true, func(s *documentRecords) error {
		requested, _ := values["id"].(string)
		if _, exists := s.Docs[requested]; exists {
			return fmt.Errorf("document identity %s already exists", requested)
		}
		base := file
		for i := 0; ; i++ {
			if i > 0 {
				ext := filepath.Ext(base)
				file = fmt.Sprintf("%s.copy-%d%s", strings.TrimSuffix(base, ext), i, ext)
			}
			var err error
			if s.document("", file) != nil {
				err = fmt.Errorf("%s already belongs to a document: %w", file, os.ErrExist)
			} else {
				err = writeNew(file, data, false)
			}
			if duplicate && errors.Is(err, os.ErrExist) {
				continue
			}
			if err != nil {
				return err
			}
			break
		}
		s.rollback = func() error { return os.Remove(file) }
		var err error
		t, err = s.rememberFile(file, requested)
		if err != nil {
			return err
		}
		for _, key := range []string{"created", "updated", "archived"} {
			if v, ok := values[key]; ok {
				t[key] = v
			}
		}
		s.Docs[t["id"].(string)] = documentMetadata(t)
		t, err = d.read(t)
		return err
	})
	return t, err
}
func documentMetadata(t ochazuke.Thing) ochazuke.Thing {
	out := maps.Clone(t)
	delete(out, "content")
	delete(out, "size")
	delete(out, "modified")
	delete(out, "previous_file")
	delete(out, "missing")
	return out
}
func (d *documentStore) patch(_ ochazuke.Ctx, key string, changes ochazuke.Thing) (out ochazuke.Thing, err error) {
	err = d.state.transaction(true, func(s *documentRecords) error {
		t, err := d.getLocked(s, key)
		if err != nil {
			return err
		}
		old := t["file"].(string)
		target := old
		fresh := s.document("", old) == nil // a new document: no record until its content is written
		if v, ok := changes["file"].(string); ok && v != "" {
			target, err = filepath.Abs(v)
			if err != nil {
				return err
			}
		}
		if name, ok := changes["name"].(string); ok && name != t["name"] {
			if name == "" || filepath.Base(name) != name || name == "." || name == ".." {
				return fmt.Errorf("name must be a file name, not a path")
			}
			target = filepath.Join(filepath.Dir(target), name)
		}
		text, contentChanged := changes["content"].(string)
		contentChanged = contentChanged && text != t["content"]
		if fresh && !contentChanged {
			return fmt.Errorf("%s is not written yet: set its content to write it", old)
		}
		var data []byte
		if contentChanged {
			data = []byte(text)
			if _, err := Build(data); err != nil {
				return err
			}
		}
		if target != old {
			if other := s.document("", target); other != nil {
				return fmt.Errorf("destination already belongs to a document: %s", target)
			}
			if err := d.move(old, target); err != nil {
				return err
			}
			s.rollback = func() error { return d.move(target, old) }
		}
		if contentChanged {
			if err := s.writeContent(t, target, data, fresh); err != nil {
				return err
			}
		}
		for k, v := range changes {
			t[k] = v
		}
		t["file"], t["name"] = target, filepath.Base(target)
		s.Docs[t["id"].(string)], s.changed = documentMetadata(t), true
		if target != old {
			t["previous_file"] = old
		}
		out, err = d.read(t)
		return err
	})
	return out, err
}

// writeContent writes a document's new content to target inside a
// transaction: the rollback puts the old content (and the old place) back.
// A fresh document, one that named no file yet, is registered by the
// write, and its rollback removes the file that was not there before.
func (s *documentRecords) writeContent(t ochazuke.Thing, target string, data []byte, fresh bool) error {
	if err := writeNew(target, data, true); err != nil {
		return err
	}
	original, moved := t["content"].(string), s.rollback
	s.rollback = func() error {
		if fresh {
			return os.Remove(target)
		}
		err := writeNew(target, []byte(original), true)
		if moved != nil {
			err = errors.Join(err, moved())
		}
		return err
	}
	if !fresh {
		return nil
	}
	metadata, err := s.rememberFile(target, "")
	if err != nil {
		return err
	}
	t["id"] = metadata["id"]
	return nil
}

func (d *documentStore) delete(c ochazuke.Ctx, key string) error {
	t, err := d.get(c, key)
	if err != nil {
		return err
	}
	switch {
	case t["missing"] != true:
		if err := d.trash(context.Background(), t["file"].(string)); err != nil {
			return err
		}
	case t["id"] == t["file"]: // a new path: no file and no record
		return fmt.Errorf("%s is not written yet: there is nothing to delete", t["file"])
	}
	// A registered file that is gone leaves only its record to remove.
	return d.state.transaction(true, func(s *documentRecords) error { delete(s.Docs, t["id"].(string)); s.changed = true; return nil })
}
