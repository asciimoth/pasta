package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeIter is the class name for IterClass.
const NodeTypeIter = "pasta/Iter"

// IterClass creates a loop iteration terminator.
//
// Iter has one pasta/loop input followed by Break and Continue pasta/trigger
// inputs. It records the latest LoopStartIteration received through its loop
// link, then sends one matching LoopEndIteration when Break or Continue is
// triggered. Trigger events are ignored while no loop link or active iteration
// is present.
type IterClass struct{}

func (IterClass) ClassName() string        { return NodeTypeIter }
func (IterClass) ShortDescription() string { return "End loop iteration" }
func (IterClass) LongDescription() string {
	return "Associates with one loop node through a pasta/loop link and ends the current iteration when Break or Continue receives a pasta/trigger event."
}
func (IterClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: TypeLoop, InitialPorts: []pasta.Port{
		{Direction: "left", Name: "Loop", Types: []string{TypeLoop}},
		{Direction: "left", Name: "Break", Types: []string{TypeTrigger}},
		{Direction: "left", Name: "Continue", Types: []string{TypeTrigger}},
	}}
}
func (IterClass) NewNode(_ configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
	if state := firstState(previous); state != nil {
		state.PrimaryType = TypeLoop
		state.Label = ""
	}
	return &iterNode{}, nil
}

type iterNode struct {
	pasta.BasicNode

	w  *pasta.Workspace
	id uint64

	loopIn     uint64
	breakIn    uint64
	continueIn uint64
	loopLink   uint64

	currentIteration uint32
	hasIteration     bool
}

func (n *iterNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	n.findPorts(restored)
	return n.w.SetNodePrimaryLocked(n.id, TypeLoop)
}

func (n *iterNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	switch port {
	case n.loopIn:
		if portDirection != "left" || linkType != TypeLoop {
			return pasta.LinkTypeErr(linkType)
		}
		snapshot, ok := n.w.PortSnapshotLocked(port)
		if ok && len(snapshot.Links) > 0 {
			return pasta.ErrLinkDup
		}
		return nil
	case n.breakIn, n.continueIn:
		if portDirection == "left" && linkType == TypeTrigger {
			return nil
		}
	}
	return pasta.LinkTypeErr(linkType)
}

func (n *iterNode) OnLinkAdd(link, port uint64, linkType, _ string) error {
	if port == n.loopIn && linkType == TypeLoop {
		n.loopLink = link
		n.hasIteration = false
	}
	return nil
}

func (n *iterNode) OnLinkRemoved(link, port uint64, linkType, _ string) error {
	if port == n.loopIn && linkType == TypeLoop && link == n.loopLink {
		n.loopLink = 0
		n.hasIteration = false
	}
	return nil
}

func (n *iterNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if receiverPortDirection != "left" {
		return nil
	}
	switch event.ReceiverPort {
	case n.loopIn:
		if linkType != TypeLoop || event.Link != n.loopLink {
			return nil
		}
		msg, ok := event.Payload.(LoopStartIteration)
		if !ok {
			return nil
		}
		n.currentIteration = msg.Iteration
		n.hasIteration = true
	case n.breakIn:
		if linkType == TypeTrigger && !IsRequest(event.Payload) {
			n.endIteration(true)
		}
	case n.continueIn:
		if linkType == TypeTrigger && !IsRequest(event.Payload) {
			n.endIteration(false)
		}
	}
	return nil
}

func (n *iterNode) findPorts(restored *pasta.NodeInitData) {
	if restored == nil {
		return
	}
	for _, port := range restored.LeftPorts {
		snapshot, ok := n.w.PortSnapshotLocked(port)
		if !ok {
			continue
		}
		switch snapshot.Name {
		case "Loop":
			n.loopIn = port
			n.loopLink = firstLoopLink(n.w, port)
		case "Break":
			n.breakIn = port
		case "Continue":
			n.continueIn = port
		}
	}
}

func (n *iterNode) endIteration(breakLoop bool) {
	if n.loopLink == 0 || !n.hasIteration {
		return
	}
	link, ok := n.w.LinkSnapshotLocked(n.loopLink)
	if !ok || link.Type != TypeLoop {
		n.loopLink = 0
		n.hasIteration = false
		return
	}
	receiverNode, receiverPort := otherEndpoint(link, n.loopIn)
	n.w.SendEventLocked(pasta.Event{
		SenderNode:   n.id,
		SenderPort:   n.loopIn,
		ReceiverNode: receiverNode,
		ReceiverPort: receiverPort,
		Payload:      LoopEndIteration{Iteration: n.currentIteration, Break: breakLoop},
	})
	n.hasIteration = false
}
