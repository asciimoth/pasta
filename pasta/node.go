package pasta

import (
	"errors"
	"iter"
	"slices"
)

var (
	ErrNodePanic = errors.New("node panic")
	ErrNoPort    = errors.New("port not found")
)

// If any node methods returns error or panics, this node should be removed
// from workspace.
type Node interface {
	// Should be called when node is added to Workspace
	// It is a place it define ports / promary types / etc.
	OnInit(
		w *Workspace,
		l Logger,
		id uint64,
		class string,
		// TODO: when restoring from config, pass restored
	) error

	// Should be called once when Workspace in ready to work.
	// E.g. after restoring from config.
	// If node is added to already ready Workspace, OnReady will be called immediately.
	OnReady() error

	// Should be called once when node is removed or Workspace is closed.
	// It is a place for resource cleanup.
	OnStop()

	OnPortAdd(
		port uint64,
		direction string,
		types []string,
	) error

	OnPortRemoved(
		port uint64,
		direction string,
	) error

	PreLinkAdd(
		port uint64,
		linkType, portDirection string,
	) (rejection error)

	OnLinkAdd(
		link, port uint64,
		linkType, portDirection string,
	) error

	OnLinkRemoved(
		link, port uint64,
		linkType, portDirection string,
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
		_ = slices.DeleteFunc(n.LeftPorts, f)
	}
	if len(n.RightPorts) > 0 {
		_ = slices.DeleteFunc(n.RightPorts, f)
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
