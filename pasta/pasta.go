// Package pasta provides an in-memory directed graph workspace for node-based
// applications.
//
// A workspace owns node records, ports, links, registered node classes, and
// UI-facing metadata. Node records have workspace-scoped IDs and unique names.
// Ports belong to exactly one node, are ordered separately by left and right
// side, and advertise one or more link types. Links connect one left port to
// one right port on different nodes; the resulting node graph is kept acyclic.
// The special AnyType can be used as a wildcard link type.
//
// Node implementations receive lifecycle callbacks for initialization,
// readiness, root-path status, port and link changes, link events, direct inbox
// messages, Formular messages, and saving. Mutation and delivery callback
// failures are contained by replacing the implementation with a placeholder
// record that preserves graph structure where possible. Placeholders are also
// used when restoring or pasting nodes whose class factory is unavailable.
//
// The package also includes class factories, snapshots and notifications for
// frontends, Formular node-menu delivery, copy/paste payloads, Config
// save/restore support, node/link resource cleanup, and bounded best-effort
// undo/redo for topology changes.
package pasta

import (
	"errors"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"sync"

	orderedmap "github.com/wk8/go-ordered-map/v2"
)

var (
	// ErrNodeDup reports that the same Node instance is already present in a workspace.
	ErrNodeDup = errors.New("node duplicate")
	// ErrNodeNameDup reports that a node name is already present in a workspace.
	ErrNodeNameDup = errors.New("node name duplicate")
	// ErrUniqueNodeClassDup reports that a unique node class already has a node.
	ErrUniqueNodeClassDup = errors.New("unique node class duplicate")
	// ErrLinkDup reports that two ports are already connected by a link.
	ErrLinkDup = errors.New("link duplicate")

	// ErrNoNode reports that a node ID does not exist in the workspace.
	ErrNoNode = errors.New("node do not exists")
	// ErrPortOrder reports that a requested node port order does not exactly
	// match the node's existing ports for that side.
	ErrPortOrder = errors.New("port order")
	// ErrCycle reports that a link would create a cycle in the node graph.
	ErrCycle = errors.New("graph cycle")
	// ErrSameDirection reports that both ports have the same direction.
	ErrSameDirection = errors.New("same direction")
	// ErrTypeCompat reports that two ports do not share a usable link type.
	ErrTypeCompat = errors.New("types are incompatible")
	// ErrWorkspaceClosed reports that an operation cannot run after Close.
	ErrWorkspaceClosed = errors.New("workspace closed")
)

// Workspace owns nodes, ports, and links and coordinates their lifecycle callbacks.
type Workspace struct {
	// mu should not be used directly. Use Lock and Unlock so post-lock hooks run.
	mu sync.Mutex

	nextid  uint64
	isReady bool
	closed  bool

	pending []func()
	// pendingLowPrior holds callbacks that may run only after regular pending
	// operations and notification deliveries are fully drained.
	pendingLowPrior []func()
	// postlocking is true while pending operations and notifications are being
	// drained after an unlock. Nested unlocks leave draining to the outer loop
	// so pending operations keep FIFO batch order.
	postlocking bool

	nodes    *orderedmap.OrderedMap[uint64, *nodeRecord]
	ports    *orderedmap.OrderedMap[uint64, *Port]
	links    *orderedmap.OrderedMap[uint64, *Link]
	classes  *orderedmap.OrderedMap[string, NodeClass]
	nameRand *rand.Rand

	resources     map[resourceKey]*resourceState
	nodeResources map[uint64]map[resourceKey]struct{}
	linkResources map[uint64]map[resourceKey]struct{}

	nextSubscriptionID  uint64
	subscribers         map[uint64]NotificationCallback
	nodeMenuSubscribers map[uint64]map[uint64]struct{}
	notifications       []WorkspaceNotification

	undoLog               []undoEntry
	redoLog               []undoEntry
	undoRecordingDisabled int

	log  Logger
	logf LogFactory
}

// NewWorkspace creates a ready workspace using logf for workspace and node loggers.
func NewWorkspace(logf LogFactory) *Workspace {
	return &Workspace{
		nextid:  1,
		isReady: true,

		pending:         make([]func(), 0),
		pendingLowPrior: make([]func(), 0),
		nodes:           orderedmap.New[uint64, *nodeRecord](),
		ports:           orderedmap.New[uint64, *Port](),
		links:           orderedmap.New[uint64, *Link](),
		classes:         orderedmap.New[string, NodeClass](),
		nameRand:        rand.New(rand.NewSource(1)),

		resources:     make(map[resourceKey]*resourceState),
		nodeResources: make(map[uint64]map[resourceKey]struct{}),
		linkResources: make(map[uint64]map[resourceKey]struct{}),

		nextSubscriptionID:  1,
		subscribers:         make(map[uint64]NotificationCallback),
		nodeMenuSubscribers: make(map[uint64]map[uint64]struct{}),
		notifications:       make([]WorkspaceNotification, 0),
		undoLog:             make([]undoEntry, 0, undoLogLimit),
		redoLog:             make([]undoEntry, 0, undoLogLimit),

		log:  logf.WorkspaceLogger(),
		logf: logf,
	}
}

// NextID returns the next workspace-scoped ID.
//
// IDs start at 1 and are never 0.
func (w *Workspace) NextID() uint64 {
	w.Lock()
	defer w.Unlock()
	return w.NextIDLocked()
}

// NextIDLocked is NextID for callers that already hold the workspace lock.
func (w *Workspace) NextIDLocked() uint64 {
	if w.closed {
		return 0
	}
	return w.nextIDLocked()
}

func (w *Workspace) nextIDLocked() uint64 {
	if w.nextid < 1 {
		w.nextid = 1
	}
	id := w.nextid
	w.nextid += 1
	return id
}

func (w *Workspace) reserveIDLocked(id uint64) bool {
	if id < 1 || !w.idAvailableLocked(id) {
		return false
	}
	if id >= w.nextid {
		w.nextid = id + 1
	}
	return true
}

func (w *Workspace) idAvailableLocked(id uint64) bool {
	if id < 1 {
		return false
	}
	if _, ok := w.nodes.Get(id); ok {
		return false
	}
	if _, ok := w.ports.Get(id); ok {
		return false
	}
	if _, ok := w.links.Get(id); ok {
		return false
	}
	return true
}

// IsReady reports whether the workspace is ready to run node OnReady callbacks.
func (w *Workspace) IsReady() bool {
	w.Lock()
	defer w.Unlock()
	return w.IsReadyLocked()
}

// IsReadyLocked is IsReady for callers that already hold the workspace lock.
func (w *Workspace) IsReadyLocked() bool {
	if w.closed {
		return false
	}
	return w.isReady
}

// Ready marks the workspace as ready and calls OnReady for existing nodes.
func (w *Workspace) Ready() {
	w.Lock()
	defer w.Unlock()
	w.ReadyLocked()
}

// ReadyLocked is Ready for callers that already hold the workspace lock.
func (w *Workspace) ReadyLocked() {
	if w.closed || w.isReady {
		return
	}

	w.isReady = true

	// Notify nodes.
	for pair := w.nodes.Newest(); pair != nil; pair = pair.Prev() {
		if err := pair.Value.OnReady(); err != nil {
			w.log.Debugf("node %d faled in OnReady", pair.Key)
			w.failNodeLocked(pair.Key, "OnReady", err, true, false)
		}
	}
	w.recomputeRootPaths(true)
}

// Lock locks the workspace.
func (w *Workspace) Lock() {
	w.mu.Lock()
}

// Unlock unlocks the workspace and runs pending operations.
func (w *Workspace) Unlock() {
	w.mu.Unlock()
	w.postlock()
}

// AddPendingOp queues op to run after current workspace operations finish.
//
// Pending operations run after the top-level Unlock. If the workspace is not
// currently inside another operation, op runs before AddPendingOp returns.
func (w *Workspace) AddPendingOp(op func()) {
	w.Lock()
	defer w.Unlock()
	w.AddPendingOpLocked(op)
}

// AddPendingOpLocked is AddPendingOp for callers that already hold the workspace lock.
func (w *Workspace) AddPendingOpLocked(op func()) {
	if w.closed {
		return
	}
	if w.pending == nil {
		w.pending = make([]func(), 0, 1)
	}
	w.pending = append(w.pending, op)
}

// addLowPriorityPendingOpLocked queues op behind all regular pending work.
//
// Low-priority operations run only after regular pending operations and
// notification deliveries are fully drained. They are still delivered after
// Unlock, outside the workspace mutex, and each low-priority operation gives
// newly queued regular work a chance to run before the next low-priority item.
func (w *Workspace) addLowPriorityPendingOpLocked(op func()) {
	if w.closed {
		return
	}
	if w.pendingLowPrior == nil {
		w.pendingLowPrior = make([]func(), 0, 1)
	}
	w.pendingLowPrior = append(w.pendingLowPrior, op)
}

// Close stops all nodes, waits for node workers to return, notifies
// subscribers, drains pending operations, and prevents future workspace
// operations from mutating state.
func (w *Workspace) Close() {
	w.Lock()
	defer w.Unlock()
	w.CloseLocked()
}

// CloseLocked is Close for callers that already hold the workspace lock.
func (w *Workspace) CloseLocked() {
	w.closed = true

	for pair := w.nodes.Oldest(); pair != nil; pair = pair.Next() {
		if pair.Value == nil {
			continue
		}
		pair.Value.OnStop()
	}
	w.closeAllResourcesLocked()

	for len(w.pending) > 0 || len(w.pendingLowPrior) > 0 {
		ops := w.pending
		w.pending = make([]func(), 0)
		if len(ops) == 0 {
			ops = w.pendingLowPrior
			w.pendingLowPrior = make([]func(), 0)
		}
		w.mu.Unlock()
		for _, op := range ops {
			if op != nil {
				op()
			}
		}
		w.mu.Lock()
	}

	w.enqueueNotification(WorkspaceNotification{Kind: NotificationWorkspaceStopped})
	deliveries := w.drainNotificationDeliveries()
	w.subscribers = make(map[uint64]NotificationCallback)
	w.nodeMenuSubscribers = make(map[uint64]map[uint64]struct{})

	w.mu.Unlock()
	deliverNotifications(deliveries)
	w.mu.Lock()
}

// RemoveNode removes a node and all of its ports and links.
func (w *Workspace) RemoveNode(id uint64) {
	w.Lock()
	defer w.Unlock()
	w.RemoveNodeLocked(id)
}

// RemoveNodeLocked is RemoveNode for callers that already hold the workspace lock.
func (w *Workspace) RemoveNodeLocked(id uint64) {
	if id < 1 {
		return
	}

	if w.closed {
		return
	}

	var (
		record  *nodeRecord
		present bool
	)
	if record, present = w.nodes.Get(id); !present || record == nil {
		return
	}
	removedEntry := w.undoRemovedNodeEntry(id, record)
	w.nodes.Delete(id)
	removed := nodeSnapshot(record)
	record.OnStop()
	delete(w.nodeMenuSubscribers, id)

	w.undoRecordingDisabled += 1
	for port := range record.Ports() {
		w.RemovePortLocked(port)
	}
	w.undoRecordingDisabled -= 1
	record.LeftPorts = nil
	record.RightPorts = nil
	w.closeNodeResourcesLocked(id)

	w.log.Debug("removed node", id)
	w.enqueueNodeNotification(NotificationNodeRemoved, id, removed)
	w.pushUndoEntry(removedEntry)
}

func (w *Workspace) failNodeLocked(id uint64, callback string, cause error, notify bool, recompute bool) bool {
	record, present := w.nodes.Get(id)
	if w.closed || !present || record == nil || record.Node == nil {
		return false
	}

	w.log.Errf(
		"node callback failed; replacing node with placeholder node=%d class=%s callback=%s cause=%v",
		id,
		record.Class,
		callback,
		cause,
	)
	w.replaceFailedNodeWithPlaceholderLocked(id, record, nodeFailureText(callback, cause), notify, recompute)
	return true
}

func (w *Workspace) replaceFailedNodeWithPlaceholderLocked(id uint64, record *nodeRecord, popupText string, notify bool, recompute bool) {
	nodeStop(record.Node)
	w.closeNodeResourcesLocked(id)
	record.Node = nil
	record.stopped = false
	record.Popups = []NodePopup{{
		ID:   w.NextIDLocked(),
		Type: NodePopupErr,
		Text: popupText,
	}}
	w.deactivateNodeLinks(record)
	w.refreshPlaceholderLinks(id, notify)
	if recompute {
		w.recomputeRootPaths(notify)
	}
	if notify {
		w.enqueueNodeNotification(NotificationNodeUpdated, id, nodeSnapshot(record))
	}
}

func nodeFailureText(callback string, cause error) string {
	if cause == nil {
		return fmt.Sprintf("%s failed", callback)
	}
	return fmt.Sprintf("%s failed: %v", callback, cause)
}

// NodesByClass returns IDs of all nodes with class in workspace insertion order.
func (w *Workspace) NodesByClass(class string) ([]uint64, error) {
	if err := ValidateClassName(class); err != nil {
		return nil, err
	}

	w.Lock()
	defer w.Unlock()
	return w.NodesByClassLocked(class)
}

// NodesByClassLocked is NodesByClass for callers that already hold the workspace lock.
func (w *Workspace) NodesByClassLocked(class string) ([]uint64, error) {
	if w.closed {
		return nil, nil
	}

	nodes := make([]uint64, 0)
	for pair := w.nodes.Oldest(); pair != nil; pair = pair.Next() {
		if pair.Value == nil || pair.Value.Class != class {
			continue
		}
		nodes = append(nodes, pair.Key)
	}
	return nodes, nil
}

// RemoveNodesByClass removes all nodes with class.
func (w *Workspace) RemoveNodesByClass(class string) error {
	if err := ValidateClassName(class); err != nil {
		return err
	}

	w.Lock()
	defer w.Unlock()
	return w.RemoveNodesByClassLocked(class)
}

// RemoveNodesByClassLocked is RemoveNodesByClass for callers that already hold the workspace lock.
func (w *Workspace) RemoveNodesByClassLocked(class string) error {
	if w.closed {
		return ErrWorkspaceClosed
	}

	for _, id := range w.nodeIDsByClassLocked(class) {
		w.RemoveNodeLocked(id)
	}
	return nil
}

// ReplaceNodesByClassWithPlaceholders replaces all nodes with class by
// placeholders, preserving their ports and links.
func (w *Workspace) ReplaceNodesByClassWithPlaceholders(class string) error {
	if err := ValidateClassName(class); err != nil {
		return err
	}

	w.Lock()
	defer w.Unlock()
	return w.ReplaceNodesByClassWithPlaceholdersLocked(class)
}

// ReplaceNodesByClassWithPlaceholdersLocked is ReplaceNodesByClassWithPlaceholders for callers that already hold the workspace lock.
func (w *Workspace) ReplaceNodesByClassWithPlaceholdersLocked(class string) error {
	if w.closed {
		return ErrWorkspaceClosed
	}

	for _, id := range w.nodeIDsByClassLocked(class) {
		if err := w.ReplaceNodeWithPlaceholderLocked(id, nil); err != nil {
			return err
		}
	}
	return nil
}

// RemovePort removes a port and all links attached to it.
func (w *Workspace) RemovePort(id uint64) {
	w.Lock()
	defer w.Unlock()
	w.RemovePortLocked(id)
}

// RemovePortLocked is RemovePort for callers that already hold the workspace lock.
func (w *Workspace) RemovePortLocked(id uint64) {
	if id < 1 {
		return
	}

	if w.closed {
		return
	}

	var (
		port    *Port
		present bool
	)
	if port, present = w.ports.Delete(id); !present || port == nil {
		return
	}
	removed := portSnapshot(port)

	links := port.Links
	port.Links = make([]uint64, 0)
	w.undoRecordingDisabled += 1
	for _, link := range links {
		w.RemoveLinkLocked(link)
	}
	w.undoRecordingDisabled -= 1

	w.nodeEvPortRemoved(port.Node, id, port.Direction)

	w.log.Debug("removed port", id)
	w.enqueuePortNotification(NotificationPortRemoved, id, removed)
	if record, present := w.nodes.Get(port.Node); present && record != nil {
		w.enqueueNodeNotification(NotificationNodeUpdated, port.Node, nodeSnapshot(record))
	}
}

// RemoveLink removes a link and updates both endpoint ports and nodes.
func (w *Workspace) RemoveLink(id uint64) {
	w.Lock()
	defer w.Unlock()
	w.RemoveLinkLocked(id)
}

// RemoveLinkLocked is RemoveLink for callers that already hold the workspace lock.
func (w *Workspace) RemoveLinkLocked(id uint64) {
	if id < 1 {
		return
	}

	if w.closed {
		return
	}

	var (
		link    *Link
		present bool
	)
	if link, present = w.links.Delete(id); !present || link == nil {
		return
	}
	removedEntry := undoRemovedLink{
		ID:         id,
		Link:       linkSnapshot(link),
		LeftIndex:  linkIndex(w, link.LeftPort, id),
		RightIndex: linkIndex(w, link.RightPort, id),
	}
	removed := linkSnapshot(link)
	w.enqueueLinkNotification(NotificationLinkRemoved, id, removed)

	port, present := w.ports.Get(link.LeftPort)
	if present {
		port.RemoveLink(id)
		w.enqueuePortNotification(NotificationPortUpdated, port.ID, portSnapshot(port))
	}
	port, present = w.ports.Get(link.RightPort)
	if present {
		port.RemoveLink(id)
		w.enqueuePortNotification(NotificationPortUpdated, port.ID, portSnapshot(port))
	}

	w.nodeEvLinkRemoved(link.LeftPortNode, id, link.LeftPort, link.Type, "left")
	w.nodeEvLinkRemoved(link.RightPortNode, id, link.RightPort, link.Type, "right")
	w.closeLinkResourcesLocked(id)

	w.log.Debug("removed link", id)
	w.recomputeRootPaths(true)
	w.pushUndoEntry(removedEntry)
}

// AddNode adds node to the workspace and returns its workspace-scoped ID.
//
// class must be a valid class name. Nodes can configure their primary type
// and label from OnInit or later callbacks with SetNodePrimary and
// SetNodeLabel. If name is empty or omitted, a generic unique name is generated.
func (w *Workspace) AddNode(node Node, class string, name ...string) (uint64, error) {
	w.Lock()
	defer w.Unlock()
	return w.AddNodeLocked(node, class, name...)
}

// AddNodeLocked is AddNode for callers that already hold the workspace lock.
func (w *Workspace) AddNodeLocked(node Node, class string, name ...string) (uint64, error) {
	return w.addNodeLocked(node, class, false, optionalName(name), "", nil, nil, false, false)
}

// AddRootNode adds node to the workspace as a root node.
func (w *Workspace) AddRootNode(node Node, class string, name ...string) (uint64, error) {
	w.Lock()
	defer w.Unlock()
	return w.AddRootNodeLocked(node, class, name...)
}

// AddRootNodeLocked is AddRootNode for callers that already hold the workspace lock.
func (w *Workspace) AddRootNodeLocked(node Node, class string, name ...string) (uint64, error) {
	return w.addNodeLocked(node, class, true, optionalName(name), "", nil, nil, false, false)
}

// AddNodeWithRoot adds node to the workspace and sets its initial root status.
func (w *Workspace) AddNodeWithRoot(node Node, class string, root bool, name ...string) (uint64, error) {
	w.Lock()
	defer w.Unlock()
	return w.AddNodeWithRootLocked(node, class, root, name...)
}

// AddNodeWithRootLocked is AddNodeWithRoot for callers that already hold the workspace lock.
func (w *Workspace) AddNodeWithRootLocked(node Node, class string, root bool, name ...string) (uint64, error) {
	return w.addNodeLocked(node, class, root, optionalName(name), "", nil, nil, false, false)
}

func (w *Workspace) addNodeByClassWithParamsLocked(node Node, class string, params NodeClassParams, name string) (uint64, error) {
	if err := validateNodeClassParams(params); err != nil {
		return 0, err
	}
	initData := &NodeInitData{
		PrimaryType: params.PrimaryType,
	}
	return w.addNodeLocked(node, class, params.Root, name, params.PrimaryType, params.InitialPorts, initData, true, false)
}

func (w *Workspace) addNodeLocked(node Node, class string, root bool, name string, primaryType string, initialPorts []Port, initData *NodeInitData, isClassConstructed bool, isRestored bool) (uint64, error) {
	return w.addNodeLockedWithIDs(node, class, root, name, primaryType, initialPorts, nil, 0, initData, isClassConstructed, isRestored)
}

func (w *Workspace) addNodeLockedWithIDs(node Node, class string, root bool, name string, primaryType string, initialPorts []Port, portIDs []uint64, nodeID uint64, initData *NodeInitData, isClassConstructed bool, isRestored bool) (uint64, error) {
	if w.closed {
		return 0, ErrWorkspaceClosed
	}

	if err := ValidateClassName(class); err != nil {
		return 0, err
	}
	if primaryType != "" {
		if err := ValidateTypeName(primaryType); err != nil {
			return 0, err
		}
	}
	if node == nil {
		return 0, ErrNoNode
	}

	// Reject if this node already exists
	for pair := w.nodes.Newest(); pair != nil; pair = pair.Prev() {
		if pair.Value != nil && pair.Value.Node == node {
			return 0, ErrNodeDup
		}
	}
	if err := w.rejectUniqueNodeDuplicateLocked(class, 0); err != nil {
		return 0, err
	}
	if name != "" {
		if err := ValidateNodeName(name); err != nil {
			return 0, err
		}
		if err := w.rejectNodeNameDuplicateLocked(name, 0); err != nil {
			return 0, err
		}
	}

	var id uint64
	if nodeID > 0 {
		if !w.reserveIDLocked(nodeID) {
			return 0, ErrNodeDup
		}
		id = nodeID
	} else {
		id = w.nextIDLocked()
	}
	if name == "" {
		name = w.generateNodeNameLocked(id, class, 0)
	}
	log := w.logf.NodeLogger(id, class)
	rec := nodeRecord{
		ID:          id,
		Node:        node,
		Class:       class,
		Name:        name,
		PrimaryType: primaryType,
		Label:       "",
		Popups:      []NodePopup{},
		Root:        root,
		LeftPorts:   []uint64{},
		RightPorts:  []uint64{},
		L:           log,
		stopped:     false,
	}
	addedPorts, err := w.addInitialPortsWithIDs(&rec, initialPorts, portIDs)
	if err != nil {
		return 0, err
	}
	if initData != nil {
		initData.PrimaryType = rec.PrimaryType
		initData.Name = rec.Name
		initData.Label = rec.Label
		initData.LeftPorts = slices.Clone(rec.LeftPorts)
		initData.RightPorts = slices.Clone(rec.RightPorts)
	}
	w.nodes.Set(id, &rec)

	if err := rec.OnInit(w, initData, false, false, isClassConstructed, isRestored); err != nil {
		w.log.Debugf("node %d faled in OnInit", id)
		w.failNodeLocked(id, "OnInit", err, false, false)
		for _, portID := range addedPorts {
			if port, present := w.ports.Get(portID); present && port != nil {
				w.enqueuePortNotification(NotificationPortAdded, portID, portSnapshot(port))
			}
		}
		w.enqueueNodeNotification(NotificationNodeAdded, id, nodeSnapshot(&rec))
		return id, err
	}

	if w.isReady {
		if err := rec.OnReady(); err != nil {
			w.log.Debugf("node %d faled in OnReady", id)
			w.failNodeLocked(id, "OnReady", err, false, false)
			for _, portID := range addedPorts {
				if port, present := w.ports.Get(portID); present && port != nil {
					w.enqueuePortNotification(NotificationPortAdded, portID, portSnapshot(port))
				}
			}
			w.enqueueNodeNotification(NotificationNodeAdded, id, nodeSnapshot(&rec))
			return id, err
		}
		if failed := w.recomputeRootPaths(false); len(failed) > 0 {
			err := failed[id]
			if err == nil {
				err = ErrNodePanic
			}
			w.log.Debugf("node %d faled in OnRootStatus", id)
			for _, portID := range addedPorts {
				if port, present := w.ports.Get(portID); present && port != nil {
					w.enqueuePortNotification(NotificationPortAdded, portID, portSnapshot(port))
				}
			}
			w.enqueueNodeNotification(NotificationNodeAdded, id, nodeSnapshot(&rec))
			return id, err
		}
	}

	w.log.Debug("node added", id)
	for _, portID := range addedPorts {
		port, _ := w.ports.Get(portID)
		w.enqueuePortNotification(NotificationPortAdded, portID, portSnapshot(port))
	}
	w.enqueueNodeNotification(NotificationNodeAdded, id, nodeSnapshot(&rec))
	w.pushUndoEntry(undoAddedNode{ID: id})

	return id, nil
}

// AddPlaceholderNode adds a placeholder node with predefined ports.
//
// Placeholder nodes have no Node implementation, but their ports and links
// participate in snapshots, copy/paste, save/restore, and DAG checks.
func (w *Workspace) AddPlaceholderNode(class string, ports []Port, name ...string) (uint64, error) {
	w.Lock()
	defer w.Unlock()
	return w.AddPlaceholderNodeLocked(class, ports, name...)
}

// AddPlaceholderNodeLocked is AddPlaceholderNode for callers that already hold the workspace lock.
func (w *Workspace) AddPlaceholderNodeLocked(class string, ports []Port, name ...string) (uint64, error) {
	return w.addPlaceholderNodeWithRootLocked(class, false, ports, nil, 0, optionalName(name))
}

// AddPlaceholderNodeWithRoot adds a placeholder node with predefined ports and
// an initial root flag. Placeholder root state is stored but ignored by
// snapshots and root-path tracing until the node is replaced by a normal node.
func (w *Workspace) AddPlaceholderNodeWithRoot(class string, root bool, ports []Port, name ...string) (uint64, error) {
	w.Lock()
	defer w.Unlock()
	return w.AddPlaceholderNodeWithRootLocked(class, root, ports, name...)
}

// AddPlaceholderNodeWithRootLocked is AddPlaceholderNodeWithRoot for callers that already hold the workspace lock.
func (w *Workspace) AddPlaceholderNodeWithRootLocked(class string, root bool, ports []Port, name ...string) (uint64, error) {
	return w.addPlaceholderNodeWithRootLocked(class, root, ports, nil, 0, optionalName(name))
}

func (w *Workspace) addPlaceholderNodeWithRoot(class string, root bool, ports []Port, portIDs []uint64, nodeID uint64, nodeName string) (uint64, error) {
	w.Lock()
	defer w.Unlock()
	return w.addPlaceholderNodeWithRootLocked(class, root, ports, portIDs, nodeID, nodeName)
}

func (w *Workspace) addPlaceholderNodeWithRootLocked(class string, root bool, ports []Port, portIDs []uint64, nodeID uint64, nodeName string) (uint64, error) {
	if w.closed {
		return 0, ErrWorkspaceClosed
	}
	if err := ValidateClassName(class); err != nil {
		return 0, err
	}
	if err := w.rejectUniqueNodeDuplicateLocked(class, 0); err != nil {
		return 0, err
	}
	if nodeName != "" {
		if err := ValidateNodeName(nodeName); err != nil {
			return 0, err
		}
		if err := w.rejectNodeNameDuplicateLocked(nodeName, 0); err != nil {
			return 0, err
		}
	}

	var id uint64
	if nodeID > 0 {
		if !w.reserveIDLocked(nodeID) {
			return 0, ErrNodeDup
		}
		id = nodeID
	} else {
		id = w.nextIDLocked()
	}
	if nodeName == "" {
		nodeName = w.generateNodeNameLocked(id, class, 0)
	}
	rec := nodeRecord{
		ID:          id,
		Node:        nil,
		Class:       class,
		Name:        nodeName,
		PrimaryType: "",
		Label:       "",
		Popups:      []NodePopup{},
		Root:        root,
		LeftPorts:   []uint64{},
		RightPorts:  []uint64{},
		L:           w.logf.NodeLogger(id, class),
	}
	added, err := w.addPlaceholderPortsWithIDs(&rec, ports, portIDs)
	if err != nil {
		return 0, err
	}
	w.nodes.Set(id, &rec)
	for _, portID := range added {
		port, _ := w.ports.Get(portID)
		w.enqueuePortNotification(NotificationPortAdded, portID, portSnapshot(port))
	}
	w.enqueueNodeNotification(NotificationNodeAdded, id, nodeSnapshot(&rec))
	w.recomputeRootPaths(true)
	w.pushUndoEntry(undoAddedNode{ID: id})
	return id, nil
}

// ReplaceNode replaces the Node implementation in an existing node record.
//
// The workspace keeps the node ID, class, primary type, label, ports, root
// state, and current root-path status. Existing node popups are cleared before
// the replacement node starts; any popups added by the replacement are treated
// as new observable state.
func (w *Workspace) ReplaceNode(id uint64, node Node) error {
	w.Lock()
	defer w.Unlock()
	return w.ReplaceNodeLocked(id, node)
}

// ReplaceNodeLocked is ReplaceNode for callers that already hold the workspace lock.
func (w *Workspace) ReplaceNodeLocked(id uint64, node Node) error {
	return w.replaceNodeLocked(id, node, "", false)
}

// ReplaceNodeWithName replaces the Node implementation and sets the node name.
//
// If name is empty, a generic unique name is generated.
func (w *Workspace) ReplaceNodeWithName(id uint64, node Node, name string) error {
	w.Lock()
	defer w.Unlock()
	return w.ReplaceNodeWithNameLocked(id, node, name)
}

// ReplaceNodeWithNameLocked is ReplaceNodeWithName for callers that already hold the workspace lock.
func (w *Workspace) ReplaceNodeWithNameLocked(id uint64, node Node, name string) error {
	return w.replaceNodeLocked(id, node, name, true)
}

func (w *Workspace) replaceNodeLocked(id uint64, node Node, name string, rename bool) error {
	if w.closed {
		return ErrWorkspaceClosed
	}
	if id < 1 {
		return ErrNoNode
	}

	record, present := w.nodes.Get(id)
	if !present || record == nil {
		return ErrNoNode
	}
	if node == nil {
		return ErrNoNode
	}
	if record.Node == node {
		return ErrNodeDup
	}
	for pair := w.nodes.Newest(); pair != nil; pair = pair.Prev() {
		if pair.Key != id && pair.Value != nil && pair.Value.Node == node {
			return ErrNodeDup
		}
	}
	if err := w.rejectUniqueNodeDuplicateLocked(record.Class, id); err != nil {
		return err
	}
	if rename {
		prepared, err := w.prepareNodeNameLocked(id, record.Class, name, id)
		if err != nil {
			return err
		}
		name = prepared
	} else if err := w.rejectNodeNameDuplicateLocked(record.Name, id); err != nil {
		return err
	}

	wasPlaceholder := record.Node == nil
	old := record.Node
	clearedPopups := len(record.Popups) > 0
	record.Popups = nil
	record.Menu = nil
	restored := record.InitData()
	nodeStop(old)
	w.closeNodeResourcesLocked(id)

	record.Node = node
	if rename {
		record.Name = name
		restored.Name = name
	}
	record.stopped = false
	if err := record.OnInit(w, &restored, true, wasPlaceholder, false, false); err != nil {
		w.log.Debugf("node %d faled in OnInit", id)
		w.failNodeLocked(id, "OnInit", err, true, true)
		return err
	}
	if w.isReady {
		if err := record.OnReady(); err != nil {
			w.log.Debugf("node %d faled in OnReady", id)
			w.failNodeLocked(id, "OnReady", err, true, true)
			return err
		}
		if wasPlaceholder {
			if failed := w.activatePlaceholderLinks(record); len(failed) > 0 {
				nodeStop(record.Node)
				w.closeNodeResourcesLocked(id)
				record.Node = nil
				record.stopped = false
				return firstError(failed)
			}
			w.refreshPlaceholderLinks(id, false)
			if !w.verifyDAG() {
				w.deactivateNodeLinks(record)
				nodeStop(record.Node)
				w.closeNodeResourcesLocked(id)
				record.Node = nil
				record.stopped = false
				w.refreshPlaceholderLinks(id, true)
				w.recomputeRootPaths(true)
				return ErrCycle
			}
			if failed := w.notifyActivatedPlaceholderLinks(record); len(failed) > 0 {
				w.deactivateNodeLinks(record)
				nodeStop(record.Node)
				w.closeNodeResourcesLocked(id)
				record.Node = nil
				record.stopped = false
				w.refreshPlaceholderLinks(id, true)
				w.recomputeRootPaths(true)
				return firstError(failed)
			}
			record.rootStatusKnown = false
			if failed := w.recomputeRootPaths(false); len(failed) > 0 {
				w.deactivateNodeLinks(record)
				nodeStop(record.Node)
				w.closeNodeResourcesLocked(id)
				record.Node = nil
				record.stopped = false
				return firstError(failed)
			}
		} else {
			if err := record.OnRootStatus(record.HasRootPath); err != nil {
				w.log.Debugf("node %d faled in OnRootStatus", id)
				w.failNodeLocked(id, "OnRootStatus", err, true, true)
				return err
			}
		}
	}
	if wasPlaceholder {
		w.refreshPlaceholderLinks(id, true)
		w.recomputeRootPaths(true)
		w.enqueueNodeNotification(NotificationNodeUpdated, id, nodeSnapshot(record))
	} else if clearedPopups || rename {
		w.enqueueNodeNotification(NotificationNodeUpdated, id, nodeSnapshot(record))
	}
	return nil
}

// ReplaceNodeWithPlaceholder replaces an existing node implementation with a
// placeholder record while preserving existing ports and links. Any supplied
// ports are added to the placeholder.
func (w *Workspace) ReplaceNodeWithPlaceholder(id uint64, ports []Port) error {
	w.Lock()
	defer w.Unlock()
	return w.ReplaceNodeWithPlaceholderLocked(id, ports)
}

// ReplaceNodeWithPlaceholderLocked is ReplaceNodeWithPlaceholder for callers that already hold the workspace lock.
func (w *Workspace) ReplaceNodeWithPlaceholderLocked(id uint64, ports []Port) error {
	return w.replaceNodeWithPlaceholderLocked(id, ports, "", false)
}

// ReplaceNodeWithPlaceholderWithName replaces an existing node implementation
// with a placeholder record and sets the node name. If name is empty, a generic
// unique name is generated.
func (w *Workspace) ReplaceNodeWithPlaceholderWithName(id uint64, ports []Port, name string) error {
	w.Lock()
	defer w.Unlock()
	return w.ReplaceNodeWithPlaceholderWithNameLocked(id, ports, name)
}

// ReplaceNodeWithPlaceholderWithNameLocked is ReplaceNodeWithPlaceholderWithName for callers that already hold the workspace lock.
func (w *Workspace) ReplaceNodeWithPlaceholderWithNameLocked(id uint64, ports []Port, name string) error {
	return w.replaceNodeWithPlaceholderLocked(id, ports, name, true)
}

func (w *Workspace) replaceNodeWithPlaceholderLocked(id uint64, ports []Port, name string, rename bool) error {
	if w.closed {
		return ErrWorkspaceClosed
	}
	record, present := w.nodes.Get(id)
	if id < 1 || !present || record == nil {
		return ErrNoNode
	}
	if err := validatePlaceholderPorts(id, ports); err != nil {
		return err
	}
	if err := w.rejectUniqueNodeDuplicateLocked(record.Class, id); err != nil {
		return err
	}
	if rename {
		prepared, err := w.prepareNodeNameLocked(id, record.Class, name, id)
		if err != nil {
			return err
		}
		name = prepared
	} else if err := w.rejectNodeNameDuplicateLocked(record.Name, id); err != nil {
		return err
	}

	old := record.Node
	if old != nil {
		nodeStop(old)
	}
	w.closeNodeResourcesLocked(id)
	record.Popups = nil
	record.Menu = nil
	record.Node = nil
	if rename {
		record.Name = name
	}
	record.stopped = false
	added, err := w.addPlaceholderPorts(record, ports)
	if err != nil {
		return err
	}
	w.deactivateNodeLinks(record)
	for _, portID := range added {
		port, _ := w.ports.Get(portID)
		w.enqueuePortNotification(NotificationPortAdded, portID, portSnapshot(port))
	}
	w.refreshPlaceholderLinks(id, true)
	changed := w.recomputeRootPaths(true)
	if _, present := changed[id]; !present {
		w.enqueueNodeNotification(NotificationNodeUpdated, id, nodeSnapshot(record))
	}
	return nil
}

// ReplacePlaceholderNode replaces a placeholder node with a normal Node.
func (w *Workspace) ReplacePlaceholderNode(id uint64, node Node) error {
	w.Lock()
	defer w.Unlock()
	return w.ReplacePlaceholderNodeLocked(id, node)
}

// ReplacePlaceholderNodeLocked is ReplacePlaceholderNode for callers that already hold the workspace lock.
func (w *Workspace) ReplacePlaceholderNodeLocked(id uint64, node Node) error {
	return w.ReplaceNodeLocked(id, node)
}

// ReplacePlaceholderNodeWithName replaces a placeholder node with a normal Node
// and sets the node name. If name is empty, a generic unique name is generated.
func (w *Workspace) ReplacePlaceholderNodeWithName(id uint64, node Node, name string) error {
	w.Lock()
	defer w.Unlock()
	return w.ReplacePlaceholderNodeWithNameLocked(id, node, name)
}

// ReplacePlaceholderNodeWithNameLocked is ReplacePlaceholderNodeWithName for callers that already hold the workspace lock.
func (w *Workspace) ReplacePlaceholderNodeWithNameLocked(id uint64, node Node, name string) error {
	return w.ReplaceNodeWithNameLocked(id, node, name)
}

func (w *Workspace) replacePlaceholderWithClassStateLocked(id uint64, class string, node Node, state NodeClassState) error {
	if w.closed {
		return ErrWorkspaceClosed
	}
	if id < 1 {
		return ErrNoNode
	}
	record, present := w.nodes.Get(id)
	if !present || record == nil || record.Node != nil || record.Class != class {
		return ErrNoNode
	}
	if node == nil {
		return ErrNoNode
	}
	for pair := w.nodes.Newest(); pair != nil; pair = pair.Prev() {
		if pair.Key != id && pair.Value != nil && pair.Value.Node == node {
			return ErrNodeDup
		}
	}
	if err := w.rejectUniqueNodeDuplicateLocked(class, id); err != nil {
		return err
	}
	if err := w.applyPlaceholderClassState(record, state); err != nil {
		return err
	}

	record.Popups = nil
	record.Menu = nil
	restored := record.InitData()
	w.closeNodeResourcesLocked(id)
	record.Node = node
	record.stopped = false
	if err := record.OnInit(w, &restored, true, true, true, false); err != nil {
		w.log.Debugf("node %d faled in OnInit", id)
		w.failNodeLocked(id, "OnInit", err, true, true)
		return err
	}
	if w.isReady {
		if err := record.OnReady(); err != nil {
			w.log.Debugf("node %d faled in OnReady", id)
			w.failNodeLocked(id, "OnReady", err, true, true)
			return err
		}
		if failed := w.activatePlaceholderLinks(record); len(failed) > 0 {
			nodeStop(record.Node)
			w.closeNodeResourcesLocked(id)
			record.Node = nil
			record.stopped = false
			return firstError(failed)
		}
		w.refreshPlaceholderLinks(id, false)
		if !w.verifyDAG() {
			w.deactivateNodeLinks(record)
			nodeStop(record.Node)
			w.closeNodeResourcesLocked(id)
			record.Node = nil
			record.stopped = false
			w.refreshPlaceholderLinks(id, true)
			w.recomputeRootPaths(true)
			return ErrCycle
		}
		if failed := w.notifyActivatedPlaceholderLinks(record); len(failed) > 0 {
			w.deactivateNodeLinks(record)
			nodeStop(record.Node)
			w.closeNodeResourcesLocked(id)
			record.Node = nil
			record.stopped = false
			w.refreshPlaceholderLinks(id, true)
			w.recomputeRootPaths(true)
			return firstError(failed)
		}
		record.rootStatusKnown = false
		if failed := w.recomputeRootPaths(false); len(failed) > 0 {
			w.deactivateNodeLinks(record)
			nodeStop(record.Node)
			w.closeNodeResourcesLocked(id)
			record.Node = nil
			record.stopped = false
			return firstError(failed)
		}
	}
	w.refreshPlaceholderLinks(id, true)
	w.recomputeRootPaths(true)
	w.enqueueNodeNotification(NotificationNodeUpdated, id, nodeSnapshot(record))
	return nil
}

// AddPort adds a port to its owner node and returns the new port ID.
//
// The input is copied, existing Links are ignored, and the port name must be
// unique among the owner node's ports on the same side.
func (w *Workspace) AddPort(port Port) (uint64, error) {
	port = port.Copy()
	port.Links = []uint64{}

	w.Lock()
	defer w.Unlock()
	return w.AddPortLocked(port)
}

// AddPortLocked is AddPort for callers that already hold the workspace lock.
func (w *Workspace) AddPortLocked(port Port) (uint64, error) {
	port = port.Copy()
	port.Links = []uint64{}

	if w.closed {
		return 0, ErrWorkspaceClosed
	}

	// Make sure node exists
	record, present := w.nodes.Get(port.Node)
	if !present || record == nil {
		return 0, ErrNoNode
	}
	if err := port.Validate(); err != nil {
		return 0, err
	}
	if err := w.validatePortNameAvailable(record, port.Direction, port.Name, 0); err != nil {
		return 0, err
	}

	id := w.NextIDLocked()
	port.ID = id
	w.ports.Set(id, &port)

	if err := record.OnPortAdd(port.ID, port.Direction, port.CopyTypes()); err != nil {
		w.log.Debugf("node %d faled in OnPortAdd", record.ID)
		w.ports.Delete(port.ID)
		w.failNodeLocked(record.ID, "OnPortAdd", err, true, true)
		return 0, err
	}

	if port.Direction == "left" {
		record.LeftPorts = append(record.LeftPorts, port.ID)
	} else {
		record.RightPorts = append(record.RightPorts, port.ID)
	}

	w.log.Debug("port added", id)
	w.enqueuePortNotification(NotificationPortAdded, id, portSnapshot(&port))
	w.enqueueNodeNotification(NotificationNodeUpdated, record.ID, nodeSnapshot(record))

	return id, nil
}

// AddLink connects two ports and returns the new link ID and selected link type.
//
// The ports must exist, belong to different nodes, have opposite directions,
// share a usable type, and preserve the workspace DAG. If either node rejects
// the link from PreLinkAdd, no link is added and the node remains active. If a
// node panics during PreLinkAdd, or panics or returns an error during
// OnLinkAdd, that node is replaced with a placeholder carrying an error popup.
func (w *Workspace) AddLink(pa, pb uint64) (uint64, string, error) {
	w.Lock()
	defer w.Unlock()
	return w.AddLinkLocked(pa, pb)
}

// AddLinkLocked is AddLink for callers that already hold the workspace lock.
func (w *Workspace) AddLinkLocked(pa, pb uint64) (uint64, string, error) {
	if w.closed {
		return 0, "", ErrWorkspaceClosed
	}

	portA, present := w.ports.Get(pa)
	if !present || portA == nil {
		return 0, "", ErrNoPort
	}

	portB, present := w.ports.Get(pb)
	if !present || portB == nil {
		return 0, "", ErrNoPort
	}

	if portA.Node == portB.Node {
		return 0, "", ErrCycle
	}

	if portA.Direction == portB.Direction {
		return 0, "", ErrSameDirection
	}

	var (
		Left, Right *Port
	)

	if portA.Direction == "left" {
		Left = portA
		Right = portB
	} else {
		Left = portB
		Right = portA
	}

	if w.portsConnected(Left, Right) {
		return 0, "", ErrLinkDup
	}

	linkType := w.portsSharedType(Left, Right)
	if linkType == "" {
		return 0, "", ErrTypeCompat
	}

	leftNode, present := w.nodes.Get(Left.Node)
	if !present || leftNode == nil {
		return 0, "", ErrNoNode
	}
	rightNode, present := w.nodes.Get(Right.Node)
	if !present || rightNode == nil {
		return 0, "", ErrNoNode
	}
	placeholder := leftNode.Node == nil || rightNode.Node == nil

	if !placeholder {
		rejection := leftNode.PreLinkAdd(Left.ID, linkType, Left.Direction)
		if rejection != nil {
			if errors.Is(rejection, ErrNodePanic) {
				w.log.Debugf("node %d faled in PreLinkAdd", leftNode.ID)
				w.failNodeLocked(leftNode.ID, "PreLinkAdd", rejection, true, true)
			}
			return 0, "", rejection
		}
		rejection = rightNode.PreLinkAdd(Right.ID, linkType, Right.Direction)
		if rejection != nil {
			if errors.Is(rejection, ErrNodePanic) {
				w.log.Debugf("node %d faled in PreLinkAdd", rightNode.ID)
				w.failNodeLocked(rightNode.ID, "PreLinkAdd", rejection, true, true)
			}
			return 0, "", rejection
		}
	}

	link := Link{
		ID:   w.NextIDLocked(),
		Type: linkType,

		LeftPort:     Left.ID,
		LeftPortNode: leftNode.ID,

		RightPort:     Right.ID,
		RightPortNode: rightNode.ID,
	}
	link.Placeholder = placeholder
	w.links.Set(link.ID, &link)

	if !w.verifyDAG() {
		w.links.Delete(link.ID)
		w.closeLinkResourcesLocked(link.ID)
		return 0, "", ErrCycle
	}

	if !link.Placeholder {
		err := leftNode.OnLinkAdd(link.ID, Left.ID, link.Type, Left.Direction)
		if err != nil {
			w.links.Delete(link.ID)
			w.closeLinkResourcesLocked(link.ID)
			w.log.Debugf("node %d faled in OnLinkAdd", leftNode.ID)
			w.failNodeLocked(leftNode.ID, "OnLinkAdd", err, true, true)
			return 0, "", err
		}
		err = rightNode.OnLinkAdd(link.ID, Right.ID, link.Type, Right.Direction)
		if err != nil {
			w.links.Delete(link.ID)
			w.log.Debugf("node %d faled in OnLinkAdd", rightNode.ID)
			w.failNodeLocked(rightNode.ID, "OnLinkAdd", err, true, true)
			w.nodeEvLinkRemoved(leftNode.ID, link.ID, Left.ID, link.Type, Left.Direction)
			w.closeLinkResourcesLocked(link.ID)
			return 0, "", err
		}
	}

	Left.Links = append(Left.Links, link.ID)
	Right.Links = append(Right.Links, link.ID)

	w.log.Debug("link added", link.ID)
	w.enqueueLinkNotification(NotificationLinkAdded, link.ID, linkSnapshot(&link))
	w.enqueuePortNotification(NotificationPortUpdated, Left.ID, portSnapshot(Left))
	w.enqueuePortNotification(NotificationPortUpdated, Right.ID, portSnapshot(Right))
	w.recomputeRootPaths(true)
	w.pushUndoEntry(undoAddedLink{ID: link.ID})
	return link.ID, link.Type, nil
}

// PortsConnected reports whether two ports are connected by a link.
func (w *Workspace) PortsConnected(pa, pb uint64) bool {
	w.Lock()
	defer w.Unlock()
	return w.PortsConnectedLocked(pa, pb)
}

// PortsConnectedLocked is PortsConnected for callers that already hold the workspace lock.
func (w *Workspace) PortsConnectedLocked(pa, pb uint64) bool {
	if w.closed {
		return false
	}

	portA, present := w.ports.Get(pa)
	if !present || portA == nil {
		return false
	}

	portB, present := w.ports.Get(pb)
	if !present || portB == nil {
		return false
	}

	return w.portsConnected(portA, portB)
}

// NodesConnected reports whether two nodes have at least one direct link.
func (w *Workspace) NodesConnected(na, nb uint64) bool {
	w.Lock()
	defer w.Unlock()
	return w.NodesConnectedLocked(na, nb)
}

// NodesConnectedLocked is NodesConnected for callers that already hold the workspace lock.
func (w *Workspace) NodesConnectedLocked(na, nb uint64) bool {
	if w.closed {
		return false
	}

	return len(w.linksBetweenNodes(na, nb)) > 0
}

// LinkByPorts returns the link connecting two ports, regardless of argument
// order.
func (w *Workspace) LinkByPorts(pa, pb uint64) (uint64, LinkSnapshot, bool) {
	w.Lock()
	defer w.Unlock()
	return w.LinkByPortsLocked(pa, pb)
}

// LinkByPortsLocked is LinkByPorts for callers that already hold the workspace lock.
func (w *Workspace) LinkByPortsLocked(pa, pb uint64) (uint64, LinkSnapshot, bool) {
	if w.closed {
		return 0, LinkSnapshot{}, false
	}

	portA, present := w.ports.Get(pa)
	if !present || portA == nil {
		return 0, LinkSnapshot{}, false
	}
	portB, present := w.ports.Get(pb)
	if !present || portB == nil {
		return 0, LinkSnapshot{}, false
	}

	link := w.linkBetweenPorts(portA, portB)
	if link == nil {
		return 0, LinkSnapshot{}, false
	}
	return link.ID, linkSnapshot(link), true
}

// GetLinkByPorts returns the link connecting two ports.
func (w *Workspace) GetLinkByPorts(pa, pb uint64) (uint64, LinkSnapshot, bool) {
	return w.LinkByPorts(pa, pb)
}

// GetLinkByPortsLocked is GetLinkByPorts for callers that already hold the workspace lock.
func (w *Workspace) GetLinkByPortsLocked(pa, pb uint64) (uint64, LinkSnapshot, bool) {
	return w.LinkByPortsLocked(pa, pb)
}

// LinksByNodes returns snapshots of all direct links between two nodes.
func (w *Workspace) LinksByNodes(na, nb uint64) map[uint64]LinkSnapshot {
	w.Lock()
	defer w.Unlock()
	return w.LinksByNodesLocked(na, nb)
}

// LinksByNodesLocked is LinksByNodes for callers that already hold the workspace lock.
func (w *Workspace) LinksByNodesLocked(na, nb uint64) map[uint64]LinkSnapshot {
	if w.closed {
		return nil
	}

	links := w.linksBetweenNodes(na, nb)
	snapshots := make(map[uint64]LinkSnapshot, len(links))
	for _, link := range links {
		snapshots[link.ID] = linkSnapshot(link)
	}
	return snapshots
}

// GetLinksByNodes returns snapshots of all direct links between two nodes.
func (w *Workspace) GetLinksByNodes(na, nb uint64) map[uint64]LinkSnapshot {
	return w.LinksByNodes(na, nb)
}

// GetLinksByNodesLocked is GetLinksByNodes for callers that already hold the workspace lock.
func (w *Workspace) GetLinksByNodesLocked(na, nb uint64) map[uint64]LinkSnapshot {
	return w.LinksByNodesLocked(na, nb)
}

// RemoveLinksByNodes removes every direct link between two nodes.
func (w *Workspace) RemoveLinksByNodes(na, nb uint64) {
	w.Lock()
	defer w.Unlock()
	w.RemoveLinksByNodesLocked(na, nb)
}

// RemoveLinksByNodesLocked is RemoveLinksByNodes for callers that already hold the workspace lock.
func (w *Workspace) RemoveLinksByNodesLocked(na, nb uint64) {
	if w.closed {
		return
	}

	links := w.linksBetweenNodes(na, nb)
	for _, link := range links {
		w.RemoveLinkLocked(link.ID)
	}
}

// SetNodePrimary sets a node's primary type.
//
// typ may be empty to clear the primary type. Non-empty values must be valid
// type names.
func (w *Workspace) SetNodePrimary(id uint64, typ string) error {
	w.Lock()
	defer w.Unlock()
	return w.SetNodePrimaryLocked(id, typ)
}

// SetNodePrimaryLocked is SetNodePrimary for callers that already hold the workspace lock.
func (w *Workspace) SetNodePrimaryLocked(id uint64, typ string) error {
	if w.closed {
		return ErrWorkspaceClosed
	}

	if typ != "" {
		if err := ValidateTypeName(typ); err != nil {
			return err
		}
	}

	record, present := w.nodes.Get(id)
	if !present || record == nil {
		return ErrNoNode
	}
	record.PrimaryType = typ
	w.enqueueNodeNotification(NotificationNodeUpdated, id, nodeSnapshot(record))
	return nil
}

// SetNodeLabel sets a node's optional display label.
func (w *Workspace) SetNodeLabel(id uint64, label string) error {
	w.Lock()
	defer w.Unlock()
	return w.SetNodeLabelLocked(id, label)
}

// SetNodeLabelLocked is SetNodeLabel for callers that already hold the workspace lock.
func (w *Workspace) SetNodeLabelLocked(id uint64, label string) error {
	if w.closed {
		return ErrWorkspaceClosed
	}

	record, present := w.nodes.Get(id)
	if !present || record == nil {
		return ErrNoNode
	}
	record.Label = label
	w.enqueueNodeNotification(NotificationNodeUpdated, id, nodeSnapshot(record))
	return nil
}

// SetNodePosition sets a node's opaque frontend position string.
//
// position may be empty. The workspace stores it without interpreting its
// coordinate system or format.
func (w *Workspace) SetNodePosition(id uint64, position string) error {
	w.Lock()
	defer w.Unlock()
	return w.SetNodePositionLocked(id, position)
}

// SetNodePositionLocked is SetNodePosition for callers that already hold the workspace lock.
func (w *Workspace) SetNodePositionLocked(id uint64, position string) error {
	if w.closed {
		return ErrWorkspaceClosed
	}

	record, present := w.nodes.Get(id)
	if !present || record == nil {
		return ErrNoNode
	}
	if record.Position == position {
		return nil
	}
	before := record.Position
	record.Position = position
	w.pushUndoEntry(undoNodePosition{ID: id, Before: before, After: position})
	w.enqueueNodeNotification(NotificationNodeUpdated, id, nodeSnapshot(record))
	return nil
}

// SetNodeName sets a node's unique name.
//
// Node names are the stable keys used by SaveConfig and WorkspaceFromConfig.
func (w *Workspace) SetNodeName(id uint64, name string) error {
	w.Lock()
	defer w.Unlock()
	return w.SetNodeNameLocked(id, name)
}

// SetNodeNameLocked is SetNodeName for callers that already hold the workspace lock.
func (w *Workspace) SetNodeNameLocked(id uint64, name string) error {
	if w.closed {
		return ErrWorkspaceClosed
	}
	if err := ValidateNodeName(name); err != nil {
		return err
	}

	record, present := w.nodes.Get(id)
	if !present || record == nil {
		return ErrNoNode
	}
	if err := w.rejectNodeNameDuplicateLocked(name, id); err != nil {
		return err
	}
	record.Name = name
	w.enqueueNodeNotification(NotificationNodeUpdated, id, nodeSnapshot(record))
	return nil
}

// NodeIDByName returns the workspace ID for a node name.
func (w *Workspace) NodeIDByName(name string) (uint64, bool) {
	if err := ValidateNodeName(name); err != nil {
		return 0, false
	}

	w.Lock()
	defer w.Unlock()
	return w.NodeIDByNameLocked(name)
}

// NodeIDByNameLocked is NodeIDByName for callers that already hold the workspace lock.
func (w *Workspace) NodeIDByNameLocked(name string) (uint64, bool) {
	if w.closed {
		return 0, false
	}
	for pair := w.nodes.Oldest(); pair != nil; pair = pair.Next() {
		if pair.Value != nil && pair.Value.Name == name {
			return pair.Key, true
		}
	}
	return 0, false
}

// AddNodePopup appends a user-facing popup note to a node and returns its ID.
//
// Popup types must be NodePopupInfo, NodePopupWard, or NodePopupErr. Popups are
// intended for node implementations to surface user-actionable state, such as
// incorrect node configuration, and usually duplicate details written to logs.
// If deduplicate is true, earlier popups on the same node with the same type
// and text are removed before the new popup is appended.
func (w *Workspace) AddNodePopup(id uint64, popupType, text string, deduplicate bool) (uint64, error) {
	w.Lock()
	defer w.Unlock()
	return w.AddNodePopupLocked(id, popupType, text, deduplicate)
}

// AddNodePopupLocked is AddNodePopup for callers that already hold the workspace lock.
func (w *Workspace) AddNodePopupLocked(id uint64, popupType, text string, deduplicate bool) (uint64, error) {
	if w.closed {
		return 0, ErrWorkspaceClosed
	}
	if err := ValidateNodePopupType(popupType); err != nil {
		return 0, err
	}

	record, present := w.nodes.Get(id)
	if !present || record == nil {
		return 0, ErrNoNode
	}
	if deduplicate {
		record.Popups = slices.DeleteFunc(record.Popups, func(popup NodePopup) bool {
			return popup.Type == popupType && popup.Text == text
		})
	}
	popupID := w.NextIDLocked()
	record.Popups = append(record.Popups, NodePopup{
		ID:   popupID,
		Type: popupType,
		Text: text,
	})
	w.enqueueNodeNotification(NotificationNodeUpdated, id, nodeSnapshot(record))
	return popupID, nil
}

// RemoveNodePopups removes every popup attached to one node.
func (w *Workspace) RemoveNodePopups(id uint64) error {
	w.Lock()
	defer w.Unlock()
	return w.RemoveNodePopupsLocked(id)
}

// RemoveNodePopupsLocked is RemoveNodePopups for callers that already hold the workspace lock.
func (w *Workspace) RemoveNodePopupsLocked(id uint64) error {
	if w.closed {
		return ErrWorkspaceClosed
	}

	record, present := w.nodes.Get(id)
	if !present || record == nil {
		return ErrNoNode
	}
	if len(record.Popups) == 0 {
		return nil
	}
	record.Popups = nil
	w.enqueueNodeNotification(NotificationNodeUpdated, id, nodeSnapshot(record))
	return nil
}

// RemoveNodePopup removes one popup from one node by popup ID.
func (w *Workspace) RemoveNodePopup(id, popupID uint64) error {
	w.Lock()
	defer w.Unlock()
	return w.RemoveNodePopupLocked(id, popupID)
}

// RemoveNodePopupLocked is RemoveNodePopup for callers that already hold the workspace lock.
func (w *Workspace) RemoveNodePopupLocked(id, popupID uint64) error {
	if w.closed {
		return ErrWorkspaceClosed
	}

	record, present := w.nodes.Get(id)
	if !present || record == nil {
		return ErrNoNode
	}
	before := len(record.Popups)
	record.Popups = slices.DeleteFunc(record.Popups, func(popup NodePopup) bool {
		return popup.ID == popupID
	})
	if len(record.Popups) != before {
		w.enqueueNodeNotification(NotificationNodeUpdated, id, nodeSnapshot(record))
	}
	return nil
}

// RemoveNodePopupsByText removes every popup with text from one node.
func (w *Workspace) RemoveNodePopupsByText(id uint64, text string) error {
	w.Lock()
	defer w.Unlock()
	return w.RemoveNodePopupsByTextLocked(id, text)
}

// RemoveNodePopupsByTextLocked is RemoveNodePopupsByText for callers that already hold the workspace lock.
func (w *Workspace) RemoveNodePopupsByTextLocked(id uint64, text string) error {
	if w.closed {
		return ErrWorkspaceClosed
	}

	record, present := w.nodes.Get(id)
	if !present || record == nil {
		return ErrNoNode
	}
	before := len(record.Popups)
	record.Popups = slices.DeleteFunc(record.Popups, func(popup NodePopup) bool {
		return popup.Text == text
	})
	if len(record.Popups) != before {
		w.enqueueNodeNotification(NotificationNodeUpdated, id, nodeSnapshot(record))
	}
	return nil
}

// RemoveNodePopupsByType removes every popup with popupType from one node.
func (w *Workspace) RemoveNodePopupsByType(id uint64, popupType string) error {
	w.Lock()
	defer w.Unlock()
	return w.RemoveNodePopupsByTypeLocked(id, popupType)
}

// RemoveNodePopupsByTypeLocked is RemoveNodePopupsByType for callers that already hold the workspace lock.
func (w *Workspace) RemoveNodePopupsByTypeLocked(id uint64, popupType string) error {
	if w.closed {
		return ErrWorkspaceClosed
	}
	if err := ValidateNodePopupType(popupType); err != nil {
		return err
	}

	record, present := w.nodes.Get(id)
	if !present || record == nil {
		return ErrNoNode
	}
	before := len(record.Popups)
	record.Popups = slices.DeleteFunc(record.Popups, func(popup NodePopup) bool {
		return popup.Type == popupType
	})
	if len(record.Popups) != before {
		w.enqueueNodeNotification(NotificationNodeUpdated, id, nodeSnapshot(record))
	}
	return nil
}

// SetNodeRoot changes whether a node is an explicit workspace root.
func (w *Workspace) SetNodeRoot(id uint64, root bool) error {
	w.Lock()
	defer w.Unlock()
	return w.SetNodeRootLocked(id, root)
}

// SetNodeRootLocked is SetNodeRoot for callers that already hold the workspace lock.
func (w *Workspace) SetNodeRootLocked(id uint64, root bool) error {
	if w.closed {
		return ErrWorkspaceClosed
	}

	record, present := w.nodes.Get(id)
	if !present || record == nil {
		return ErrNoNode
	}
	if record.Root == root {
		return nil
	}

	record.Root = root
	changed := w.recomputeRootPaths(true)
	if _, present := changed[id]; !present {
		w.enqueueNodeNotification(NotificationNodeUpdated, id, nodeSnapshot(record))
	}
	return nil
}

// SetNodePortOrder sets a node's explicit port order for one direction.
//
// direction must be "left" or "right". ports must contain exactly the node's
// current ports for that direction, with no omissions, additions, or duplicates.
func (w *Workspace) SetNodePortOrder(id uint64, direction string, ports []uint64) error {
	w.Lock()
	defer w.Unlock()
	return w.SetNodePortOrderLocked(id, direction, ports)
}

// SetNodePortOrderLocked is SetNodePortOrder for callers that already hold the workspace lock.
func (w *Workspace) SetNodePortOrderLocked(id uint64, direction string, ports []uint64) error {
	if w.closed {
		return ErrWorkspaceClosed
	}

	record, present := w.nodes.Get(id)
	if !present || record == nil {
		return ErrNoNode
	}
	ordered, err := validateNodePortOrder(record, direction, ports)
	if err != nil {
		return err
	}
	if direction == "left" {
		record.LeftPorts = ordered
	} else {
		record.RightPorts = ordered
	}
	w.enqueueNodeNotification(NotificationNodeUpdated, id, nodeSnapshot(record))
	return nil
}

// SetNodePortsOrder sets a node's explicit left and right port order.
//
// Each list must contain exactly the node's current ports for that side, with
// no omissions, additions, or duplicates.
func (w *Workspace) SetNodePortsOrder(id uint64, leftPorts, rightPorts []uint64) error {
	w.Lock()
	defer w.Unlock()
	return w.SetNodePortsOrderLocked(id, leftPorts, rightPorts)
}

// SetNodePortsOrderLocked is SetNodePortsOrder for callers that already hold the workspace lock.
func (w *Workspace) SetNodePortsOrderLocked(id uint64, leftPorts, rightPorts []uint64) error {
	if w.closed {
		return ErrWorkspaceClosed
	}

	record, present := w.nodes.Get(id)
	if !present || record == nil {
		return ErrNoNode
	}
	left, err := validateNodePortOrder(record, "left", leftPorts)
	if err != nil {
		return err
	}
	right, err := validateNodePortOrder(record, "right", rightPorts)
	if err != nil {
		return err
	}

	record.LeftPorts = left
	record.RightPorts = right
	w.enqueueNodeNotification(NotificationNodeUpdated, id, nodeSnapshot(record))
	return nil
}

// SetPortName sets a port's display name.
func (w *Workspace) SetPortName(id uint64, name string) error {
	w.Lock()
	defer w.Unlock()
	return w.SetPortNameLocked(id, name)
}

// SetPortNameLocked is SetPortName for callers that already hold the workspace lock.
func (w *Workspace) SetPortNameLocked(id uint64, name string) error {
	if w.closed {
		return ErrWorkspaceClosed
	}

	port, present := w.ports.Get(id)
	if !present || port == nil {
		return ErrNoPort
	}
	record, present := w.nodes.Get(port.Node)
	if !present || record == nil {
		return ErrNoNode
	}
	if err := ValidatePortName(name); err != nil {
		return err
	}
	if err := w.validatePortNameAvailable(record, port.Direction, name, id); err != nil {
		return err
	}
	port.Name = name
	w.enqueuePortNotification(NotificationPortUpdated, id, portSnapshot(port))
	return nil
}

// SetPortTypes sets a port's supported link types.
//
// Existing links that are no longer compatible with the updated type list are
// removed. The type list must be non-empty and every type must be valid.
func (w *Workspace) SetPortTypes(id uint64, types []string) error {
	w.Lock()
	defer w.Unlock()
	return w.SetPortTypesLocked(id, types)
}

// SetPortTypesLocked is SetPortTypes for callers that already hold the workspace lock.
func (w *Workspace) SetPortTypesLocked(id uint64, types []string) error {
	if w.closed {
		return ErrWorkspaceClosed
	}

	port, present := w.ports.Get(id)
	if !present || port == nil {
		return ErrNoPort
	}
	replacement := port.Copy()
	replacement.Types = append([]string{}, types...)
	if err := replacement.Validate(); err != nil {
		return err
	}
	port.Types = replacement.Types
	w.removeIncompatiblePlaceholderPortLinks(port)
	w.enqueuePortNotification(NotificationPortUpdated, id, portSnapshot(port))
	return nil
}

// verifyDAG reports whether the current link graph is acyclic.
func (w *Workspace) verifyDAG() bool {
	graph := make(map[uint64][]uint64, w.nodes.Len())
	for pair := w.nodes.Oldest(); pair != nil; pair = pair.Next() {
		graph[pair.Key] = nil
	}
	for pair := w.links.Oldest(); pair != nil; pair = pair.Next() {
		link := pair.Value
		if link == nil {
			continue
		}
		if link.LeftPortNode == link.RightPortNode {
			return false
		}
		graph[link.LeftPortNode] = append(graph[link.LeftPortNode], link.RightPortNode)
		if _, present := graph[link.RightPortNode]; !present {
			graph[link.RightPortNode] = nil
		}
	}

	visits := make(map[uint64]uint8, len(graph))
	var visit func(uint64) bool
	visit = func(node uint64) bool {
		switch visits[node] {
		case 1:
			return false
		case 2:
			return true
		}

		visits[node] = 1
		for _, next := range graph[node] {
			if !visit(next) {
				return false
			}
		}
		visits[node] = 2
		return true
	}

	for node := range graph {
		if !visit(node) {
			return false
		}
	}
	return true
}

// recomputeRootPaths updates path-to-root status for every node.
//
// Links are treated as undirected for this purpose. When notify is true,
// changed node snapshots are enqueued. The return value contains nodes whose
// OnRootStatus callback failed and were replaced with placeholders.
func (w *Workspace) recomputeRootPaths(notify bool) map[uint64]error {
	if !w.isReady {
		return nil
	}

	reachable := make(map[uint64]bool, w.nodes.Len())
	queue := make([]uint64, 0)
	for pair := w.nodes.Oldest(); pair != nil; pair = pair.Next() {
		record := pair.Value
		if record == nil {
			continue
		}
		reachable[pair.Key] = false
		if record.Node != nil && record.Root {
			reachable[pair.Key] = true
			queue = append(queue, pair.Key)
		}
	}

	adjacency := make(map[uint64][]uint64, w.nodes.Len())
	for pair := w.links.Oldest(); pair != nil; pair = pair.Next() {
		link := pair.Value
		if link == nil {
			continue
		}
		if link.Placeholder {
			continue
		}
		if _, present := reachable[link.LeftPortNode]; !present {
			continue
		}
		if _, present := reachable[link.RightPortNode]; !present {
			continue
		}
		adjacency[link.LeftPortNode] = append(adjacency[link.LeftPortNode], link.RightPortNode)
		adjacency[link.RightPortNode] = append(adjacency[link.RightPortNode], link.LeftPortNode)
	}

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		for _, next := range adjacency[node] {
			if reachable[next] {
				continue
			}
			reachable[next] = true
			queue = append(queue, next)
		}
	}

	failed := make(map[uint64]error)
	for pair := w.nodes.Oldest(); pair != nil; pair = pair.Next() {
		record := pair.Value
		if record == nil {
			continue
		}
		hasRootPath := reachable[pair.Key]
		changed := !record.rootStatusKnown || record.HasRootPath != hasRootPath
		record.HasRootPath = hasRootPath
		record.rootStatusKnown = true
		if !changed {
			continue
		}
		if notify {
			w.enqueueNodeNotification(NotificationNodeUpdated, pair.Key, nodeSnapshot(record))
		}
		if record.Node == nil {
			continue
		}
		if err := record.OnRootStatus(hasRootPath); err != nil {
			w.log.Debugf("node %d faled in OnRootStatus", record.ID)
			failed[record.ID] = err
			w.failNodeLocked(record.ID, "OnRootStatus", err, notify, false)
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return failed
}

func validateNodePortOrder(record *nodeRecord, direction string, ports []uint64) ([]uint64, error) {
	var current []uint64
	switch direction {
	case "left":
		current = record.LeftPorts
	case "right":
		current = record.RightPorts
	default:
		return nil, errors.Join(ErrPortDirection, errors.New(direction))
	}

	if len(ports) != len(current) {
		return nil, ErrPortOrder
	}
	available := make(map[uint64]int, len(current))
	for _, port := range current {
		available[port] += 1
	}
	ordered := slices.Clone(ports)
	for _, port := range ordered {
		if available[port] < 1 {
			return nil, ErrPortOrder
		}
		available[port] -= 1
	}
	return ordered, nil
}

func (w *Workspace) nodeIDsByClassLocked(class string) []uint64 {
	nodes := make([]uint64, 0)
	for pair := w.nodes.Oldest(); pair != nil; pair = pair.Next() {
		if pair.Value == nil || pair.Value.Class != class {
			continue
		}
		nodes = append(nodes, pair.Key)
	}
	return nodes
}

func (w *Workspace) nodeClassIsUniqueLocked(class string) bool {
	nodeClass, present := w.classes.Get(class)
	if !present || nodeClass == nil {
		return false
	}
	return nodeClass.DefaultNodeParams().Unique
}

func (w *Workspace) rejectUniqueNodeDuplicateLocked(class string, except uint64) error {
	if !w.nodeClassIsUniqueLocked(class) {
		return nil
	}
	for pair := w.nodes.Oldest(); pair != nil; pair = pair.Next() {
		if pair.Value == nil || pair.Value.Class != class || pair.Key == except {
			continue
		}
		return errors.Join(ErrUniqueNodeClassDup, errors.New(class))
	}
	return nil
}

func (w *Workspace) prepareNodeNameLocked(id uint64, class, name string, except uint64) (string, error) {
	if name != "" {
		if err := ValidateNodeName(name); err != nil {
			return "", err
		}
		if err := w.rejectNodeNameDuplicateLocked(name, except); err != nil {
			return "", err
		}
		return name, nil
	}
	return w.generateNodeNameLocked(id, class, except), nil
}

func (w *Workspace) rejectNodeNameDuplicateLocked(name string, except uint64) error {
	if err := ValidateNodeName(name); err != nil {
		return err
	}
	for pair := w.nodes.Oldest(); pair != nil; pair = pair.Next() {
		if pair.Key == except || pair.Value == nil {
			continue
		}
		if pair.Value.Name == name {
			return ErrNodeNameDup
		}
	}
	return nil
}

func (w *Workspace) generateNodeNameLocked(id uint64, class string, except uint64) string {
	base := shortClassName(class)
	candidate := fmt.Sprintf("%s %d", base, id)
	if w.nodeNameAvailableLocked(candidate, except) {
		return candidate
	}
	for length := 2; ; length++ {
		for attempt := 0; attempt < 16; attempt++ {
			candidate = base + " " + w.randomNodeNameSuffix(length)
			if w.nodeNameAvailableLocked(candidate, except) {
				return candidate
			}
		}
	}
}

func (w *Workspace) nodeNameAvailableLocked(name string, except uint64) bool {
	for pair := w.nodes.Oldest(); pair != nil; pair = pair.Next() {
		if pair.Key == except || pair.Value == nil {
			continue
		}
		if pair.Value.Name == name {
			return false
		}
	}
	return true
}

func (w *Workspace) randomNodeNameSuffix(length int) string {
	if length < 1 {
		length = 1
	}
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	const alphaNumeric = letters + "0123456789"
	if w.nameRand == nil {
		w.nameRand = rand.New(rand.NewSource(1))
	}
	buf := make([]byte, length)
	buf[0] = letters[w.nameRand.Intn(len(letters))]
	for i := 1; i < length; i++ {
		buf[i] = alphaNumeric[w.nameRand.Intn(len(alphaNumeric))]
	}
	return string(buf)
}

func shortClassName(class string) string {
	_, suffix, ok := strings.Cut(class, "/")
	if !ok || suffix == "" {
		return class
	}
	return suffix
}

func optionalName(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func (w *Workspace) removeUniqueNodeClassDuplicatesLocked(class string) {
	nodes := w.nodeIDsByClassLocked(class)
	if len(nodes) < 2 {
		return
	}
	keep := slices.Min(nodes)
	for _, id := range nodes {
		if id != keep {
			w.RemoveNodeLocked(id)
		}
	}
}

type nodeClassPlaceholderCandidate struct {
	id    uint64
	state NodeClassState
}

func (w *Workspace) nodeClassPlaceholderCandidatesLocked(class string) []nodeClassPlaceholderCandidate {
	placeholders := make([]nodeClassPlaceholderCandidate, 0)
	for pair := w.nodes.Oldest(); pair != nil; pair = pair.Next() {
		if pair.Value == nil || pair.Value.Class != class || pair.Value.Node != nil {
			continue
		}
		placeholders = append(placeholders, nodeClassPlaceholderCandidate{
			id:    pair.Key,
			state: w.placeholderClassStateLocked(pair.Value),
		})
	}
	return placeholders
}

func (w *Workspace) placeholderClassStateLocked(record *nodeRecord) NodeClassState {
	state := NodeClassState{
		Root:        record.Root,
		PrimaryType: record.PrimaryType,
		Name:        record.Name,
		Label:       record.Label,
		LeftPorts:   make([]Port, 0, len(record.LeftPorts)),
		RightPorts:  make([]Port, 0, len(record.RightPorts)),
	}
	for _, portID := range record.LeftPorts {
		if port, present := w.ports.Get(portID); present && port != nil {
			state.LeftPorts = append(state.LeftPorts, port.Copy())
		}
	}
	for _, portID := range record.RightPorts {
		if port, present := w.ports.Get(portID); present && port != nil {
			state.RightPorts = append(state.RightPorts, port.Copy())
		}
	}
	return state
}

func (w *Workspace) applyPlaceholderClassState(record *nodeRecord, state NodeClassState) error {
	if state.PrimaryType != "" {
		if err := ValidateTypeName(state.PrimaryType); err != nil {
			return err
		}
	}
	name, err := w.prepareNodeNameLocked(record.ID, record.Class, state.Name, record.ID)
	if err != nil {
		return err
	}
	leftPorts, err := w.preparePlaceholderClassPorts(record, state.LeftPorts, "left")
	if err != nil {
		return err
	}
	rightPorts, err := w.preparePlaceholderClassPorts(record, state.RightPorts, "right")
	if err != nil {
		return err
	}

	record.Root = state.Root
	record.PrimaryType = state.PrimaryType
	record.Name = name
	record.Label = state.Label

	w.removeOmittedPlaceholderClassPorts(record, leftPorts, record.LeftPorts)
	w.removeOmittedPlaceholderClassPorts(record, rightPorts, record.RightPorts)

	for i := range leftPorts {
		if leftPorts[i].ID == 0 {
			leftPorts[i].ID = w.NextIDLocked()
			leftPorts[i].Node = record.ID
			leftPorts[i].Links = []uint64{}
			w.ports.Set(leftPorts[i].ID, &leftPorts[i])
			w.enqueuePortNotification(NotificationPortAdded, leftPorts[i].ID, portSnapshot(&leftPorts[i]))
			continue
		}
		w.updatePlaceholderClassPort(leftPorts[i])
	}
	for i := range rightPorts {
		if rightPorts[i].ID == 0 {
			rightPorts[i].ID = w.NextIDLocked()
			rightPorts[i].Node = record.ID
			rightPorts[i].Links = []uint64{}
			w.ports.Set(rightPorts[i].ID, &rightPorts[i])
			w.enqueuePortNotification(NotificationPortAdded, rightPorts[i].ID, portSnapshot(&rightPorts[i]))
			continue
		}
		w.updatePlaceholderClassPort(rightPorts[i])
	}
	record.LeftPorts = portIDs(leftPorts)
	record.RightPorts = portIDs(rightPorts)
	return nil
}

func (w *Workspace) preparePlaceholderClassPorts(record *nodeRecord, ports []Port, direction string) ([]Port, error) {
	want := record.LeftPorts
	if direction == "right" {
		want = record.RightPorts
	}
	prepared := make([]Port, 0, len(ports))
	seenExisting := make(map[uint64]struct{}, len(ports))
	wantSet := make(map[uint64]struct{}, len(want))
	for _, id := range want {
		wantSet[id] = struct{}{}
	}
	for _, port := range ports {
		check := port.Copy()
		check.Node = record.ID
		check.Links = nil
		if check.Direction != direction {
			return nil, ErrPortOrder
		}
		if check.ID != 0 {
			if _, ok := seenExisting[check.ID]; ok {
				return nil, ErrPortOrder
			}
			seenExisting[check.ID] = struct{}{}
			if _, ok := wantSet[check.ID]; !ok {
				return nil, ErrPortOrder
			}
			existing, present := w.ports.Get(check.ID)
			if !present || existing == nil || existing.Node != record.ID || existing.Direction != direction {
				return nil, ErrPortOrder
			}
		}
		if err := check.Validate(); err != nil {
			return nil, err
		}
		prepared = append(prepared, check)
	}
	if err := validatePortNameListUnique(prepared); err != nil {
		return nil, err
	}
	return prepared, nil
}

func (w *Workspace) removeOmittedPlaceholderClassPorts(record *nodeRecord, desired []Port, existing []uint64) {
	kept := make(map[uint64]struct{}, len(desired))
	for _, port := range desired {
		if port.ID != 0 {
			kept[port.ID] = struct{}{}
		}
	}
	for _, id := range slices.Clone(existing) {
		if _, ok := kept[id]; !ok {
			w.undoRecordingDisabled += 1
			w.RemovePortLocked(id)
			w.undoRecordingDisabled -= 1
		}
	}
}

func (w *Workspace) updatePlaceholderClassPort(replacement Port) {
	port, _ := w.ports.Get(replacement.ID)
	links := port.CopyLinks()
	port.Direction = replacement.Direction
	port.Name = replacement.Name
	port.Types = replacement.CopyTypes()
	port.Links = links
	w.removeIncompatiblePlaceholderPortLinks(port)
	w.enqueuePortNotification(NotificationPortUpdated, port.ID, portSnapshot(port))
}

func (w *Workspace) removeIncompatiblePlaceholderPortLinks(port *Port) {
	if port == nil {
		return
	}
	for _, linkID := range port.CopyLinks() {
		link, present := w.links.Get(linkID)
		if !present || link == nil {
			continue
		}
		var peerID uint64
		switch port.ID {
		case link.LeftPort:
			peerID = link.RightPort
		case link.RightPort:
			peerID = link.LeftPort
		default:
			continue
		}
		peer, present := w.ports.Get(peerID)
		if !present || peer == nil {
			w.undoRecordingDisabled += 1
			w.RemoveLinkLocked(linkID)
			w.undoRecordingDisabled -= 1
			continue
		}
		if link.Type == AnyType && !portsSupportLinkType(port, peer, link.Type) {
			linkType := w.portsSharedType(port, peer)
			if linkType != "" {
				link.Type = linkType
				w.enqueueLinkNotification(NotificationLinkUpdated, link.ID, linkSnapshot(link))
				continue
			}
		}
		if !portsSupportLinkType(port, peer, link.Type) {
			w.undoRecordingDisabled += 1
			w.RemoveLinkLocked(linkID)
			w.undoRecordingDisabled -= 1
		}
	}
}

func portsSupportLinkType(portA, portB *Port, linkType string) bool {
	if linkType == AnyType {
		return slices.Contains(portA.Types, AnyType) || slices.Contains(portB.Types, AnyType)
	}
	portASupports := slices.Contains(portA.Types, linkType) || slices.Contains(portA.Types, AnyType)
	portBSupports := slices.Contains(portB.Types, linkType) || slices.Contains(portB.Types, AnyType)
	return portASupports && portBSupports
}

func portIDs(ports []Port) []uint64 {
	ids := make([]uint64, 0, len(ports))
	for _, port := range ports {
		ids = append(ids, port.ID)
	}
	return ids
}

func (w *Workspace) addPlaceholderPorts(record *nodeRecord, ports []Port) ([]uint64, error) {
	return w.addPlaceholderPortsWithIDs(record, ports, nil)
}

func (w *Workspace) addPlaceholderPortsWithIDs(record *nodeRecord, ports []Port, ids []uint64) ([]uint64, error) {
	if err := validatePlaceholderPorts(record.ID, ports); err != nil {
		return nil, err
	}
	if ids != nil && len(ids) != len(ports) {
		return nil, ErrPortOrder
	}
	if ids != nil && !uniqueUndoIDs(ids) {
		return nil, ErrNoPort
	}
	prepared := make([]Port, 0, len(ports))
	for i, port := range ports {
		port = port.Copy()
		if ids != nil {
			port.ID = ids[i]
			if !w.idAvailableLocked(port.ID) {
				return nil, ErrNoPort
			}
		} else {
			port.ID = 0
		}
		port.Node = record.ID
		port.Links = []uint64{}
		if err := w.validatePortNameAvailable(record, port.Direction, port.Name, 0); err != nil {
			return nil, err
		}
		prepared = append(prepared, port)
	}
	if err := validatePortNameListUnique(prepared); err != nil {
		return nil, err
	}

	added := make([]uint64, 0, len(prepared))
	for _, port := range prepared {
		if port.ID > 0 {
			if !w.reserveIDLocked(port.ID) {
				return nil, ErrNoPort
			}
		} else {
			port.ID = w.nextIDLocked()
		}
		w.ports.Set(port.ID, &port)
		if port.Direction == "left" {
			record.LeftPorts = append(record.LeftPorts, port.ID)
		} else {
			record.RightPorts = append(record.RightPorts, port.ID)
		}
		added = append(added, port.ID)
	}
	return added, nil
}

func (w *Workspace) addInitialPortsWithIDs(record *nodeRecord, ports []Port, ids []uint64) ([]uint64, error) {
	if err := validateDefaultPorts(ports); err != nil {
		return nil, err
	}
	if ids != nil && len(ids) != len(ports) {
		return nil, ErrPortOrder
	}
	if ids != nil && !uniqueUndoIDs(ids) {
		return nil, ErrNoPort
	}
	prepared := make([]Port, 0, len(ports))
	for i, port := range ports {
		port = port.Copy()
		if ids != nil {
			port.ID = ids[i]
			if !w.idAvailableLocked(port.ID) {
				return nil, ErrNoPort
			}
		} else {
			port.ID = 0
		}
		port.Node = record.ID
		port.Links = []uint64{}
		prepared = append(prepared, port)
	}

	added := make([]uint64, 0, len(prepared))
	for _, port := range prepared {
		if port.ID > 0 {
			if !w.reserveIDLocked(port.ID) {
				return nil, ErrNoPort
			}
		} else {
			port.ID = w.nextIDLocked()
		}
		w.ports.Set(port.ID, &port)
		if port.Direction == "left" {
			record.LeftPorts = append(record.LeftPorts, port.ID)
		} else {
			record.RightPorts = append(record.RightPorts, port.ID)
		}
		added = append(added, port.ID)
	}
	return added, nil
}

func validatePlaceholderPorts(node uint64, ports []Port) error {
	prepared := make([]Port, 0, len(ports))
	for _, port := range ports {
		port = port.Copy()
		port.ID = 0
		port.Node = node
		port.Links = nil
		if err := port.Validate(); err != nil {
			return err
		}
		prepared = append(prepared, port)
	}
	return validatePortNameListUnique(prepared)
}

func validateDefaultPorts(ports []Port) error {
	prepared := make([]Port, 0, len(ports))
	for _, port := range ports {
		port = port.Copy()
		port.ID = 0
		port.Node = 1
		port.Links = nil
		if err := port.Validate(); err != nil {
			return err
		}
		prepared = append(prepared, port)
	}
	return validatePortNameListUnique(prepared)
}

func (w *Workspace) validatePortNameAvailable(record *nodeRecord, direction, name string, exclude uint64) error {
	var ports []uint64
	switch direction {
	case "left":
		ports = record.LeftPorts
	case "right":
		ports = record.RightPorts
	default:
		return errors.Join(ErrPortDirection, errors.New(direction))
	}
	for _, id := range ports {
		if id == exclude {
			continue
		}
		port, present := w.ports.Get(id)
		if present && port != nil && port.Name == name {
			return ErrPortName
		}
	}
	return nil
}

func validatePortNameListUnique(ports []Port) error {
	left := make(map[string]struct{}, len(ports))
	right := make(map[string]struct{}, len(ports))
	for _, port := range ports {
		names := left
		if port.Direction == "right" {
			names = right
		}
		if _, ok := names[port.Name]; ok {
			return ErrPortName
		}
		names[port.Name] = struct{}{}
	}
	return nil
}

func (w *Workspace) linkIsPlaceholder(link *Link) bool {
	if link == nil {
		return false
	}
	left, leftPresent := w.nodes.Get(link.LeftPortNode)
	right, rightPresent := w.nodes.Get(link.RightPortNode)
	return !leftPresent || left == nil || left.Node == nil ||
		!rightPresent || right == nil || right.Node == nil
}

func (w *Workspace) refreshPlaceholderLinks(node uint64, notify bool) {
	for _, link := range w.linksForNode(node) {
		placeholder := w.linkIsPlaceholder(link)
		if link.Placeholder == placeholder {
			continue
		}
		link.Placeholder = placeholder
		if notify {
			w.enqueueLinkNotification(NotificationLinkUpdated, link.ID, linkSnapshot(link))
		}
	}
}

func (w *Workspace) deactivateNodeLinks(record *nodeRecord) {
	for _, link := range w.linksForNode(record.ID) {
		if link.Placeholder {
			continue
		}
		if link.LeftPortNode != record.ID {
			w.nodeEvLinkRemoved(link.LeftPortNode, link.ID, link.LeftPort, link.Type, "left")
		}
		if link.RightPortNode != record.ID {
			w.nodeEvLinkRemoved(link.RightPortNode, link.ID, link.RightPort, link.Type, "right")
		}
	}
}

func (w *Workspace) activatePlaceholderLinks(record *nodeRecord) map[uint64]error {
	failed := make(map[uint64]error)
	for _, link := range w.linksForNode(record.ID) {
		if !link.Placeholder || w.linkIsPlaceholder(link) {
			continue
		}
		leftNode, leftOK := w.nodes.Get(link.LeftPortNode)
		rightNode, rightOK := w.nodes.Get(link.RightPortNode)
		if !leftOK || !rightOK || leftNode == nil || rightNode == nil {
			continue
		}
		if err := leftNode.PreLinkAdd(link.LeftPort, link.Type, "left"); err != nil {
			if errors.Is(err, ErrNodePanic) {
				w.log.Debugf("node %d faled in PreLinkAdd", leftNode.ID)
				w.failNodeLocked(leftNode.ID, "PreLinkAdd", err, true, true)
			}
			failed[leftNode.ID] = err
			return failed
		}
		if err := rightNode.PreLinkAdd(link.RightPort, link.Type, "right"); err != nil {
			if errors.Is(err, ErrNodePanic) {
				w.log.Debugf("node %d faled in PreLinkAdd", rightNode.ID)
				w.failNodeLocked(rightNode.ID, "PreLinkAdd", err, true, true)
			}
			failed[rightNode.ID] = err
			return failed
		}
	}
	return nil
}

func (w *Workspace) notifyActivatedPlaceholderLinks(record *nodeRecord) map[uint64]error {
	failed := make(map[uint64]error)
	for _, link := range w.linksForNode(record.ID) {
		if link.Placeholder || w.linkIsPlaceholder(link) {
			continue
		}
		leftNode, leftOK := w.nodes.Get(link.LeftPortNode)
		rightNode, rightOK := w.nodes.Get(link.RightPortNode)
		if !leftOK || !rightOK || leftNode == nil || rightNode == nil {
			continue
		}
		if err := leftNode.OnLinkAdd(link.ID, link.LeftPort, link.Type, "left"); err != nil {
			w.log.Debugf("node %d faled in OnLinkAdd", leftNode.ID)
			w.failNodeLocked(leftNode.ID, "OnLinkAdd", err, true, true)
			failed[leftNode.ID] = err
			return failed
		}
		if err := rightNode.OnLinkAdd(link.ID, link.RightPort, link.Type, "right"); err != nil {
			w.log.Debugf("node %d faled in OnLinkAdd", rightNode.ID)
			w.failNodeLocked(rightNode.ID, "OnLinkAdd", err, true, true)
			w.nodeEvLinkRemoved(leftNode.ID, link.ID, link.LeftPort, link.Type, "left")
			failed[rightNode.ID] = err
			return failed
		}
	}
	return nil
}

func (w *Workspace) linksForNode(node uint64) []*Link {
	links := make([]*Link, 0)
	for pair := w.links.Oldest(); pair != nil; pair = pair.Next() {
		link := pair.Value
		if link == nil {
			continue
		}
		if link.LeftPortNode == node || link.RightPortNode == node {
			links = append(links, link)
		}
	}
	return links
}

func firstError(errs map[uint64]error) error {
	for _, err := range errs {
		return err
	}
	return nil
}

// postlock executes pending operations and notifications after an unlock.
func (w *Workspace) postlock() {
	w.mu.Lock()
	if w.postlocking {
		w.mu.Unlock()
		return
	}
	w.postlocking = true
	w.mu.Unlock()

	for {
		w.drainRegularPendingAndNotifications()

		if op, ok := w.popLowPriorityPendingOp(); ok {
			if op != nil {
				op()
			}
			continue
		}

		w.mu.Lock()
		done := len(w.pending) == 0 && len(w.notifications) == 0 && len(w.pendingLowPrior) == 0
		if done {
			w.postlocking = false
			w.mu.Unlock()
			return
		}
		w.mu.Unlock()
	}
}

// drainRegularPendingAndNotifications drains regular pending operations and
// notifications until no newly queued regular work remains. Low-priority work
// is intentionally left queued for postlock to run one item at a time.
func (w *Workspace) drainRegularPendingAndNotifications() {
	for {
		w.mu.Lock()
		ops := w.pending
		w.pending = make([]func(), 0)
		deliveries := w.drainNotificationDeliveries()
		w.mu.Unlock()

		runPendingOps(ops)
		deliverNotifications(deliveries)

		w.mu.Lock()
		done := len(w.pending) == 0 && len(w.notifications) == 0
		w.mu.Unlock()
		if done {
			return
		}
	}
}

// popLowPriorityPendingOp returns one low-priority operation only when regular
// pending operations and notifications are quiet. This preserves regular work
// priority even when another goroutine queues work while postlock is running.
func (w *Workspace) popLowPriorityPendingOp() (func(), bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.pending) > 0 || len(w.notifications) > 0 || len(w.pendingLowPrior) == 0 {
		return nil, false
	}

	op := w.pendingLowPrior[0]
	copy(w.pendingLowPrior, w.pendingLowPrior[1:])
	w.pendingLowPrior[len(w.pendingLowPrior)-1] = nil
	w.pendingLowPrior = w.pendingLowPrior[:len(w.pendingLowPrior)-1]
	return op, true
}

func runPendingOps(ops []func()) {
	for _, op := range ops {
		if op != nil {
			op()
		}
	}
}

func (w *Workspace) portsSharedType(portA, portB *Port) string {
	// Each port must always had at least one type
	portAAny := slices.Contains(portA.Types, AnyType)
	portBAny := slices.Contains(portB.Types, AnyType)
	if portAAny && portBAny {
		return AnyType
	}
	if portAAny {
		if len(portB.Types) == 1 {
			return portB.Types[0]
		}
		return AnyType
	}
	if portBAny {
		if len(portA.Types) == 1 {
			return portA.Types[0]
		}
		return AnyType
	}
	if len(portA.Types) > 1 {
		if len(portB.Types) > 1 {
			return ""
		}
		if slices.Contains(portA.Types, portB.Types[0]) {
			return portB.Types[0]
		}
	} else {
		if slices.Contains(portB.Types, portA.Types[0]) {
			return portA.Types[0]
		}
	}
	return ""
}

func (w *Workspace) portsConnected(portA, portB *Port) bool {
	return w.linkBetweenPorts(portA, portB) != nil
}

func (w *Workspace) linkBetweenPorts(portA, portB *Port) *Link {
	for lid := range multiSLiceIter(portA.Links, portB.Links) {
		link, present := w.links.Get(lid)
		if present && link != nil &&
			((link.LeftPort == portA.ID && link.RightPort == portB.ID) ||
				(link.LeftPort == portB.ID && link.RightPort == portA.ID)) {
			return link
		}
	}
	return nil
}

func (w *Workspace) linksBetweenNodes(na, nb uint64) []*Link {
	links := make([]*Link, 0)
	for pair := w.links.Oldest(); pair != nil; pair = pair.Next() {
		link := pair.Value
		if link == nil {
			continue
		}
		if (link.LeftPortNode == na && link.RightPortNode == nb) ||
			(link.LeftPortNode == nb && link.RightPortNode == na) {
			links = append(links, link)
		}
	}
	return links
}

func (w *Workspace) nodeEvPortRemoved(
	nodeID uint64,
	port uint64,
	direction string,
) {
	record, present := w.nodes.Get(nodeID)
	if present && record != nil {
		record.RemovePort(port)
		if err := record.OnPortRemoved(port, direction); err != nil {
			w.log.Debugf("node %d faled in OnPortRemoved", nodeID)
			w.failNodeLocked(nodeID, "OnPortRemoved", err, true, true)
		}
	}
}

func (w *Workspace) nodeEvLinkRemoved(
	nodeID uint64,
	link, port uint64,
	linkType, portDirection string,
) {
	record, present := w.nodes.Get(nodeID)
	if present && record != nil {
		if err := record.OnLinkRemoved(link, port, linkType, portDirection); err != nil {
			w.log.Debugf("node %d faled in OnLinkRemoved", nodeID)
			w.failNodeLocked(nodeID, "OnLinkRemoved", err, true, true)
		}
	}
}
