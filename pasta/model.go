package pasta

// PortDirection is the side and direction of a port.
type PortDirection string

const (
	// InputPort receives directed links from output ports.
	InputPort PortDirection = "input"
	// OutputPort emits directed links to input ports.
	OutputPort PortDirection = "output"
)

// ObjectState describes whether a model object can currently operate.
type ObjectState string

const (
	// StateActive means the object is installed and operational.
	StateActive ObjectState = "active"
	// StateInactive means the object is preserved but cannot currently operate.
	StateInactive ObjectState = "inactive"
)

// PortSpec describes a node port.
type PortSpec struct {
	ID            PortID
	Name          string
	Direction     PortDirection
	FixedType     string
	AcceptedTypes []string
	Multiple      bool
	Metadata      map[string]string
}

// NodeState is the public and private dynamic state of a node.
type NodeState struct {
	DisplayName string
	Description string
	PrimaryType string
	Coordinate  string
	Metadata    map[string]string
	Private     any
}

// ClassSpec describes a node class.
type ClassSpec struct {
	Name        string
	DisplayName string
	Description string
	Default     NodeState
	Inputs      []PortSpec
	Outputs     []PortSpec
	Metadata    map[string]string
}

// NodeOptions customizes node creation.
type NodeOptions struct {
	State    NodeState
	UseState bool
}

// LinkOptions customizes link creation.
type LinkOptions struct {
	Type      string
	Object    any
	Waypoints []string
}

// Clipboard contains serialized nodes and their internal links for paste.
type Clipboard struct {
	Nodes []SaveNode
	Links []SaveLink
}

// Library defines node classes during registration.
type Library interface {
	Name() string
	DefineClasses(LibraryScope) error
}

// StaticLibrary is a simple Library implementation backed by ClassSpec values.
type StaticLibrary struct {
	LibraryName string
	Classes     []ClassSpec
}

func (l StaticLibrary) Name() string { return l.LibraryName }

func (l StaticLibrary) DefineClasses(scope LibraryScope) error {
	for _, class := range l.Classes {
		if err := scope.DefineClass(class); err != nil {
			return err
		}
	}
	return nil
}

// WorkspaceRO is the concurrent-safe read-only workspace surface.
type WorkspaceRO interface {
	Snapshot() Snapshot
	Node(NodeID) (NodeSnapshot, bool)
	Link(LinkID) (LinkSnapshot, bool)
}

// LibraryScope is the write surface available to one registered library.
type LibraryScope interface {
	DefineClass(ClassSpec) error
	RecallClass(string) error
	CreateNode(string, NodeOptions) (NodeID, error)
	DeleteNode(NodeID) error
	CreateLink(FullPortID, FullPortID, LinkOptions) (LinkID, error)
	DeleteLink(LinkID) error
	ReadOnly() WorkspaceRO
}

// Snapshot is an immutable copy of workspace state.
type Snapshot struct {
	Libraries []LibrarySnapshot
	Classes   []ClassSnapshot
	Nodes     []NodeSnapshot
	Links     []LinkSnapshot
}

// LibrarySnapshot is a read-only library record.
type LibrarySnapshot struct {
	Name   string
	Active bool
}

// ClassSnapshot is a read-only class record.
type ClassSnapshot struct {
	Spec    ClassSpec
	Library string
	Active  bool
}

// NodeSnapshot is a read-only node record.
type NodeSnapshot struct {
	ID      NodeID
	Class   string
	Library string
	State   ObjectState
	Dynamic NodeState
	Inputs  []PortSpec
	Outputs []PortSpec
}

// LinkSnapshot is a read-only link record.
type LinkSnapshot struct {
	ID        LinkID
	Input     FullPortID
	Output    FullPortID
	Type      string
	State     ObjectState
	Waypoints []string
}
