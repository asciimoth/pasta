package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeStringConstant is the class name for StringConstantClass.
const NodeTypeStringConstant = "pasta/StringConstant"

// StringConstantClass creates nodes with one editable string value and one
// right-directed pasta/string output port.
type StringConstantClass struct{}

func (StringConstantClass) ClassName() string        { return NodeTypeStringConstant }
func (StringConstantClass) ShortDescription() string { return "String constant" }
func (StringConstantClass) LongDescription() string {
	return "Outputs an editable string value on one pasta/string right-directed port. The label and menu field always show the current value."
}
func (StringConstantClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: TypeString, InitialPorts: []pasta.Port{rightPort(TypeString)}}
}
func (StringConstantClass) NewNode(cfg configer.Config, _ ...*pasta.NodeClassState) (pasta.Node, error) {
	return newStringConstantNode(readString(cfg, "")), nil
}

type stringConstantNode struct {
	pasta.BasicNode

	value string
	w     *pasta.Workspace
	id    uint64
	out   uint64
}

func newStringConstantNode(value string) *stringConstantNode {
	return &stringConstantNode{value: value}
}

func (n *stringConstantNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if restored != nil && len(restored.RightPorts) > 0 {
		n.out = restored.RightPorts[0]
	}
	if err := n.w.SetNodePrimary(n.id, TypeString); err != nil {
		return err
	}
	if err := n.updateLabel(); err != nil {
		return err
	}
	n.sendMenuSnapshot()
	return nil
}

func (n *stringConstantNode) OnReady() error {
	n.sendAll()
	return nil
}

func (n *stringConstantNode) PreLinkAdd(port uint64, linkType, _ string) error {
	if port != n.out || linkType != TypeString {
		return errUnsupportedType(linkType)
	}
	return nil
}

func (n *stringConstantNode) OnLinkAdd(link, port uint64, _, _ string) error {
	if port == n.out {
		n.sendToLink(link)
	}
	return nil
}

func (n *stringConstantNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if event.ReceiverPort != n.out || receiverPortDirection != "right" || linkType != TypeString || !isValueRequest(event.Payload) {
		return nil
	}
	n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: event.SenderNode, ReceiverPort: event.SenderPort, Payload: String(n.value)})
	return nil
}

func (n *stringConstantNode) OnFormularMsg(message any) error {
	msg, ok := message.(formular.FieldUpdateMessage)
	if !ok || msg.MenuID != pasta.NodeMenuID(n.id) || msg.Field.BlockID != "state" || msg.Field.FieldID != "value" {
		return nil
	}
	next, ok := parseStringAny(msg.Value)
	if !ok || next == n.value {
		return nil
	}
	n.value = next
	if err := n.updateLabel(); err != nil {
		return err
	}
	n.sendMenuBlock()
	n.sendAll()
	return nil
}

func (n *stringConstantNode) OnSave(cfg configer.Config) error {
	if err := pasta.DeleteNodeOwnedConfigKeys(cfg); err != nil {
		return err
	}
	return cfg.Set(configer.Path{"value"}, n.value)
}

func (n *stringConstantNode) updateLabel() error {
	return n.w.SetNodeLabel(n.id, n.value)
}

func (n *stringConstantNode) sendAll() {
	port, ok := n.w.PortSnapshot(n.out)
	if !ok {
		return
	}
	for _, link := range port.Links {
		n.sendToLink(link)
	}
}

func (n *stringConstantNode) sendToLink(link uint64) {
	snapshot, ok := n.w.LinkSnapshot(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, n.out)
	n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: String(n.value)})
}

func (n *stringConstantNode) sendMenuSnapshot() {
	n.w.SendNodeMenuMsg(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageMenuSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1},
		Blocks:      []formular.Block{n.menuBlock()},
	})
}

func (n *stringConstantNode) sendMenuBlock() {
	n.w.SendNodeMenuMsg(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageBlockSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1, BlockGeneration: 1},
		Block:       n.menuBlock(),
	})
}

func (n *stringConstantNode) menuBlock() formular.Block {
	return formular.Block{
		ID: "state", Order: 10, Generation: 1,
		Items: []formular.Item{{Type: formular.ItemField, ID: "value", Label: "Value", Field: &formular.Field{Kind: formular.FieldText, Value: n.value}}},
	}
}
