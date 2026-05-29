package main

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asciimoth/configer/hujson"
	"github.com/asciimoth/formular"
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
	w, err := pasta.WorkspaceFromConfig(Classes(), cfg, testLogFactory{})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	snapshot := w.Snapshot()
	if got, want := len(snapshot.Nodes), 46; got != want {
		t.Fatalf("nodes = %d, want %d", got, want)
	}
	if got, want := len(snapshot.Links), 70; got != want {
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
	for _, class := range Classes() {
		if !classesInGraph[class.ClassName()] {
			t.Fatalf("initial config does not include std class %s", class.ClassName())
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
		"NetSelect:Out -> Loopback:Network",
		"ServerB:Network -> NetSelect:In 1",
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
	w, err := pasta.WorkspaceFromConfig(Classes(), cfg, testLogFactory{})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
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

func TestInitialConfigNetworkHTTPExample(t *testing.T) {
	// clientPort := freeTCPPort(t)
	// serverAPort := freeTCPPort(t)
	// configText := strings.ReplaceAll(initialConfig, "8081", strconv.Itoa(serverAPort))
	// configText = strings.ReplaceAll(configText, "8080", strconv.Itoa(clientPort))
	cfg, err := hujson.Parse([]byte(initialConfig))
	if err != nil {
		t.Fatal(err)
	}
	w, err := pasta.WorkspaceFromConfig(Classes(), cfg, testLogFactory{})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	client := nodeIDByName(t, w, "Client")
	server := nodeIDByName(t, w, "ServerB")
	state := formular.NewMenuSnapshotState()
	var mu sync.Mutex
	sub := w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		if notification.Kind != pasta.NotificationNodeMenu {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		state.Apply(notification.Formular)
	})
	if !w.SubscribeNodeMenu(client, sub) {
		t.Fatal("SubscribeNodeMenu DemoClient returned false")
	}
	if !w.SubscribeNodeMenu(server, sub) {
		t.Fatal("SubscribeNodeMenu DemoServerB returned false")
	}
	waitForMenuLog(t, &mu, state, pasta.NodeMenuID(server), "listening on 127.0.0.1:8080")
	// request := formular.FormApplyMessage{
	// 	MessageBase: formular.MessageBase{Type: formular.MessageFormApply, MenuID: pasta.NodeMenuID(client), MenuGeneration: 1, BlockGeneration: 1},
	// 	BlockID:     "request",
	// 	Values: map[string]any{
	// 		"url":    "http://127.0.0.1:8080/",
	// 		"method": "GET",
	// 		"body":   "",
	// 	},
	// }
	request := formular.ButtonPressMessage{
		MessageBase: formular.MessageBase{
			Type:            formular.MessageButtonPress,
			MenuID:          pasta.NodeMenuID(client),
			MenuGeneration:  1,
			BlockGeneration: 1,
		},
		BlockID:  "request",
		ButtonID: "send",
	}
	w.SendNodeFormularMsg(client, request)

	deadline := time.Now().Add(5 * time.Second)
	var lastErr any
	for time.Now().Before(deadline) {
		mu.Lock()
		response := menuFieldValue(state, pasta.NodeMenuID(client), "result", "response")
		errText := menuFieldValue(state, pasta.NodeMenuID(client), "result", "error")
		mu.Unlock()
		if response == "Response from demo server B" {
			return
		}
		if errText != "" {
			lastErr = errText
			w.SendNodeFormularMsg(client, request)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for demo HTTP response; last client error: %v", lastErr)
}

func waitForMenuLog(t *testing.T, mu *sync.Mutex, state *formular.MenuSnapshotState, menuID, text string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		found := menuHasLog(state, menuID, text)
		mu.Unlock()
		if found {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	mu.Lock()
	snapshot, _ := state.Snapshot(menuID)
	mu.Unlock()
	t.Fatalf("timed out waiting for menu log %q; snapshot=%#v", text, snapshot)
}

func nodeIDByName(t *testing.T, w *pasta.Workspace, name string) uint64 {
	t.Helper()
	snapshot := w.Snapshot()
	for id, node := range snapshot.Nodes {
		if node.Name == name {
			return id
		}
	}
	t.Fatalf("node %q not found", name)
	return 0
}

func menuFieldValue(state *formular.MenuSnapshotState, menuID, blockID, fieldID string) any {
	snapshot, ok := state.Snapshot(menuID)
	if !ok {
		return nil
	}
	for _, block := range snapshot.Blocks {
		if block.ID != blockID {
			continue
		}
		for _, item := range block.Items {
			if item.ID == fieldID && item.Field != nil {
				return item.Field.Value //nolint
			}
		}
	}
	return nil
}

func menuHasLog(state *formular.MenuSnapshotState, menuID, text string) bool {
	snapshot, ok := state.Snapshot(menuID)
	if !ok {
		return false
	}
	for _, block := range snapshot.Blocks {
		for _, item := range block.Items {
			for _, line := range item.Logs {
				if strings.Contains(line.Text, text) {
					return true
				}
			}
		}
	}
	return false
}
