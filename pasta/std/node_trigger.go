package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeTrigger is the class name for TriggerClass.
const NodeTypeTrigger = "pasta/Trigger"

// TriggerClass creates a manual trigger source node.
//
// The node has no inputs and one right-directed pasta/trigger output. Pressing
// the menu button or calling Workspace.Trigger on the node emits one fresh
// Trigger{} event to every currently connected output link.
type TriggerClass struct{}

func (TriggerClass) ClassName() string        { return NodeTypeTrigger }
func (TriggerClass) ShortDescription() string { return "Emit trigger events" }
func (TriggerClass) LongDescription() string {
	return "Emits one pasta/trigger event from its output when the node menu button is pressed or the node is triggered through the workspace API."
}
func (TriggerClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: TypeTrigger, InitialPorts: []pasta.Port{
		{Direction: "right", Name: "Trigger", Types: []string{TypeTrigger}},
	}}
}
func (TriggerClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	return &triggerNode{}, nil
}

type triggerNode struct {
	pasta.BasicNode

	w   *pasta.Workspace
	id  uint64
	out uint64
}

func (n *triggerNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if restored != nil && len(restored.RightPorts) > 0 {
		n.out = restored.RightPorts[0]
	}
	if err := n.w.SetNodePrimaryLocked(n.id, TypeTrigger); err != nil {
		return err
	}
	n.sendMenuSnapshot()
	return nil
}

func (n *triggerNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	if port != n.out || portDirection != "right" || linkType != TypeTrigger {
		return pasta.LinkTypeErr(linkType)
	}
	return nil
}

func (n *triggerNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if event.ReceiverPort != n.out || receiverPortDirection != "right" || linkType != TypeTrigger || !IsRequest(event.Payload) {
		return nil
	}
	return nil
}

func (n *triggerNode) OnFormularMsg(message any) error {
	msg, ok := message.(formular.ButtonPressMessage)
	if !ok || msg.MenuID != pasta.NodeMenuID(n.id) || msg.BlockID != "state" || msg.ButtonID != "trigger" {
		return nil
	}
	n.sendAll()
	return nil
}

func (n *triggerNode) OnTrigger() error {
	n.sendAll()
	return nil
}

func (n *triggerNode) sendAll() {
	port, ok := n.w.PortSnapshotLocked(n.out)
	if !ok {
		return
	}
	for _, link := range port.Links {
		snapshot, ok := n.w.LinkSnapshotLocked(link)
		if !ok {
			continue
		}
		receiverNode, receiverPort := otherEndpoint(snapshot, n.out)
		n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: Trigger{}})
	}
}

func (n *triggerNode) sendMenuSnapshot() {
	n.w.SendNodeMenuMsgLocked(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageMenuSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1},
		Blocks: []formular.Block{{
			ID: "state", Order: 10, Generation: 1,
			Items: []formular.Item{{Type: formular.ItemButton, ID: "trigger", Label: "Trigger"}},
		}},
	})
}
