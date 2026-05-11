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
	Runtime     NodeClass `json:"-"`
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

// NodeClass creates the runtime object for nodes of a class.
//
// Hooks run outside the workspace lock. Implementations may inspect the
// workspace through the read-only API on NodeContext, but all mutations must go
// through Workspace or LibraryScope methods.
type NodeClass interface {
	InitNode(NodeContext, NodeState, InitMode) (NodeRuntime, error)
}

// InitMode distinguishes normal creation from restore/paste initialization.
type InitMode string

const (
	// InitNew is used for freshly created nodes initialized from defaults or options.
	InitNew InitMode = "new"
	// InitRestore is used for nodes initialized from persisted or clipboard state.
	InitRestore InitMode = "restore"
)

// NodeContext identifies a node while lifecycle hooks are running.
type NodeContext struct {
	ID       NodeID
	Class    string
	Library  string
	ReadOnly WorkspaceRO
	Node     NodeScope
}

// NodeRuntime is the application-owned runtime object for a node.
//
// Runtime values can implement any of the optional hook interfaces below.
type NodeRuntime interface{}

// NodeScope is the mutation surface for one node implementation.
//
// Methods are concurrent-safe and mutate only the node named by ID. They return
// ErrNotFound after the node has been deleted and ErrClosed after workspace close.
type NodeScope interface {
	ID() NodeID
	ReadOnly() WorkspaceRO
	Snapshot() (NodeSnapshot, bool)
	SetState(NodeState) error
	SetPrivate(any) error
	SetCoordinate(string) error
	SetPorts(inputs, outputs []PortSpec) error
}

// LinkEndpoint describes one node's view of a link.
type LinkEndpoint struct {
	Link      LinkID
	Self      FullPortID
	Peer      FullPortID
	Type      string
	Direction PortDirection
}

// InactiveReason describes why an active object is becoming inactive.
type InactiveReason string

const (
	// InactiveClassRecall means the node class was recalled by its library.
	InactiveClassRecall InactiveReason = "class-recall"
	// InactiveLibraryUnregister means the owning library was unregistered.
	InactiveLibraryUnregister InactiveReason = "library-unregister"
	// InactiveWorkspaceClose means the workspace is closing.
	InactiveWorkspaceClose InactiveReason = "workspace-close"
)

// LinkObjectProvider lets the input node supply the link object passed to the output node.
type LinkObjectProvider interface {
	LinkObject(LinkEndpoint) (any, error)
}

// LinkAttachHook validates and observes link attachment.
type LinkAttachHook interface {
	BeforeLinkAttach(LinkEndpoint, any) error
	AfterLinkAttach(LinkEndpoint, any)
}

// LinkDetachHook validates and observes link detachment.
type LinkDetachHook interface {
	BeforeLinkDetach(LinkEndpoint) error
	AfterLinkDetach(LinkEndpoint)
}

// LinkInactiveHook observes a link becoming inactive while it is preserved.
type LinkInactiveHook interface {
	AfterLinkInactive(LinkEndpoint, InactiveReason)
}

// NodeInactiveHook validates and observes a node becoming inactive.
type NodeInactiveHook interface {
	BeforeInactive(InactiveReason) error
	AfterInactive(InactiveReason)
}

// NodeDeleteHook observes node deletion.
type NodeDeleteHook interface {
	BeforeDelete() error
	AfterDelete()
}

// NodeCloseHook releases resources owned by a node runtime.
//
// Close runs outside the workspace lock when a runtime is deleted, becomes
// inactive, or the workspace closes. If the node is later reactivated, the
// workspace initializes a new runtime for it.
type NodeCloseHook interface {
	Close() error
}

// NodePrivateExportHook lets a runtime provide its current private state for persistence.
type NodePrivateExportHook interface {
	ExportPrivateState() (any, error)
}

// NodePrivateImportHook lets a runtime receive restored or default private state.
type NodePrivateImportHook interface {
	ImportPrivateState(any) error
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
	Class(string) (ClassSnapshot, bool)
	Node(NodeID) (NodeSnapshot, bool)
	Link(LinkID) (LinkSnapshot, bool)
}

// LibraryScope is the write surface available to one registered library.
type LibraryScope interface {
	DefineClass(ClassSpec) error
	RecallClass(string) error
	CreateNode(string, NodeOptions) (NodeID, error)
	DeleteNode(NodeID) error
	SetNodePrivate(NodeID, any) error
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
