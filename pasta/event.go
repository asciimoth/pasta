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

	// ID of link. Autofilled by Workspace
	Link uint64
}

// SendEvent schedules delivery of event to the receiver node.
//
// The event is validated immediately before delivery, not when it is queued.
// If either endpoint no longer exists, the ports no longer belong to the given
// nodes, or the ports are no longer connected, the event is dropped.
func (w *Workspace) SendEvent(event Event) {
	w.Lock()
	defer w.Unlock()
	w.SendEventLocked(event)
}

// SendEventLocked is SendEvent for callers that already hold the workspace lock.
func (w *Workspace) SendEventLocked(event Event) {
	w.AddPendingOpLocked(func() {
		w.deliverEvent(event)
	})
}

// SendLowPriorityEvent schedules delivery of event after all regular pending
// operations, regular events, and notification deliveries are complete.
//
// The event is validated immediately before delivery, not when it is queued.
// If a low-priority event queues new regular work while it is being delivered,
// that regular work runs before the next low-priority event.
func (w *Workspace) SendLowPriorityEvent(event Event) {
	w.Lock()
	defer w.Unlock()
	w.SendLowPriorityEventLocked(event)
}

// SendLowPriorityEventLocked is SendLowPriorityEvent for callers that already
// hold the workspace lock.
func (w *Workspace) SendLowPriorityEventLocked(event Event) {
	w.addLowPriorityPendingOpLocked(func() {
		w.deliverEvent(event)
	})
}

// EmitEvent builds event from sender id, link id and payload and sends it.
// sender can be ether sender node or sender port
func (w *Workspace) EmitEvent(sender, link uint64, payload any) {
	w.Lock()
	defer w.Unlock()
	w.EmitEventLocked(sender, link, payload)
}

// EmitEventLocked is EmitEvent for callers that already hold the workspace lock.
func (w *Workspace) EmitEventLocked(sender, link uint64, payload any) {
	event, ok := w.eventFromLinkLocked(sender, link, payload)
	if !ok {
		return
	}

	w.SendEventLocked(event)
}

// EmitLowPriorityEvent builds an event from sender id, link id, and payload,
// then schedules it behind all regular pending work.
//
// sender can be either the sender node ID or sender port ID.
func (w *Workspace) EmitLowPriorityEvent(sender, link uint64, payload any) {
	w.Lock()
	defer w.Unlock()
	w.EmitLowPriorityEventLocked(sender, link, payload)
}

// EmitLowPriorityEventLocked is EmitLowPriorityEvent for callers that already
// hold the workspace lock.
func (w *Workspace) EmitLowPriorityEventLocked(sender, link uint64, payload any) {
	event, ok := w.eventFromLinkLocked(sender, link, payload)
	if !ok {
		return
	}

	w.SendLowPriorityEventLocked(event)
}

func (w *Workspace) eventFromLinkLocked(sender, link uint64, payload any) (Event, bool) {
	event := Event{
		SenderNode: sender,
		Link:       link,
		Payload:    payload,
	}

	ls, ok := w.LinkSnapshotLocked(link)
	if !ok {
		return Event{}, false
	}

	if ls.LeftPortNode == sender {
		event.SenderPort = ls.LeftPort

		event.ReceiverPort = ls.RightPort
		event.ReceiverNode = ls.RightPortNode
	} else if ls.RightPortNode == sender {
		event.SenderPort = ls.RightPort

		event.ReceiverPort = ls.LeftPort
		event.ReceiverNode = ls.LeftPortNode
	} else if ls.LeftPort == sender {
		event.SenderNode = ls.LeftPortNode
		event.SenderPort = ls.LeftPort

		event.ReceiverPort = ls.RightPort
		event.ReceiverNode = ls.RightPortNode
	} else if ls.RightPort == sender {
		event.SenderNode = ls.RightPortNode
		event.SenderPort = ls.RightPort

		event.ReceiverPort = ls.LeftPort
		event.ReceiverNode = ls.LeftPortNode
	} else {
		return Event{}, false
	}

	return event, true
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

	event.Link = link.ID

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
