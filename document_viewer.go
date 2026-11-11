package main

import (
	"path/filepath"
)

func registerDocumentsForViewer(a *App) {
	a.documents = registerDocuments(a.r, func() string { return a.Path }, func(old, file string) {
		current, _ := filepath.Abs(a.Path)
		if a.Path != "" && current == old {
			a.Path = file
			a.r.Instances[pageID].Data = map[string]any{"document": file}
			if a.Doc != nil {
				a.Doc.Name = filepath.Base(file)
			}
			if page := a.r.PageOf(pageID); page != nil {
				page.Title = filepath.Base(file)
			}
		}
	})
	registerDocumentActions(a.r, a.documents, a.openSource)
}

func (a *App) rememberDocument(path string) error {
	if path == "" || path == "-" || a.documents == nil {
		return nil
	}
	_, err := a.documents.rememberFile(path)
	return err
}
