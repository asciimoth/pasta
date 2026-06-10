package main

import (
	"fmt"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

const nodeTypePopupDemo = "demo.pasta/PopupDemo"

type popupDemoClass struct{}

func (popupDemoClass) ClassName() string        { return nodeTypePopupDemo }
func (popupDemoClass) ShortDescription() string { return "Popup demo" }
func (popupDemoClass) LongDescription() string {
	return "A no-op demo node that exposes Formular buttons for spawning node popups."
}
func (popupDemoClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{}
}
func (popupDemoClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	return &popupDemoNode{}, nil
}

type popupDemoNode struct {
	pasta.BasicNode

	w     *pasta.Workspace
	id    uint64
	count int
}

func (n *popupDemoNode) OnInit(
	w *pasta.Workspace,
	_ pasta.Logger,
	id uint64,
	_ string,
	_ *pasta.NodeInitData,
	_, _, _, _ bool,
) error {
	n.w = w
	n.id = id
	_ = n.w.SetNodeLabel(n.id, "popups")
	if _, err := n.w.AddNodePopup(n.id, pasta.NodePopupInfo, "Popup demo node is ready.", true); err != nil {
		return err
	}
	n.sendMenu()
	return nil
}

func (n *popupDemoNode) OnFormularMsg(message any) error {
	msg, ok := message.(formular.ButtonPressMessage)
	if !ok || msg.MenuID != pasta.NodeMenuID(n.id) || msg.BlockID != "popups" {
		return nil
	}
	popupType := ""
	switch msg.ButtonID {
	case "add-info":
		popupType = pasta.NodePopupInfo
	case "add-warning":
		popupType = pasta.NodePopupWard
	case "add-error":
		popupType = pasta.NodePopupErr
	default:
		return nil
	}
	n.count++
	_, err := n.w.AddNodePopup(n.id, popupType, fmt.Sprintf("%s popup %d from the demo node menu", popupType, n.count), false)
	return err
}

func (n *popupDemoNode) sendMenu() {
	if n.w == nil || n.id == 0 {
		return
	}
	n.w.SendNodeMenuMsg(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{
			Type:           formular.MessageMenuSnapshot,
			MenuID:         pasta.NodeMenuID(n.id),
			MenuGeneration: 1,
		},
		Blocks: []formular.Block{{
			ID:         "popups",
			Order:      10,
			Generation: 1,
			Items: []formular.Item{
				{Type: formular.ItemButton, ID: "add-info", Label: "Add info popup"},
				{Type: formular.ItemButton, ID: "add-warning", Label: "Add warning popup"},
				{Type: formular.ItemButton, ID: "add-error", Label: "Add error popup"},
			},
		}},
	})
}
