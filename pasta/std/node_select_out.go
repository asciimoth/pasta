package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeSelectOut is the class name for SelectOutClass.
const NodeTypeSelectOut = "pasta/SelectOut"

// SelectOutClass creates a bidirectional output selector node.
//
// Selector is a pasta/bool input. In, Out 0, and Out 1 start as any/any data
// ports. The first typed link attached to a data port derives one shared data
// type for all three data ports and the node primary type. When Selector is
// false, events are relayed between In and Out 0; when true, events are relayed
// between In and Out 1. When SelectOut needs the active path value again, it
// sends RequestValue over both sides of the active data path. Payloads
// and closed when SelectOut switches to another path.
type SelectOutClass struct{}

func (SelectOutClass) ClassName() string        { return NodeTypeSelectOut }
func (SelectOutClass) ShortDescription() string { return "Select one of two outputs" }
func (SelectOutClass) LongDescription() string {
	return "Relays events between In and Out 0 when Selector is false, or between In and Out 1 when Selector is true. The data ports derive a shared type from the first typed data link."
}
func (SelectOutClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{InitialPorts: []pasta.Port{
		{Direction: "left", Name: "In", Types: []string{pasta.AnyType}},
		{Direction: "left", Name: "Selector", Types: []string{TypeBool}},
		{Direction: "right", Name: "Out 0", Types: []string{pasta.AnyType}},
		{Direction: "right", Name: "Out 1", Types: []string{pasta.AnyType}},
	}}
}
func (SelectOutClass) NewNode(_ configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
	n := newSelectOutNode()
	if state := firstState(previous); state != nil {
		n.dataType = state.PrimaryType
	}
	return n, nil
}

type selectOutNode struct {
	pasta.BasicNode

	selector bool
	dataType string

	w  *pasta.Workspace
	id uint64

	in           uint64
	selectorPort uint64
	out0         uint64
	out1         uint64

	payloads []ClosablePayload
}

func newSelectOutNode() *selectOutNode {
	return &selectOutNode{}
}

func (n *selectOutNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if restored != nil {
		for _, port := range restored.LeftPorts {
			snapshot, ok := w.PortSnapshotLocked(port)
			if !ok {
				continue
			}
			switch snapshot.Name {
			case "In":
				n.in = port
			case "Selector":
				n.selectorPort = port
			}
		}
		for _, port := range restored.RightPorts {
			snapshot, ok := w.PortSnapshotLocked(port)
			if !ok {
				continue
			}
			switch snapshot.Name {
			case "Out 0":
				n.out0 = port
			case "Out 1":
				n.out1 = port
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

func (n *selectOutNode) OnReady() error {
	n.requestActivePath()
	return nil
}

func (n *selectOutNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	if linkType == TypeLoop {
		return pasta.LinkTypeErr(linkType)
	}
	if portDirection == "left" && (port == n.selectorPort || linkType != TypeTrigger) {
		snapshot, ok := n.w.PortSnapshotLocked(port)
		if ok && len(snapshot.Links) > 0 {
			return pasta.ErrLinkDup
		}
	}
	if port == n.selectorPort {
		if linkType != TypeBool {
			return pasta.LinkTypeErr(linkType)
		}
		return nil
	}
	if !n.isDataPort(port) {
		return pasta.LinkTypeErr(linkType)
	}
	if n.dataType != "" && linkType != n.dataType && linkType != pasta.AnyType {
		return pasta.LinkTypeErr(linkType)
	}
	return nil
}

func (n *selectOutNode) OnLinkAdd(link uint64, port uint64, linkType, _ string) error {
	if n.isDataPort(port) && n.dataType == "" && linkType != pasta.AnyType {
		if err := n.applyDataType(linkType); err != nil {
			return err
		}
	}
	if port != n.selectorPort && (n.dataType == TypeTrigger || linkType == TypeTrigger) {
		return nil
	}
	if port == n.selectorPort || port == n.in {
		n.requestLink(link, port)
	}
	if port == n.activeOutput() {
		n.requestActivePath()
	}
	return nil
}

func (n *selectOutNode) OnLinkRemoved(_ uint64, port uint64, _ string, _ string) error {
	if n.dataType == TypeTrigger {
		return nil
	}
	if port == n.activeOutput() {
		n.requestActivePath()
	}
	return nil
}

func (n *selectOutNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
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
			n.closePayloads()
			n.requestActivePath()
		}
		return nil
	}
	if n.isDataPort(event.ReceiverPort) && n.dataType == "" && linkType != pasta.AnyType {
		if err := n.applyDataType(linkType); err != nil {
			return err
		}
	}
	if linkType == TypeTrigger {
		if receiverPortDirection == "left" && event.ReceiverPort == n.in && !IsRequest(event.Payload) {
			n.forwardToActiveOutput(event.Payload)
		}
		return nil
	}
	if receiverPortDirection == "left" {
		if event.ReceiverPort == n.in {
			n.trackPayload(event.Payload)
			n.forwardToActiveOutput(event.Payload)
		}
		return nil
	}
	if event.ReceiverPort == n.activeOutput() {
		n.trackPayload(event.Payload)
		n.forwardToIn(event.Payload)
	}
	return nil
}

func (n *selectOutNode) applyDataType(typ string) error {
	n.dataType = typ
	if err := n.w.SetNodePrimaryLocked(n.id, typ); err != nil {
		return err
	}
	for _, port := range []uint64{n.in, n.out0, n.out1} {
		if port == 0 {
			continue
		}
		if err := n.w.SetPortTypesLocked(port, []string{typ}); err != nil {
			return err
		}
	}
	return nil
}

func (n *selectOutNode) activeOutput() uint64 {
	if n.selector {
		return n.out1
	}
	return n.out0
}

func (n *selectOutNode) isDataPort(port uint64) bool {
	return port == n.in || port == n.out0 || port == n.out1
}

func (n *selectOutNode) requestActivePath() {
	if n.dataType == TypeTrigger {
		return
	}
	n.requestPort(n.in)
	n.requestPort(n.activeOutput())
}

func (n *selectOutNode) requestPort(port uint64) {
	snapshot, ok := n.w.PortSnapshotLocked(port)
	if !ok || len(snapshot.Links) == 0 {
		return
	}
	for _, link := range snapshot.Links {
		n.requestLink(link, port)
	}
}

func (n *selectOutNode) requestLink(link, port uint64) {
	linkSnapshot, ok := n.w.LinkSnapshotLocked(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(linkSnapshot, port)
	n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: port, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: RequestValue{}})
}

func (n *selectOutNode) forwardToActiveOutput(payload any) {
	port := n.activeOutput()
	snapshot, ok := n.w.PortSnapshotLocked(port)
	if !ok {
		return
	}
	for _, link := range snapshot.Links {
		linkSnapshot, ok := n.w.LinkSnapshotLocked(link)
		if !ok {
			continue
		}
		receiverNode, receiverPort := otherEndpoint(linkSnapshot, port)
		n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: port, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: payload})
	}
}

func (n *selectOutNode) forwardToIn(payload any) {
	snapshot, ok := n.w.PortSnapshotLocked(n.in)
	if !ok || len(snapshot.Links) == 0 {
		return
	}
	linkSnapshot, ok := n.w.LinkSnapshotLocked(snapshot.Links[0])
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(linkSnapshot, n.in)
	n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: n.in, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: payload})
}

func (n *selectOutNode) trackPayload(payload any) {
	closable, ok := payload.(ClosablePayload)
	if !ok || closable == nil {
		return
	}
	n.payloads = append(n.payloads, closable)
}

func (n *selectOutNode) closePayloads() {
	for _, payload := range n.payloads {
		if payload != nil {
			pasta.CloseBackground(payload)
		}
	}
	n.payloads = nil
}

func (n *selectOutNode) updateLabel() error {
	if n.selector {
		return n.w.SetNodeLabelLocked(n.id, "in -> out 1")
	}
	return n.w.SetNodeLabelLocked(n.id, "in -> out 0")
}

func (n *selectOutNode) sendMenuSnapshot() {
	n.w.SendNodeMenuMsgLocked(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageMenuSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1},
		Blocks:      []formular.Block{n.menuBlock()},
	})
}

func (n *selectOutNode) sendMenuBlock() {
	if n.w == nil || n.id == 0 {
		return
	}
	n.w.SendNodeMenuMsgLocked(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageBlockSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1, BlockGeneration: 1},
		Block:       n.menuBlock(),
	})
}

func (n *selectOutNode) menuBlock() formular.Block {
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
