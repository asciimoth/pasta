package pasta_test

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/configer/hujson"
	"github.com/asciimoth/pasta/pasta"
)

func TestWorkspaceFromConfigRestoresNodesLinksAndNextID(t *testing.T) {
	var restoredCfg configer.Config
	var restoredState *pasta.NodeClassState
	restoredNode := &workspaceNode{}
	class := testFactoryNodeClass{
		testNodeClass: testNodeClass{
			name: "example.com/Restored",
			params: pasta.NodeClassParams{
				PrimaryType: "example.com/default",
				InitialPorts: []pasta.Port{{
					Direction: "right",
					Name:      "out",
					Types:     []string{pasta.AnyType},
				}},
			},
		},
		newNode: func(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
			restoredCfg = cfg
			if len(previous) > 0 {
				state := *previous[0]
				state.LeftPorts = append([]pasta.Port(nil), previous[0].LeftPorts...)
				state.RightPorts = append([]pasta.Port(nil), previous[0].RightPorts...)
				restoredState = &state
			}
			return restoredNode, nil
		},
	}

	cfg := configer.NewMemory(map[string]any{
		"alpha 12": map[string]any{
			"Class":   "example.com/Restored",
			"Primary": "example.com/custom",
			"Pos":     "10 20",
			"Links": []any{
				"out -> [target] in",
				"extra -> [target] aux",
			},
		},
		"target": map[string]any{
			"Class": "example.com/Missing",
		},
	})

	w, err := pasta.WorkspaceFromConfig([]pasta.NodeClass{class}, cfg, &StringLoggerFactory{})
	if err != nil {
		t.Fatalf("WorkspaceFromConfig: %v", err)
	}

	alphaID, ok := w.NodeIDByName("alpha 12")
	if !ok {
		t.Fatal("restored alpha node missing")
	}
	if alphaID != 21 {
		t.Fatalf("alpha node ID = %d, want initial nextID 21", alphaID)
	}
	targetID, ok := w.NodeIDByName("target")
	if !ok {
		t.Fatal("restored target node missing")
	}
	snapshot := w.Snapshot()
	alpha := snapshot.Nodes[alphaID]
	target := snapshot.Nodes[targetID]

	if alpha.Placeholder || !target.Placeholder {
		t.Fatalf("placeholder states alpha=%v target=%v, want false/true", alpha.Placeholder, target.Placeholder)
	}
	if alpha.PrimaryType != "example.com/custom" || alpha.Position != "10 20" {
		t.Fatalf("alpha state = %#v", alpha)
	}
	if !restoredNode.initFlags.isRestored || !restoredNode.initFlags.isClassConstructed {
		t.Fatalf("init flags = %#v, want restored class construction", restoredNode.initFlags)
	}
	if restoredCfg == nil {
		t.Fatal("factory did not receive config view")
	}
	if got, err := restoredCfg.Get(configer.Path{"Pos"}); err != nil || got != "10 20" {
		t.Fatalf("factory config Pos = %#v, %v; want 10 20, nil", got, err)
	}
	if restoredState == nil || restoredState.Name != "alpha 12" || restoredState.PrimaryType != "example.com/custom" {
		t.Fatalf("factory state = %#v", restoredState)
	}
	if got := portNames(snapshot, alpha.RightPorts); !reflect.DeepEqual(got, []string{"out", "extra"}) {
		t.Fatalf("right ports = %#v, want out/extra", got)
	}
	if got := portNames(snapshot, target.LeftPorts); !reflect.DeepEqual(got, []string{"aux", "in"}) {
		t.Fatalf("target left ports = %#v, want aux/in", got)
	}
	if len(snapshot.Links) != 2 {
		t.Fatalf("links = %#v, want 2", snapshot.Links)
	}

	nextID := w.NextID()
	if nextID != 29 {
		t.Fatalf("NextID after restore = %d, want 29", nextID)
	}
}

func TestWorkspaceFromConfigRestoresExplicitEmptyPrimary(t *testing.T) {
	var restoredState *pasta.NodeClassState
	class := testFactoryNodeClass{
		testNodeClass: testNodeClass{
			name:   "example.com/DefaultPrimary",
			params: pasta.NodeClassParams{PrimaryType: "example.com/default"},
		},
		newNode: func(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
			if len(previous) > 0 {
				restoredState = previous[0]
			}
			return &workspaceNode{}, nil
		},
	}
	cfg := configer.NewMemory(map[string]any{
		"node": map[string]any{
			"Class":   "example.com/DefaultPrimary",
			"Primary": "",
		},
	})

	w, err := pasta.WorkspaceFromConfig([]pasta.NodeClass{class}, cfg, &StringLoggerFactory{})
	if err != nil {
		t.Fatalf("WorkspaceFromConfig: %v", err)
	}
	nodeID, ok := w.NodeIDByName("node")
	if !ok {
		t.Fatal("restored node missing")
	}
	if got := w.Snapshot().Nodes[nodeID].PrimaryType; got != "" {
		t.Fatalf("primary type = %q, want empty", got)
	}
	if restoredState == nil || restoredState.PrimaryType != "" {
		t.Fatalf("factory state = %#v, want empty primary", restoredState)
	}
}

func TestWorkspaceFromConfigRestoresNodeCommentsAsInfoPopups(t *testing.T) {
	class := testFactoryNodeClass{
		testNodeClass: testNodeClass{name: "example.com/Commented"},
		newNode: func(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
			return &workspaceNode{}, nil
		},
	}
	cfg, err := hujson.Parse([]byte(`{
		// This will become popup
		"commented": {
			// This should stay node config only
			"Class": "example.com/Commented"
		},
		// Missing classes get the same config comment popup
		"placeholder": {
			"Class": "example.com/Missing"
		},
		"plain": {
			"Class": "example.com/Commented"
		}
	}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	w, err := pasta.WorkspaceFromConfig([]pasta.NodeClass{class}, cfg, &StringLoggerFactory{})
	if err != nil {
		t.Fatalf("WorkspaceFromConfig: %v", err)
	}

	commentedID, ok := w.NodeIDByName("commented")
	if !ok {
		t.Fatal("commented node missing")
	}
	placeholderID, ok := w.NodeIDByName("placeholder")
	if !ok {
		t.Fatal("placeholder node missing")
	}
	plainID, ok := w.NodeIDByName("plain")
	if !ok {
		t.Fatal("plain node missing")
	}

	snapshot := w.Snapshot()
	assertNodePopups(t, snapshot.Nodes[commentedID].Popups, []pasta.NodePopup{{
		Type: pasta.NodePopupInfo,
		Text: "This will become popup",
	}})
	assertNodePopups(t, snapshot.Nodes[placeholderID].Popups, []pasta.NodePopup{{
		Type: pasta.NodePopupInfo,
		Text: "Missing classes get the same config comment popup",
	}})
	assertNodePopups(t, snapshot.Nodes[plainID].Popups, nil)
}

func TestWorkspaceFromConfigIgnoresCommentsWhenConfigDoesNotSupportThem(t *testing.T) {
	class := testFactoryNodeClass{
		testNodeClass: testNodeClass{name: "example.com/NoComments"},
		newNode: func(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
			return &workspaceNode{}, nil
		},
	}
	cfg := configer.NewMemory(map[string]any{
		"node": map[string]any{
			"Class": "example.com/NoComments",
		},
	})

	w, err := pasta.WorkspaceFromConfig([]pasta.NodeClass{class}, cfg, &StringLoggerFactory{})
	if err != nil {
		t.Fatalf("WorkspaceFromConfig: %v", err)
	}
	nodeID, ok := w.NodeIDByName("node")
	if !ok {
		t.Fatal("node missing")
	}
	if got := w.Snapshot().Nodes[nodeID].Popups; len(got) != 0 {
		t.Fatalf("popups = %#v, want none", got)
	}
}

func TestWorkspaceSaveRestoreOrderStableAcrossMultiplePasses(t *testing.T) {
	original, _ := buildCalcGraph(t, []string{
		"c10:sum.a", "c5:sum.b", "c2:sum.c", "c3:sum.d",
		"sum:sub.a", "c4:sub.b", "sub:div.a", "c2:div.b",
		"div:mult.a", "c5:mult.b", "sum:sumResult.in", "sub:subResult.in",
		"div:divResult.in", "mult:final.in",
	})
	once, _ := restoreCalcGraph(t, original)
	twice, _ := restoreCalcGraph(t, once)

	onceSignature := workspaceOrderSignature(once.w)
	twiceSignature := workspaceOrderSignature(twice.w)
	if onceSignature != twiceSignature {
		t.Fatalf("workspace order changed after second restore\nonce:\n%s\ntwice:\n%s", onceSignature, twiceSignature)
	}

	onceNext := once.w.NextID()
	twiceNext := twice.w.NextID()
	if onceNext != twiceNext {
		t.Fatalf("NextID after restore passes = %d, %d; want same", onceNext, twiceNext)
	}

	onceConfig := mustSaveHuJSON(t, once.w)
	twiceConfig := mustSaveHuJSON(t, twice.w)
	if onceConfig != twiceConfig {
		t.Fatalf("saved config order changed after second restore\nonce:\n%s\ntwice:\n%s", onceConfig, twiceConfig)
	}
}

func portNames(snapshot pasta.WorkspaceSnapshot, ids []uint64) []string {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, snapshot.Ports[id].Name)
	}
	return names
}

func workspaceOrderSignature(w *pasta.Workspace) string {
	snapshot := w.Snapshot()
	var b strings.Builder

	nodeIDs := sortedUint64Keys(snapshot.Nodes)
	for _, id := range nodeIDs {
		node := snapshot.Nodes[id]
		fmt.Fprintf(&b, "node %d %s %s left=%v right=%v\n", id, node.Name, node.Class, node.LeftPorts, node.RightPorts)
	}
	portIDs := sortedUint64Keys(snapshot.Ports)
	for _, id := range portIDs {
		port := snapshot.Ports[id]
		fmt.Fprintf(&b, "port %d node=%d %s %s links=%v\n", id, port.Node, port.Direction, port.Name, port.Links)
	}
	linkIDs := sortedUint64Keys(snapshot.Links)
	for _, id := range linkIDs {
		link := snapshot.Links[id]
		fmt.Fprintf(
			&b,
			"link %d left=%d:%d right=%d:%d placeholder=%v type=%s\n",
			id,
			link.LeftPortNode,
			link.LeftPort,
			link.RightPortNode,
			link.RightPort,
			link.Placeholder,
			link.Type,
		)
	}
	return b.String()
}

func sortedUint64Keys[T any](values map[uint64]T) []uint64 {
	keys := make([]uint64, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})
	return keys
}

func mustSaveHuJSON(t *testing.T, w *pasta.Workspace) string {
	t.Helper()

	cfg, err := hujson.Parse([]byte(`{}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := w.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return string(cfg.Pack())
}

func assertNodePopups(t *testing.T, got, want []pasta.NodePopup) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("popups = %#v, want %#v", got, want)
	}
	if len(want) == 0 {
		return
	}
	for i := range got {
		if got[i].ID == 0 {
			t.Fatalf("popup %d ID = 0, want workspace ID", i)
		}
		got[i].ID = 0
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("popups = %#v, want %#v", got, want)
	}
}
