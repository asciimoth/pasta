package pasta

// Trigger calls node's OnTrigger callback.
//
// It returns ErrNoNode when node does not identify a live node implementation,
// ErrWorkspaceClosed after the workspace has been closed, or the callback
// error returned by the node. If OnTrigger returns an error or panics, the node
// is replaced with a placeholder carrying an error popup before Trigger
// returns.
func (w *Workspace) Trigger(node uint64) error {
	w.Lock()
	defer w.Unlock()
	return w.TriggerLocked(node)
}

// TriggerLocked is Trigger for callers that already hold the workspace lock.
func (w *Workspace) TriggerLocked(node uint64) error {
	if w.closed {
		return ErrWorkspaceClosed
	}

	record, present := w.nodes.Get(node)
	if !present || record == nil || record.Node == nil {
		return ErrNoNode
	}

	if err := record.OnTrigger(); err != nil {
		w.log.Debugf("node %d faled in OnTrigger", node)
		w.failNodeLocked(node, "OnTrigger", err, true, true)
		return err
	}
	return nil
}
