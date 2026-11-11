package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"ofam.dev/ochazuke"
)

func persistentDocumentStore(dir string) *documentStore {
	return &documentStore{
		state: &documentState{dir: func() string { return dir }},
		move:  func(from, to string) error { return moveDocumentFile(from, to, os.Link) },
	}
}

func TestDocumentExclusivePublicationKeepsCompetingFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "claimed.json")
	err := writeAtomic(file, false, func(w io.Writer) error {
		if _, err := io.WriteString(w, `{"new":true}`); err != nil {
			return err
		}
		// Another writer claims the destination after our temporary bytes are
		// ready, but before publication. No timing or scheduler luck is needed.
		return os.WriteFile(file, []byte(`{"winner":true}`), 0600)
	})
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("publication accepted an occupied destination: %v", err)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != `{"winner":true}` {
		t.Fatalf("competing file was replaced: %q %v", data, err)
	}
}

func TestDocumentContendingCreatesDoNotReplace(t *testing.T) {
	root := t.TempDir()
	dir, file := filepath.Join(root, "state"), filepath.Join(root, "new.json")
	type result struct {
		content string
		err     error
	}
	const writers = 8
	ready, results := make(chan struct{}), make(chan result, writers)
	for i := range writers {
		d := persistentDocumentStore(dir)
		go func() {
			<-ready
			content := fmt.Sprintf(`{"writer":%d}`, i)
			_, err := d.create(ochazuke.Ctx{}, ochazuke.Thing{"file": file, "content": content})
			results <- result{content, err}
		}()
	}
	close(ready)
	var winner string
	for range writers {
		r := <-results
		if r.err == nil {
			if winner != "" {
				t.Error("more than one create claimed the same file")
			}
			winner = r.content
		} else if !errors.Is(r.err, os.ErrExist) {
			t.Errorf("unexpected create error: %v", r.err)
		}
	}
	if winner == "" {
		t.Fatal("no create succeeded")
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != winner {
		t.Fatalf("winner was overwritten: %q %v", data, err)
	}
	rows, err := persistentDocumentStore(dir).list(ochazuke.Ctx{}, ochazuke.ListQuery{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("durable documents: %v %v", rows, err)
	}
}

func TestDocumentContendingDuplicatesRetryOccupiedNames(t *testing.T) {
	root := t.TempDir()
	dir, file := filepath.Join(root, "state"), filepath.Join(root, "source.json")
	d := persistentDocumentStore(dir)
	original, err := d.create(ochazuke.Ctx{}, ochazuke.Thing{"file": file, "content": "[1]"})
	if err != nil {
		t.Fatal(err)
	}
	occupied := filepath.Join(root, "source.copy-1.json")
	if err := os.WriteFile(occupied, []byte("[99]"), 0600); err != nil {
		t.Fatal(err)
	}
	type result struct {
		doc ochazuke.Thing
		err error
	}
	const writers = 8
	ready, results := make(chan struct{}), make(chan result, writers)
	for range writers {
		copyStore := persistentDocumentStore(dir)
		go func() {
			<-ready
			doc, err := copyStore.create(ochazuke.Ctx{Args: ochazuke.Arguments{"from": original["id"]}}, ochazuke.Thing{"file": file, "content": "[1]"})
			results <- result{doc, err}
		}()
	}
	close(ready)
	ids, paths := map[any]bool{original["id"]: true}, map[string]bool{file: true, occupied: true}
	for range writers {
		r := <-results
		if r.err != nil {
			t.Errorf("duplicate: %v", r.err)
			continue
		}
		path := r.doc["file"].(string)
		if ids[r.doc["id"]] || paths[path] {
			t.Errorf("duplicate reused an identity or file: %v", r.doc)
		}
		ids[r.doc["id"]], paths[path] = true, true
		if data, err := os.ReadFile(path); err != nil || string(data) != "[1]" {
			t.Errorf("copied content: %q %v", data, err)
		}
	}
	if data, err := os.ReadFile(occupied); err != nil || string(data) != "[99]" {
		t.Fatalf("unregistered competing file changed: %q %v", data, err)
	}
	rows, err := d.list(ochazuke.Ctx{}, ochazuke.ListQuery{})
	if err != nil || len(rows) != writers+1 {
		t.Fatalf("durable copies: %v %v", rows, err)
	}
}

func TestDocumentMoveAndDestinationImportShareLock(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "state")
	d := persistentDocumentStore(dir)
	file, target := filepath.Join(root, "before.json"), filepath.Join(root, "after.json")
	created, err := d.create(ochazuke.Ctx{}, ochazuke.Thing{"file": file, "content": "[1]"})
	if err != nil {
		t.Fatal(err)
	}
	moved, release := make(chan error, 1), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	d.move = func(from, to string) error {
		if err := moveDocumentFile(from, to, os.Link); err != nil {
			return err
		}
		lock, err := os.OpenFile(filepath.Join(dir, "documents.lock"), os.O_RDWR, 0600)
		if err == nil {
			err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
			if err == nil {
				_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
				err = errors.New("filesystem moved outside the metadata lock")
			} else if errors.Is(err, syscall.EWOULDBLOCK) {
				err = nil
			}
			_ = lock.Close()
		}
		moved <- err
		<-release
		return nil
	}
	type result struct {
		doc ochazuke.Thing
		err error
	}
	patched := make(chan result, 1)
	go func() {
		doc, err := d.patch(ochazuke.Ctx{}, created["id"].(string), ochazuke.Thing{"file": target})
		patched <- result{doc, err}
	}()
	select {
	case err := <-moved:
		if err != nil {
			t.Error(err)
		}
	case r := <-patched:
		t.Fatalf("move failed before the destination transition: %v", r.err)
	}
	reading, read := make(chan struct{}), make(chan result, 1)
	go func() {
		close(reading)
		doc, err := persistentDocumentStore(dir).get(ochazuke.Ctx{}, target)
		read <- result{doc, err}
	}()
	<-reading
	unblock()
	for _, r := range []result{<-patched, <-read} {
		if r.err != nil || r.doc["id"] != created["id"] || r.doc["file"] != target {
			t.Errorf("move/import lost identity: %v %v", r.doc, r.err)
		}
	}
	rows, err := persistentDocumentStore(dir).list(ochazuke.Ctx{}, ochazuke.ListQuery{})
	if err != nil || len(rows) != 1 || rows[0]["id"] != created["id"] {
		t.Fatalf("move imported another durable identity: %v %v", rows, err)
	}
}

func TestDocumentMetadataFailureRollsBackFilesAndIdentity(t *testing.T) {
	for _, change := range []string{"move", "content", "move-and-content"} {
		t.Run(change, func(t *testing.T) {
			_, d, call := documentFixture(t)
			root := t.TempDir()
			dir := filepath.Join(root, "state")
			d.state.dir = func() string { return dir }
			file, target := filepath.Join(root, "before.json"), filepath.Join(root, "after.json")
			created := record(t, call("new", ochazuke.Arguments{"file": file, "content": "[1]"}))
			id := created["id"].(string)
			call("annotate", ochazuke.Arguments{"id": id, "text": "keep identity"})
			call("relate", ochazuke.Arguments{"id": id, "to": "project:temp", "kind": "document"})
			before, err := os.ReadFile(filepath.Join(dir, "documents.json"))
			if err != nil {
				t.Fatal(err)
			}
			changes := ochazuke.Thing{}
			if change != "content" {
				changes["file"] = target
			}
			if change != "move" {
				changes["content"] = "[2]"
			}
			refused := errors.New("metadata commit refused")
			d.state.persist = func(string, []byte) error { return refused }
			if _, err := d.patch(ochazuke.Ctx{}, id, changes); !errors.Is(err, refused) {
				t.Fatalf("metadata failure: %v", err)
			}
			d.state.persist = nil
			if data, err := os.ReadFile(file); err != nil || string(data) != "[1]" {
				t.Errorf("original file not restored: %q %v", data, err)
			}
			if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("failed move retained destination: %v", err)
			}
			after, err := os.ReadFile(filepath.Join(dir, "documents.json"))
			if err != nil || !bytes.Equal(before, after) {
				t.Errorf("metadata changed after failed commit: %s %v", after, err)
			}
			got, err := persistentDocumentStore(dir).get(ochazuke.Ctx{}, file)
			if err != nil || got["id"] != id {
				t.Errorf("restored file lost durable identity: %v %v", got, err)
			}
			notes, err := d.state.NotesForTest()
			if err != nil || len(notes) != 1 || notes[0]["about"] != ochazuke.AnyRef("doc", id) {
				t.Errorf("annotations changed: %v %v", notes, err)
			}
			relations, err := d.state.Relations(ochazuke.RelationQuery{Thing: ochazuke.AnyRef("doc", id)})
			if err != nil || len(relations) != 1 {
				t.Errorf("relations changed: %v %v", relations, err)
			}
		})
	}
}

func TestDocumentCreateCommitFailureRemovesOnlyNewFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "state")
	d := persistentDocumentStore(dir)
	file := filepath.Join(root, "original.json")
	original, err := d.create(ochazuke.Ctx{}, ochazuke.Thing{"file": file, "content": "[1]"})
	if err != nil {
		t.Fatal(err)
	}
	refused := errors.New("metadata commit refused")
	d.state.persist = func(string, []byte) error { return refused }
	for _, duplicate := range []bool{false, true} {
		file := filepath.Join(root, "new.json")
		ctx := ochazuke.Ctx{}
		if duplicate {
			file = original["file"].(string)
			ctx.Args = ochazuke.Arguments{"from": original["id"]}
		}
		if _, err := d.create(ctx, ochazuke.Thing{"file": file, "content": "[2]"}); !errors.Is(err, refused) {
			t.Fatalf("create metadata failure: %v", err)
		}
	}
	for _, name := range []string{"new.json", "original.copy-1.json"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("failed creation retained %s: %v", name, err)
		}
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "[1]" {
		t.Fatalf("failed creation damaged original: %q %v", data, err)
	}
	rows, err := persistentDocumentStore(dir).list(ochazuke.Ctx{}, ochazuke.ListQuery{})
	if err != nil || len(rows) != 1 || rows[0]["id"] != original["id"] {
		t.Fatalf("failed creation changed durable registry: %v %v", rows, err)
	}
}

func TestDocumentViewerOpenPreservesStdin(t *testing.T) {
	a := empty(t)
	a.Stdin = strings.NewReader(`{"stdin":[1,2]}`)
	docDo(t, a, "open", ochazuke.Arguments{"path": "-"})
	if a.Doc == nil || a.Path != "-" || a.LoadErr != nil {
		t.Fatalf("doc.open stdin: path %q, error %v, doc %v", a.Path, a.LoadErr, a.Doc)
	}
	if got := Compact(a.Doc.Root, 0); got != `{"stdin":[1,2]}` {
		t.Fatalf("stdin bytes were not loaded: %s", got)
	}
	rows, err := a.documents.list(ochazuke.Ctx{}, ochazuke.ListQuery{})
	if err != nil || len(rows) != 0 {
		t.Fatalf("stdin became a fake file record: %v %v", rows, err)
	}
}
