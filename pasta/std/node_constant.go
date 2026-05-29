package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

type constantNode struct {
	pasta.BasicNode

	value numberValue
	w     *pasta.Workspace
	id    uint64
	out   uint64
}

func newConstantNode(typ string, value any) *constantNode {
	switch typ {
	case TypeInt:
		v, _ := value.(int)
		return &constantNode{value: intValue(v)}
	case TypeFloat:
		v, _ := value.(float64)
		return &constantNode{value: floatValue(v)}
	default:
		return &constantNode{value: intValue(0)}
	}
}

func (n *constantNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if restored != nil && len(restored.RightPorts) > 0 {
		n.out = restored.RightPorts[0]
	}
	if err := n.w.SetNodePrimary(n.id, n.value.typ); err != nil {
		return err
	}
	if err := n.updateLabel(); err != nil {
		return err
	}
	n.sendMenuSnapshot()
	return nil
}

func (n *constantNode) OnReady() error {
	n.sendAll()
	return nil
}

func (n *constantNode) PreLinkAdd(port uint64, linkType, _ string) error {
	if port != n.out {
		return pasta.LinkTypeErr(linkType)
	}
	return nil
}

func (n *constantNode) OnLinkAdd(link, port uint64, _, _ string) error {
	if port == n.out {
		n.sendToLink(link)
	}
	return nil
}

func (n *constantNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if event.ReceiverPort != n.out || receiverPortDirection != "right" {
		return nil
	}
	if (linkType == TypeInt || linkType == TypeFloat) && isValueRequest(event.Payload) {
		n.sendToLinkByEvent(event)
	}
	return nil
}

func (n *constantNode) OnFormularMsg(message any) error {
	msg, ok := message.(formular.FieldUpdateMessage)
	if !ok || msg.MenuID != pasta.NodeMenuID(n.id) || msg.Field.BlockID != "state" || msg.Field.FieldID != "value" {
		return nil
	}
	next := n.value // nolint
	if n.value.typ == TypeFloat {
		v, ok := parseFloatAny(msg.Value)
		if !ok {
			return nil
		}
		next = floatValue(v)
	} else {
		v, ok := parseIntAny(msg.Value)
		if !ok {
			return nil
		}
		next = intValue(v)
	}
	if next.payload() == n.value.payload() {
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

func (n *constantNode) OnSave(cfg configer.Config) error {
	if err := pasta.DeleteNodeOwnedConfigKeys(cfg); err != nil {
		return err
	}
	return cfg.Set(configer.Path{"value"}, formatSaveValue(n.value))
}

func (n *constantNode) updateLabel() error {
	return n.w.SetNodeLabel(n.id, n.value.label())
}

func (n *constantNode) sendAll() {
	port, ok := n.w.PortSnapshot(n.out)
	if !ok {
		return
	}
	for _, link := range port.Links {
		n.sendToLink(link)
	}
}

func (n *constantNode) sendToLink(link uint64) {
	snapshot, ok := n.w.LinkSnapshot(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, n.out)
	n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: n.value.payload()})
}

func (n *constantNode) sendToLinkByEvent(event pasta.Event) {
	n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: event.SenderNode, ReceiverPort: event.SenderPort, Payload: n.value.payload()})
}

func (n *constantNode) sendMenuSnapshot() {
	n.w.SendNodeMenuMsg(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageMenuSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1},
		Blocks:      []formular.Block{n.menuBlock()},
	})
}

func (n *constantNode) sendMenuBlock() {
	n.w.SendNodeMenuMsg(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageBlockSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1, BlockGeneration: 1},
		Block:       n.menuBlock(),
	})
}

func (n *constantNode) menuBlock() formular.Block {
	return formular.Block{
		ID: "state", Order: 10, Generation: 1,
		Items: []formular.Item{{Type: formular.ItemField, ID: "value", Label: "Value", Field: &formular.Field{Kind: menuFieldKind(n.value.typ), Value: n.value.menuValue()}}},
	}
}
