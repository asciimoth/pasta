package pasta

import (
	"errors"
	"iter"
	"slices"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
)

var (
	// ErrNodePanic reports that a node callback panicked.
	ErrNodePanic = errors.New("node panic")
	// ErrNoPort reports that a port ID does not exist in the workspace.
	ErrNoPort = errors.New("port not found")
	// ErrNodePopupType reports that a node popup type is unsupported.
	ErrNodePopupType = errors.New("node popup type")
	// ErrBasicNodeLinkRejected reports that BasicNode rejected a link by default.
	ErrBasicNodeLinkRejected = errors.New("basic node link rejected")
)

const (
	// NodePopupInfo marks an informational node popup.
	NodePopupInfo = "info"
	// NodePopupWard marks a warning node popup.
	NodePopupWard = "ward"
	// NodePopupErr marks an error node popup.
	NodePopupErr = "err"
)

// NodePopup is a short user-facing note attached to a node.
//
// Popups are intended for node implementations to report user-actionable
// conditions, such as incorrect node configuration. They are observable
// workspace state, are included in snapshots, and usually duplicate details
// written to logs for operators or tests.
type NodePopup struct {
	// ID is workspace-scoped and can be used to remove one popup later.
	ID uint64 `json:"id"`
	// Type is one of NodePopupInfo, NodePopupWard, or NodePopupErr.
	Type string `json:"type"`
	// Text is user-facing and intentionally not interpreted by the workspace.
	Text string `json:"text"`
}

// ValidateNodePopupType reports whether typ is a supported node popup type.
func ValidateNodePopupType(typ string) error {
	switch typ {
	case NodePopupInfo, NodePopupWard, NodePopupErr:
		return nil
	default:
		return errors.Join(ErrNodePopupType, errors.New(typ))
	}
}

// Node receives lifecycle and graph mutation callbacks from a workspace.
//
// If a node callback returns an error or panics, the workspace may remove the
// node to keep the graph consistent.
type Node interface {
	// OnInit is called when the node is added to a workspace.
	//
	// Implementations can use this callback to keep the workspace ID, create
	// initial ports, or configure node-local state.
	//
	// isReplacement is true when OnInit is running for a node implementation
	// that replaced an existing node record. isPlaceholderReplacement is true
	// when the replaced record was a placeholder. isClassConstructed is true
	// when the node was created by a NodeClassFactory. isRestored is true when
	// the node is being constructed by WorkspaceFromConfig.
	OnInit(
		w *Workspace,
		l Logger,
		id uint64,
		class string,
		restored *NodeInitData,
		isReplacement bool,
		isPlaceholderReplacement bool,
		isClassConstructed bool,
		isRestored bool,
	) error

	// OnReady is called once the workspace is ready to run.
	//
	// Nodes added to an already-ready workspace receive OnReady immediately.
	OnReady() error

	// OnRootStatus is called after OnReady with the node's current path-to-root
	// status, and again whenever that status changes.
	OnRootStatus(hasRootPath bool) error

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
	// Returning a non-nil error rejects the link. Rejections are ordinary
	// validation decisions, not node failures. A panic is still treated as a
	// node failure by the workspace.
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

	// OnInbox is called when a message is delivered directly to this node.
	OnInbox(message InboxMessage) error

	// OnFormularMsg is called when external code sends a Formular
	// frontend-to-backend message to this node's menu.
	OnFormularMsg(message any) error

	// OnSave is called when the workspace is explicitly saved to a Config.
	//
	// The Config is rooted at this node's object. Workspace-owned keys use
	// CamelCase names, so node implementations should prefer lower-case keys
	// for their own state.
	OnSave(cfg configer.Config) error
}

// BasicNode is a minimal Node implementation intended for embedding.
//
// All callbacks are no-ops except PreLinkAdd, which rejects every link by
// default. Third-party node implementations can embed BasicNode and override
// only the callbacks they need.
type BasicNode struct{}

var _ Node = BasicNode{}

func (BasicNode) OnInit(
	w *Workspace,
	l Logger,
	id uint64,
	class string,
	restored *NodeInitData,
	isReplacement bool,
	isPlaceholderReplacement bool,
	isClassConstructed bool,
	isRestored bool,
) error {
	return nil
}

func (BasicNode) OnReady() error {
	return nil
}

func (BasicNode) OnRootStatus(hasRootPath bool) error {
	return nil
}

func (BasicNode) OnStop() {}

func (BasicNode) OnPortAdd(
	port uint64,
	direction string,
	types []string,
) error {
	return nil
}

func (BasicNode) OnPortRemoved(
	port uint64,
	direction string,
) error {
	return nil
}

func (BasicNode) PreLinkAdd(
	port uint64,
	linkType, portDirection string,
) (rejection error) {
	return ErrBasicNodeLinkRejected
}

func (BasicNode) OnLinkAdd(
	link, port uint64,
	linkType, portDirection string,
) error {
	return nil
}

func (BasicNode) OnLinkRemoved(
	link, port uint64,
	linkType, portDirection string,
) error {
	return nil
}

func (BasicNode) OnEvent(
	event Event,
	linkType string,
	receiverPortTypes []string,
	receiverPortDirection string,
) error {
	return nil
}

func (BasicNode) OnInbox(message InboxMessage) error {
	return nil
}

func (BasicNode) OnFormularMsg(message any) error {
	return nil
}

func (BasicNode) OnSave(cfg configer.Config) error {
	return nil
}

// NodeInitData carries existing node-owned workspace state into OnInit.
//
// It is nil for ordinary node additions. AddNodeByClass passes class default
// primary type and initial port IDs. Workspace operations that preserve an
// existing node record, such as node replacement, pass a snapshot of the state
// the new Node implementation inherits.
type NodeInitData struct {
	// PrimaryType, Name, and Label are the workspace-owned values inherited by
	// the node before OnInit runs.
	PrimaryType string
	Name        string
	Label       string
	// LeftPorts and RightPorts are copies of the current ordered port IDs.
	LeftPorts  []uint64
	RightPorts []uint64
}

type nodeRecord struct {
	ID    uint64 // must be Workspace unique
	Node  Node
	Class string // node class name
	Name  string // must be Workspace unique

	PrimaryType string
	Label       string
	Position    string
	Popups      []NodePopup
	Root        bool
	HasRootPath bool
	Menu        *formular.MenuSnapshotState

	LeftPorts  []uint64
	RightPorts []uint64

	L Logger

	stopped         bool
	rootStatusKnown bool
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

func (n *nodeRecord) InitData() NodeInitData {
	return NodeInitData{
		PrimaryType: n.PrimaryType,
		Name:        n.Name,
		Label:       n.Label,
		LeftPorts:   slices.Clone(n.LeftPorts),
		RightPorts:  slices.Clone(n.RightPorts),
	}
}

func (n *nodeRecord) OnInit(
	w *Workspace,
	restored *NodeInitData,
	isReplacement bool,
	isPlaceholderReplacement bool,
	isClassConstructed bool,
	isRestored bool,
) (err error) {
	if n.stopped || n.Node == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			n.stopped = true
			err = ErrNodePanic
		}
	}()
	err = n.Node.OnInit(w, n.L, n.ID, n.Class, restored, isReplacement, isPlaceholderReplacement, isClassConstructed, isRestored)
	return
}

func (n *nodeRecord) PreLinkAdd(
	port uint64,
	linkType, portDirection string,
) (rejection error) {
	if n.stopped || n.Node == nil {
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
	if n.stopped || n.Node == nil {
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
	if n.stopped || n.Node == nil {
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
	if n.stopped || n.Node == nil {
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
	if n.stopped || n.Node == nil {
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
	if n.stopped || n.Node == nil {
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

func (n *nodeRecord) OnInbox(message InboxMessage) (err error) {
	if n.stopped || n.Node == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			n.stopped = true
			err = ErrNodePanic
		}
	}()
	err = n.Node.OnInbox(message)
	return
}

func (n *nodeRecord) OnFormularMsg(message any) (err error) {
	if n.stopped || n.Node == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			n.stopped = true
			err = ErrNodePanic
		}
	}()
	err = n.Node.OnFormularMsg(copyFormularMessage(message))
	return
}

func (n *nodeRecord) OnSave(cfg configer.Config) (err error) {
	if n.stopped || n.Node == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			err = ErrNodePanic
		}
	}()
	err = n.Node.OnSave(cfg)
	return
}

func (n *nodeRecord) OnReady() (err error) {
	if n.stopped || n.Node == nil {
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

func (n *nodeRecord) OnRootStatus(hasRootPath bool) (err error) {
	if n.stopped || n.Node == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			n.stopped = true
			err = ErrNodePanic
		}
	}()
	err = n.Node.OnRootStatus(hasRootPath)
	return
}

func (n *nodeRecord) OnStop() {
	if n.stopped || n.Node == nil {
		return
	}
	n.stopped = true
	nodeStop(n.Node)
}

func nodeStop(n Node) {
	if n == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil { //nolint
			// Ignore panics on stop
		}
	}()
	n.OnStop()
}
