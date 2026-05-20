package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/asciimoth/pasta/pasta"
)

const (
	MenuLibraryName = "menus.pasta.demo"

	MenuImmediateClass   = MenuLibraryName + "/Immediate"
	MenuCommittableClass = MenuLibraryName + "/Committable"
)

type MenuLibrary struct{}

func (MenuLibrary) Name() string { return MenuLibraryName }

func (MenuLibrary) DefineClasses(scope pasta.LibraryScope) error {
	for _, class := range MenuClasses() {
		if err := scope.DefineClass(class); err != nil {
			return err
		}
	}
	return nil
}

func MenuClasses() []pasta.ClassSpec {
	return []pasta.ClassSpec{
		{
			Name:        MenuImmediateClass,
			DisplayName: "Menu Immediate",
			Description: "Portless demo node whose menu edits are committed immediately.",
			Default:     menuDefault("Menu Immediate", false),
			Runtime:     menuDemoNodeClass{committable: false},
		},
		{
			Name:        MenuCommittableClass,
			DisplayName: "Menu Committable",
			Description: "Portless demo node whose GUI edits are sent only by Apply.",
			Default:     menuDefault("Menu Committable", true),
			Runtime:     menuDemoNodeClass{committable: true},
		},
	}
}

func menuDefault(display string, committable bool) pasta.NodeState {
	return pasta.NodeState{
		DisplayName: display,
		Private: menuDemoState{
			Text:    "edit me",
			Mode:    "alpha",
			Enabled: true,
			Count:   1,
			Rows: []menuDemoRow{
				{ID: "first", Title: "First row", Name: "alpha", Quantity: 1, Active: true},
				{ID: "second", Title: "Second row", Name: "beta", Quantity: 2, Active: false},
			},
			Committable: committable,
		},
	}
}

type menuDemoNodeClass struct {
	committable bool
}

func (c menuDemoNodeClass) InitNode(ctx pasta.NodeContext, state pasta.NodeState, _ pasta.InitMode) (pasta.NodeRuntime, error) {
	runCtx, cancel := context.WithCancel(context.Background())
	node := &menuDemoNode{
		ctx:    ctx,
		cancel: cancel,
		state:  menuDemoStateFromAny(state.Private),
	}
	node.state.Committable = c.committable
	node.logLocked("initialized committable=%t", c.committable)
	if err := ctx.Node.SetMenu(node.menu()); err != nil {
		cancel()
		return nil, err
	}
	go node.run(runCtx)
	return node, nil
}

type menuDemoNode struct {
	mu     sync.RWMutex
	ctx    pasta.NodeContext
	cancel context.CancelFunc
	state  menuDemoState
}

type menuDemoState struct {
	Text        string        `json:"text"`
	Mode        string        `json:"mode"`
	Enabled     bool          `json:"enabled"`
	Count       int64         `json:"count"`
	Rows        []menuDemoRow `json:"rows,omitempty"`
	Ticks       int64         `json:"ticks"`
	Committable bool          `json:"committable"`
	Logs        []string      `json:"logs,omitempty"`
}

type menuDemoRow struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Name     string `json:"name"`
	Quantity int64  `json:"quantity"`
	Active   bool   `json:"active"`
}

func (n *menuDemoNode) run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.mu.Lock()
			n.state.Ticks++
			n.logLocked("node refreshed menu tick=%d", n.state.Ticks)
			n.mu.Unlock()
			n.updateMenu()
		}
	}
}

func (n *menuDemoNode) ApplyMenuUpdate(update pasta.MenuStateUpdate) (pasta.MenuStateUpdate, error) {
	n.mu.Lock()
	for _, field := range update.Fields {
		if field.Block != "main" {
			continue
		}
		n.logLocked("field update %s=%v", field.Field, field.Value)
		switch field.Field {
		case "text":
			n.state.Text = stringFromAny(field.Value)
		case "mode":
			n.state.Mode = stringFromAny(field.Value)
		case "enabled":
			n.state.Enabled = boolFromAny(field.Value)
		case "count":
			n.state.Count = int64(menuNumberValue(field.Value))
		}
	}
	for _, repeat := range update.Repeats {
		if repeat.Block != "main" || repeat.Repeat != "rows" {
			continue
		}
		rows := make([]menuDemoRow, 0, len(repeat.Items))
		for _, item := range repeat.Items {
			row := menuDemoRow{
				ID:    item.ID,
				Title: item.Title,
			}
			if row.Title == "" {
				row.Title = item.ID
			}
			row.Name = stringFromAny(item.Fields["name"])
			row.Quantity = int64(menuNumberValue(item.Fields["quantity"]))
			row.Active = boolFromAny(item.Fields["active"])
			rows = append(rows, row)
		}
		n.state.Rows = rows
		n.logLocked("repeat update rows=%d", len(rows))
	}
	n.mu.Unlock()
	n.updateMenu()
	return update, nil
}

func (n *menuDemoNode) TriggerMenuButton(ref pasta.MenuButtonRef) error {
	if ref.Block != "main" {
		return nil
	}
	n.mu.Lock()
	switch ref.Button {
	case "increment":
		n.state.Count++
		n.logLocked("button increment clicked; count=%d", n.state.Count)
	case "refresh":
		n.state.Ticks++
		n.logLocked("button refresh clicked; tick=%d", n.state.Ticks)
	case "clear-log":
		n.state.Logs = nil
		n.logLocked("button clear-log clicked")
	default:
		n.logLocked("unknown button %s clicked", ref.Button)
	}
	n.mu.Unlock()
	n.updateMenuSoon()
	return nil
}

func (n *menuDemoNode) Close() error {
	n.cancel()
	return nil
}

func (n *menuDemoNode) updateMenu() {
	n.mu.RLock()
	state := n.state
	n.mu.RUnlock()
	_ = n.ctx.Node.SetPrivate(state)
	_ = n.ctx.Node.SetMenu(n.menu())
}

func (n *menuDemoNode) updateMenuSoon() {
	time.AfterFunc(25*time.Millisecond, n.updateMenu)
}

func (n *menuDemoNode) menu() pasta.NodeMenu {
	n.mu.RLock()
	state := n.state
	n.mu.RUnlock()
	return pasta.NodeMenu{
		Committable: state.Committable,
		Blocks: []pasta.MenuBlock{{
			ID:    "main",
			Title: "Menu Features",
			Fields: []pasta.MenuField{
				{ID: "text", Label: "Text", Kind: pasta.MenuFieldString, Value: state.Text},
				{ID: "mode", Label: "Mode", Kind: pasta.MenuFieldString, Value: state.Mode, Options: []pasta.MenuOption{
					{Value: "alpha", Label: "Alpha"},
					{Value: "beta", Label: "Beta"},
					{Value: "gamma", Label: "Gamma", Disabled: true},
				}},
				{ID: "enabled", Label: "Enabled", Kind: pasta.MenuFieldBool, Value: state.Enabled, Render: pasta.MenuRenderCheckbox},
				{ID: "count", Label: "Count", Kind: pasta.MenuFieldInt64, Value: state.Count},
				{ID: "ticks", Label: "Node menu updates", Kind: pasta.MenuFieldReadOnly, ReadOnly: true, Value: state.Ticks},
				{ID: "logs", Label: "Menu event log", Kind: pasta.MenuFieldReadOnly, ReadOnly: true, Value: strings.Join(state.Logs, "\n")},
			},
			Buttons: []pasta.MenuButton{
				{ID: "increment", Label: "Increment"},
				{ID: "refresh", Label: "Node Refresh"},
				{ID: "disabled", Label: "Disabled Button", Disabled: true},
				{ID: "clear-log", Label: "Clear Log"},
			},
			Repeats: []pasta.MenuRepeat{{
				ID:    "rows",
				Title: "Repeat rows",
				Template: []pasta.MenuField{
					{ID: "name", Label: "Name", Kind: pasta.MenuFieldString},
					{ID: "quantity", Label: "Quantity", Kind: pasta.MenuFieldInt64},
					{ID: "active", Label: "Active", Kind: pasta.MenuFieldBool, Render: pasta.MenuRenderCheckbox},
					{ID: "summary", Label: "Summary", Kind: pasta.MenuFieldReadOnly, ReadOnly: true},
				},
				Items: menuRepeatItems(state.Rows),
			}},
		}},
	}
}

func menuRepeatItems(rows []menuDemoRow) []pasta.MenuRepeatItem {
	items := make([]pasta.MenuRepeatItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, pasta.MenuRepeatItem{
			ID:    firstNonEmptyString(row.ID, menuRepeatItemID(len(items)+1)),
			Title: firstNonEmptyString(row.Title, row.Name),
			Fields: []pasta.MenuField{
				{ID: "name", Value: row.Name},
				{ID: "quantity", Value: row.Quantity},
				{ID: "active", Value: row.Active},
				{ID: "summary", Value: fmt.Sprintf("%s x%d active=%t", row.Name, row.Quantity, row.Active)},
			},
		})
	}
	return items
}

func menuRepeatItemID(n int) string {
	return fmt.Sprintf("row%d", n)
}

func (n *menuDemoNode) logLocked(format string, args ...any) {
	line := time.Now().Format("15:04:05") + " " + fmt.Sprintf(format, args...)
	n.state.Logs = append(n.state.Logs, line)
	if len(n.state.Logs) > 40 {
		n.state.Logs = append([]string(nil), n.state.Logs[len(n.state.Logs)-40:]...)
	}
}

func menuDemoStateFromAny(v any) menuDemoState {
	state, ok := v.(menuDemoState)
	if ok {
		return state
	}
	m, ok := v.(map[string]any)
	if !ok {
		return menuDemoState{Text: "edit me", Mode: "alpha", Enabled: true, Count: 1}
	}
	return menuDemoState{
		Text:        stringFromAny(m["text"]),
		Mode:        firstNonEmptyString(stringFromAny(m["mode"]), "alpha"),
		Enabled:     boolFromAny(m["enabled"]),
		Count:       int64(menuNumberValue(m["count"])),
		Rows:        menuDemoRowsFromAny(m["rows"]),
		Ticks:       int64(menuNumberValue(m["ticks"])),
		Committable: boolFromAny(m["committable"]),
	}
}

func menuDemoRowsFromAny(v any) []menuDemoRow {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	rows := make([]menuDemoRow, 0, len(raw))
	for i, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, menuDemoRow{
			ID:       firstNonEmptyString(stringFromAny(m["id"]), menuRepeatItemID(i+1)),
			Title:    stringFromAny(m["title"]),
			Name:     stringFromAny(m["name"]),
			Quantity: int64(menuNumberValue(m["quantity"])),
			Active:   boolFromAny(m["active"]),
		})
	}
	return rows
}

func boolFromAny(v any) bool {
	value, ok := v.(bool)
	return ok && value
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func menuNumberValue(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case int32:
		return float64(x)
	case uint:
		return float64(x)
	case uint64:
		return float64(x)
	case uint32:
		return float64(x)
	default:
		return 0
	}
}
