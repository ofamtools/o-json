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
	r.Define("doc").Verb("open", func(c ochazuke.Ctx) ochazuke.Result {
		if path := ochazuke.StringArg(c.Args, "path", ""); open != nil && path != "" {
			return open(path)
		}
		key := subject(c)
		if key == "" {
			return ochazuke.Failed(c.Need("path", "choose a JSON file or document"))
		}
		// Resolve identities off the loop, then keep the viewer's unsaved-edit
		// checks on the loop or launch the standalone viewer from the CLI.
		var file string
		return ochazuke.Pending(func(context.Context) (any, error) {
			var err error
			file, err = d.filePath(key)
			return nil, err
		}).Then(func(c ochazuke.Ctx, _ any, err error) {
			if err != nil {
				return
			}
			var request ochazuke.Request
			if open != nil {
				request = ochazuke.Request{Noun: "viewer", Verb: "open", Target: pageID, Args: ochazuke.Arguments{"path": file}}
			} else {
				request = ochazuke.Request{Noun: "doc", Verb: "launch", Args: ochazuke.Arguments{"file": file}}
			}
			if _, err := c.Runtime.Execute(request); err != nil {
				c.Error(err.Error())
			}
		})
	}, "Open the document in the JSON viewer (not the system opener)").Positional("path").OpensPage("documents").Foreground().Fetch()
	documentParams(r, "doc.open", map[string]ochazuke.Parameter{"id": documentSubject(), "path": {Type: "path", Path: "file", Covers: true, Description: "JSON file to open"}, "file": {Type: "path", Path: "file", Covers: true, Description: "JSON source file"}})
	if open == nil {
		r.Define("doc").Verb("launch", func(c ochazuke.Ctx) ochazuke.Result {
			exe, err := os.Executable()
			if err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Foreground(exec.Command(exe, "--data", d.state.dir(), "view", ochazuke.StringArg(c.Args, "file", "")))
		}, "Run the standalone JSON viewer in the foreground").Foreground().Fetch()
		documentParams(r, "doc.launch", map[string]ochazuke.Parameter{"file": {Type: "path", Path: "file", Required: true}})
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

func sessionEnv() string {
	if s := os.Getenv("MTM_SESSION"); s != "" {
		return s
	}
	return os.Getenv("TUIOS_SESSION")
}

func declareDocumentPage(r *ochazuke.Runtime) {
	if err := ocli.DeclarePage(r, ocli.CatalogPage{
		ID: "documents", Title: "JSON", Layout: ocli.LayoutSplit,
		Lists: []ocli.CatalogList{{ID: "documents/list", Noun: "doc", Title: "Documents", Source: "doc.documents"}},
		Detail: &ocli.CatalogDetail{Sections: []ocli.CatalogDetailSection{
			{Kind: "fields", Fields: []ocli.CatalogDetailField{{Label: "File", Field: "file", Format: "path"}, {Label: "Modified", Field: "modified", Format: "time"}}},
			{Kind: "json", Field: "content", Parse: true},
			{Kind: "related"}, {Kind: "history"},
		}},
	}); err != nil {
		panic(err)
	}
}
