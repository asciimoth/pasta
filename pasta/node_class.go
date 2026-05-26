package pasta

import "errors"

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
	Root         bool
	Unique       bool
	PrimaryType  string
	InitialPorts []Port
}

// NodeClassFactory is the optional node-construction capability for NodeClass.
//
// Classes that implement this interface can be used with AddNodeByClass.
type NodeClassFactory interface {
	NewNode() (Node, error)
	ReplacePlaceholder(NodeClassPlaceholderState) (*NodeClassPlaceholderReplacement, error)
}

// NodeClassPlaceholderState describes a placeholder node that can be migrated
// when its class is registered.
type NodeClassPlaceholderState struct {
	Root        bool
	PrimaryType string
	Name        string
	Label       string
	LeftPorts   []Port
	RightPorts  []Port
}

// NodeClassPlaceholderReplacement is a factory suggestion for replacing one
// placeholder with a live node.
//
// State is the desired replacement state. Ports with existing placeholder IDs
// are kept and updated, ports omitted from State are removed with their links,
// and ports with ID 0 are added as new ports. Returning nil from
// ReplacePlaceholder leaves the placeholder unchanged.
type NodeClassPlaceholderReplacement struct {
	Node  Node
	State NodeClassPlaceholderState
}

// AddNodeClass adds or replaces a registered node class.
//
// Re-adding the same class name replaces the previous class and emits the same
// class-added notification as adding a new class.
func (w *Workspace) AddNodeClass(class NodeClass) error {
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

	w.Lock()
	if w.closed {
		w.Unlock()
		return ErrWorkspaceClosed
	}

	_, wasPresent := w.classes.Get(name)
	w.classes.Set(name, class)
	w.enqueueNodeClassNotification(NotificationNodeClassAdded, name, nodeClassSnapshot(class))
	if params.Unique {
		w.removeUniqueNodeClassDuplicatesLocked(name)
	}
	placeholders := []nodeClassPlaceholderCandidate{}
	if !wasPresent {
		placeholders = w.nodeClassPlaceholderCandidatesLocked(name)
	}
	w.Unlock()

	factory, ok := class.(NodeClassFactory)
	if !ok || wasPresent {
		return nil
	}
	for _, placeholder := range placeholders {
		replacement, err := factory.ReplacePlaceholder(placeholder.state)
		if err != nil {
			return err
		}
		if replacement == nil {
			continue
		}
		if err := w.replacePlaceholderWithClassState(placeholder.id, name, replacement.Node, replacement.State); err != nil {
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
	if w.closed {
		return ErrWorkspaceClosed
	}

	class, present := w.classes.Delete(name)
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
	if w.closed {
		return nil, false
	}
	return w.classes.Get(name)
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
	if w.closed {
		return "", false
	}
	class, present := w.classes.Get(name)
	if !present || class == nil {
		return "", false
	}
	return class.LongDescription(), true
}

// AddNodeByClass constructs and adds a node using a registered class factory.
//
// If name is empty or omitted, a generic unique name is generated.
func (w *Workspace) AddNodeByClass(class string, name ...string) (uint64, error) {
	if err := ValidateClassName(class); err != nil {
		return 0, err
	}
	requestedName := optionalName(name)

	w.Lock()
	if w.closed {
		w.Unlock()
		return 0, ErrWorkspaceClosed
	}
	if requestedName != "" {
		if err := ValidateNodeName(requestedName); err != nil {
			w.Unlock()
			return 0, err
		}
		if err := w.rejectNodeNameDuplicateLocked(requestedName, 0); err != nil {
			w.Unlock()
			return 0, err
		}
	}
	nodeClass, present := w.classes.Get(class)
	if present && nodeClass != nil {
		params := nodeClass.DefaultNodeParams()
		if params.Unique {
			if err := w.rejectUniqueNodeDuplicateLocked(class, 0); err != nil {
				w.Unlock()
				return 0, err
			}
		}
	}
	w.Unlock()
	if !present || nodeClass == nil {
		return 0, ErrNoNodeClass
	}

	factory, ok := nodeClass.(NodeClassFactory)
	if !ok {
		return 0, ErrNodeClassFactory
	}
	node, err := factory.NewNode()
	if err != nil {
		return 0, err
	}
	if node == nil {
		return 0, ErrNoNode
	}
	params := nodeClass.DefaultNodeParams()
	return w.addNodeByClassWithParams(node, class, params, requestedName)
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
