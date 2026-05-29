package std

import (
	"strings"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeStringSplit is the class name for StringSplitClass.
const NodeTypeStringSplit = "pasta/StringSplit"

// StringSplitClass creates two-input nodes that split a string at the first
// separator occurrence and output the before and after parts.
type StringSplitClass struct{}

func (StringSplitClass) ClassName() string        { return NodeTypeStringSplit }
func (StringSplitClass) ShortDescription() string { return "Split string" }
func (StringSplitClass) LongDescription() string {
	return "Splits Text at the first Separator occurrence. Before receives the text before the separator and After receives the text after it. If the separator is empty or missing, Before is Text and After is empty."
}
func (StringSplitClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: TypeString, InitialPorts: []pasta.Port{
		{Direction: "right", Name: "Before", Types: []string{TypeString}},
		{Direction: "right", Name: "After", Types: []string{TypeString}},
		{Direction: "left", Name: "Text", Types: []string{TypeString}},
		{Direction: "left", Name: "Separator", Types: []string{TypeString}},
	}}
}
func (StringSplitClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	return newStringSplitNode(), nil
}

type stringSplitNode struct {
	pasta.BasicNode

	inputs map[uint64]string
	before string
	after  string

	w  *pasta.Workspace
	id uint64

	beforePort    uint64
	afterPort     uint64
	textPort      uint64
	separatorPort uint64
}

func newStringSplitNode() *stringSplitNode {
	return &stringSplitNode{inputs: map[uint64]string{}}
}

func (n *stringSplitNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if restored != nil {
		for _, port := range restored.RightPorts {
			snapshot, ok := w.PortSnapshot(port)
			if !ok {
				continue
			}
			switch snapshot.Name {
			case "Before":
				n.beforePort = port
			case "After":
				n.afterPort = port
			}
		}
		for _, port := range restored.LeftPorts {
			snapshot, ok := w.PortSnapshot(port)
			if !ok {
				continue
			}
			switch snapshot.Name {
			case "Text":
				n.textPort = port
			case "Separator":
				n.separatorPort = port
			}
		}
	}
	if err := n.w.SetNodePrimary(n.id, TypeString); err != nil {
		return err
	}
	n.recalculate(false)
	if err := n.updateLabel(); err != nil {
		return err
	}
	n.sendMenuSnapshot()
	return nil
}

func (n *stringSplitNode) OnReady() error {
	n.requestAll()
	n.sendAll()
	return nil
}

func (n *stringSplitNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	if linkType != TypeString {
		return pasta.LinkTypeErr(linkType)
	}
	if portDirection == "left" {
		snapshot, ok := n.w.PortSnapshot(port)
		if ok && len(snapshot.Links) > 0 {
			return pasta.ErrLinkDup
		}
		return nil
	}
	if port == n.beforePort || port == n.afterPort {
		return nil
	}
	return pasta.LinkTypeErr(linkType)
}

func (n *stringSplitNode) OnLinkAdd(link, port uint64, _ string, portDirection string) error {
	if portDirection == "left" {
		n.requestLink(link, port)
		return nil
	}
	n.sendToLink(link, port)
	return nil
}

func (n *stringSplitNode) OnLinkRemoved(_ uint64, port uint64, _ string, portDirection string) error {
	if portDirection == "left" {
		delete(n.inputs, port)
		n.recalculate(true)
	}
	return nil
}

func (n *stringSplitNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if linkType != TypeString {
		return nil
	}
	if receiverPortDirection == "right" {
		if isValueRequest(event.Payload) {
			n.w.SendEvent(pasta.Event{
				SenderNode:   n.id,
				SenderPort:   event.ReceiverPort,
				ReceiverNode: event.SenderNode,
				ReceiverPort: event.SenderPort,
				Payload:      String(n.valueForPort(event.ReceiverPort)),
			})
		}
		return nil
	}
	value, ok := parseStringAny(event.Payload)
	if !ok {
		return nil
	}
	n.inputs[event.ReceiverPort] = value
	n.recalculate(true)
	return nil
}

func (n *stringSplitNode) recalculate(broadcast bool) {
	oldBefore, oldAfter := n.before, n.after
	text := n.input(n.textPort)
	separator := n.input(n.separatorPort)
	if separator == "" {
		n.before = text
		n.after = ""
	} else {
		before, after, found := strings.Cut(text, separator)
		if found {
			n.before = before
			n.after = after
		} else {
			n.before = text
			n.after = ""
		}
	}
	_ = n.updateLabel()
	n.sendMenuBlock()
	if !broadcast {
		return
	}
	if oldBefore != n.before {
		n.sendPort(n.beforePort)
	}
	if oldAfter != n.after {
		n.sendPort(n.afterPort)
	}
}

func (n *stringSplitNode) input(port uint64) string {
	return n.inputs[port]
}

func (n *stringSplitNode) valueForPort(port uint64) string {
	if port == n.afterPort {
		return n.after
	}
	return n.before
}

func (n *stringSplitNode) requestAll() {
	for _, port := range []uint64{n.textPort, n.separatorPort} {
		snapshot, ok := n.w.PortSnapshot(port)
		if !ok {
			continue
		}
		for _, link := range snapshot.Links {
			n.requestLink(link, port)
		}
	}
}

func (n *stringSplitNode) requestLink(link, port uint64) {
	snapshot, ok := n.w.LinkSnapshot(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, port)
	n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: port, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: RequestValue{}})
}

func (n *stringSplitNode) sendAll() {
	n.sendPort(n.beforePort)
	n.sendPort(n.afterPort)
}

func (n *stringSplitNode) sendPort(port uint64) {
	snapshot, ok := n.w.PortSnapshot(port)
	if !ok {
		return
	}
	for _, link := range snapshot.Links {
		n.sendToLink(link, port)
	}
}

func (n *stringSplitNode) sendToLink(link, port uint64) {
	snapshot, ok := n.w.LinkSnapshot(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, port)
	n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: port, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: String(n.valueForPort(port))})
}

func (n *stringSplitNode) updateLabel() error {
	return n.w.SetNodeLabel(n.id, n.before+" | "+n.after)
}

func (n *stringSplitNode) sendMenuSnapshot() {
	n.w.SendNodeMenuMsg(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageMenuSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1},
		Blocks:      []formular.Block{n.menuBlock()},
	})
}

func (n *stringSplitNode) sendMenuBlock() {
	if n.w == nil || n.id == 0 {
		return
	}
	n.w.SendNodeMenuMsg(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageBlockSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1, BlockGeneration: 1},
		Block:       n.menuBlock(),
	})
}

func (n *stringSplitNode) menuBlock() formular.Block {
	return formular.Block{
		ID: "state", Order: 10, Generation: 1,
		Items: []formular.Item{
			{Type: formular.ItemField, ID: "before", Label: "Before", Field: &formular.Field{Kind: formular.FieldText, Value: n.before, Readonly: true}},
			{Type: formular.ItemField, ID: "after", Label: "After", Field: &formular.Field{Kind: formular.FieldText, Value: n.after, Readonly: true}},
		},
	}
}
