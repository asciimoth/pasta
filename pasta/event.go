package pasta

// Event is an ephemeral message sent over an existing link.
//
// Payload is intentionally untyped. Node implementations that agree on a link
// type own the payload contract and should cast it before doing related work.
type Event struct {
	// SenderNode and ReceiverNode identify the nodes expected to be connected
	// when the event is delivered.
	SenderNode   uint64
	ReceiverNode uint64
	// SenderPort and ReceiverPort identify the ports expected to carry the
	// event. Delivery is dropped if the current link graph no longer matches.
	SenderPort   uint64
	ReceiverPort uint64
	// Payload is owned by cooperating node implementations for the link type.
	Payload any
}

// SendEvent schedules delivery of event to the receiver node.
//
// The event is validated immediately before delivery, not when it is queued.
// If either endpoint no longer exists, the ports no longer belong to the given
// nodes, or the ports are no longer connected, the event is dropped.
func (w *Workspace) SendEvent(event Event) {
	w.AddPendingOp(func() {
		w.deliverEvent(event)
	})
}

func (w *Workspace) deliverEvent(event Event) {
	w.Lock()
	defer w.Unlock()
	if w.closed {
		return
	}

	sender, present := w.nodes.Get(event.SenderNode)
	if !present || sender == nil {
		return
	}

	receiver, present := w.nodes.Get(event.ReceiverNode)
	if !present || receiver == nil {
		return
	}

	senderPort, present := w.ports.Get(event.SenderPort)
	if !present || senderPort == nil || senderPort.Node != sender.ID {
		return
	}

	receiverPort, present := w.ports.Get(event.ReceiverPort)
	if !present || receiverPort == nil || receiverPort.Node != receiver.ID {
		return
	}

	link := w.linkBetweenPorts(senderPort, receiverPort)
	if link == nil {
		return
	}
	if link.Placeholder {
		return
	}

	if err := receiver.OnEvent(
		event,
		link.Type,
		receiverPort.CopyTypes(),
		receiverPort.Direction,
	); err != nil {
		w.log.Debugf("node %d faled in OnEvent", receiver.ID)
		w.failNodeLocked(receiver.ID, "OnEvent", err, true, true)
	}
}
