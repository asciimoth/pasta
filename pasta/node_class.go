package pasta

import (
	"errors"

	"github.com/asciimoth/configer/configer"
)

var (
	// ErrNoNodeClass reports that a node class is not registered in the workspace.
	ErrNoNodeClass = errors.New("node class not found")
	// ErrNodeClassFactory reports that a node class cannot construct nodes.
	ErrNodeClassFactory = errors.New("node class factory not available")
)

// NodeClass describes a node class that can be registered in a workspace.
//
// ClassName must return a valid class name. ShortDescription is plain text for
// class lists. LongDescription may contain Markdown; the workspace stores the
// value but snapshots intentionally expose only the short description.
type NodeClass interface {
	ClassName() string
	ShortDescription() string
	LongDescription() string
	DefaultNodeParams() NodeClassParams
}

// NodeClassParams describes the default workspace state for nodes of a class.
//
// Root is used as the initial explicit root status for nodes created with
// AddNodeByClass. PrimaryType may be empty or a valid type name. InitialPorts
// are created before OnInit runs; their assigned IDs are passed to OnInit via
// NodeInitData.LeftPorts and NodeInitData.RightPorts. Unique allows only one
// node of this class in a workspace.
type NodeClassParams struct {
	// Root makes freshly constructed nodes explicit roots until changed.
	Root bool
	// Unique allows at most one node record of the class in a workspace.
	Unique bool
	// PrimaryType is the initial node primary type; empty means no primary type.
	PrimaryType string
	// InitialPorts are copied and assigned workspace IDs before OnInit runs.
	InitialPorts []Port
}

// NodeClassFactory is the optional node-construction capability for NodeClass.
//
// Classes that implement this interface can be used with AddNodeByClass.
// NewNode receives a node-scoped Config when the node is being restored from
// configuration; cfg is nil for fresh nodes. When restoring an existing record,
// the workspace passes one NodeClassState pointer; factories may mutate it
// before returning the replacement node. Returning nil for a restore leaves the
// existing record unchanged.
type NodeClassFactory interface {
	NewNode(cfg configer.Config, previous ...*NodeClassState) (Node, error)
}

// NodeClassState describes existing workspace state that can be used to
// construct or restore a node. Ports with existing IDs are kept and updated,
// ports omitted from the state are removed with their links, and ports with ID
// 0 are added as new ports.
type NodeClassState struct {
	// Root, PrimaryType, Name, and Label are the workspace-owned values the
	// constructed node should inherit.
	Root        bool
	PrimaryType string
	Name        string
	Label       string
	// LeftPorts and RightPorts are desired ordered port states. Existing port
	// IDs are preserved, omitted IDs are removed, and ID 0 creates a new port.
	LeftPorts  []Port
	RightPorts []Port
}

// AddNodeClass adds or replaces a registered node class.
//
// Re-adding the same class name replaces the previous class and emits the same
// class-added notification as adding a new class.
func (w *Workspace) AddNodeClass(class NodeClass) error {
	w.Lock()
	defer w.Unlock()
	return w.AddNodeClassLocked(class)
}

// AddNodeClassLocked is AddNodeClass for callers that already hold the workspace lock.
func (w *Workspace) AddNodeClassLocked(class NodeClass) error {
	if class == nil {
		return ErrNoNodeClass
	}
	name := class.ClassName()
	if err := ValidateClassName(name); err != nil {
		return err
	}
	params := class.DefaultNodeParams()
	if err := validateNodeClassParams(params); err != nil {
		return err
	}

	if w.closed {
		return ErrWorkspaceClosed
	}

	_, wasPresent := w.classes[name]
	w.classes[name] = class
	w.enqueueNodeClassNotification(NotificationNodeClassAdded, name, nodeClassSnapshot(class))
	if params.Unique {
		w.undoRecordingDisabled += 1
		w.removeUniqueNodeClassDuplicatesLocked(name)
		w.undoRecordingDisabled -= 1
	}
	placeholders := []nodeClassPlaceholderCandidate{}
	if !wasPresent {
		placeholders = w.nodeClassPlaceholderCandidatesLocked(name)
	}

	factory, ok := class.(NodeClassFactory)
	if !ok || wasPresent {
		return nil
	}
	for _, placeholder := range placeholders {
		state := placeholder.state
		w.mu.Unlock()
		node, err := factory.NewNode(nil, &state)
		w.mu.Lock()
		if err != nil {
			return err
		}
		if node == nil {
			continue
		}
		if err := w.replacePlaceholderWithClassStateLocked(placeholder.id, name, node, state); err != nil {
			return err
		}
	}
	return nil
}

// RemoveNodeClass removes a registered node class by name.
//
// Removing a class does not remove or change existing node records of that
// class; those records keep their string Class value.
func (w *Workspace) RemoveNodeClass(name string) error {
	if err := ValidateClassName(name); err != nil {
		return err
	}

	w.Lock()
	defer w.Unlock()
	return w.RemoveNodeClassLocked(name)
}

// RemoveNodeClassLocked is RemoveNodeClass for callers that already hold the workspace lock.
func (w *Workspace) RemoveNodeClassLocked(name string) error {
	if w.closed {
		return ErrWorkspaceClosed
	}

	class, present := w.classes[name]
	if present {
		delete(w.classes, name)
	}
	if !present || class == nil {
		return ErrNoNodeClass
	}
	w.enqueueNodeClassNotification(NotificationNodeClassRemoved, name, nodeClassSnapshot(class))
	return nil
}

// NodeClass returns the registered class instance by name.
func (w *Workspace) NodeClass(name string) (NodeClass, bool) {
	if err := ValidateClassName(name); err != nil {
		return nil, false
	}

	w.Lock()
	defer w.Unlock()
	return w.NodeClassLocked(name)
}

// NodeClassLocked is NodeClass for callers that already hold the workspace lock.
func (w *Workspace) NodeClassLocked(name string) (NodeClass, bool) {
	if w.closed {
		return nil, false
	}
	class, present := w.classes[name]
	return class, present
}

// NodeClassLongDescription returns the registered class long description.
//
// The returned text may contain Markdown formatting.
func (w *Workspace) NodeClassLongDescription(name string) (string, bool) {
	if err := ValidateClassName(name); err != nil {
		return "", false
	}

	w.Lock()
	defer w.Unlock()
	return w.NodeClassLongDescriptionLocked(name)
}

// NodeClassLongDescriptionLocked is NodeClassLongDescription for callers that already hold the workspace lock.
func (w *Workspace) NodeClassLongDescriptionLocked(name string) (string, bool) {
	if w.closed {
		return "", false
	}
	class, present := w.classes[name]
	if !present || class == nil {
		return "", false
	}
	return class.LongDescription(), true
}

// AddNodeByClass constructs and adds a node using a registered class factory.
//
// The class must implement NodeClassFactory. Default class parameters are
// applied before OnInit. If name is empty or omitted, a generic unique name is
// generated.
func (w *Workspace) AddNodeByClass(class string, name ...string) (uint64, error) {
	w.Lock()
	defer w.Unlock()
	return w.AddNodeByClassLocked(class, name...)
}

// AddNodeByClassLocked is AddNodeByClass for callers that already hold the workspace lock.
func (w *Workspace) AddNodeByClassLocked(class string, name ...string) (uint64, error) {
	if err := ValidateClassName(class); err != nil {
		return 0, err
	}
	requestedName := optionalName(name)

	if w.closed {
		return 0, ErrWorkspaceClosed
	}
	if requestedName != "" {
		if err := ValidateNodeName(requestedName); err != nil {
			return 0, err
		}
		if err := w.rejectNodeNameDuplicateLocked(requestedName, 0); err != nil {
			return 0, err
		}
	}
	nodeClass, present := w.classes[class]
	if present && nodeClass != nil {
		params := nodeClass.DefaultNodeParams()
		if params.Unique {
			if err := w.rejectUniqueNodeDuplicateLocked(class, 0); err != nil {
				return 0, err
			}
		}
	}
	if !present || nodeClass == nil {
		return 0, ErrNoNodeClass
	}

	factory, ok := nodeClass.(NodeClassFactory)
	if !ok {
		return 0, ErrNodeClassFactory
	}
	w.mu.Unlock()
	node, err := factory.NewNode(nil)
	w.mu.Lock()
	if err != nil {
		return 0, err
	}
	if node == nil {
		return 0, ErrNoNode
	}
	params := nodeClass.DefaultNodeParams()
	return w.addNodeByClassWithParamsLocked(node, class, params, requestedName)
}

func nodeClassSnapshot(class NodeClass) NodeClassSnapshot {
	if class == nil {
		return NodeClassSnapshot{}
	}
	params := class.DefaultNodeParams()
	return NodeClassSnapshot{
		Class:            class.ClassName(),
		ShortDescription: class.ShortDescription(),
		Unique:           params.Unique,
		PrimaryType:      params.PrimaryType,
		InitialPorts:     nodeClassPortSnapshots(params.InitialPorts),
	}
}

func validateNodeClassParams(params NodeClassParams) error {
	if params.PrimaryType != "" {
		if err := ValidateTypeName(params.PrimaryType); err != nil {
			return err
		}
	}
	return validateDefaultPorts(params.InitialPorts)
}

func nodeClassPortSnapshots(ports []Port) []NodeClassPortSnapshot {
	snapshots := make([]NodeClassPortSnapshot, 0, len(ports))
	for _, port := range ports {
		port = port.Copy()
		snapshots = append(snapshots, NodeClassPortSnapshot{
			Direction: port.Direction,
			Name:      port.Name,
			Types:     port.CopyTypes(),
		})
	}
	return snapshots
}
