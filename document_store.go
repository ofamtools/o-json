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
		fact("size", "integer", "Source size in bytes"),
		fact("modified", ochazuke.PropTime, "Source modification time"),
	}
}

// Both runtime entry points use this declaration and the same persisted state.
func registerDocuments(r *ochazuke.Runtime, current func() string, moved func(string, string)) *documentStore {
	state := &documentState{dir: func() string { return r.Config().StateDirectory() }}
	d := &documentStore{state: state, current: current, trash: trashDocument}
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

func (d *documentStore) get(_ ochazuke.Ctx, key string) (ochazuke.Thing, error) {
	if key == "" || key == "-" {
		return nil, fmt.Errorf("a document file or identity is required")
	}
	abs, err := filepath.Abs(key)
	if err != nil {
		return nil, err
	}
	var out ochazuke.Thing
	err = d.state.transaction(false, func(s *documentRecords) error {
		if t, ok := s.Docs[key]; ok {
			out = maps.Clone(t)
			return nil
		}
		for _, t := range s.Docs {
			if t["file"] == abs {
				out = maps.Clone(t)
				return nil
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	fresh := out == nil
	if fresh {
		out = ochazuke.Thing{"file": abs}
	}
	out, err = d.read(out)
	if err != nil {
		return nil, err
	}
	if fresh {
		var metadata ochazuke.Thing
		metadata, err = d.rememberFile(abs)
		if err == nil {
			maps.Copy(out, metadata)
		}
	}
	return out, err
}

// rememberFile imports metadata without reading source content. A list of
// recently parsed files must not parse every document again.
func (d *documentStore) rememberFile(path string) (ochazuke.Thing, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	var out ochazuke.Thing
	find := func(s *documentRecords) error {
		for _, t := range s.Docs {
			if t["file"] == abs {
				out = maps.Clone(t)
				break
			}
		}
		return nil
	}
	if err := d.state.transaction(false, find); err != nil {
		return nil, err
	}
	if out != nil {
		return out, nil
	}
	err = d.state.transaction(true, func(s *documentRecords) error {
		_ = find(s)
		if out != nil {
			return nil
		}
		var identity [12]byte
		if _, err := rand.Read(identity[:]); err != nil {
			return err
		}
		id := fmt.Sprintf("j%x", identity)
		out = ochazuke.Thing{"id": id, "file": abs, "name": filepath.Base(abs), "archived": false}
		s.Docs[id] = maps.Clone(out)
		return nil
	})
	return out, err
}
func (d *documentStore) read(t ochazuke.Thing) (ochazuke.Thing, error) {
	file := fmt.Sprint(t["file"])
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(file)
	if err != nil {
		return nil, err
	}
	t["content"], t["size"], t["modified"] = string(data), len(data), info.ModTime().UTC().Format(time.RFC3339)
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
	if from := ochazuke.StringArg(c.Args, "from", ""); from != "" {
		if _, err := os.Lstat(file); err == nil {
			ext := filepath.Ext(file)
			stem := strings.TrimSuffix(file, ext)
			for i := 1; ; i++ {
				file = fmt.Sprintf("%s.copy-%d%s", stem, i, ext)
				if _, err := os.Lstat(file); errors.Is(err, os.ErrNotExist) {
					break
				} else if err != nil {
					return nil, err
				}
			}
		}
	}
	content, ok := values["content"].(string)
	if !ok || content == "" {
		content = "{}\n"
	}
	if _, err := Build([]byte(content)); err != nil {
		return nil, err
	}
	if err := writeNew(file, []byte(content), false); err != nil {
		return nil, err
	}
	t, err := d.get(c, file)
	if err != nil {
		return nil, err
	}
	err = d.state.transaction(true, func(s *documentRecords) error {
		id := t["id"].(string)
		if requested, _ := values["id"].(string); requested != "" && requested != id {
			if _, exists := s.Docs[requested]; exists {
				return fmt.Errorf("document identity %s already exists", requested)
			}
			delete(s.Docs, id)
			id = requested
			t["id"] = id
		}
		for _, key := range []string{"created", "updated", "archived"} {
			if v, ok := values[key]; ok {
				t[key] = v
			}
		}
		s.Docs[id] = documentMetadata(t)
		return nil
	})
	return t, err
}
func documentMetadata(t ochazuke.Thing) ochazuke.Thing {
	out := maps.Clone(t)
	delete(out, "content")
	delete(out, "size")
	delete(out, "modified")
	delete(out, "previous_file")
	return out
}
func (d *documentStore) patch(c ochazuke.Ctx, key string, changes ochazuke.Thing) (ochazuke.Thing, error) {
	t, err := d.get(c, key)
	if err != nil {
		return nil, err
	}
	old := t["file"].(string)
	target := old
	if v, ok := changes["file"].(string); ok && v != "" {
		target, err = filepath.Abs(v)
		if err != nil {
			return nil, err
		}
	}
	if name, ok := changes["name"].(string); ok && name != t["name"] {
		if name == "" || filepath.Base(name) != name || name == "." || name == ".." {
			return nil, fmt.Errorf("name must be a file name, not a path")
		}
		target = filepath.Join(filepath.Dir(target), name)
	}
	if text, ok := changes["content"].(string); ok {
		if _, err := Build([]byte(text)); err != nil {
			return nil, err
		}
	}
	if target != old {
		if _, err := os.Lstat(target); err == nil {
			return nil, fmt.Errorf("destination already exists: %s", target)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err := moveDocumentFile(old, target, os.Link); err != nil {
			return nil, err
		}
	}
	if text, ok := changes["content"].(string); ok && text != t["content"] {
		if err := writeNew(target, []byte(text), true); err != nil {
			return nil, err
		}
	}
	for k, v := range changes {
		t[k] = v
	}
	t["file"], t["name"] = target, filepath.Base(target)
	err = d.state.transaction(true, func(s *documentRecords) error { s.Docs[t["id"].(string)] = documentMetadata(t); return nil })
	if err != nil {
		return nil, err
	}
	if target != old {
		t["previous_file"] = old
	}
	return d.read(t)
}
func (d *documentStore) delete(c ochazuke.Ctx, key string) error {
	t, err := d.get(c, key)
	if err != nil {
		return err
	}
	if err := d.trash(context.Background(), t["file"].(string)); err != nil {
		return err
	}
	return d.state.transaction(true, func(s *documentRecords) error { delete(s.Docs, t["id"].(string)); return nil })
}
