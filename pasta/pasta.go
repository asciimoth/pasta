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
	// ErrCycle reports that a link would create a cycle in the node graph.
	ErrCycle = errors.New("graph cycle")
	// ErrSameDirection reports that both ports have the same direction.
	ErrSameDirection = errors.New("same direction")
	// ErrTypeCompat reports that two ports do not share a usable link type.
	ErrTypeCompat = errors.New("types are incompatible")
)

// Workspace owns nodes, ports, and links and coordinates their lifecycle callbacks.
type Workspace struct {
	// mu should not be used directly. Use Lock and Unlock so post-lock hooks run.
	mu *badlock.BadLock

	nextid  uint64
	isReady bool

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
	return w.isReady
}

// Ready marks the workspace as ready and calls OnReady for existing nodes.
func (w *Workspace) Ready() {
	w.Lock()
	defer w.Unlock()

	if w.isReady {
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
	if w.pending == nil {
		w.pending = make([]func(), 0, 1)
	}
	w.pending = append(w.pending, op)
}

// RemoveNode removes a node and all of its ports and links.
func (w *Workspace) RemoveNode(id uint64) {
	if id < 1 {
		return
	}

	w.Lock()
	defer w.Unlock()

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
}

// AddNode adds node to the workspace and returns its workspace-scoped ID.
//
// class must be a valid class name. Nodes can configure their primary type
// from OnInit or later callbacks with SetNodePrimary.
func (w *Workspace) AddNode(node Node, class string) (uint64, error) {
	if err := ValidateClassName(class); err != nil {
		return 0, err
	}

	w.Lock()
	defer w.Unlock()

	// Reject if this node already exists
	for pair := w.nodes.Newest(); pair != nil; pair = pair.Prev() {
		if pair.Value.Node == node {
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
		LeftPorts:   []uint64{},
		RightPorts:  []uint64{},
		L:           log,
		stopped:     false,
	}
	w.nodes.Set(id, &rec)

	if err := rec.OnInit(w); err != nil {
		w.RemoveNode(id)
		return 0, err
	}

	if w.isReady {
		if err := rec.OnReady(); err != nil {
			w.RemoveNode(id)
			w.log.Debugf("node %d faled in OnReady", id)
			return 0, err
		}
	}

	w.log.Debug("node added", id)
	w.enqueueNodeNotification(NotificationNodeAdded, id, nodeSnapshot(&rec))

	return id, nil
}

// AddPort adds a port to its owner node and returns the new port ID.
func (w *Workspace) AddPort(port Port) (uint64, error) {
	port = port.Copy()
	port.Links = []uint64{}
	if err := port.Validate(); err != nil {
		return 0, err
	}

	w.Lock()
	defer w.Unlock()

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

	link := Link{
		ID:   w.NextID(),
		Type: linkType,

		LeftPort:     Left.ID,
		LeftPortNode: leftNode.ID,

		RightPort:     Right.ID,
		RightPortNode: rightNode.ID,
	}
	w.links.Set(link.ID, &link)

	if !w.verifyDAG() {
		w.links.Delete(link.ID)
		return 0, "", ErrCycle
	}

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

	Left.Links = append(Left.Links, link.ID)
	Right.Links = append(Right.Links, link.ID)

	w.log.Debug("link added", link.ID)
	w.enqueueLinkNotification(NotificationLinkAdded, link.ID, linkSnapshot(&link))
	w.enqueuePortNotification(NotificationPortUpdated, Left.ID, portSnapshot(Left))
	w.enqueuePortNotification(NotificationPortUpdated, Right.ID, portSnapshot(Right))
	return link.ID, link.Type, nil
}

// PortsConnected reports whether two ports are connected by a link.
func (w *Workspace) PortsConnected(pa, pb uint64) bool {
	w.Lock()
	defer w.Unlock()

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

	return len(w.linksBetweenNodes(na, nb)) > 0
}

// LinkByPorts returns the link connecting two ports.
func (w *Workspace) LinkByPorts(pa, pb uint64) (uint64, LinkSnapshot, bool) {
	w.Lock()
	defer w.Unlock()

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
	if typ != "" {
		if err := ValidateTypeName(typ); err != nil {
			return err
		}
	}

	w.Lock()
	defer w.Unlock()

	record, present := w.nodes.Get(id)
	if !present || record == nil {
		return ErrNoNode
	}
	record.PrimaryType = typ
	w.enqueueNodeNotification(NotificationNodeUpdated, id, nodeSnapshot(record))
	return nil
}

// SetPortName sets a port's display name.
func (w *Workspace) SetPortName(id uint64, name string) error {
	w.Lock()
	defer w.Unlock()

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
