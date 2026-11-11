package main

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

func TestDocumentPageUsesStoreIndexAndHydratesSelectedJSON(t *testing.T) {
	ext := newShell(strings.NewReader(""), io.Discard, io.Discard)
	ext.Runtime.Config().Disable()
	if _, _, found := ext.Runtime.ResolveAction("doc.documents"); found {
		t.Fatal("document listing still has a second application handler")
	}
	raw, err := ocli.Catalog(ext.Runtime, "o-json", "test")
	if err != nil {
		t.Fatal(err)
	}
	var catalog ocli.CatalogDoc
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	var page ocli.CatalogPage
	for _, candidate := range catalog.Pages {
		if candidate.ID == "documents" {
			page = candidate
			break
		}
	}
	if len(page.Lists) != 1 || page.Lists[0].Kind != ocli.ListKindIndex || page.Lists[0].Source != "" {
		t.Fatalf("JSON page does not use the store-backed index: %+v", page)
	}

	host := ochazuke.NewRuntime()
	host.Config().Disable()
	d := registerDocuments(host, nil, nil)
	root := t.TempDir()
	var records []ochazuke.Thing
	names := []string{"first.json", "second.json"}
	for i, value := range []string{"first-payload", "second-payload"} {
		doc, err := d.create(ochazuke.Ctx{}, ochazuke.Thing{"file": filepath.Join(root, names[i]), "content": `{"payload":"` + value + `"}`})
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, doc)
	}
	m := ochazuke.NewModel(host)
	m.Width, m.Height = 160, 40
	view, err := ocli.MountPage(m, catalog, page, ocli.PageConfig{FetchRecord: func(_ context.Context, _ string, key string) ([]byte, error) {
		doc, err := d.get(ochazuke.Ctx{}, key)
		if err != nil {
			return nil, err
		}
		return json.Marshal(doc)
	}})
	if err != nil {
		t.Fatal(err)
	}
	list := view.List("documents/list")
	if list == nil || list.From() != "doc" {
		t.Fatalf("JSON list is not bound to the document store: %v", list)
	}
	for i, doc := range records {
		id := doc["id"].(string)
		if err := view.OpenRow("doc", id); err != nil {
			t.Fatal(err)
		}
		if err := ocli.Settle(host, nil); err != nil {
			t.Fatal(err)
		}
		selected, ok := list.Selected()
		if !ok || selected.ID != "documents/list/"+id || len(list.Items()) != 2 {
			t.Fatalf("native record open lost the store selection: %+v (%v), rows %v", selected, ok, list.Items())
		}
		row, ok := view.Row(selected.ID)
		data, hasData := row.Data.(map[string]any)
		if !ok || !hasData || data["content"] != doc["content"] {
			t.Fatalf("selected JSON content was not hydrated: %+v", row)
		}
		frame := m.View().Content
		want, other := "first-payload", "second-payload"
		if i == 1 {
			want, other = other, want
		}
		if !strings.Contains(frame, want) || strings.Contains(frame, other) {
			t.Fatalf("JSON detail did not follow the selected record %s:\n%s", id, frame)
		}
	}
}
