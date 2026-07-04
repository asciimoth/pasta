package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

type boolNode struct {
	pasta.BasicNode

	op     string
	value  bool
	inputs map[uint64]bool

	w     *pasta.Workspace
	id    uint64
	out   uint64
	lefts []uint64
}

func newBoolConstantNode(value bool) *boolNode {
	return &boolNode{op: "const", value: value, inputs: map[uint64]bool{}}
}

func newBoolNode(op string) *boolNode {
	return &boolNode{op: op, inputs: map[uint64]bool{}}
}

func (n *boolNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if restored != nil {
		if len(restored.RightPorts) > 0 {
			n.out = restored.RightPorts[0]
		}
		n.lefts = append([]uint64{}, restored.LeftPorts...)
	}
	if err := n.w.SetNodePrimaryLocked(n.id, TypeBool); err != nil {
		return err
	}
	n.recalculate(false)
	if err := n.updateLabel(); err != nil {
		return err
	}
	n.sendMenuSnapshot()
	return nil
}

func (n *boolNode) OnReady() error {
	n.requestAll()
	n.sendAll()
	return nil
}

func (n *boolNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	if linkType != TypeBool {
		return pasta.LinkTypeErr(linkType)
	}
	if portDirection == "left" {
		snapshot, ok := n.w.PortSnapshotLocked(port)
		if ok && len(snapshot.Links) > 0 {
			return pasta.ErrLinkDup
		}
	}
	return nil
}

func (n *boolNode) OnLinkAdd(link, port uint64, _ string, portDirection string) error {
	if portDirection == "left" {
		n.requestLink(link, port)
		return nil
	}
	if port == n.out {
		n.sendToLink(link)
	}
	return nil
}

func (n *boolNode) OnLinkRemoved(_ uint64, port uint64, _ string, portDirection string) error {
	if portDirection == "left" {
		delete(n.inputs, port)
		n.recalculate(true)
	}
	return nil
}

func (n *boolNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if linkType != TypeBool {
		return nil
	}
	if receiverPortDirection == "right" {
		if isBoolRequest(event.Payload) {
			n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: event.SenderNode, ReceiverPort: event.SenderPort, Payload: n.value})
		}
		return nil
	}
	value, ok := event.Payload.(bool)
	if !ok {
		return nil
	}
	n.inputs[event.ReceiverPort] = value
	n.recalculate(true)
	return nil
}

func (n *boolNode) OnSave(cfg configer.Config) error {
	if n.op != "const" {
		return nil
	}
	if err := pasta.DeleteNodeOwnedConfigKeys(cfg); err != nil {
		return err
	}
	return cfg.Set(configer.Path{"value"}, n.value)
}

func (n *boolNode) recalculate(broadcast bool) {
	old := n.value
	switch n.op {
	case "and":
		n.value = n.input(0) && n.input(1)
	case "or":
		n.value = n.input(0) || n.input(1)
	case "not":
		n.value = !n.input(0)
	}
	_ = n.updateLabel()
	n.sendMenuBlock()
	if broadcast && old != n.value {
		n.sendAll()
	}
}

func (n *boolNode) input(index int) bool {
	if index >= len(n.lefts) {
		return false
	}
	return n.inputs[n.lefts[index]]
}

func (n *boolNode) requestAll() {
	for _, port := range n.lefts {
		snapshot, ok := n.w.PortSnapshotLocked(port)
		if !ok {
			continue
		}
		for _, link := range snapshot.Links {
			n.requestLink(link, port)
		}
	}
}

func (n *boolNode) requestLink(link, port uint64) {
	snapshot, ok := n.w.LinkSnapshotLocked(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, port)
	n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: port, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: RequestValue{}})
}

func (n *boolNode) sendAll() {
	port, ok := n.w.PortSnapshotLocked(n.out)
	if !ok {
		return
	}
	for _, link := range port.Links {
		n.sendToLink(link)
	}
}

func (n *boolNode) sendToLink(link uint64) {
	snapshot, ok := n.w.LinkSnapshotLocked(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, n.out)
	n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: n.value})
}

func (n *boolNode) updateLabel() error {
	return n.w.SetNodeLabelLocked(n.id, boolLabel(n.value))
}

func (n *boolNode) sendMenuSnapshot() {
	n.w.SendNodeMenuMsgLocked(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageMenuSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1},
		Blocks:      []formular.Block{n.menuBlock()},
	})
}

func (n *boolNode) sendMenuBlock() {
	if n.w == nil || n.id == 0 {
		return
	}
	n.w.SendNodeMenuMsgLocked(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageBlockSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1, BlockGeneration: 1},
		Block:       n.menuBlock(),
	})
}

func (n *boolNode) menuBlock() formular.Block {
	return formular.Block{
		ID: "state", Order: 10, Generation: 1,
		Items: []formular.Item{{
			Type:  formular.ItemField,
			ID:    "value",
			Label: "Value",
			Field: &formular.Field{Kind: formular.FieldCheckbox, Value: n.value, Readonly: true},
		}},
	}
}

func boolLabel(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func isBoolRequest(payload any) bool {
	_, ok := payload.(RequestValue)
	return ok
}
