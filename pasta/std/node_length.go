package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
	"github.com/asciimoth/persist"
)

// NodeTypeLength is the class name for LengthClass.
const NodeTypeLength = "pasta/Length"

// LengthClass creates one-input nodes that measure top-level object values.
type LengthClass struct{}

func (LengthClass) ClassName() string        { return NodeTypeLength }
func (LengthClass) ShortDescription() string { return "Object length" }
func (LengthClass) LongDescription() string {
	return "Outputs the top-level size of one pasta/object input as pasta/int: map entry count, vector length, string byte length, or 0 for all other values."
}
func (LengthClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: TypeInt, InitialPorts: []pasta.Port{
		rightPort(TypeInt),
		{Direction: "left", Name: "input", Types: []string{TypeObject}},
	}}
}
func (LengthClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	return newLengthNode(), nil
}

type lengthNode struct {
	pasta.BasicNode

	value int

	w   *pasta.Workspace
	id  uint64
	in  uint64
	out uint64
}

func newLengthNode() *lengthNode {
	return &lengthNode{}
}

func (n *lengthNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if restored != nil {
		if len(restored.LeftPorts) > 0 {
			n.in = restored.LeftPorts[0]
		}
		if len(restored.RightPorts) > 0 {
			n.out = restored.RightPorts[0]
		}
	}
	if err := n.w.SetNodePrimaryLocked(n.id, TypeInt); err != nil {
		return err
	}
	if err := n.updateLabel(); err != nil {
		return err
	}
	n.sendMenuSnapshot()
	return nil
}

func (n *lengthNode) OnReady() error {
	n.requestInput()
	n.sendAll()
	return nil
}

func (n *lengthNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	if portDirection == "left" {
		if port != n.in || linkType != TypeObject {
			return pasta.LinkTypeErr(linkType)
		}
		snapshot, ok := n.w.PortSnapshotLocked(port)
		if ok && len(snapshot.Links) > 0 {
			return pasta.ErrLinkDup
		}
		return nil
	}
	if port == n.out && linkType == TypeInt {
		return nil
	}
	return pasta.LinkTypeErr(linkType)
}

func (n *lengthNode) OnLinkAdd(link, port uint64, _, portDirection string) error {
	if portDirection == "left" {
		n.requestLink(link, port)
		return nil
	}
	if port == n.out {
		n.sendToLink(link)
	}
	return nil
}

func (n *lengthNode) OnLinkRemoved(_ uint64, port uint64, _ string, portDirection string) error {
	if portDirection == "left" && port == n.in {
		n.setValue(0, true)
	}
	return nil
}

func (n *lengthNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if receiverPortDirection == "right" {
		if event.ReceiverPort == n.out && linkType == TypeInt && isValueRequest(event.Payload) {
			n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: event.SenderNode, ReceiverPort: event.SenderPort, Payload: Int(n.value)})
		}
		return nil
	}
	if event.ReceiverPort != n.in || linkType != TypeObject {
		return nil
	}
	n.setValue(lengthOfObjectPayload(event.Payload), true)
	return nil
}

func (n *lengthNode) setValue(value int, broadcast bool) {
	old := n.value
	n.value = value
	_ = n.updateLabel()
	n.sendMenuBlock()
	if broadcast && old != n.value {
		n.sendAll()
	}
}

func (n *lengthNode) updateLabel() error {
	if n.w == nil || n.id == 0 {
		return nil
	}
	return n.w.SetNodeLabelLocked(n.id, intValue(n.value).label())
}

func (n *lengthNode) requestInput() {
	snapshot, ok := n.w.PortSnapshotLocked(n.in)
	if !ok {
		return
	}
	for _, link := range snapshot.Links {
		n.requestLink(link, n.in)
	}
}

func (n *lengthNode) requestLink(link, port uint64) {
	snapshot, ok := n.w.LinkSnapshotLocked(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, port)
	n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: port, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: RequestValue{}})
}

func (n *lengthNode) sendAll() {
	port, ok := n.w.PortSnapshotLocked(n.out)
	if !ok {
		return
	}
	for _, link := range port.Links {
		n.sendToLink(link)
	}
}

func (n *lengthNode) sendToLink(link uint64) {
	snapshot, ok := n.w.LinkSnapshotLocked(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, n.out)
	n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: Int(n.value)})
}

func (n *lengthNode) sendMenuSnapshot() {
	if n.w == nil || n.id == 0 {
		return
	}
	n.w.SendNodeMenuMsgLocked(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageMenuSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1},
		Blocks:      []formular.Block{n.menuBlock()},
	})
}

func (n *lengthNode) sendMenuBlock() {
	if n.w == nil || n.id == 0 {
		return
	}
	n.w.SendNodeMenuMsgLocked(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageBlockSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1, BlockGeneration: 1},
		Block:       n.menuBlock(),
	})
}

func (n *lengthNode) menuBlock() formular.Block {
	return formular.Block{
		ID: "state", Order: 10, Generation: 1,
		Items: []formular.Item{{
			Type:  formular.ItemField,
			ID:    "value",
			Label: "Value",
			Field: &formular.Field{Kind: formular.FieldInt, Value: n.value, Readonly: true},
		}},
	}
}

// lengthOfObjectPayload accepts the standard Object payload and raw values used
// by custom object producers that expose scalar JSON values directly.
func lengthOfObjectPayload(payload any) int {
	switch v := payload.(type) {
	case Object:
		return lengthOfPersistValue(v.Value())
	case persist.Value:
		return lengthOfPersistValue(v)
	case persist.Map:
		return v.Len()
	case persist.Vector:
		return v.Len()
	case map[string]any:
		return len(v)
	case []any:
		return len(v)
	case String:
		return len(v)
	case string:
		return len(v)
	default:
		return 0
	}
}

func lengthOfPersistValue(value persist.Value) int {
	if m, ok := value.Map(); ok {
		return m.Len()
	}
	if v, ok := value.Vector(); ok {
		return v.Len()
	}
	if s, ok := value.StringValue(); ok {
		return len(s)
	}
	return 0
}
