package main

import (
	"fmt"

	"ofam.dev/ochazuke"
)

// Where the Settings page files o-json's settings: its own tab (owner
// ochazuke.OwnerApp, titled o-json), a few sections in this order, each
// setting named in exactly one of them. A setting declares through
// declareOwn, which gives it its section and group from this table, so
// the page never shows the one-setting sections the first-dot rule would
// make (structure, undo, rebuild, ...). TestEverySettingHasASection fails on
// a setting the table does not name.

// settingSection is one section of the o-json tab and the settings in it,
// by group (a heading inside the section; "" for none).
type settingSection struct {
	ochazuke.SettingSection
	groups []settingGroup
}

type settingGroup struct {
	title    string
	settings []string
}

var settingSections = []settingSection{
	{ochazuke.SettingSection{ID: "viewer", Title: "Viewer", Description: "Editing the structure in place, the trees, opening, pasting and undo"}, []settingGroup{
		{"", []string{settingEditable, settingStructureOrder, settingDeclaredModes, settingEditGate, settingRawEditor, settingRawPane, settingRawDetach, settingRawGrants, settingScalarFollow, settingLitWidth, settingObjectView, settingDuplicateTone, settingRecent, settingReloadPlace, settingEmptyHint, settingUndoAtStart}},
		{"Paste", []string{settingPasteOnTree, settingPasteText}},
	}},
	{ochazuke.SettingSection{ID: "data", Title: "Data", Description: "The data pane and the detail: value edits, the value editor, tables and nested data"}, []settingGroup{
		{"Values", []string{settingDataEditable, settingValueEditor, settingValueLeave, settingValueStableHeight, settingRebuildValueEdit}},
		{"Tables", []string{settingStableColumns, settingNested, settingHoverRows}},
		{"FLAT", []string{settingFlatDepthColors, settingFlatPadIndexes}},
	}},
	{ochazuke.SettingSection{ID: "query", Title: "Search and jq", Description: "The search line (/: substring, fuzzy, regex or jq), query mode and the jq panel"}, []settingGroup{
		{"", []string{settingQueryLine, settingSearchType, settingChipFocus, settingCompletePaths}},
		{"Panel", []string{settingJQPanel, settingJQPanelHeight}},
	}},
	{ochazuke.SettingSection{ID: "restructure", Title: "Restructure", Description: "The restructure page and the edits on the restructured tree"}, []settingGroup{
		{"", []string{settingLayout, settingPreview, settingNewKey, settingVisualPut, settingVisualWrap, settingVisualEdge, settingRepeatScope}},
	}},
	{ochazuke.SettingSection{ID: "reshape", Title: "Reshape", Description: "The reshape steps: flatten, key by and the marks they act on"}, []settingGroup{
		{"Result", []string{settingReshapeFocus}},
		{"Flatten", []string{settingFlattenArrays, settingFlattenJoin, settingFlattenNames}},
		{"Key by", []string{settingKeyByKeep, settingKeyByDup, settingKeyByMissing}},
		{"Marks", []string{settingMarksCount}},
	}},
	{ochazuke.SettingSection{ID: "export", Title: "Export", Description: "csv, tsv, jsonl and markdown exports: arrays, joins and nulls"}, []settingGroup{
		{"", []string{settingExportArrays, settingExportJoin, settingExportNull}},
	}},
}

// settingPlace is where settingSections files name: its section and group.
func settingPlace(name string) (section, group string, ok bool) {
	for _, s := range settingSections {
		for _, g := range s.groups {
			for _, n := range g.settings {
				if n == name {
					return s.ID, g.title, true
				}
			}
		}
	}
	return "", "", false
}

// declareSettingLayout declares the o-json tab and its sections.
func declareSettingLayout(r *ochazuke.Runtime) error {
	if err := r.DeclareSettingOwner(ochazuke.SettingOwner{ID: ochazuke.OwnerApp, Title: appName(), Description: "The viewer, data pane, jq, restructure, reshape steps and export"}); err != nil {
		return err
	}
	for _, s := range settingSections {
		one := s.SettingSection
		one.Owner = ochazuke.OwnerApp
		if err := r.DeclareSettingSection(one); err != nil {
			return err
		}
	}
	return nil
}

// declareOwn declares o-json's own settings, each in the section and group
// settingSections gives it.
func declareOwn(r *ochazuke.Runtime, settings ...ochazuke.Setting) error {
	for i, s := range settings {
		section, group, ok := settingPlace(s.Name)
		if !ok {
			return fmt.Errorf("setting %s has no section in settingSections", s.Name)
		}
		settings[i].Section, settings[i].Group = section, group
	}
	return r.DeclareSetting(settings...)
}
