package std

import (
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

type compareNode struct {
	pasta.BasicNode

	op     string
	value  bool
	inputs map[uint64]Comparable

	w     *pasta.Workspace
	id    uint64
	out   uint64
	lefts []uint64
}

func newCompareNode(op string) *compareNode {
	return &compareNode{op: op, inputs: map[uint64]Comparable{}}
}

func (n *compareNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if restored != nil {
		if len(restored.RightPorts) > 0 {
			n.out = restored.RightPorts[0]
		}
		n.lefts = append([]uint64{}, restored.LeftPorts...)
	}
	if err := n.w.SetNodePrimary(n.id, TypeBool); err != nil {
		return err
	}
	n.recalculate(false)
	if err := n.updateLabel(); err != nil {
		return err
	}
	n.sendMenuSnapshot()
	return nil
}

func (n *compareNode) OnReady() error {
	n.requestAll()
	n.sendAll()
	return nil
}

func (n *compareNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	switch portDirection {
	case "left":
		snapshot, ok := n.w.PortSnapshot(port)
		if ok && len(snapshot.Links) > 0 {
			return pasta.ErrLinkDup
		}
		return nil
	case "right":
		if linkType != TypeBool {
			return errUnsupportedType(linkType)
		}
		return nil
	default:
		return errUnsupportedType(linkType)
	}
}

func (n *compareNode) OnLinkAdd(link, port uint64, linkType, portDirection string) error {
	if portDirection == "left" {
		n.requestLink(link, port, linkType)
		return nil
	}
	if port == n.out {
		n.sendToLink(link)
	}
	return nil
}

func (n *compareNode) OnLinkRemoved(_ uint64, port uint64, _ string, portDirection string) error {
	if portDirection == "left" {
		delete(n.inputs, port)
		n.recalculate(true)
	}
	return nil
}

func (n *compareNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if receiverPortDirection == "right" {
		if linkType == TypeBool && isBoolRequest(event.Payload) {
			n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: event.SenderNode, ReceiverPort: event.SenderPort, Payload: n.value})
		}
		return nil
	}
	value, ok := event.Payload.(Comparable)
	if !ok {
		return nil
	}
	n.inputs[event.ReceiverPort] = value
	n.recalculate(true)
	return nil
}

func (n *compareNode) recalculate(broadcast bool) {
	old := n.value
	left := n.input(0)
	right := n.input(1)
	switch n.op {
	case "more":
		n.value = left.More(right)
	case "less":
		n.value = left.Less(right)
	case "equal":
		n.value = left.Equal(right)
	case "notEqual":
		n.value = left.NotEqual(right)
	}
	_ = n.updateLabel()
	n.sendMenuBlock()
	if broadcast && old != n.value {
		n.sendAll()
	}
}

func (n *compareNode) input(index int) Comparable {
	if index >= len(n.lefts) {
		return Int(0)
	}
	if value, ok := n.inputs[n.lefts[index]]; ok {
		return value
	}
	return Int(0)
}

func (n *compareNode) requestAll() {
	for _, port := range n.lefts {
		snapshot, ok := n.w.PortSnapshot(port)
		if !ok {
			continue
		}
		for _, link := range snapshot.Links {
			n.requestLink(link, port, "")
		}
	}
}

func (n *compareNode) requestLink(link, port uint64, linkType string) {
	snapshot, ok := n.w.LinkSnapshot(link)
	if !ok {
		return
	}
	if linkType == "" {
		linkType = snapshot.Type
	}
	var payload any = RequestIntValue{}
	if linkType == TypeFloat {
		payload = RequestFloatValue{}
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, port)
	n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: port, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: payload})
}

func (n *compareNode) sendAll() {
	port, ok := n.w.PortSnapshot(n.out)
	if !ok {
		return
	}
	for _, link := range port.Links {
		n.sendToLink(link)
	}
}

func (n *compareNode) sendToLink(link uint64) {
	snapshot, ok := n.w.LinkSnapshot(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, n.out)
	n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: n.value})
}

func (n *compareNode) updateLabel() error {
	return n.w.SetNodeLabel(n.id, boolLabel(n.value))
}

func (n *compareNode) sendMenuSnapshot() {
	n.w.SendNodeMenuMsg(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageMenuSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1},
		Blocks:      []formular.Block{n.menuBlock()},
	})
}

func (n *compareNode) sendMenuBlock() {
	if n.w == nil || n.id == 0 {
		return
	}
	n.w.SendNodeMenuMsg(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageBlockSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1, BlockGeneration: 1},
		Block:       n.menuBlock(),
	})
}

func (n *compareNode) menuBlock() formular.Block {
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
