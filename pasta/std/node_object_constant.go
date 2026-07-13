package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeObjectConstant is the class name for ObjectConstantClass.
const NodeTypeObjectConstant = "pasta/ObjectConstant"

// ObjectConstantClass creates nodes with one editable object value and one
// right-directed pasta/object output port.
type ObjectConstantClass struct{}

func (ObjectConstantClass) ClassName() string        { return NodeTypeObjectConstant }
func (ObjectConstantClass) ShortDescription() string { return "Object constant" }
func (ObjectConstantClass) LongDescription() string {
	return "Outputs an editable JSON object, array, or null value on one pasta/object right-directed port."
}
func (ObjectConstantClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: TypeObject, InitialPorts: []pasta.Port{rightPort(TypeObject)}}
}
func (ObjectConstantClass) NewNode(cfg configer.Config, _ ...*pasta.NodeClassState) (pasta.Node, error) {
	value := NilObject()
	if cfg != nil {
		if raw, err := cfg.Get(configer.Path{"value"}); err == nil {
			value = readObject(raw, value)
		}
	}
	return newObjectConstantNode(value), nil
}

type objectConstantNode struct {
	pasta.BasicNode

	value Object
	w     *pasta.Workspace
	id    uint64
	out   uint64
}

func newObjectConstantNode(value Object) *objectConstantNode {
	return &objectConstantNode{value: value}
}

func (n *objectConstantNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if restored != nil && len(restored.RightPorts) > 0 {
		n.out = restored.RightPorts[0]
	}
	if err := n.w.SetNodePrimaryLocked(n.id, TypeObject); err != nil {
		return err
	}
	if err := n.updateLabel(); err != nil {
		return err
	}
	n.sendMenuSnapshot(false)
	return nil
}

func (n *objectConstantNode) OnReady() error {
	n.sendAll()
	return nil
}

func (n *objectConstantNode) PreLinkAdd(port uint64, linkType, _ string) error {
	if port != n.out || linkType != TypeObject {
		return pasta.LinkTypeErr(linkType)
	}
	return nil
}

func (n *objectConstantNode) OnLinkAdd(link, port uint64, _, _ string) error {
	if port == n.out {
		n.sendToLink(link)
	}
	return nil
}

func (n *objectConstantNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if event.ReceiverPort != n.out || receiverPortDirection != "right" || linkType != TypeObject || !isValueRequest(event.Payload) {
		return nil
	}
	n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: event.SenderNode, ReceiverPort: event.SenderPort, Payload: n.value})
	return nil
}

func (n *objectConstantNode) OnFormularMsg(message any) error {
	msg, ok := message.(formular.FormApplyMessage)
	if !ok || msg.MenuID != pasta.NodeMenuID(n.id) || msg.BlockID != "state" {
		return nil
	}
	raw, ok := parseStringAny(msg.Values["value"])
	if !ok {
		n.sendMenuSnapshot(true)
		return nil
	}
	next, err := ObjectFromJSON([]byte(raw))
	if err != nil {
		n.sendMenuSnapshot(true)
		return nil
	}
	changed := !next.Equal(n.value)
	n.value = next
	if changed {
		if err := n.updateLabel(); err != nil {
			return err
		}
	}
	n.sendMenuBlock()
	n.sendAll()
	return nil
}

func (n *objectConstantNode) OnSave(cfg configer.Config) error {
	if err := pasta.DeleteNodeOwnedConfigKeys(cfg); err != nil {
		return err
	}
	value, err := objectToConfigValue(n.value)
	if err != nil {
		return err
	}
	return cfg.Set(configer.Path{"value"}, value)
}

func (n *objectConstantNode) updateLabel() error {
	return n.w.SetNodeLabelLocked(n.id, "")
}

func (n *objectConstantNode) sendAll() {
	port, ok := n.w.PortSnapshotLocked(n.out)
	if !ok {
		return
	}
	for _, link := range port.Links {
		n.sendToLink(link)
	}
}

func (n *objectConstantNode) sendToLink(link uint64) {
	snapshot, ok := n.w.LinkSnapshotLocked(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, n.out)
	n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: n.value})
}

func (n *objectConstantNode) sendMenuSnapshot(force bool) {
	n.w.SendNodeMenuMsgLocked(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageMenuSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1},
		Force:       force,
		Blocks:      []formular.Block{n.menuBlock()},
	})
}

func (n *objectConstantNode) sendMenuBlock() {
	n.w.SendNodeMenuMsgLocked(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageBlockSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1, BlockGeneration: 1},
		Block:       n.menuBlock(),
	})
}

func (n *objectConstantNode) menuBlock() formular.Block {
	return formular.Block{
		ID: "state", Order: 10, Generation: 1, Form: true,
		Items: []formular.Item{{
			Type:  formular.ItemField,
			ID:    "value",
			Label: "Value",
			Field: &formular.Field{
				Kind:        formular.FieldText,
				Value:       n.value.PrettyJSONString(),
				Placeholder: `{"name":"Ada","scores":[1,2,null]}`,
				Multiline:   true,
			},
		}},
	}
}
