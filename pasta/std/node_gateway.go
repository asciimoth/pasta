package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeGateway is the class name for GatewayClass.
const NodeTypeGateway = "pasta/Gateway"

// GatewayClass creates a trigger-gated bidirectional data relay.
//
// Gateway has one right-directed any/any Out port, then left-directed
// pasta/trigger Trigger and any/any In ports. Non-request events received on In
// and Out are stored as the latest payload for their link type and are relayed
// only when Trigger receives a trigger event. RequestValue events are relayed
// immediately in both directions. Stored ClosablePayload values are closed when
// replaced, when their side is disconnected, or when the node stops.
type GatewayClass struct{}

func (GatewayClass) ClassName() string        { return NodeTypeGateway }
func (GatewayClass) ShortDescription() string { return "Gate events by trigger" }
func (GatewayClass) LongDescription() string {
	return "Stores the latest non-trigger data event by type on In and Out and relays stored values only when Trigger receives a pasta/trigger event. RequestValue passes through immediately."
}
func (GatewayClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{InitialPorts: []pasta.Port{
		{Direction: "right", Name: "Out", Types: []string{pasta.AnyType}},
		{Direction: "left", Name: "Trigger", Types: []string{TypeTrigger}},
		{Direction: "left", Name: "In", Types: []string{pasta.AnyType}},
	}}
}
func (GatewayClass) NewNode(_ configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
	n := newGatewayNode()
	if state := firstState(previous); state != nil {
		n.dataType = state.PrimaryType
	}
	return n, nil
}

type gatewayNode struct {
	pasta.BasicNode

	w  *pasta.Workspace
	id uint64

	out      uint64
	trigger  uint64
	in       uint64
	dataType string

	inEvents  map[string]gatewayStoredEvent
	outEvents map[string]gatewayStoredEvent
}

type gatewayStoredEvent struct {
	payload any
}

func newGatewayNode() *gatewayNode {
	return &gatewayNode{
		inEvents:  map[string]gatewayStoredEvent{},
		outEvents: map[string]gatewayStoredEvent{},
	}
}

func (n *gatewayNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if n.inEvents == nil {
		n.inEvents = map[string]gatewayStoredEvent{}
	}
	if n.outEvents == nil {
		n.outEvents = map[string]gatewayStoredEvent{}
	}
	if restored != nil {
		if restored.PrimaryType != "" {
			n.dataType = restored.PrimaryType
		}
		for _, port := range restored.RightPorts {
			snapshot, ok := w.PortSnapshotLocked(port)
			if ok && snapshot.Name == "Out" {
				n.out = port
			}
		}
		for _, port := range restored.LeftPorts {
			snapshot, ok := w.PortSnapshotLocked(port)
			if !ok {
				continue
			}
			switch snapshot.Name {
			case "Trigger":
				n.trigger = port
			case "In":
				n.in = port
			}
		}
	}
	if n.dataType != "" {
		if err := n.applyDataType(n.dataType); err != nil {
			return err
		}
	}
	return nil
}

func (n *gatewayNode) OnStop() {
	n.drainEvents(n.inEvents)
	n.drainEvents(n.outEvents)
}

func (n *gatewayNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	switch port {
	case n.trigger:
		if portDirection != "left" || linkType != TypeTrigger {
			return pasta.LinkTypeErr(linkType)
		}
		return nil
	case n.in:
		if portDirection != "left" {
			return pasta.LinkTypeErr(linkType)
		}
		if linkType == TypeObject {
			snapshot, ok := n.w.PortSnapshotLocked(port)
			if ok && len(snapshot.Links) > 0 {
				return pasta.ErrLinkDup
			}
		}
		if n.dataType != "" && linkType != n.dataType && linkType != pasta.AnyType {
			return pasta.LinkTypeErr(linkType)
		}
		return nil
	case n.out:
		if portDirection != "right" {
			return pasta.LinkTypeErr(linkType)
		}
		if n.dataType != "" && linkType != n.dataType && linkType != pasta.AnyType {
			return pasta.LinkTypeErr(linkType)
		}
		return nil
	default:
		return pasta.LinkTypeErr(linkType)
	}
}

func (n *gatewayNode) OnLinkAdd(_ uint64, port uint64, linkType, _ string) error {
	if (port == n.in || port == n.out) && n.dataType == "" && linkType != pasta.AnyType {
		return n.applyDataType(linkType)
	}
	return nil
}

func (n *gatewayNode) OnLinkRemoved(_ uint64, port uint64, _ string, _ string) error {
	switch port {
	case n.in:
		n.drainEvents(n.inEvents)
	case n.out:
		n.drainEvents(n.outEvents)
	}
	return nil
}

func (n *gatewayNode) applyDataType(typ string) error {
	n.dataType = typ
	if err := n.w.SetNodePrimaryLocked(n.id, typ); err != nil {
		return err
	}
	for _, port := range []uint64{n.in, n.out} {
		if port == 0 {
			continue
		}
		if err := n.w.SetPortTypesLocked(port, []string{typ}); err != nil {
			return err
		}
	}
	return nil
}

func (n *gatewayNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	switch event.ReceiverPort {
	case n.trigger:
		if receiverPortDirection == "left" && linkType == TypeTrigger && !IsRequest(event.Payload) {
			n.flush()
		}
	case n.in:
		if receiverPortDirection != "left" || linkType == TypeTrigger {
			return nil
		}
		if isValueRequest(event.Payload) {
			n.forwardToPort(n.out, event.Payload, linkType)
			return nil
		}
		n.store(n.inEvents, linkType, event.Payload)
	case n.out:
		if receiverPortDirection != "right" || linkType == TypeTrigger {
			return nil
		}
		if isValueRequest(event.Payload) {
			n.forwardToPort(n.in, event.Payload, linkType)
			return nil
		}
		n.store(n.outEvents, linkType, event.Payload)
	}
	return nil
}

func (n *gatewayNode) OnTrigger() error {
	n.flush()
	return nil
}

func (n *gatewayNode) store(events map[string]gatewayStoredEvent, linkType string, payload any) {
	if previous, ok := events[linkType]; ok {
		closePayload(previous.payload)
	}
	events[linkType] = gatewayStoredEvent{payload: payload}
}

func (n *gatewayNode) flush() {
	for linkType, event := range n.inEvents {
		n.forwardToPort(n.out, event.payload, linkType)
	}
	for linkType, event := range n.outEvents {
		n.forwardToPort(n.in, event.payload, linkType)
	}
}

func (n *gatewayNode) forwardToPort(port uint64, payload any, linkType string) {
	snapshot, ok := n.w.PortSnapshotLocked(port)
	if !ok {
		return
	}
	for _, link := range snapshot.Links {
		linkSnapshot, ok := n.w.LinkSnapshotLocked(link)
		if !ok || !gatewayLinkMatches(linkSnapshot.Type, linkType) {
			continue
		}
		receiverNode, receiverPort := otherEndpoint(linkSnapshot, port)
		n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: port, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: payload})
	}
}

func gatewayLinkMatches(linkType, eventType string) bool {
	return linkType == eventType || linkType == pasta.AnyType || eventType == pasta.AnyType
}

func (n *gatewayNode) drainEvents(events map[string]gatewayStoredEvent) {
	for linkType, event := range events {
		closePayload(event.payload)
		delete(events, linkType)
	}
}

func closePayload(payload any) {
	closable, ok := payload.(ClosablePayload)
	if ok && closable != nil {
		pasta.CloseBackground(closable)
	}
}
