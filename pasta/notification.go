package pasta

// NotificationKind identifies the workspace change described by a notification.
type NotificationKind string

const (
	// NotificationWorkspaceSnapshot carries a full workspace snapshot.
	NotificationWorkspaceSnapshot NotificationKind = "workspace_snapshot"
	// NotificationWorkspaceStopped reports that the workspace has been closed.
	NotificationWorkspaceStopped NotificationKind = "workspace_stopped"

	NotificationNodeAdded   NotificationKind = "node_added"
	NotificationNodeRemoved NotificationKind = "node_removed"
	NotificationNodeUpdated NotificationKind = "node_updated"

	NotificationPortAdded   NotificationKind = "port_added"
	NotificationPortRemoved NotificationKind = "port_removed"
	NotificationPortUpdated NotificationKind = "port_updated"

	NotificationLinkAdded   NotificationKind = "link_added"
	NotificationLinkRemoved NotificationKind = "link_removed"
	NotificationLinkUpdated NotificationKind = "link_updated"
)

// WorkspaceNotification describes one observable workspace state change.
//
// Snapshot is set for NotificationWorkspaceSnapshot. Node, Port, or Link is set
// for matching node/port/link notifications. Removed notifications carry the
// last snapshot of the removed entity.
type WorkspaceNotification struct {
	SubscriptionID uint64 `json:"-"`

	Kind NotificationKind `json:"kind"`
	ID   uint64           `json:"id,omitempty"`

	Snapshot *WorkspaceSnapshot `json:"snapshot,omitempty"`
	Node     *NodeSnapshot      `json:"node,omitempty"`
	Port     *PortSnapshot      `json:"port,omitempty"`
	Link     *LinkSnapshot      `json:"link,omitempty"`

	snapshotRequest bool
}

// NotificationCallback receives workspace notifications synchronously.
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
	if w.closed {
		w.Unlock()
		return 0
	}
	id := w.nextSubscriptionID
	if id < 1 {
		id = 1
	}
	w.nextSubscriptionID = id + 1
	w.subscribers[id] = callback
	snapshot := w.snapshotLocked()
	w.Unlock()

	callback(WorkspaceNotification{
		SubscriptionID: id,
		Kind:           NotificationWorkspaceSnapshot,
		Snapshot:       &snapshot,
	})
	return id
}

// UnsubscribeNotifications removes a notification subscription.
func (w *Workspace) UnsubscribeNotifications(id uint64) bool {
	if id < 1 {
		return false
	}

	w.Lock()
	defer w.Unlock()
	if w.closed {
		return false
	}

	if _, present := w.subscribers[id]; !present {
		return false
	}
	delete(w.subscribers, id)
	return true
}

// RequestFullSnapshot schedules a full workspace snapshot notification for one
// notification subscription.
//
// The snapshot is formed when notifications are drained for delivery, not when
// the request is made.
func (w *Workspace) RequestFullSnapshot(subscriptionID uint64) bool {
	if subscriptionID < 1 {
		return false
	}

	w.Lock()
	defer w.Unlock()
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
