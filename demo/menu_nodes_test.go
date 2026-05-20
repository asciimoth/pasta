package main

import (
	"testing"

	"github.com/asciimoth/pasta/pasta"
)

func TestMenuDemoNodesExposeAndApplyRepeats(t *testing.T) {
	for _, className := range []string{MenuImmediateClass, MenuCommittableClass} {
		t.Run(className, func(t *testing.T) {
			w := pasta.NewWorkspace()
			if err := w.RegisterLibrary(MenuLibrary{}); err != nil {
				t.Fatalf("RegisterLibrary() error = %v", err)
			}
			node, err := w.CreateNode(className, pasta.NodeOptions{})
			if err != nil {
				t.Fatalf("CreateNode(%s) error = %v", className, err)
			}
			menu, ok := w.NodeMenu(node)
			if !ok {
				t.Fatal("menu missing")
			}
			repeat := findDemoRepeat(t, menu)
			if len(repeat.Template) != 4 || len(repeat.Items) != 2 {
				t.Fatalf("repeat shape = %#v", repeat)
			}
			if repeat.Items[0].Fields[0].Value != "alpha" {
				t.Fatalf("first repeat item = %#v", repeat.Items[0])
			}

			updated, err := w.UpdateNodeMenuState(node, pasta.MenuStateUpdate{
				Repeats: []pasta.MenuRepeatUpdate{{
					Block:  "main",
					Repeat: "rows",
					Items: []pasta.MenuRepeatItemState{{
						ID:    "only",
						Title: "Only row",
						Fields: map[string]any{
							"name":     "delta",
							"quantity": int64(7),
							"active":   true,
						},
					}},
				}},
			})
			if err != nil {
				t.Fatalf("UpdateNodeMenuState(repeat) error = %v", err)
			}
			repeat = findDemoRepeat(t, updated)
			if len(repeat.Items) != 1 {
				t.Fatalf("updated item count = %d, want 1", len(repeat.Items))
			}
			item := repeat.Items[0]
			if item.ID != "only" || item.Title != "Only row" || item.Fields[0].Value != "delta" || item.Fields[1].Value != int64(7) || item.Fields[2].Value != true {
				t.Fatalf("updated repeat item = %#v", item)
			}
		})
	}
}

func findDemoRepeat(t *testing.T, menu pasta.NodeMenu) pasta.MenuRepeat {
	t.Helper()
	for _, block := range menu.Blocks {
		if block.ID != "main" {
			continue
		}
		for _, repeat := range block.Repeats {
			if repeat.ID == "rows" {
				return repeat
			}
		}
	}
	t.Fatal("rows repeat missing")
	return pasta.MenuRepeat{}
}
