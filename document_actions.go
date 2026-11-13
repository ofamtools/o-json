package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

func documentSubject() ochazuke.Parameter {
	return ochazuke.Parameter{Type: "string", Noun: "doc", Free: true, Focus: true, Description: "Document identity or JSON file"}
}
func hostSubject(noun string, required bool) ochazuke.Parameter {
	return ochazuke.Parameter{Type: "string", Noun: noun, HostNoun: noun, Free: true, Required: required}
}
func documentParams(r *ochazuke.Runtime, key string, params map[string]ochazuke.Parameter) {
	op := r.Operations.At(ochazuke.Key(r.CanonicalKey(key)))
	op.Parameters = params
	r.Operations.Set(op)
}

func registerDocumentActions(r *ochazuke.Runtime, d *documentStore, open func(string) ochazuke.Result) {
	subject := func(c ochazuke.Ctx) string {
		for _, p := range []string{"id", "file", "path"} {
			if v := ochazuke.StringArg(c.Args, p, ""); v != "" {
				return v
			}
		}
		if d.current != nil {
			return d.current()
		}
		return ""
	}
	// launch runs the standalone viewer on a source from the command line,
	// where no viewer is running: the child reads the source itself (its
	// standard input is this process's).
	launch := func(source string) ochazuke.Result {
		exe, err := os.Executable()
		if err != nil {
			return ochazuke.Failed(err)
		}
		return ochazuke.Foreground(exec.Command(exe, viewerArgs(d.state.dir(), source)...))
	}
	cli := open == nil
	if cli {
		open = launch
	}
	r.Define("doc").Verb("open", func(c ochazuke.Ctx) ochazuke.Result {
		// The source by any of its names: path (the word), file (as the
		// other doc verbs say it) or target (as the Open line's opener
		// rows hand a viewer its text).
		source := expandHome(ochazuke.StringArg(c.Args, "path", ochazuke.StringArg(c.Args, "file", ochazuke.StringArg(c.Args, "target", ""))))
		id := ochazuke.StringArg(c.Args, "id", "")
		if source != "" || id == "" {
			// A source as given: a file, - (standard input), a path that
			// names no file yet, or nothing at all. None is refused.
			return open(source)
		}
		// Resolve an identity off the loop, then open its file as a source.
		var file string
		return ochazuke.Pending(func(context.Context) (any, error) {
			var err error
			file, err = d.filePath(id)
			return nil, err
		}).Then(func(c ochazuke.Ctx, _ any, err error) {
			if err != nil {
				return
			}
			if _, err := c.Runtime.Execute(ochazuke.Request{Noun: "doc", Verb: "open", Args: ochazuke.Arguments{"path": file}}); err != nil {
				c.Error(err.Error())
			}
		})
	}, "Open a document in the JSON viewer (not the system opener): a JSON file, - for standard input, a path that names no file yet (a new, named, unsaved document), or with no source an empty unnamed document").Positional("path").OpensPage("documents").Foreground().Fetch()
	sourceFile := ochazuke.Parameter{Type: ochazuke.PropPath, Path: "file", New: true, Covers: true}
	described := func(p ochazuke.Parameter, text string) ochazuke.Parameter { p.Description = text; return p }
	plumbing := func(p ochazuke.Parameter) ochazuke.Parameter { p.Plumbing = true; return p }
	// The identity is a choice, never a subject the framework must ask
	// for: it is not filled from the focus, and like a source it covers
	// the verb alone (Covers, which keeps an optional parameter out of
	// subject.ask). With nothing given, doc.open opens an empty document.
	known := documentSubject()
	known.Focus, known.Covers, known.Description = false, true, "a document o-json knows, by identity, instead of a source"
	documentParams(r, "doc.open", map[string]ochazuke.Parameter{"id": known,
		"path":   described(sourceFile, "the source: a JSON file, - for standard input, or a path that names no file yet (a new document of that name); omit for an empty unnamed document"),
		"file":   described(sourceFile, "the source, by the name the other doc verbs give their file"),
		"target": plumbing(described(sourceFile, "the source, as the Open line's opener rows hand it over (open.openers: action viewer, to doc.open)"))})
	if cli {
		r.Define("doc").Verb("launch", func(c ochazuke.Ctx) ochazuke.Result { return launch(ochazuke.StringArg(c.Args, "file", "")) },
			"Run the standalone JSON viewer in the foreground on a source (what doc.open does from the command line)").Positional("file").Foreground().Fetch()
		documentParams(r, "doc.launch", map[string]ochazuke.Parameter{"file": described(sourceFile, "the source: a JSON file, - for standard input, a new path, or none for an empty document")})
	}
	r.Define("doc").Verb("move", func(c ochazuke.Ctx) ochazuke.Result {
		value, err := r.Execute(ochazuke.Request{Noun: "doc", Verb: "set", Args: ochazuke.Arguments{"id": subject(c), "props": map[string]any{"file": ochazuke.StringArg(c.Args, "to", "")}}})
		if err != nil {
			return ochazuke.Failed(err)
		}
		return ochazuke.Done(value)
	}, "Move the JSON file on disk; refuse an existing destination and preserve its document identity").Positional("id", "to").
		Verb("rename", func(c ochazuke.Ctx) ochazuke.Result {
			value, err := r.Execute(ochazuke.Request{Noun: "doc", Verb: "set", Args: ochazuke.Arguments{"id": subject(c), "props": map[string]any{"name": ochazuke.StringArg(c.Args, "name", "")}}})
			if err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Done(value)
		}, "Rename the JSON file in its folder, without replacing another file").Positional("id", "name")
	documentParams(r, "doc.move", map[string]ochazuke.Parameter{"id": documentSubject(), "to": {Type: "path", Path: "file", New: true, Required: true}})
	documentParams(r, "doc.rename", map[string]ochazuke.Parameter{"id": documentSubject(), "name": {Type: "string", Required: true, Edits: "name"}})

	// The host owns panes, projects and notes; all requests cross its rights and
	// history boundary through Host.Call, never a daemon or an installed binary.
	hostCall := func(c ochazuke.Ctx, fn func(context.Context, ocli.Host, ochazuke.Thing) (any, error)) ochazuke.Result {
		key := subject(c)
		if key == "" {
			return ochazuke.Failed(c.Need("id", "choose a document"))
		}
		host, ok, err := ocli.HostFromEnv()
		if err != nil {
			return ochazuke.Failed(err)
		}
		if !ok {
			return ochazuke.Failed(fmt.Errorf("this action requires a host with panes, notes and projects (OCHAZUKE_HOST)"))
		}
		return ochazuke.Pending(func(ctx context.Context) (any, error) {
			t, err := d.get(c, key)
			if err != nil {
				return nil, err
			}
			value, err := fn(ctx, host, t)
			if raw, ok := value.([]byte); ok && err == nil {
				var decoded any
				err = json.Unmarshal(raw, &decoded)
				return decoded, err
			}
			return value, err
		})
	}
	session := func(c ochazuke.Ctx) string { return ochazuke.StringArg(c.Args, "session", sessionEnv()) }
	r.Define("doc").Verb("edit-pane", func(c ochazuke.Ctx) ochazuke.Result {
		return hostCall(c, func(ctx context.Context, host ocli.Host, t ochazuke.Thing) (any, error) {
			argv := editorCommand(r)
			file := t["file"].(string)
			return host.Call(ctx, "pane", "open", ochazuke.Arguments{"session": session(c), "cwd": filepath.Dir(file), "name": t["name"], "command": append(argv, file)})
		})
	}, "Open the source file in a new host pane with raw.editor, VISUAL, EDITOR, or nvim; editor writes are explicit source writes").
		Verb("send-pane", func(c ochazuke.Ctx) ochazuke.Result {
			return hostCall(c, func(ctx context.Context, host ocli.Host, t ochazuke.Thing) (any, error) {
				return host.Call(ctx, "pane", "send-text", ochazuke.Arguments{"session": session(c), "window": c.Args["pane"], "text": t["content"], "paste": true, "submit": false})
			})
		}, "Paste the document's JSON into a host pane without submitting it").Reciprocal("pane", "receive-json-doc", "doc").
		Verb("send-note", func(c ochazuke.Ctx) ochazuke.Result {
			return hostCall(c, func(ctx context.Context, host ocli.Host, t ochazuke.Thing) (any, error) {
				about := ochazuke.AnyRef("json.doc", t["id"].(string))
				if note := ochazuke.StringArg(c.Args, "note", ""); note != "" {
					raw, err := host.Call(ctx, "note", "get", ochazuke.Arguments{"id": note})
					if err != nil {
						return nil, err
					}
					var old map[string]any
					if err := json.Unmarshal(raw, &old); err != nil {
						return nil, err
					}
					text, _ := old["text"].(string)
					return host.Call(ctx, "note", "set", ochazuke.Arguments{"id": note, "props": map[string]any{"text": strings.TrimRight(text, "\n") + "\n\n" + t["content"].(string)}})
				}
				return host.Call(ctx, "note", "new", ochazuke.Arguments{"text": t["content"], "about": about, "project": ochazuke.StringArg(c.Args, "project", "")})
			})
		}, "Append JSON to a chosen host note, or create a note about this document").Reciprocal("note", "receive-json-doc", "doc").
		Verb("add-project", func(c ochazuke.Ctx) ochazuke.Result {
			return hostCall(c, func(ctx context.Context, host ocli.Host, t ochazuke.Thing) (any, error) {
				return host.Call(ctx, "relation", "create", ochazuke.Arguments{"from": ochazuke.AnyRef("project", ochazuke.StringArg(c.Args, "project", "")), "to": ochazuke.AnyRef("json.doc", t["id"].(string)), "kind": "document"})
			})
		}, "Relate a host project and this document; both ends retain the same relation").Reciprocal("project", "add-json-doc", "doc")
	documentParams(r, "doc.edit-pane", map[string]ochazuke.Parameter{"id": documentSubject(), "session": hostSubject("session", false)})
	documentParams(r, "doc.send-pane", map[string]ochazuke.Parameter{"id": documentSubject(), "pane": hostSubject("pane", true), "session": hostSubject("session", false)})
	documentParams(r, "doc.send-note", map[string]ochazuke.Parameter{"id": documentSubject(), "note": hostSubject("note", false), "project": hostSubject("project", false)})
	documentParams(r, "doc.add-project", map[string]ochazuke.Parameter{"id": documentSubject(), "project": hostSubject("project", true)})
	if err := r.AddActionAlias(ochazuke.ActionAlias{Alias: "doc.new", Action: "doc.create"}); err != nil {
		panic(err)
	}
	setHistory(r, ochazuke.HistoryChange, "doc.move", "doc.rename", "doc.edit-pane", "doc.send-pane", "doc.send-note", "doc.add-project")
}

// viewerArgs is the standalone viewer's command line for a source: a file,
// - (standard input), a path that names no file yet, or "" for none.
func viewerArgs(state, source string) []string {
	args := []string{"--data", state, "view"}
	if source != "" {
		args = append(args, source)
	}
	return args
}

func sessionEnv() string {
	return paneEnv(os.Getenv, "SESSION")
}

func declareDocumentPage(r *ochazuke.Runtime) {
	if err := ocli.DeclarePage(r, ocli.CatalogPage{
		ID: "documents", Title: "JSON", Layout: ocli.LayoutSplit,
		Lists: []ocli.CatalogList{{ID: "documents/list", Noun: "doc", Title: "Documents", Kind: ocli.ListKindIndex}},
		Detail: &ocli.CatalogDetail{Sections: []ocli.CatalogDetailSection{
			{Kind: "fields", Fields: []ocli.CatalogDetailField{{Label: "File", Field: "file", Format: "path"}, {Label: "Modified", Field: "modified", Format: "time"}}},
			{Kind: "json", Field: "content", Parse: true},
			{Kind: "related"}, {Kind: "history"},
		}},
	}); err != nil {
		panic(err)
	}
}
