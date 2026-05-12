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

// PortSpec describes one public node port.
//
// A port belongs to either a node's input set or output set. ID values are
// unique within that node and must use the same direction as Direction. A port
// can declare one FixedType or a list of AcceptedTypes; linked ports may only
// be replaced when every existing link still satisfies the new contract.
//
// Metadata is public string data for editor and application annotations, such
// as labels, grouping hints, UI affordances, or integration-specific tags. The
// workspace stores, persists, and defensively copies it, but does not interpret
// it for validation, type compatibility, lifecycle hooks, or link behavior.
type PortSpec struct {
	ID            PortID
	Name          string
	Direction     PortDirection
	FixedType     string
	AcceptedTypes []string
	Multiple      bool
	Metadata      map[string]string
}

// NodeState is the dynamic state stored for a node.
//
// DisplayName, Description, PrimaryType, Coordinate, and Metadata are public
// editor-facing values. Metadata is public string data for annotations that
// should be visible in snapshots and persistence, but should not affect graph
// validation or runtime contracts. Private is application-owned runtime state;
// the workspace stores, copies, and restores it without interpretation. Runtimes
// that need their latest volatile private value included in SaveWithRuntimeState
// or Copy should implement NodePrivateExportHook.
type NodeState struct {
	DisplayName string
	Description string
	PrimaryType string
	Coordinate  string
	Metadata    map[string]string
	Private     any
}

// ClassSpec describes a node class definition supplied by a Library.
//
// Name must be under the defining library's prefix. Default, Inputs, Outputs,
// and Metadata are copied into newly created nodes. Class Metadata describes
// the class itself, for example catalog, palette, or documentation annotations;
// use Default.Metadata for metadata that should become per-node state. Runtime
// is optional; when it is non-nil, the workspace calls InitNode for each active
// node instance and stores the returned NodeRuntime for later lifecycle hooks.
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
//
// By default CreateNode starts from the class default state. Set UseState to
// true to use State instead, for example when a controller wants to seed public
// metadata or private state explicitly. Metadata supplied through State is
// copied into the new node's dynamic state and remains editable after creation.
type NodeOptions struct {
	State    NodeState
	UseState bool
}

// LinkOptions customizes link creation.
//
// Type is the fixed link type to create. If Type is empty, the workspace chooses
// a fixed type from one endpoint. Object is the application-owned link contract
// value; when it is nil, the input runtime may provide it through
// LinkObjectProvider. Waypoints are opaque editor route coordinates.
type LinkOptions struct {
	Type      string
	Object    any
	Waypoints []string
}

// Clipboard contains serialized nodes and their internal links for paste.
//
// Copy fills this value with the selected nodes and links whose endpoints are
// both inside the selection. Paste creates fresh node and link IDs and does not
// reconnect links to nodes outside the clipboard.
type Clipboard struct {
	Nodes []SaveNode
	Links []SaveLink
}

// Library defines node classes during registration.
//
// Name must be a valid library name. DefineClasses receives a LibraryScope
// restricted to that library's prefix and ownership boundary. Registration is
// transactional: if DefineClasses returns an error or panics, the workspace
// restores the previous model and closes any runtimes initialized during the
// failed registration.
type Library interface {
	Name() string
	DefineClasses(LibraryScope) error
}

// NodeClass creates the runtime object for nodes of a class.
//
// InitNode runs outside the workspace lock with a node-scoped mutation API that
// remains valid during initialization. The mode is InitNew for normal creation
// and InitRestore for restore, paste, or reactivation. If initialization fails
// or panics, the node creation or recovery is rolled back and any initialized
// runtimes from the same transaction are closed.
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
//
// ReadOnly gives a defensive snapshot/query surface for inspecting the graph.
// Node is restricted to this node's mutable state and can be used by runtime
// goroutines after initialization until the node is deleted or the workspace is
// closed.
type NodeContext struct {
	ID       NodeID
	Class    string
	Library  string
	ReadOnly WorkspaceRO
	Node     NodeScope
}

// NodeRuntime is the application-owned runtime object for a node.
//
// Runtime values can implement any of the optional hook interfaces below. The
// workspace never type-checks link contract values beyond calling those hooks;
// nodes should validate any link object they receive before accepting it.
type NodeRuntime any

// NodeScope is the mutation surface for one node implementation.
//
// Methods are concurrent-safe and mutate only the node named by ID. They may be
// used during InitNode to update the pending node record before it is committed.
// After deletion they return ErrNotFound, and after workspace close they return
// ErrClosed.
type NodeScope interface {
	ID() NodeID
	ReadOnly() WorkspaceRO
	Snapshot() (NodeSnapshot, bool)
	AddMessage(MessageType, string) (MessageID, error)
	RemoveMessage(MessageID) error
	SetMenu(NodeMenu) error
	ClearMenu() error
	UpdateMenuState(MenuStateUpdate) (NodeMenu, error)
	SetState(NodeState) error
	SetPrivate(any) error
	SetCoordinate(string) error
	SetMetadata(map[string]string) error
	SetMetadataValue(string, string) error
	DeleteMetadataValue(string) error
	SetPorts(inputs, outputs []PortSpec) error
}

// LinkEndpoint describes one node's directional view of a link.
//
// Self is the port owned by the runtime receiving the hook. Peer is the other
// endpoint. Direction is Self's direction, so input runtimes see InputPort and
// output runtimes see OutputPort.
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

// LinkObjectProvider lets the input runtime supply a link object.
//
// LinkObject is called only on the input-side runtime and only when the caller
// did not provide LinkOptions.Object. It runs outside the workspace lock before
// attach hooks. Returning an error or panicking rejects link creation and rolls
// back the reserved link ID.
type LinkObjectProvider interface {
	LinkObject(LinkEndpoint) (any, error)
}

// LinkAttachHook validates and observes link attachment.
//
// BeforeLinkAttach is called on the input runtime first and the output runtime
// second, outside the workspace lock. Returning an error or panicking aborts the
// link and leaves the graph unchanged. AfterLinkAttach runs in the same order
// after the link has committed; its panic is logged and does not roll back.
type LinkAttachHook interface {
	BeforeLinkAttach(LinkEndpoint, any) error
	AfterLinkAttach(LinkEndpoint, any)
}

// LinkDetachHook validates and observes link detachment.
//
// BeforeLinkDetach is used for explicit link deletion. Links that must be
// removed to repair broken model state, such as links pruned by class
// redefinition, still receive AfterLinkDetach after the repair commits.
type LinkDetachHook interface {
	BeforeLinkDetach(LinkEndpoint) error
	AfterLinkDetach(LinkEndpoint)
}

// LinkInactiveHook observes a link becoming inactive while it is preserved.
//
// The hook runs outside the workspace lock after the state change has committed.
// It is a notification point for link contracts that need to unblock in-flight
// work because a class, library, or workspace is no longer active.
type LinkInactiveHook interface {
	AfterLinkInactive(LinkEndpoint, InactiveReason)
}

// NodeInactiveHook validates and observes a node becoming inactive.
//
// BeforeInactive runs outside the workspace lock before class recall, library
// unregister, or workspace close commits. Returning an error or panicking vetoes
// that operation. AfterInactive runs after commit and cannot roll it back.
type NodeInactiveHook interface {
	BeforeInactive(InactiveReason) error
	AfterInactive(InactiveReason)
}

// NodeDeleteHook validates and observes node deletion.
//
// BeforeDelete runs before attached links are detached and before the node is
// removed. AfterDelete runs after removal. Both hooks run outside the workspace
// lock; before-hook errors or panics abort deletion.
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

// NodePrivateExportHook lets a runtime provide current private state for
// persistence and copy.
//
// ExportPrivateState runs outside the workspace lock for active nodes. The
// returned value replaces NodeState.Private in SaveWithRuntimeState and Copy
// output, but does not mutate the live node state by itself.
type NodePrivateExportHook interface {
	ExportPrivateState() (any, error)
}

// NodePrivateImportHook lets a runtime receive restored or default private state.
//
// ImportPrivateState runs immediately after InitNode with a clone of the stored
// private value. An error or panic aborts the initialization transaction.
type NodePrivateImportHook interface {
	ImportPrivateState(any) error
}

// NodeMenuUpdateHook lets a runtime validate or normalize external menu edits.
//
// ApplyMenuUpdate runs outside the workspace lock after the workspace validates
// the proposed update against the current menu schema. Returning an error
// rejects the update. Returning a non-zero MenuStateUpdate lets the runtime
// normalize the accepted state before the workspace commits it.
type NodeMenuUpdateHook interface {
	ApplyMenuUpdate(MenuStateUpdate) (MenuStateUpdate, error)
}

// NodeMenuButtonHook observes an external menu button trigger.
//
// TriggerMenuButton runs outside the workspace lock after the workspace
// verifies that the button exists and is enabled. Returning an error rejects
// the trigger event.
type NodeMenuButtonHook interface {
	TriggerMenuButton(MenuButtonRef) error
}

// StaticLibrary is a simple Library implementation backed by ClassSpec values.
//
// It is useful for applications whose class set is known up front and for tests
// that do not need custom registration behavior.
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
//
// All returned values are defensive snapshots. Callers may retain and mutate the
// returned slices and maps without changing workspace-owned state.
type WorkspaceRO interface {
	Snapshot() Snapshot
	Class(string) (ClassSnapshot, bool)
	Classes() []ClassSnapshot
	ClassesByLibrary(string) []ClassSnapshot
	Node(NodeID) (NodeSnapshot, bool)
	Link(LinkID) (LinkSnapshot, bool)
	NodeMessages(NodeID) []NodeMessage
	NodeMenu(NodeID) (NodeMenu, bool)
}

// LibraryScope is the write surface available to one registered library.
//
// A scoped library may define and recall only its own classes, mutate only nodes
// owned by those classes, and create or mutate links only when both endpoint
// nodes are owned by the same library. Read methods apply the same ownership
// boundary where relevant.
type LibraryScope interface {
	DefineClass(ClassSpec) error
	RecallClass(string) error
	Classes() []ClassSnapshot
	CanCreateNode(string) error
	CreateNode(string, NodeOptions) (NodeID, error)
	CanDeleteNode(NodeID) error
	DeleteNode(NodeID) error
	SetNodeState(NodeID, NodeState) error
	SetNodePrivate(NodeID, any) error
	SetNodeCoordinate(NodeID, string) error
	SetNodeMetadata(NodeID, map[string]string) error
	SetNodeMetadataValue(NodeID, string, string) error
	DeleteNodeMetadataValue(NodeID, string) error
	AddNodeMessage(NodeID, MessageType, string) (MessageID, error)
	RemoveNodeMessage(NodeID, MessageID) error
	SetNodeMenu(NodeID, NodeMenu) error
	ClearNodeMenu(NodeID) error
	UpdateNodeMenuState(NodeID, MenuStateUpdate) (NodeMenu, error)
	TriggerNodeMenuButton(NodeID, MenuButtonRef) error
	CanSetNodePorts(NodeID, []PortSpec, []PortSpec) error
	SetNodePorts(NodeID, []PortSpec, []PortSpec) error
	CanCreateLink(FullPortID, FullPortID, string) error
	CreateLink(FullPortID, FullPortID, LinkOptions) (LinkID, error)
	CanSetLinkWaypoints(LinkID) error
	SetLinkWaypoints(LinkID, []string) error
	CanDeleteLink(LinkID) error
	DeleteLink(LinkID) error
	ReadOnly() WorkspaceRO
}

// Snapshot is an immutable, deterministic copy of workspace state.
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
//
// Spec is a defensive copy of the registered class definition. Active is false
// after the class has been recalled or its library has been unregistered.
type ClassSnapshot struct {
	Spec    ClassSpec
	Library string
	Active  bool
}

// NodeSnapshot is a read-only node record.
//
// Dynamic, Inputs, and Outputs are defensive copies. Inactive snapshots are kept
// when the node's class or library is unavailable so editors can present
// recoverable model state.
type NodeSnapshot struct {
	ID       NodeID
	Class    string
	Library  string
	State    ObjectState
	Dynamic  NodeState
	Inputs   []PortSpec
	Outputs  []PortSpec
	Messages []NodeMessage
	Menu     *NodeMenu
}

// LinkSnapshot is a read-only link record.
//
// StateInactive means the endpoints still exist but at least one endpoint node
// is inactive. Links with missing endpoint nodes or ports are removed rather
// than exposed as snapshots.
type LinkSnapshot struct {
	ID        LinkID
	Input     FullPortID
	Output    FullPortID
	Type      string
	State     ObjectState
	Waypoints []string
}

// MessageID identifies one ephemeral node message in a workspace.
//
// Message IDs are not persisted and are regenerated after restore.
type MessageID int64

// MessageType classifies an ephemeral node message.
type MessageType string

const (
	// MessageNote is informational.
	MessageNote MessageType = "note"
	// MessageWarn is a warning.
	MessageWarn MessageType = "warn"
	// MessageErr is an error.
	MessageErr MessageType = "err"
)

// NodeMessage is an ephemeral text message attached to one node.
//
// Messages are intended for transient UI notifications, diagnostics, or
// popups. They are exposed in snapshots and watcher events, but are not saved,
// copied, restored, or pasted.
type NodeMessage struct {
	ID   MessageID
	Node NodeID
	Type MessageType
	Text string
}

// MessageEventKind describes a watcher event for an ephemeral node message.
type MessageEventKind string

const (
	// MessageAdded means Message was attached to its node.
	MessageAdded MessageEventKind = "added"
	// MessageRemoved means Message was removed from its node.
	MessageRemoved MessageEventKind = "removed"
)

// MessageEvent is delivered to message subscriptions after add/remove changes.
type MessageEvent struct {
	Kind    MessageEventKind
	Message NodeMessage
}

// MenuEventKind describes a watcher event for an ephemeral node menu.
type MenuEventKind string

const (
	// MenuReplaced means a node menu was replaced.
	MenuReplaced MenuEventKind = "replaced"
	// MenuCleared means a node menu was cleared.
	MenuCleared MenuEventKind = "cleared"
	// MenuStateChanged means an external or node-scoped state update was accepted.
	MenuStateChanged MenuEventKind = "state-changed"
	// MenuButtonTriggered means an enabled menu button was triggered.
	MenuButtonTriggered MenuEventKind = "button-triggered"
)

// MenuEvent is delivered to menu subscriptions after menu changes or button triggers.
type MenuEvent struct {
	Kind   MenuEventKind
	Node   NodeID
	Menu   *NodeMenu
	Update MenuStateUpdate
	Button MenuButtonRef
}
