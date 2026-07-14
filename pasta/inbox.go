package pasta

// InboxMessage is an ephemeral message delivered directly to one node.
//
// Inbox messages are not attached to links or ports and intentionally do not
// carry sender information. They are useful for node-owned background workers
// to report results back to their parent node through the workspace queue.
type InboxMessage struct {
	// ReceiverNode is resolved at delivery time; missing receivers drop the
	// message without an error.
	ReceiverNode uint64
	// Payload is owned by the sending code and receiving node implementation.
	Payload any
}

// SendInbox schedules delivery of message to its receiver node.
//
// The receiver node is validated immediately before delivery. If it no longer
// exists, the message is dropped.
func (w *Workspace) SendInbox(message InboxMessage) {
	w.Lock()
	defer w.Unlock()
	w.SendInboxLocked(message)
}

// SendInboxLocked is SendInbox for callers that already hold the workspace lock.
func (w *Workspace) SendInboxLocked(message InboxMessage) {
	w.AddPendingOpLocked(func() {
		w.deliverInbox(message)
	})
}

func (w *Workspace) deliverInbox(message InboxMessage) {
	w.Lock()
	defer w.Unlock()
	if w.closed {
		return
	}

	receiver, present := w.nodes[message.ReceiverNode]
	if !present || receiver == nil {
		return
	}

	if err := receiver.OnInbox(message); err != nil {
		w.log.Debugf("node %d faled in OnInbox", receiver.ID)
		w.failNodeLocked(receiver.ID, "OnInbox", err, true, true)
	}
}
