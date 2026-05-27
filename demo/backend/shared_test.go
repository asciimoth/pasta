package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/asciimoth/configer/hujson"
	"github.com/asciimoth/pasta/pasta"
)

type testLogFactory struct{}
type testLogger struct{}

func (testLogFactory) WorkspaceLogger() pasta.Logger          { return testLogger{} }
func (testLogFactory) NodeLogger(uint64, string) pasta.Logger { return testLogger{} }
func (testLogger) Debug(...any)                               {}
func (testLogger) Debugf(string, ...any)                      {}
func (testLogger) Info(...any)                                {}
func (testLogger) Infof(string, ...any)                       {}
func (testLogger) Warn(...any)                                {}
func (testLogger) Warnf(string, ...any)                       {}
func (testLogger) Err(...any)                                 {}
func (testLogger) Errf(string, ...any)                        {}
func (testLogger) Fatal(...any)                               {}
func (testLogger) Fatalf(string, ...any)                      {}

func TestInitialConfigRestoresWorkspace(t *testing.T) {
	cfg, err := hujson.Parse([]byte(initialConfig))
	if err != nil {
		t.Fatal(err)
	}
	w, err := pasta.WorkspaceFromConfig(stdClasses(), cfg, testLogFactory{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := w.Snapshot()
	if got, want := len(snapshot.Nodes), 27; got != want {
		t.Fatalf("nodes = %d, want %d", got, want)
	}
	if got, want := len(snapshot.Links), 50; got != want {
		t.Errorf("links = %d, want %d", got, want)
	}
	classesInGraph := map[string]bool{}
	nodeLinks := map[uint64]int{}
	for id, node := range snapshot.Nodes {
		classesInGraph[node.Class] = true
		nodeLinks[id] = 0
	}
	for _, link := range snapshot.Links {
		nodeLinks[link.LeftPortNode]++
		nodeLinks[link.RightPortNode]++
	}
	for _, class := range stdClasses() {
		if !classesInGraph[class.ClassName()] {
			t.Fatalf("initial config does not include std class %s", class.ClassName())
		}
	}
	for id, count := range nodeLinks {
		if count == 0 {
			t.Fatalf("node %q has no links", snapshot.Nodes[id].Name)
		}
	}
	linksByName := map[string]bool{}
	for _, link := range snapshot.Links {
		source := snapshot.Nodes[link.RightPortNode].Name
		sourcePort := snapshot.Ports[link.RightPort].Name
		target := snapshot.Nodes[link.LeftPortNode].Name
		targetPort := snapshot.Ports[link.LeftPort].Name
		linksByName[fmt.Sprintf("%s:%s -> %s:%s", source, sourcePort, target, targetPort)] = true
	}
	for _, want := range []string{
		"FloatValue:output -> Summary:Float",
		"Ratio:output -> Summary:Ratio",
		"SelectedText:Out -> Summary:Selected",
		"SplitGreeting:After -> Summary:After",
		"TextLength:output -> Summary:Length",
	} {
		if !linksByName[want] {
			t.Errorf("missing restored link %s", want)
		}
	}
}

func TestSaveConfigFormatsHuJSONText(t *testing.T) {
	cfg, err := hujson.Parse([]byte(initialConfig))
	if err != nil {
		t.Fatal(err)
	}
	w, err := pasta.WorkspaceFromConfig(stdClasses(), cfg, testLogFactory{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	text, err := formatHuJSONText(cfg.Pack())
	if err != nil {
		t.Fatalf("formatHuJSONText: %v", err)
	}
	if strings.Contains(text, `"Links": ["`) {
		t.Fatalf("saved config contains compact Links array:\n%s", text)
	}
	if !strings.Contains(text, "\"Links\": [\n") {
		t.Fatalf("saved config does not contain pretty Links array:\n%s", text)
	}
	if !strings.Contains(text, "// Positions are frontend-owned JSON strings. Pasta preserves them.") {
		t.Fatalf("saved config did not preserve initial comment:\n%s", text)
	}
}
