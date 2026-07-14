package pasta

// NotificationKind identifies the workspace change described by a notification.
type NotificationKind string

const (
	// NotificationWorkspaceSnapshot carries a full workspace snapshot.
	NotificationWorkspaceSnapshot NotificationKind = "workspace_snapshot"
	// NotificationWorkspaceStopped reports that the workspace has been closed.
	NotificationWorkspaceStopped NotificationKind = "workspace_stopped"

	// NotificationNodeAdded carries a snapshot of the newly added node.
	NotificationNodeAdded NotificationKind = "node_added"
	// NotificationNodeRemoved carries the last snapshot of the removed node.
	NotificationNodeRemoved NotificationKind = "node_removed"
	// NotificationNodeUpdated carries the node snapshot after a mutation.
	NotificationNodeUpdated NotificationKind = "node_updated"
	// NotificationNodesRemoved carries the last snapshots of removed nodes and
	// a full workspace snapshot after the batch mutation.
	NotificationNodesRemoved NotificationKind = "nodes_removed"
	// NotificationNodesUpdated carries node snapshots after a batch mutation and
	// a full workspace snapshot after the batch mutation.
	NotificationNodesUpdated NotificationKind = "nodes_updated"

	// NotificationNodeClassAdded carries the registered class snapshot.
	NotificationNodeClassAdded NotificationKind = "node_class_added"
	// NotificationNodeClassRemoved carries the last registered class snapshot.
	NotificationNodeClassRemoved NotificationKind = "node_class_removed"

	// NotificationPortAdded carries a snapshot of the newly added port.
	NotificationPortAdded NotificationKind = "port_added"
	// NotificationPortRemoved carries the last snapshot of the removed port.
	NotificationPortRemoved NotificationKind = "port_removed"
	// NotificationPortUpdated carries the port snapshot after a mutation.
	NotificationPortUpdated NotificationKind = "port_updated"

	// NotificationLinkAdded carries a snapshot of the newly added link.
	NotificationLinkAdded NotificationKind = "link_added"
	// NotificationLinkRemoved carries the last snapshot of the removed link.
	NotificationLinkRemoved NotificationKind = "link_removed"
	// NotificationLinkUpdated carries the link snapshot after a mutation.
	NotificationLinkUpdated NotificationKind = "link_updated"

	// NotificationWorkerSpawned carries a snapshot of a newly spawned worker.
	NotificationWorkerSpawned NotificationKind = "worker_spawned"
	// NotificationWorkerStopped carries the last snapshot of a normally stopped worker.
	NotificationWorkerStopped NotificationKind = "worker_stopped"
	// NotificationWorkerFailed carries the last snapshot of a panicked worker.
	NotificationWorkerFailed NotificationKind = "worker_failed"

	// NotificationNodeMenu carries one Formular backend-to-frontend message for
	// a node menu. It is delivered only to notification subscribers that
	// explicitly subscribed to that node menu.
	NotificationNodeMenu NotificationKind = "node_menu"
)

// WorkspaceNotification describes one observable workspace state change.
//
// Snapshot is set for NotificationWorkspaceSnapshot and batch node
// notifications. NodeClass, Node, Port, Link, or Worker is set for matching
// node-class/node/port/link/worker notifications. Nodes is set for batch node
// notifications. Formular is set for NotificationNodeMenu. Removed, stopped,
// and failed notifications carry the last snapshot of the removed entity.
type WorkspaceNotification struct {
	SubscriptionID uint64 `json:"-"`

	Kind NotificationKind `json:"kind"`
	ID   uint64           `json:"id,omitempty"`

	ClassName string `json:"class_name,omitempty"`

	Snapshot  *WorkspaceSnapshot      `json:"snapshot,omitempty"`
	NodeClass *NodeClassSnapshot      `json:"node_class,omitempty"`
	Node      *NodeSnapshot           `json:"node,omitempty"`
	Nodes     map[uint64]NodeSnapshot `json:"nodes,omitempty"`
	Port      *PortSnapshot           `json:"port,omitempty"`
	Link      *LinkSnapshot           `json:"link,omitempty"`
	Formular  any                     `json:"formular,omitempty"`

	snapshotRequest bool
}

// NotificationCallback receives workspace notifications synchronously during
// workspace notification delivery.
type NotificationCallback func(WorkspaceNotification)

type notificationDelivery struct {
	callback     NotificationCallback
	notification WorkspaceNotification
}

// SubscribeNotifications subscribes callback to workspace notifications.
//
// The callback receives a full workspace snapshot before SubscribeNotifications
// returns. The returned subscription ID can be passed to
// UnsubscribeNotifications.
func (w *Workspace) SubscribeNotifications(callback NotificationCallback) uint64 {
	if callback == nil {
		return 0
	}

	w.Lock()
	defer w.Unlock()
	return w.SubscribeNotificationsLocked(callback)
}

// SubscribeNotificationsLocked is SubscribeNotifications for callers that already hold the workspace lock.
func (w *Workspace) SubscribeNotificationsLocked(callback NotificationCallback) uint64 {
	if callback == nil {
		return 0
	}

	if w.closed {
		return 0
	}
	id := w.nextSubscriptionID
	if id < 1 {
		id = 1
	}
	w.nextSubscriptionID = id + 1
	w.subscribers[id] = callback
	snapshot := w.snapshotLocked()

	w.mu.Unlock()
	callback(WorkspaceNotification{
		SubscriptionID: id,
		Kind:           NotificationWorkspaceSnapshot,
		Snapshot:       &snapshot,
	})
	w.mu.Lock()
	return id
}

// UnsubscribeNotifications removes a notification subscription.
func (w *Workspace) UnsubscribeNotifications(id uint64) bool {
	w.Lock()
	defer w.Unlock()
	return w.UnsubscribeNotificationsLocked(id)
}

// UnsubscribeNotificationsLocked is UnsubscribeNotifications for callers that already hold the workspace lock.
func (w *Workspace) UnsubscribeNotificationsLocked(id uint64) bool {
	if id < 1 {
		return false
	}

	if w.closed {
		return false
	}

	if _, present := w.subscribers[id]; !present {
		return false
	}
	delete(w.subscribers, id)
	for nodeID, subscribers := range w.nodeMenuSubscribers {
		delete(subscribers, id)
		if len(subscribers) == 0 {
			delete(w.nodeMenuSubscribers, nodeID)
		}
	}
	return true
}

// RequestFullSnapshot schedules a full workspace snapshot notification for one
// notification subscription.
//
// The snapshot is formed when notifications are drained for delivery, not when
// the request is made.
func (w *Workspace) RequestFullSnapshot(subscriptionID uint64) bool {
	w.Lock()
	defer w.Unlock()
	return w.RequestFullSnapshotLocked(subscriptionID)
}

// RequestFullSnapshotLocked is RequestFullSnapshot for callers that already hold the workspace lock.
func (w *Workspace) RequestFullSnapshotLocked(subscriptionID uint64) bool {
	if subscriptionID < 1 {
		return false
	}

	if w.closed {
		return false
	}
	if _, present := w.subscribers[subscriptionID]; !present {
		return false
	}
	w.enqueueNotification(WorkspaceNotification{
		SubscriptionID:  subscriptionID,
		Kind:            NotificationWorkspaceSnapshot,
		snapshotRequest: true,
	})
	return true
}

func (w *Workspace) enqueueNotification(notification WorkspaceNotification) {
	if len(w.subscribers) == 0 {
		return
	}
	w.notifications = append(w.notifications, notification)
}

func (w *Workspace) enqueueNodeNotification(kind NotificationKind, id uint64, node NodeSnapshot) {
	w.enqueueNotification(WorkspaceNotification{
		Kind: kind,
		ID:   id,
		Node: &node,
	})
}

func (w *Workspace) enqueueNodesNotification(kind NotificationKind, nodes map[uint64]NodeSnapshot, snapshot WorkspaceSnapshot) {
	w.enqueueNotification(WorkspaceNotification{
		Kind:     kind,
		Nodes:    nodes,
		Snapshot: &snapshot,
	})
}

func (w *Workspace) enqueueNodeClassNotification(kind NotificationKind, name string, class NodeClassSnapshot) {
	w.enqueueNotification(WorkspaceNotification{
		Kind:      kind,
		ClassName: name,
		NodeClass: &class,
	})
}

func (w *Workspace) enqueuePortNotification(kind NotificationKind, id uint64, port PortSnapshot) {
	w.enqueueNotification(WorkspaceNotification{
		Kind: kind,
		ID:   id,
		Port: &port,
	})
}

func (w *Workspace) enqueueLinkNotification(kind NotificationKind, id uint64, link LinkSnapshot) {
	w.enqueueNotification(WorkspaceNotification{
		Kind: kind,
		ID:   id,
		Link: &link,
	})
}

func (w *Workspace) drainNotificationDeliveries() []notificationDelivery {
	if len(w.notifications) == 0 || len(w.subscribers) == 0 {
		w.notifications = nil
		return nil
	}

	deliveries := make([]notificationDelivery, 0, len(w.notifications)*len(w.subscribers))
	for id, callback := range w.subscribers {
		for _, notification := range w.notifications {
			if callback == nil {
				continue
			}
			if notification.SubscriptionID != 0 && notification.SubscriptionID != id {
				continue
			}
			notification.SubscriptionID = id
			if notification.snapshotRequest {
				snapshot := w.snapshotLocked()
				notification.Snapshot = &snapshot
				notification.snapshotRequest = false
			}
			deliveries = append(deliveries, notificationDelivery{
				callback:     callback,
				notification: notification,
			})
		}
	}
	w.notifications = nil
	return deliveries
}

func deliverNotifications(deliveries []notificationDelivery) {
	for _, delivery := range deliveries {
		delivery.callback(delivery.notification)
	}
}
