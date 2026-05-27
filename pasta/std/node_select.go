package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeSelect is the class name for SelectClass.
const NodeTypeSelect = "pasta/Select"

// SelectClass creates a bidirectional selector node.
//
// Selector is a pasta/bool input. In 0, In 1, and Out start as any/any data
// ports. The first typed link attached to a data port derives one shared data
// type for all three data ports and the node primary type. When Selector is
// false, events are relayed between In 0 and Out; when true, events are relayed
// between In 1 and Out. When Select needs the active input value again, it sends
// RequestValue over the active input link.
type SelectClass struct{}

func (SelectClass) ClassName() string        { return NodeTypeSelect }
func (SelectClass) ShortDescription() string { return "Select one of two inputs" }
func (SelectClass) LongDescription() string {
	return "Relays events between Out and In 0 when Selector is false, or between Out and In 1 when Selector is true. The data ports derive a shared type from the first typed data link."
}
func (SelectClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{InitialPorts: []pasta.Port{
		{Direction: "right", Name: "Out", Types: []string{pasta.AnyType}},
		{Direction: "left", Name: "Selector", Types: []string{TypeBool}},
		{Direction: "left", Name: "In 0", Types: []string{pasta.AnyType}},
		{Direction: "left", Name: "In 1", Types: []string{pasta.AnyType}},
	}}
}
func (SelectClass) NewNode(_ configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
	n := newSelectNode()
	if state := firstState(previous); state != nil {
		n.dataType = state.PrimaryType
	}
	return n, nil
}

type selectNode struct {
	pasta.BasicNode

	selector bool
	dataType string

	w  *pasta.Workspace
	id uint64

	out          uint64
	selectorPort uint64
	in0          uint64
	in1          uint64
}

func newSelectNode() *selectNode {
	return &selectNode{}
}

func (n *selectNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if restored != nil {
		for _, port := range restored.RightPorts {
			snapshot, ok := w.PortSnapshot(port)
			if ok && snapshot.Name == "Out" {
				n.out = port
			}
		}
		for _, port := range restored.LeftPorts {
			snapshot, ok := w.PortSnapshot(port)
			if !ok {
				continue
			}
			switch snapshot.Name {
			case "Selector":
				n.selectorPort = port
			case "In 0":
				n.in0 = port
			case "In 1":
				n.in1 = port
			}
		}
	}
	if n.dataType != "" {
		if err := n.applyDataType(n.dataType); err != nil {
			return err
		}
	}
	if err := n.updateLabel(); err != nil {
		return err
	}
	n.sendMenuSnapshot()
	return nil
}

func (n *selectNode) OnReady() error {
	n.requestActive()
	return nil
}

func (n *selectNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	if portDirection == "left" {
		snapshot, ok := n.w.PortSnapshot(port)
		if ok && len(snapshot.Links) > 0 {
			return pasta.ErrLinkDup
		}
	}
	if port == n.selectorPort {
		if linkType != TypeBool {
			return errUnsupportedType(linkType)
		}
		return nil
	}
	if !n.isDataPort(port) {
		return errUnsupportedType(linkType)
	}
	if n.dataType != "" && linkType != n.dataType && linkType != pasta.AnyType {
		return errUnsupportedType(linkType)
	}
	return nil
}

func (n *selectNode) OnLinkAdd(_ uint64, port uint64, linkType, _ string) error {
	if n.isDataPort(port) && n.dataType == "" && linkType != pasta.AnyType {
		if err := n.applyDataType(linkType); err != nil {
			return err
		}
	}
	if port == n.selectorPort || port == n.activeInput() {
		n.requestPort(port)
	}
	if port == n.out {
		n.requestActive()
	}
	return nil
}

func (n *selectNode) OnLinkRemoved(_ uint64, port uint64, _ string, _ string) error {
	if port == n.activeInput() {
		n.requestActive()
	}
	return nil
}

func (n *selectNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if event.ReceiverPort == n.selectorPort {
		value, ok := event.Payload.(bool)
		if !ok {
			return nil
		}
		changed := n.selector != value
		n.selector = value
		_ = n.updateLabel()
		n.sendMenuBlock()
		if changed {
			n.requestActive()
		}
		return nil
	}
	if n.isDataPort(event.ReceiverPort) && n.dataType == "" && linkType != pasta.AnyType {
		if err := n.applyDataType(linkType); err != nil {
			return err
		}
	}
	if receiverPortDirection == "left" {
		if event.ReceiverPort == n.activeInput() {
			n.forwardToOut(event.Payload)
		}
		return nil
	}
	if event.ReceiverPort == n.out {
		n.forwardToActiveInput(event.Payload)
	}
	return nil
}

func (n *selectNode) applyDataType(typ string) error {
	n.dataType = typ
	if err := n.w.SetNodePrimary(n.id, typ); err != nil {
		return err
	}
	for _, port := range []uint64{n.in0, n.in1, n.out} {
		if port == 0 {
			continue
		}
		if err := n.w.SetPortTypes(port, []string{typ}); err != nil {
			return err
		}
	}
	return nil
}

func (n *selectNode) activeInput() uint64 {
	if n.selector {
		return n.in1
	}
	return n.in0
}

func (n *selectNode) isDataPort(port uint64) bool {
	return port == n.in0 || port == n.in1 || port == n.out
}

func (n *selectNode) requestActive() {
	n.requestPort(n.activeInput())
}

func (n *selectNode) requestPort(port uint64) {
	snapshot, ok := n.w.PortSnapshot(port)
	if !ok || len(snapshot.Links) == 0 {
		return
	}
	for _, link := range snapshot.Links {
		linkSnapshot, ok := n.w.LinkSnapshot(link)
		if !ok {
			continue
		}
		receiverNode, receiverPort := otherEndpoint(linkSnapshot, port)
		n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: port, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: RequestValue{}})
	}
}

func (n *selectNode) forwardToOut(payload any) {
	snapshot, ok := n.w.PortSnapshot(n.out)
	if !ok {
		return
	}
	for _, link := range snapshot.Links {
		linkSnapshot, ok := n.w.LinkSnapshot(link)
		if !ok {
			continue
		}
		receiverNode, receiverPort := otherEndpoint(linkSnapshot, n.out)
		n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: payload})
	}
}

func (n *selectNode) forwardToActiveInput(payload any) {
	port := n.activeInput()
	snapshot, ok := n.w.PortSnapshot(port)
	if !ok || len(snapshot.Links) == 0 {
		return
	}
	linkSnapshot, ok := n.w.LinkSnapshot(snapshot.Links[0])
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(linkSnapshot, port)
	n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: port, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: payload})
}

func (n *selectNode) updateLabel() error {
	if n.selector {
		return n.w.SetNodeLabel(n.id, "1")
	}
	return n.w.SetNodeLabel(n.id, "0")
}

func (n *selectNode) sendMenuSnapshot() {
	n.w.SendNodeMenuMsg(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageMenuSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1},
		Blocks:      []formular.Block{n.menuBlock()},
	})
}

func (n *selectNode) sendMenuBlock() {
	if n.w == nil || n.id == 0 {
		return
	}
	n.w.SendNodeMenuMsg(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageBlockSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1, BlockGeneration: 1},
		Block:       n.menuBlock(),
	})
}

func (n *selectNode) menuBlock() formular.Block {
	return formular.Block{
		ID: "state", Order: 10, Generation: 1,
		Items: []formular.Item{{
			Type:  formular.ItemField,
			ID:    "selector",
			Label: "Selector",
			Field: &formular.Field{Kind: formular.FieldCheckbox, Value: n.selector, Readonly: true},
		}},
	}
}
