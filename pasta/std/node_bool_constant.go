package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeBoolConstant is the class name for BoolConstantClass.
const NodeTypeBoolConstant = "pasta/BoolConstant"

// BoolConstantClass creates nodes with one editable bool value and one
// right-directed pasta/bool output port.
type BoolConstantClass struct{}

func (BoolConstantClass) ClassName() string        { return NodeTypeBoolConstant }
func (BoolConstantClass) ShortDescription() string { return "Boolean constant" }
func (BoolConstantClass) LongDescription() string {
	return "Outputs an editable bool value on one pasta/bool right-directed port. The label and menu checkbox show and allow changing the current value."
}
func (BoolConstantClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: TypeBool, InitialPorts: []pasta.Port{rightPort(TypeBool)}}
}
func (BoolConstantClass) NewNode(cfg configer.Config, _ ...*pasta.NodeClassState) (pasta.Node, error) {
	return newMutableBoolConstantNode(readBool(cfg, false)), nil
}

type mutableBoolConstantNode struct {
	pasta.BasicNode

	value bool
	w     *pasta.Workspace
	id    uint64
	out   uint64
}

func newMutableBoolConstantNode(value bool) *mutableBoolConstantNode {
	return &mutableBoolConstantNode{value: value}
}

func (n *mutableBoolConstantNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if restored != nil && len(restored.RightPorts) > 0 {
		n.out = restored.RightPorts[0]
	}
	if err := n.w.SetNodePrimary(n.id, TypeBool); err != nil {
		return err
	}
	if err := n.updateLabel(); err != nil {
		return err
	}
	n.sendMenuSnapshot()
	return nil
}

func (n *mutableBoolConstantNode) OnReady() error {
	n.sendAll()
	return nil
}

func (n *mutableBoolConstantNode) PreLinkAdd(port uint64, linkType, _ string) error {
	if port != n.out {
		return errUnsupportedType(linkType)
	}
	if linkType != TypeBool {
		return errUnsupportedType(linkType)
	}
	return nil
}

func (n *mutableBoolConstantNode) OnLinkAdd(link, port uint64, _, _ string) error {
	if port == n.out {
		n.sendToLink(link)
	}
	return nil
}

func (n *mutableBoolConstantNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if event.ReceiverPort != n.out || receiverPortDirection != "right" {
		return nil
	}
	if linkType == TypeBool && isBoolRequest(event.Payload) {
		n.sendToLinkByEvent(event)
	}
	return nil
}

// OnFormularMsg handles checkbox updates from the formular menu
func (n *mutableBoolConstantNode) OnFormularMsg(message any) error {
	msg, ok := message.(formular.FieldUpdateMessage)
	if !ok || msg.MenuID != pasta.NodeMenuID(n.id) || msg.Field.BlockID != "state" || msg.Field.FieldID != "value" {
		return nil
	}
	v, ok := msg.Value.(bool)
	if !ok {
		return nil
	}
	if v == n.value {
		return nil // no change, skip propagation
	}
	n.value = v
	if err := n.updateLabel(); err != nil {
		return err
	}
	n.sendMenuBlock() // update menu UI
	n.sendAll()       // propagate new value to all connected links
	return nil
}

func (n *mutableBoolConstantNode) OnSave(cfg configer.Config) error {
	if err := pasta.DeleteNodeOwnedConfigKeys(cfg); err != nil {
		return err
	}
	return cfg.Set(configer.Path{"value"}, n.value)
}

func (n *mutableBoolConstantNode) updateLabel() error {
	return n.w.SetNodeLabel(n.id, boolLabel(n.value))
}

func (n *mutableBoolConstantNode) sendAll() {
	port, ok := n.w.PortSnapshot(n.out)
	if !ok {
		return
	}
	for _, link := range port.Links {
		n.sendToLink(link)
	}
}

func (n *mutableBoolConstantNode) sendToLink(link uint64) {
	snapshot, ok := n.w.LinkSnapshot(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, n.out)
	n.w.SendEvent(pasta.Event{
		SenderNode:   n.id,
		SenderPort:   n.out,
		ReceiverNode: receiverNode,
		ReceiverPort: receiverPort,
		Payload:      n.value,
	})
}

func (n *mutableBoolConstantNode) sendToLinkByEvent(event pasta.Event) {
	n.w.SendEvent(pasta.Event{
		SenderNode:   n.id,
		SenderPort:   n.out,
		ReceiverNode: event.SenderNode,
		ReceiverPort: event.SenderPort,
		Payload:      n.value,
	})
}

func (n *mutableBoolConstantNode) sendMenuSnapshot() {
	n.w.SendNodeMenuMsg(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{
			Type:           formular.MessageMenuSnapshot,
			MenuID:         pasta.NodeMenuID(n.id),
			MenuGeneration: 1,
		},
		Blocks: []formular.Block{n.menuBlock()},
	})
}

func (n *mutableBoolConstantNode) sendMenuBlock() {
	n.w.SendNodeMenuMsg(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{
			Type:            formular.MessageBlockSnapshot,
			MenuID:          pasta.NodeMenuID(n.id),
			MenuGeneration:  1,
			BlockGeneration: 1,
		},
		Block: n.menuBlock(),
	})
}

// menuBlock returns the checkbox field WITHOUT Readonly: true
func (n *mutableBoolConstantNode) menuBlock() formular.Block {
	return formular.Block{
		ID: "state", Order: 10, Generation: 1,
		Items: []formular.Item{{
			Type:  formular.ItemField,
			ID:    "value",
			Label: "Value",
			Field: &formular.Field{
				Kind:  formular.FieldCheckbox,
				Value: n.value,
				// Readonly: true  <-- OMITTED to make it mutable
			},
		}},
	}
}
