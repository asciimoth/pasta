package pasta

import (
	"encoding/json"
	"fmt"

	"github.com/asciimoth/formular"
)

// NodeMenuID returns the Formular menu id reserved for a node.
func NodeMenuID(node uint64) string {
	return fmt.Sprintf("NODE%dMENU", node)
}

// SubscribeNodeMenu subscribes an existing notification subscription to one
// node menu.
//
// Node menu messages are not part of ordinary workspace notifications. A
// watcher must first call SubscribeNotifications, then opt in to each menu it
// wants to render with the returned subscription id. If a cached menu snapshot
// exists, the watcher receives a forced snapshot before SubscribeNodeMenu
// returns.
func (w *Workspace) SubscribeNodeMenu(node, subscriptionID uint64) bool {
	if node < 1 || subscriptionID < 1 {
		return false
	}

	w.Lock()
	if w.closed {
		w.Unlock()
		return false
	}
	callback, subscribed := w.subscribers[subscriptionID]
	record, present := w.nodes.Get(node)
	if !subscribed || !present || record == nil || callback == nil {
		w.Unlock()
		return false
	}
	if w.nodeMenuSubscribers[node] == nil {
		w.nodeMenuSubscribers[node] = make(map[uint64]struct{})
	}
	w.nodeMenuSubscribers[node][subscriptionID] = struct{}{}

	var snapshot *formular.MenuSnapshotMessage
	if record.Menu != nil {
		if msg, ok := record.Menu.ForceSnapshot(NodeMenuID(node)); ok {
			copied := msg.Copy()
			snapshot = &copied
		}
	}
	w.Unlock()

	if snapshot != nil {
		callback(WorkspaceNotification{
			SubscriptionID: subscriptionID,
			Kind:           NotificationNodeMenu,
			ID:             node,
			Formular:       snapshot.Copy(),
		})
	}
	return true
}

// UnsubscribeNodeMenu removes one notification subscription from one node menu.
func (w *Workspace) UnsubscribeNodeMenu(node, subscriptionID uint64) bool {
	if node < 1 || subscriptionID < 1 {
		return false
	}

	w.Lock()
	defer w.Unlock()
	if w.closed {
		return false
	}
	subscribers := w.nodeMenuSubscribers[node]
	if subscribers == nil {
		return false
	}
	if _, present := subscribers[subscriptionID]; !present {
		return false
	}
	delete(subscribers, subscriptionID)
	if len(subscribers) == 0 {
		delete(w.nodeMenuSubscribers, node)
	}
	return true
}

// SendNodeMenuMsg sends one Formular backend-to-frontend message from a node
// menu to subscribed watchers and updates the workspace's cached menu state.
func (w *Workspace) SendNodeMenuMsg(node uint64, message any) {
	if node < 1 || message == nil {
		return
	}

	w.Lock()
	defer w.Unlock()
	if w.closed {
		return
	}
	record, present := w.nodes.Get(node)
	if !present || record == nil {
		return
	}
	if record.Menu == nil {
		record.Menu = formular.NewMenuSnapshotState()
	}
	copied := copyFormularMessage(message)
	record.Menu.Apply(copied)

	for subscriptionID := range w.nodeMenuSubscribers[node] {
		if _, present := w.subscribers[subscriptionID]; !present {
			continue
		}
		w.notifications = append(w.notifications, WorkspaceNotification{
			SubscriptionID: subscriptionID,
			Kind:           NotificationNodeMenu,
			ID:             node,
			Formular:       copyFormularMessage(copied),
		})
	}
}

// SendNodeFormularMsg delivers one Formular frontend-to-backend message to a
// node. Missing nodes, placeholders, nil messages, and closed workspaces drop
// the message silently.
func (w *Workspace) SendNodeFormularMsg(node uint64, message any) {
	if node < 1 || message == nil {
		return
	}

	w.Lock()
	defer w.Unlock()
	if w.closed {
		return
	}
	record, present := w.nodes.Get(node)
	if !present || record == nil || record.Node == nil {
		return
	}
	if err := record.OnFormularMsg(message); err != nil {
		w.log.Debugf("node %d faled in OnFormularMsg", node)
		w.failNodeLocked(node, "OnFormularMsg", err, true, true)
	}
}

func copyFormularMessage(message any) any {
	switch msg := message.(type) {
	case formular.MessageBase:
		return msg.Copy()
	case *formular.MessageBase:
		if msg == nil {
			return nil
		}
		return msg.Copy()
	case formular.MenuSnapshotMessage:
		return msg.Copy()
	case *formular.MenuSnapshotMessage:
		if msg == nil {
			return nil
		}
		return msg.Copy()
	case formular.BlockSnapshotMessage:
		return msg.Copy()
	case *formular.BlockSnapshotMessage:
		if msg == nil {
			return nil
		}
		return msg.Copy()
	case formular.BlockDeleteMessage:
		return msg.Copy()
	case *formular.BlockDeleteMessage:
		if msg == nil {
			return nil
		}
		return msg.Copy()
	case formular.FieldStatusMessage:
		return msg.Copy()
	case *formular.FieldStatusMessage:
		if msg == nil {
			return nil
		}
		return msg.Copy()
	case formular.AutocompleteHintsMessage:
		return msg.Copy()
	case *formular.AutocompleteHintsMessage:
		if msg == nil {
			return nil
		}
		return msg.Copy()
	case formular.FieldUpdateMessage:
		return msg.Copy()
	case *formular.FieldUpdateMessage:
		if msg == nil {
			return nil
		}
		return msg.Copy()
	case formular.FieldValidateMessage:
		return msg.Copy()
	case *formular.FieldValidateMessage:
		if msg == nil {
			return nil
		}
		return msg.Copy()
	case formular.FormApplyMessage:
		return msg.Copy()
	case *formular.FormApplyMessage:
		if msg == nil {
			return nil
		}
		return msg.Copy()
	case formular.ButtonPressMessage:
		return msg.Copy()
	case *formular.ButtonPressMessage:
		if msg == nil {
			return nil
		}
		return msg.Copy()
	case formular.AutocompleteRequestMessage:
		return msg.Copy()
	case *formular.AutocompleteRequestMessage:
		if msg == nil {
			return nil
		}
		return msg.Copy()
	case json.RawMessage:
		return append(json.RawMessage(nil), msg...)
	case []byte:
		return append([]byte(nil), msg...)
	case map[string]any:
		return copyAnyJSONMap(msg)
	default:
		return message
	}
}

func copyAnyJSONMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	data, err := json.Marshal(in)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}
