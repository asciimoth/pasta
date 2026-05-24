package pasta

import (
	"errors"
	"iter"
	"slices"
)

var (
	// ErrNodePanic reports that a node callback panicked.
	ErrNodePanic = errors.New("node panic")
	// ErrNoPort reports that a port ID does not exist in the workspace.
	ErrNoPort = errors.New("port not found")
)

// Node receives lifecycle and graph mutation callbacks from a workspace.
//
// If a node callback returns an error or panics, the workspace may remove the
// node to keep the graph consistent.
type Node interface {
	// OnInit is called when the node is added to a workspace.
	//
	// Implementations can use this callback to keep the workspace ID, create
	// initial ports, or configure node-local state.
	OnInit(
		w *Workspace,
		l Logger,
		id uint64,
		class string,
		// TODO: when restoring from config, pass restored
	) error

	// OnReady is called once the workspace is ready to run.
	//
	// Nodes added to an already-ready workspace receive OnReady immediately.
	OnReady() error

	// OnStop is called once when the node is removed or the workspace closes.
	OnStop()

	// OnPortAdd is called after a port is added to this node.
	OnPortAdd(
		port uint64,
		direction string,
		types []string,
	) error

	// OnPortRemoved is called after a port is removed from this node.
	OnPortRemoved(
		port uint64,
		direction string,
	) error

	// PreLinkAdd is called before a link is added to one of this node's ports.
	//
	// Returning a non-nil error rejects the link.
	PreLinkAdd(
		port uint64,
		linkType, portDirection string,
	) (rejection error)

	// OnLinkAdd is called after a link is added to one of this node's ports.
	OnLinkAdd(
		link, port uint64,
		linkType, portDirection string,
	) error

	// OnLinkRemoved is called after a link is removed from one of this node's ports.
	OnLinkRemoved(
		link, port uint64,
		linkType, portDirection string,
	) error

	// OnEvent is called when another node sends an event through an existing link.
	OnEvent(
		event Event,
		linkType string,
		receiverPortTypes []string,
		receiverPortDirection string,
	) error
}

type nodeRecord struct {
	ID    uint64 // must be Workspace unique
	Node  Node
	Class string // node class name

	PrimaryType string

	LeftPorts  []uint64
	RightPorts []uint64

	L Logger

	stopped bool
}

func (n *nodeRecord) RemovePort(id uint64) {
	f := func(e uint64) bool {
		return e == id
	}
	if len(n.LeftPorts) > 0 {
		n.LeftPorts = slices.DeleteFunc(n.LeftPorts, f)
	}
	if len(n.RightPorts) > 0 {
		n.RightPorts = slices.DeleteFunc(n.RightPorts, f)
	}
}

func (n *nodeRecord) Ports() iter.Seq[uint64] {
	return func(yield func(uint64) bool) {
		for _, p := range n.LeftPorts {
			if !yield(p) {
				return
			}
		}
		for _, p := range n.RightPorts {
			if !yield(p) {
				return
			}
		}
	}
}

func (n *nodeRecord) OnInit(
	w *Workspace,
) (err error) {
	if n.stopped {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			n.stopped = true
			err = ErrNodePanic
		}
	}()
	err = n.Node.OnInit(w, n.L, n.ID, n.Class)
	return
}

func (n *nodeRecord) PreLinkAdd(
	port uint64,
	linkType, portDirection string,
) (rejection error) {
	if n.stopped {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			n.stopped = true
			rejection = ErrNodePanic
		}
	}()
	rejection = n.Node.PreLinkAdd(port, linkType, portDirection)
	return
}

func (n *nodeRecord) OnLinkAdd(
	link, port uint64,
	linkType, portDirection string,
) (err error) {
	if n.stopped {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			n.stopped = true
			err = ErrNodePanic
		}
	}()
	err = n.Node.OnLinkAdd(link, port, linkType, portDirection)
	return
}

func (n *nodeRecord) OnPortAdd(
	port uint64,
	direction string,
	types []string,
) (err error) {
	if n.stopped {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			n.stopped = true
			err = ErrNodePanic
		}
	}()
	err = n.Node.OnPortAdd(port, direction, types)
	return
}

func (n *nodeRecord) OnPortRemoved(
	port uint64,
	direction string,
) (err error) {
	if n.stopped {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			n.stopped = true
			err = ErrNodePanic
		}
	}()
	err = n.Node.OnPortRemoved(port, direction)
	return
}

func (n *nodeRecord) OnLinkRemoved(
	link, port uint64,
	linkType, portDirection string,
) (err error) {
	if n.stopped {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			n.stopped = true
			err = ErrNodePanic
		}
	}()
	err = n.Node.OnLinkRemoved(link, port, linkType, portDirection)
	return
}

func (n *nodeRecord) OnEvent(
	event Event,
	linkType string,
	receiverPortTypes []string,
	receiverPortDirection string,
) (err error) {
	if n.stopped {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			n.stopped = true
			err = ErrNodePanic
		}
	}()
	err = n.Node.OnEvent(event, linkType, receiverPortTypes, receiverPortDirection)
	return
}

func (n *nodeRecord) OnReady() (err error) {
	if n.stopped {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			n.stopped = true
			err = ErrNodePanic
		}
	}()
	err = n.Node.OnReady()
	return
}

func (n *nodeRecord) OnStop() {
	if n.stopped {
		return
	}
	n.stopped = true
	nodeStop(n.Node)
}

func nodeStop(n Node) {
	defer func() {
		if r := recover(); r != nil { //nolint
			// Ignore panics on stop
		}
	}()
	n.OnStop()
}
