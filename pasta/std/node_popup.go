package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypePopUp is the class name for PopUpClass.
const NodeTypePopUp = "pasta/PopUp"

// PopUpClass creates a trigger-driven popup node.
//
// The node has a pasta/trigger input followed by pasta/string Lvl and Text
// inputs. Every received trigger, and every Workspace.Trigger call on the node,
// appends a node popup using the latest Lvl and Text values. Empty or missing
// Lvl means info. Unknown level names produce an error popup with the supplied
// text.
type PopUpClass struct{}

func (PopUpClass) ClassName() string        { return NodeTypePopUp }
func (PopUpClass) ShortDescription() string { return "Show popup on trigger" }
func (PopUpClass) LongDescription() string {
	return "Creates a node popup with the latest Lvl and Text string inputs whenever its pasta/trigger input receives a trigger event or the node is triggered through the workspace API."
}
func (PopUpClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{InitialPorts: []pasta.Port{
		{Direction: "left", Name: "Trigger", Types: []string{TypeTrigger}},
		{Direction: "left", Name: "Lvl", Types: []string{TypeString}},
		{Direction: "left", Name: "Text", Types: []string{TypeString}},
	}}
}
func (PopUpClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	return &popupNode{}, nil
}

type popupNode struct {
	pasta.BasicNode

	w  *pasta.Workspace
	id uint64

	trigger uint64
	lvl     uint64
	text    uint64

	lvlValue  string
	textValue string
}

func (n *popupNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if restored != nil {
		for _, port := range restored.LeftPorts {
			snapshot, ok := w.PortSnapshotLocked(port)
			if !ok {
				continue
			}
			switch snapshot.Name {
			case "Trigger":
				n.trigger = port
			case "Lvl":
				n.lvl = port
			case "Text":
				n.text = port
			}
		}
	}
	return nil
}

func (n *popupNode) OnReady() error {
	n.requestStringPort(n.lvl)
	n.requestStringPort(n.text)
	return nil
}

func (n *popupNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	if portDirection != "left" {
		return pasta.LinkTypeErr(linkType)
	}
	switch port {
	case n.trigger:
		if linkType != TypeTrigger {
			return pasta.LinkTypeErr(linkType)
		}
		return nil
	case n.lvl, n.text:
		if linkType != TypeString {
			return pasta.LinkTypeErr(linkType)
		}
		snapshot, ok := n.w.PortSnapshotLocked(port)
		if ok && len(snapshot.Links) > 0 {
			return pasta.ErrLinkDup
		}
		return nil
	default:
		return pasta.LinkTypeErr(linkType)
	}
}

func (n *popupNode) OnLinkAdd(link, port uint64, linkType, _ string) error {
	if linkType == TypeString && (port == n.lvl || port == n.text) {
		n.requestLink(link, port)
	}
	return nil
}

func (n *popupNode) OnLinkRemoved(_ uint64, port uint64, _ string, _ string) error {
	switch port {
	case n.lvl:
		n.lvlValue = ""
	case n.text:
		n.textValue = ""
	}
	return nil
}

func (n *popupNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if receiverPortDirection != "left" {
		return nil
	}
	switch event.ReceiverPort {
	case n.trigger:
		if linkType == TypeTrigger && !IsRequest(event.Payload) {
			return n.addPopup()
		}
	case n.lvl:
		if linkType == TypeString {
			if value, ok := parseStringAny(event.Payload); ok {
				n.lvlValue = value
			}
		}
	case n.text:
		if linkType == TypeString {
			if value, ok := parseStringAny(event.Payload); ok {
				n.textValue = value
			}
		}
	}
	return nil
}

func (n *popupNode) OnTrigger() error {
	return n.addPopup()
}

func (n *popupNode) requestStringPort(port uint64) {
	snapshot, ok := n.w.PortSnapshotLocked(port)
	if !ok {
		return
	}
	for _, link := range snapshot.Links {
		n.requestLink(link, port)
	}
}

func (n *popupNode) requestLink(link, port uint64) {
	snapshot, ok := n.w.LinkSnapshotLocked(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, port)
	n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: port, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: RequestValue{}})
}

func (n *popupNode) addPopup() error {
	_, err := n.w.AddNodePopupLocked(n.id, popupLevel(n.lvlValue), n.textValue, false)
	return err
}

func popupLevel(value string) string {
	switch value {
	case "", pasta.NodePopupInfo:
		return pasta.NodePopupInfo
	case pasta.NodePopupWard:
		return pasta.NodePopupWard
	case pasta.NodePopupErr:
		return pasta.NodePopupErr
	default:
		return pasta.NodePopupErr
	}
}
