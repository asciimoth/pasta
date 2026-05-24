package pasta

import (
	"errors"
	"slices"

	"github.com/asciimoth/badlock"
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

var (
	// ErrNodeDup reports that the same Node instance is already present in a workspace.
	ErrNodeDup = errors.New("node duplicate")
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
	mu *badlock.BadLock

	nextid  uint64
	isReady bool
	closed  bool

	pending []func()

	nodes *orderedmap.OrderedMap[uint64, *nodeRecord]
	ports *orderedmap.OrderedMap[uint64, *Port]
	links *orderedmap.OrderedMap[uint64, *Link]

	nextSubscriptionID uint64
	subscribers        map[uint64]NotificationCallback
	notifications      []WorkspaceNotification

	log  Logger
	logf LogFactory
}

// NewWorkspace creates a ready workspace using logf for workspace and node loggers.
func NewWorkspace(logf LogFactory) *Workspace {
	return &Workspace{
		mu: badlock.New(),

		nextid:  1,
		isReady: true,

		pending: make([]func(), 0),
		nodes:   orderedmap.New[uint64, *nodeRecord](),
		ports:   orderedmap.New[uint64, *Port](),
		links:   orderedmap.New[uint64, *Link](),

		nextSubscriptionID: 1,
		subscribers:        make(map[uint64]NotificationCallback),
		notifications:      make([]WorkspaceNotification, 0),

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
	if w.closed {
		return 0
	}
	if w.nextid < 1 {
		w.nextid = 1
	}
	id := w.nextid
	w.nextid += 1
	return id
}

// IsReady reports whether the workspace is ready to run node OnReady callbacks.
func (w *Workspace) IsReady() bool {
	w.Lock()
	defer w.Unlock()
	if w.closed {
		return false
	}
	return w.isReady
}

// Ready marks the workspace as ready and calls OnReady for existing nodes.
func (w *Workspace) Ready() {
	w.Lock()
	defer w.Unlock()

	if w.closed || w.isReady {
		return
	}

	w.isReady = true

	// Notify nodes.
	for pair := w.nodes.Newest(); pair != nil; pair = pair.Prev() {
		if err := pair.Value.OnReady(); err != nil {
			// Remove node on panic
			w.AddPendingOp(func() {
				w.RemoveNode(pair.Key)
			})
		}
	}
	w.recomputeRootPaths(true)
}

// Lock locks the workspace.
func (w *Workspace) Lock() {
	w.mu.Lock()
}

// Unlock unlocks the workspace and runs pending operations after the top-level unlock.
func (w *Workspace) Unlock() {
	r := w.mu.Unlock()
	if r > 0 {
		return
	}
	w.postlock()
}

// AddPendingOp queues op to run after current workspace operations finish.
//
// Pending operations run after the top-level Unlock. If the workspace is not
// currently inside another operation, op runs before AddPendingOp returns.
func (w *Workspace) AddPendingOp(op func()) {
	w.Lock()
	defer w.Unlock()
	if w.closed {
		return
	}
	if w.pending == nil {
		w.pending = make([]func(), 0, 1)
	}
	w.pending = append(w.pending, op)
}

// Close stops all nodes, notifies subscribers, drains pending operations, and
// prevents future workspace operations from mutating state.
func (w *Workspace) Close() {
	w.Lock()
	if w.closed {
		w.Unlock()
		return
	}
	w.closed = true

	for pair := w.nodes.Oldest(); pair != nil; pair = pair.Next() {
		if pair.Value == nil {
			continue
		}
		pair.Value.OnStop()
	}

	for len(w.pending) > 0 {
		ops := w.pending
		w.pending = make([]func(), 0)
		w.Unlock()
		for _, op := range ops {
			if op != nil {
				op()
			}
		}
		w.Lock()
	}

	w.enqueueNotification(WorkspaceNotification{Kind: NotificationWorkspaceStopped})
	deliveries := w.drainNotificationDeliveries()
	w.subscribers = make(map[uint64]NotificationCallback)
	w.Unlock()

	deliverNotifications(deliveries)
}

// RemoveNode removes a node and all of its ports and links.
func (w *Workspace) RemoveNode(id uint64) {
	if id < 1 {
		return
	}

	w.Lock()
	defer w.Unlock()
	if w.closed {
		return
	}

	var (
		record  *nodeRecord
		present bool
	)
	if record, present = w.nodes.Delete(id); !present || record == nil {
		return
	}
	removed := nodeSnapshot(record)
	record.OnStop()

	for port := range record.Ports() {
		w.RemovePort(port)
	}
	record.LeftPorts = nil
	record.RightPorts = nil

	w.log.Debug("removed node", id)
	w.enqueueNodeNotification(NotificationNodeRemoved, id, removed)
}

// RemovePort removes a port and all links attached to it.
func (w *Workspace) RemovePort(id uint64) {
	if id < 1 {
		return
	}

	w.Lock()
	defer w.Unlock()
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
	for _, link := range links {
		w.RemoveLink(link)
	}

	w.nodeEvPortRemoved(port.Node, id, port.Direction)

	w.log.Debug("removed port", id)
	w.enqueuePortNotification(NotificationPortRemoved, id, removed)
	if record, present := w.nodes.Get(port.Node); present && record != nil {
		w.enqueueNodeNotification(NotificationNodeUpdated, port.Node, nodeSnapshot(record))
	}
}

// RemoveLink removes a link and updates both endpoint ports and nodes.
func (w *Workspace) RemoveLink(id uint64) {
	if id < 1 {
		return
	}

	w.Lock()
	defer w.Unlock()
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

	w.log.Debug("removed link", id)
	w.recomputeRootPaths(true)
}

// AddNode adds node to the workspace and returns its workspace-scoped ID.
//
// class must be a valid class name. Nodes can configure their primary type
// from OnInit or later callbacks with SetNodePrimary.
func (w *Workspace) AddNode(node Node, class string) (uint64, error) {
	return w.AddNodeWithRoot(node, class, false)
}

// AddRootNode adds node to the workspace as a root node.
func (w *Workspace) AddRootNode(node Node, class string) (uint64, error) {
	return w.AddNodeWithRoot(node, class, true)
}

// AddNodeWithRoot adds node to the workspace and sets its initial root status.
func (w *Workspace) AddNodeWithRoot(node Node, class string, root bool) (uint64, error) {
	w.Lock()
	defer w.Unlock()
	if w.closed {
		return 0, ErrWorkspaceClosed
	}

	if err := ValidateClassName(class); err != nil {
		return 0, err
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

	id := w.NextID()
	log := w.logf.NodeLogger(id, class)
	rec := nodeRecord{
		ID:          id,
		Node:        node,
		Class:       class,
		PrimaryType: "",
		Root:        root,
		LeftPorts:   []uint64{},
		RightPorts:  []uint64{},
		L:           log,
		stopped:     false,
	}
	w.nodes.Set(id, &rec)

	if err := rec.OnInit(w, nil); err != nil {
		w.RemoveNode(id)
		return 0, err
	}

	if w.isReady {
		if err := rec.OnReady(); err != nil {
			w.RemoveNode(id)
			w.log.Debugf("node %d faled in OnReady", id)
			return 0, err
		}
		if failed := w.recomputeRootPaths(false); len(failed) > 0 {
			err := failed[id]
			if err == nil {
				err = ErrNodePanic
			}
			w.RemoveNode(id)
			w.log.Debugf("node %d faled in OnRootStatus", id)
			return 0, err
		}
	}

	w.log.Debug("node added", id)
	w.enqueueNodeNotification(NotificationNodeAdded, id, nodeSnapshot(&rec))

	return id, nil
}

// AddPlaceholderNode adds a placeholder node with predefined ports.
func (w *Workspace) AddPlaceholderNode(class string, ports []Port) (uint64, error) {
	return w.AddPlaceholderNodeWithRoot(class, false, ports)
}

// AddPlaceholderNodeWithRoot adds a placeholder node with predefined ports and
// an initial root flag. Placeholder root state is stored but ignored by
// snapshots and root-path tracing until the node is replaced by a normal node.
func (w *Workspace) AddPlaceholderNodeWithRoot(class string, root bool, ports []Port) (uint64, error) {
	w.Lock()
	defer w.Unlock()
	if w.closed {
		return 0, ErrWorkspaceClosed
	}
	if err := ValidateClassName(class); err != nil {
		return 0, err
	}

	id := w.NextID()
	rec := nodeRecord{
		ID:          id,
		Node:        nil,
		Class:       class,
		PrimaryType: "",
		Root:        root,
		LeftPorts:   []uint64{},
		RightPorts:  []uint64{},
		L:           w.logf.NodeLogger(id, class),
	}
	added, err := w.addPlaceholderPorts(&rec, ports)
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
	return id, nil
}

// ReplaceNode replaces the Node implementation in an existing node record.
//
// The workspace keeps the node ID, class, primary type, ports, root state, and
// current root-path status. A successful replacement does not enqueue workspace
// notifications because the snapshot-observable node state is unchanged.
func (w *Workspace) ReplaceNode(id uint64, node Node) error {
	w.Lock()
	defer w.Unlock()
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

	wasPlaceholder := record.Node == nil
	old := record.Node
	restored := record.InitData()
	nodeStop(old)

	record.Node = node
	record.stopped = false
	if err := record.OnInit(w, &restored); err != nil {
		if wasPlaceholder {
			record.Node = nil
			record.stopped = false
		} else {
			w.RemoveNode(id)
		}
		return err
	}
	if w.isReady {
		if err := record.OnReady(); err != nil {
			if wasPlaceholder {
				nodeStop(record.Node)
				record.Node = nil
				record.stopped = false
			} else {
				w.RemoveNode(id)
			}
			w.log.Debugf("node %d faled in OnReady", id)
			return err
		}
		if wasPlaceholder {
			if failed := w.activatePlaceholderLinks(record); len(failed) > 0 {
				nodeStop(record.Node)
				record.Node = nil
				record.stopped = false
				return firstError(failed)
			}
			w.refreshPlaceholderLinks(id, false)
			if !w.verifyDAG() {
				w.deactivateNodeLinks(record)
				nodeStop(record.Node)
				record.Node = nil
				record.stopped = false
				w.refreshPlaceholderLinks(id, true)
				w.recomputeRootPaths(true)
				return ErrCycle
			}
			if failed := w.notifyActivatedPlaceholderLinks(record); len(failed) > 0 {
				w.deactivateNodeLinks(record)
				nodeStop(record.Node)
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
				record.Node = nil
				record.stopped = false
				return firstError(failed)
			}
		} else {
			if err := record.OnRootStatus(record.HasRootPath); err != nil {
				w.RemoveNode(id)
				w.log.Debugf("node %d faled in OnRootStatus", id)
				return err
			}
		}
	}
	if wasPlaceholder {
		w.refreshPlaceholderLinks(id, true)
		w.recomputeRootPaths(true)
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

	old := record.Node
	if old != nil {
		nodeStop(old)
	}
	record.Node = nil
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
	return w.ReplaceNode(id, node)
}

// AddPort adds a port to its owner node and returns the new port ID.
func (w *Workspace) AddPort(port Port) (uint64, error) {
	port = port.Copy()
	port.Links = []uint64{}

	w.Lock()
	defer w.Unlock()
	if w.closed {
		return 0, ErrWorkspaceClosed
	}

	if err := port.Validate(); err != nil {
		return 0, err
	}

	// Make sure node exists
	record, present := w.nodes.Get(port.Node)
	if !present || record == nil {
		return 0, ErrNoNode
	}

	id := w.NextID()
	port.ID = id
	w.ports.Set(id, &port)

	if err := record.OnPortAdd(port.ID, port.Direction, port.CopyTypes()); err != nil {
		w.log.Debugf("node %d faled in OnReady", record.ID)
		w.ports.Delete(port.ID)
		w.AddPendingOp(func() {
			w.RemoveNode(record.ID)
		})
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
// the link or panics during PreLinkAdd, no link is added. If a node panics or
// returns an error during OnLinkAdd, that node is scheduled for removal.
func (w *Workspace) AddLink(pa, pb uint64) (uint64, string, error) {
	w.Lock()
	defer w.Unlock()
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
				w.AddPendingOp(func() {
					w.RemoveNode(leftNode.ID)
				})
			}
			return 0, "", rejection
		}
		rejection = rightNode.PreLinkAdd(Right.ID, linkType, Right.Direction)
		if rejection != nil {
			if errors.Is(rejection, ErrNodePanic) {
				w.log.Debugf("node %d faled in PreLinkAdd", rightNode.ID)
				w.AddPendingOp(func() {
					w.RemoveNode(rightNode.ID)
				})
			}
			return 0, "", rejection
		}
	}

	link := Link{
		ID:   w.NextID(),
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
		return 0, "", ErrCycle
	}

	if !link.Placeholder {
		err := leftNode.OnLinkAdd(link.ID, Left.ID, link.Type, Left.Direction)
		if err != nil {
			w.links.Delete(link.ID)
			w.log.Debugf("node %d faled in OnLinkAdd", leftNode.ID)
			w.AddPendingOp(func() {
				w.RemoveNode(leftNode.ID)
			})
			return 0, "", err
		}
		err = rightNode.OnLinkAdd(link.ID, Right.ID, link.Type, Right.Direction)
		if err != nil {
			w.links.Delete(link.ID)
			w.log.Debugf("node %d faled in OnLinkAdd", rightNode.ID)
			w.AddPendingOp(func() {
				w.RemoveNode(rightNode.ID)
			})
			w.nodeEvLinkRemoved(leftNode.ID, link.ID, Left.ID, link.Type, Left.Direction)
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
	return link.ID, link.Type, nil
}

// PortsConnected reports whether two ports are connected by a link.
func (w *Workspace) PortsConnected(pa, pb uint64) bool {
	w.Lock()
	defer w.Unlock()
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
	if w.closed {
		return false
	}

	return len(w.linksBetweenNodes(na, nb)) > 0
}

// LinkByPorts returns the link connecting two ports.
func (w *Workspace) LinkByPorts(pa, pb uint64) (uint64, LinkSnapshot, bool) {
	w.Lock()
	defer w.Unlock()
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

// LinksByNodes returns snapshots of all direct links between two nodes.
func (w *Workspace) LinksByNodes(na, nb uint64) map[uint64]LinkSnapshot {
	w.Lock()
	defer w.Unlock()
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

// RemoveLinksByNodes removes every direct link between two nodes.
func (w *Workspace) RemoveLinksByNodes(na, nb uint64) {
	w.Lock()
	defer w.Unlock()
	if w.closed {
		return
	}

	links := w.linksBetweenNodes(na, nb)
	for _, link := range links {
		w.RemoveLink(link.ID)
	}
}

// SetNodePrimary sets a node's primary type.
//
// typ may be empty to clear the primary type. Non-empty values must be valid
// type names.
func (w *Workspace) SetNodePrimary(id uint64, typ string) error {
	w.Lock()
	defer w.Unlock()
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

// SetNodeRoot changes whether a node is an explicit workspace root.
func (w *Workspace) SetNodeRoot(id uint64, root bool) error {
	w.Lock()
	defer w.Unlock()
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
	if w.closed {
		return ErrWorkspaceClosed
	}

	port, present := w.ports.Get(id)
	if !present || port == nil {
		return ErrNoPort
	}
	port.Name = name
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
// OnRootStatus callback failed and were scheduled for removal.
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
			nodeID := record.ID
			w.AddPendingOp(func() {
				w.RemoveNode(nodeID)
			})
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

func (w *Workspace) addPlaceholderPorts(record *nodeRecord, ports []Port) ([]uint64, error) {
	if err := validatePlaceholderPorts(record.ID, ports); err != nil {
		return nil, err
	}
	prepared := make([]Port, 0, len(ports))
	for _, port := range ports {
		port = port.Copy()
		port.ID = 0
		port.Node = record.ID
		port.Links = []uint64{}
		prepared = append(prepared, port)
	}

	added := make([]uint64, 0, len(prepared))
	for _, port := range prepared {
		port.ID = w.NextID()
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
	for _, port := range ports {
		port = port.Copy()
		port.ID = 0
		port.Node = node
		port.Links = nil
		if err := port.Validate(); err != nil {
			return err
		}
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
				nodeID := leftNode.ID
				if nodeID != record.ID {
					w.AddPendingOp(func() {
						w.RemoveNode(nodeID)
					})
				}
			}
			failed[leftNode.ID] = err
			return failed
		}
		if err := rightNode.PreLinkAdd(link.RightPort, link.Type, "right"); err != nil {
			if errors.Is(err, ErrNodePanic) {
				nodeID := rightNode.ID
				if nodeID != record.ID {
					w.AddPendingOp(func() {
						w.RemoveNode(nodeID)
					})
				}
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
			if errors.Is(err, ErrNodePanic) {
				nodeID := leftNode.ID
				if nodeID != record.ID {
					w.AddPendingOp(func() {
						w.RemoveNode(nodeID)
					})
				}
			}
			failed[leftNode.ID] = err
			return failed
		}
		if err := rightNode.OnLinkAdd(link.ID, link.RightPort, link.Type, "right"); err != nil {
			if errors.Is(err, ErrNodePanic) {
				nodeID := rightNode.ID
				if nodeID != record.ID {
					w.AddPendingOp(func() {
						w.RemoveNode(nodeID)
					})
				}
			}
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

// postlock executes after top-level unlock (one with recursion == 0).
func (w *Workspace) postlock() {
	// It is only place where we should call mu.Lock/mu.Unlock directly instead
	// of w.Lock/w.Unlock.
	w.mu.Lock()
	defer w.mu.Unlock()

	// Call pending operations
	for len(w.pending) > 0 {
		ops := w.pending
		w.pending = make([]func(), 0)
		for _, op := range ops {
			op()
		}
	}

	deliveries := w.drainNotificationDeliveries()
	deliverNotifications(deliveries)
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
			w.AddPendingOp(func() {
				w.RemoveNode(nodeID)
			})
		}
	}
}

// func (w *Workspace) nodeEvPortAdd(
// 	nodeID uint64,
// 	port uint64,
// 	direction string,
// 	types []string,
// ) {
// 	record, present := w.nodes.Get(nodeID)
// 	if present && record != nil {
// 		if err := record.OnPortAdd(port, direction, types); err != nil {
// 			w.log.Debugf("node %d faled in OnPortAdd", nodeID)
// 			w.AddPendingOp(func() {
// 				w.RemoveNode(nodeID)
// 			})
// 		}
// 	}
// }

func (w *Workspace) nodeEvLinkRemoved(
	nodeID uint64,
	link, port uint64,
	linkType, portDirection string,
) {
	record, present := w.nodes.Get(nodeID)
	if present && record != nil {
		if err := record.OnLinkRemoved(link, port, linkType, portDirection); err != nil {
			w.log.Debugf("node %d faled in OnLinkRemoved", nodeID)
			w.AddPendingOp(func() {
				w.RemoveNode(nodeID)
			})
		}
	}
}
