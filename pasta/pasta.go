package pasta

import (
	"errors"
	"slices"

	"github.com/asciimoth/badlock"
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

var (
	ErrNodeDup = errors.New("node duplicate")
	ErrLinkDup = errors.New("link duplicate")

	ErrNoNode        = errors.New("node do not exists")
	ErrCycle         = errors.New("graph cycle")
	ErrSameDirection = errors.New("same direction")
	ErrTypeCompat    = errors.New("types are incompatible")
)

type Workspace struct {
	// mu should NOT be used directly. Instead use w.Lock() and w.Unlock() methods
	// because they execute some post-lock hooks.
	mu *badlock.BadLock

	nextid  uint64
	isReady bool

	pending []func()

	nodes *orderedmap.OrderedMap[uint64, *nodeRecord]
	ports *orderedmap.OrderedMap[uint64, *Port]
	links *orderedmap.OrderedMap[uint64, *Link]

	log  Logger
	logf LogFactory
}

func NewWorkspace(logf LogFactory) *Workspace {
	return &Workspace{
		mu: badlock.New(),

		nextid:  1,
		isReady: true,

		pending: make([]func(), 0),
		nodes:   orderedmap.New[uint64, *nodeRecord](),
		ports:   orderedmap.New[uint64, *Port](),
		links:   orderedmap.New[uint64, *Link](),

		log:  logf.WorkspaceLogger(),
		logf: logf,
	}
}

// ID is newer 0
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

func (w *Workspace) IsReady() bool {
	w.Lock()
	defer w.Unlock()
	return w.isReady
}

func (w *Workspace) Ready() {
	w.Lock()
	defer w.Unlock()

	if w.isReady {
		return
	}

	w.isReady = true

	//Notify nodes
	for pair := w.nodes.Newest(); pair != nil; pair = pair.Prev() {
		if err := pair.Value.OnReady(); err != nil {
			// Remove node on panic
			w.AddPendingOp(func() {
				w.RemoveNode(pair.Key)
			})
		}
	}
}

// Lock is safe for concurrent calls.
func (w *Workspace) Lock() {
	w.mu.Lock()
}

// Unlock unlocks internal recursive mutex. After top-level Unlock
func (w *Workspace) Unlock() {
	r := w.mu.Unlock()
	if r > 0 {
		return
	}
	w.postlock()
}

// AddPendingOp adds pending operation that will be executed after current
// in-fly operations wit Workspace (after top-level unlock). If there is no
// other in-fly operation then AddPendingOpimmediately, it will executed immediately.
func (w *Workspace) AddPendingOp(op func()) {
	w.Lock()
	defer w.Unlock()
	if w.pending == nil {
		w.pending = []func(){op}
	}
	w.pending = append(w.pending, op)
}

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
	record.OnStop()

	for port := range record.Ports() {
		w.RemovePort(port)
	}
	record.LeftPorts = nil
	record.RightPorts = nil

	w.log.Debug("removed node", id)
	// TODO: Send notifocation
}

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

	links := port.Links
	port.Links = make([]uint64, 0)
	for _, link := range links {
		w.RemoveLink(link)
	}

	w.nodeEvPortRemoved(port.Node, id, port.Direction)

	w.log.Debug("removed port", id)
	// TODO: Send notification
}

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

	port, present := w.ports.Get(link.LeftPort)
	if present {
		port.RemoveLink(id)
	}
	port, present = w.ports.Get(link.RightPort)
	if present {
		port.RemoveLink(id)
	}

	w.nodeEvLinkRemoved(link.LeftPortNode, id, link.LeftPort, link.Type, "left")
	w.nodeEvLinkRemoved(link.RightPortNode, id, link.RightPort, link.Type, "right")

	w.log.Debug("removed link", id)
	// TODO: Send notifocation
}

func (w *Workspace) AddNode(node Node, class, ptype string) (uint64, error) {
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

	if err := rec.OnInit(w); err != nil {
		return 0, err
	}

	w.nodes.Set(id, &rec)

	if w.isReady {
		if err := rec.OnReady(); err != nil {
			w.RemoveNode(id)
			w.log.Debugf("node %d faled in OnReady", id)
			return 0, err
		}
	}

	w.log.Debug("node added", id)
	// TODO: Send notification

	return id, nil
}

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
	if !present && record == nil {
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

	w.log.Debug("port added", id)
	// TODO: Send notification

	return id, nil
}

// Both ports must exists.
// One of ports must be left type and one must be right type.
// There must be link types supported by both ports.
// At least one of ports must have exactly one supported type which will be used
// as link's one.
// Ports must be owned by different nodes.
// There should not be already existed link for this two ports.
// Both nodes must accepted new link.
// Nodes graph must stay DAG.
// If any of nodes rejects link or panic on PreLinkAdd, link will not be added.
// If any of nodes will panic on OnLinkAdd, node will be removed back.
func (w *Workspace) AddLink(pa, pb uint64) (uint64, string, error) {
	w.Lock()
	defer w.Unlock()

	portA, present := w.ports.Get(pa)
	if !present || portA == nil {
		return 0, "", ErrNoPort
	}

	portB, present := w.ports.Get(pa)
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
	rightNode, present := w.nodes.Get(Left.Node)
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

	w.log.Debug("link added", link.ID)
	// TODO: Send notification
	return 0, link.Type, nil
}

func (w *Workspace) PortsConnected(pa, pb uint64) bool {
	w.Lock()
	defer w.Unlock()

	portA, present := w.ports.Get(pa)
	if !present || portA == nil {
		return false
	}

	portB, present := w.ports.Get(pa)
	if !present || portB == nil {
		return false
	}

	return w.portsConnected(portA, portB)
}

// Any link creation should be tested with verifyDAG() and rolled back if
// false reported
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
	defer w.mu.Unlock() //nolint

	// Call pending operations
	for len(w.pending) > 0 {
		for _, op := range w.pending {
			op()
		}
	}
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
	for lid := range multiSLiceIter(portA.Links, portB.Links) {
		link, present := w.links.Get(lid)
		if present && link != nil &&
			((link.LeftPort == portA.ID && link.RightPort == portB.ID) ||
				(link.LeftPort == portB.ID && link.RightPort == portA.ID)) {
			return true
		}
	}
	return false
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
