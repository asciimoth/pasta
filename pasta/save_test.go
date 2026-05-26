package pasta_test

import (
	"reflect"
	"testing"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/configer/hujson"
	"github.com/asciimoth/pasta/pasta"
)

type saveNode struct {
	workspaceNode
	onSave func(configer.Config) error
}

func (n *saveNode) OnSave(cfg configer.Config) error {
	if n.onSave != nil {
		return n.onSave(cfg)
	}
	return n.workspaceNode.OnSave(cfg)
}

func TestWorkspaceSaveConfigPreservesNodeStateAndRewritesOwnedKeys(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})
	if err := w.AddNodeClass(testNodeClass{
		name: "example.com/Source",
		params: pasta.NodeClassParams{
			PrimaryType: "example.com/number",
			InitialPorts: []pasta.Port{{
				Direction: "right",
				Name:      "output",
				Types:     []string{"example.com/number"},
			}},
		},
	}); err != nil {
		t.Fatalf("AddNodeClass source: %v", err)
	}
	if err := w.AddNodeClass(testNodeClass{
		name: "example.com/Target",
		params: pasta.NodeClassParams{
			InitialPorts: []pasta.Port{{
				Direction: "left",
				Name:      "input A",
				Types:     []string{"example.com/number"},
			}},
		},
	}); err != nil {
		t.Fatalf("AddNodeClass target: %v", err)
	}

	source := &saveNode{onSave: func(cfg configer.Config) error {
		if err := cfg.Set(configer.Path{"Class"}, "example.com/Wrong"); err != nil {
			return err
		}
		if err := cfg.Set(configer.Path{"Primary"}, "example.com/wrong"); err != nil {
			return err
		}
		if err := cfg.Set(configer.Path{"Links"}, []string{"wrong -> [missing] input"}); err != nil {
			return err
		}
		return cfg.Set(configer.Path{"value"}, "kept")
	}}
	sourceID, err := w.AddNode(source, "example.com/Source", "source")
	if err != nil {
		t.Fatalf("AddNode source: %v", err)
	}
	if err := w.SetNodePrimary(sourceID, "example.com/number"); err != nil {
		t.Fatalf("SetNodePrimary: %v", err)
	}
	if err := w.SetNodePosition(sourceID, "10 20"); err != nil {
		t.Fatalf("SetNodePosition: %v", err)
	}
	targetID, err := w.AddNode(&workspaceNode{}, "example.com/Target", "target")
	if err != nil {
		t.Fatalf("AddNode target: %v", err)
	}
	sourcePort, err := w.AddPort(pasta.Port{
		Node:      sourceID,
		Direction: "right",
		Name:      "output",
		Types:     []string{"example.com/number"},
	})
	if err != nil {
		t.Fatalf("AddPort source: %v", err)
	}
	targetPort, err := w.AddPort(pasta.Port{
		Node:      targetID,
		Direction: "left",
		Name:      "input A",
		Types:     []string{"example.com/number"},
	})
	if err != nil {
		t.Fatalf("AddPort target: %v", err)
	}
	if _, _, err := w.AddLink(sourcePort, targetPort); err != nil {
		t.Fatalf("AddLink: %v", err)
	}

	cfg := configer.NewMemory(map[string]any{
		"source": map[string]any{
			"Class":   "example.com/Old",
			"Primary": "example.com/old",
			"Pos":     "old",
			"Links":   []any{"stale -> [target] input A"},
			"custom":  "preserved",
		},
		"target": map[string]any{
			"Class":   "example.com/OldTarget",
			"Primary": "example.com/old",
			"unknown": "preserved",
		},
		"deleted node": map[string]any{"Class": "example.com/Deleted"},
	})

	if err := w.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	want := map[string]any{
		"source": map[string]any{
			"Class":  "example.com/Source",
			"Pos":    "10 20",
			"Links":  []any{"output -> [target] input A"},
			"custom": "preserved",
			"value":  "kept",
		},
		"target": map[string]any{
			"Class":   "example.com/Target",
			"unknown": "preserved",
		},
	}
	if got := cfg.Snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("saved config = %#v, want %#v", got, want)
	}
}

func TestWorkspaceSaveHuJSONPreservesCommentsOnUpdatedKeys(t *testing.T) {
	w := pasta.NewWorkspace(&StringLoggerFactory{})
	if err := w.AddNodeClass(testNodeClass{
		name: "example.com/Commented",
		params: pasta.NodeClassParams{
			PrimaryType: "example.com/number",
		},
	}); err != nil {
		t.Fatalf("AddNodeClass: %v", err)
	}
	nodeID, err := w.AddNode(&workspaceNode{}, "example.com/Commented", "commented")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := w.SetNodePrimary(nodeID, "example.com/number"); err != nil {
		t.Fatalf("SetNodePrimary: %v", err)
	}

	cfg, err := hujson.Parse([]byte(`{
		"commented": {
			// class comment
			"Class": "example.com/Old",
			// primary comment
			"Primary": "example.com/old",
			// position comment
			"Pos": "old",
			// links comment
			"Links": ["stale -> [missing] input"],
			// custom comment
			"custom": "preserved"
		},
		// removed node comment
		"removed": {
			"Class": "example.com/Removed"
		}
	}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if err := w.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	assertConfigValue(t, cfg, configer.Path{"commented", "Class"}, "example.com/Commented")
	assertConfigValue(t, cfg, configer.Path{"commented", "Primary"}, "example.com/number")
	assertConfigValue(t, cfg, configer.Path{"commented", "Pos"}, "")
	assertConfigValue(t, cfg, configer.Path{"commented", "Links"}, []any{})
	assertConfigValue(t, cfg, configer.Path{"commented", "custom"}, "preserved")
	assertConfigMissing(t, cfg, configer.Path{"removed"})
	assertConfigComment(t, cfg, configer.Path{"commented", "Class"}, "class comment")
	assertConfigComment(t, cfg, configer.Path{"commented", "Primary"}, "primary comment")
	assertConfigComment(t, cfg, configer.Path{"commented", "Pos"}, "position comment")
	assertConfigComment(t, cfg, configer.Path{"commented", "Links"}, "links comment")
	assertConfigComment(t, cfg, configer.Path{"commented", "custom"}, "custom comment")
}

func TestDeleteNodeOwnedConfigKeysRemovesLowerCaseKeysAndComments(t *testing.T) {
	cfg, err := hujson.Parse([]byte(`{
		"node": {
			// class comment
			"Class": "example.com/Node",
			// lower-case comment
			"value": "10",
			// another node-owned comment
			"state": {
				"nested": true
			},
			"_private": "kept",
			"Links": ["out -> [target] in"]
		}
	}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if err := pasta.DeleteNodeOwnedConfigKeys(cfg.View(configer.Path{"node"})); err != nil {
		t.Fatalf("DeleteNodeOwnedConfigKeys: %v", err)
	}

	assertConfigValue(t, cfg, configer.Path{"node", "Class"}, "example.com/Node")
	assertConfigValue(t, cfg, configer.Path{"node", "_private"}, "kept")
	assertConfigValue(t, cfg, configer.Path{"node", "Links"}, []any{"out -> [target] in"})
	assertConfigMissing(t, cfg, configer.Path{"node", "value"})
	assertConfigMissing(t, cfg, configer.Path{"node", "state"})
	assertConfigComment(t, cfg, configer.Path{"node", "Class"}, "class comment")
}

func TestWorkspaceSaveCalcGraphValuesAndLinks(t *testing.T) {
	graph, _ := buildCalcGraph(t, []string{
		"c10:sum.a", "c5:sum.b", "c2:sum.c", "c3:sum.d",
		"sum:sub.a", "c4:sub.b", "sub:div.a", "c2:div.b",
		"div:mult.a", "c5:mult.b", "sum:sumResult.in", "sub:subResult.in",
		"div:divResult.in", "mult:final.in",
	})

	cfg := configer.NewMemory(nil)
	if err := graph.w.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	name := func(key string) string {
		snapshot := graph.w.Snapshot()
		return snapshot.Nodes[graph.nodes[key].id].Name
	}
	want := map[string]any{
		name("c10"): map[string]any{"Class": "example.com/CalcC10", "value": "10", "Links": []any{"out -> [" + name("sum") + "] a"}},
		name("c5"):  map[string]any{"Class": "example.com/CalcC5", "value": "5", "Links": []any{"out -> [" + name("sum") + "] b", "out -> [" + name("mult") + "] b"}},
		name("c2"):  map[string]any{"Class": "example.com/CalcC2", "value": "2", "Links": []any{"out -> [" + name("sum") + "] c", "out -> [" + name("div") + "] b"}},
		name("c3"):  map[string]any{"Class": "example.com/CalcC3", "value": "3", "Links": []any{"out -> [" + name("sum") + "] d"}},
		name("c4"):  map[string]any{"Class": "example.com/CalcC4", "value": "4", "Links": []any{"out -> [" + name("sub") + "] b"}},
		name("sum"): map[string]any{"Class": "example.com/CalcSum", "Links": []any{
			"out -> [" + name("sub") + "] a",
			"out -> [" + name("sumResult") + "] in",
		}},
		name("sub"): map[string]any{"Class": "example.com/CalcSub", "Links": []any{
			"out -> [" + name("div") + "] a",
			"out -> [" + name("subResult") + "] in",
		}},
		name("div"): map[string]any{"Class": "example.com/CalcDiv", "Links": []any{
			"out -> [" + name("mult") + "] a",
			"out -> [" + name("divResult") + "] in",
		}},
		name("mult"):      map[string]any{"Class": "example.com/CalcMult", "Links": []any{"out -> [" + name("final") + "] in"}},
		name("sumResult"): map[string]any{"Class": "example.com/CalcSumResult"},
		name("subResult"): map[string]any{"Class": "example.com/CalcSubResult"},
		name("divResult"): map[string]any{"Class": "example.com/CalcDivResult"},
		name("final"):     map[string]any{"Class": "example.com/CalcFinal"},
	}
	if got := cfg.Snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("saved calc config = %#v, want %#v", got, want)
	}
}

func assertConfigValue(t *testing.T, cfg configer.Config, path configer.Path, want any) {
	t.Helper()
	got, err := cfg.Get(path)
	if err != nil {
		t.Fatalf("Get(%v): %v", path, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Get(%v) = %#v, want %#v", path, got, want)
	}
}

func assertConfigMissing(t *testing.T, cfg configer.Config, path configer.Path) {
	t.Helper()
	if got, err := cfg.Get(path); err == nil {
		t.Fatalf("Get(%v) = %#v, want missing", path, got)
	}
}

func assertConfigComment(t *testing.T, cfg configer.Config, path configer.Path, want string) {
	t.Helper()
	commenter, ok := cfg.(configer.Commenter)
	if !ok {
		t.Fatalf("config does not support comments")
	}
	got, err := commenter.GetComment(path)
	if err != nil {
		t.Fatalf("GetComment(%v): %v", path, err)
	}
	if got != want {
		t.Fatalf("GetComment(%v) = %q, want %q", path, got, want)
	}
}
