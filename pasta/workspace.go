package pasta

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sort"
	"sync"
)

// Workspace owns libraries, classes, nodes, links, and ID generation.
//
// Workspace is the controller-facing mutation API and the synchronization
// boundary for the graph. All exported methods are safe for concurrent use.
// Mutations validate the model under the workspace lock, call application hooks
// outside the lock when needed, then revalidate before commit.
type Workspace struct {
	mu       sync.RWMutex
	logger   Logger
	closed   bool
	nextNode NodeID
	nextLink LinkID

	libraries map[string]Library
	classes   map[string]*classRecord
	nodes     map[NodeID]*nodeRecord
	links     map[LinkID]*linkRecord
}

type classRecord struct {
	spec    ClassSpec
	library string
	active  bool
}

type nodeRecord struct {
	id      NodeID
	class   string
	library string
	state   ObjectState
	dynamic NodeState
	inputs  []PortSpec
	outputs []PortSpec
	runtime NodeRuntime
}

type linkRecord struct {
	id        LinkID
	input     FullPortID
	output    FullPortID
	typ       string
	state     ObjectState
	waypoints []string
	object    any
}

// WorkspaceOption configures a Workspace.
type WorkspaceOption func(*Workspace)

// WithLogger configures panic and diagnostic logging.
//
// The logger is used for recovered hook panics and non-fatal lifecycle
// diagnostics. Logger implementations must be safe for concurrent use.
func WithLogger(logger Logger) WorkspaceOption {
	return func(w *Workspace) { w.logger = logger }
}

// NewWorkspace creates an empty workspace.
//
// The returned workspace starts open with node and link ID generators beginning
// at 1. Register one or more libraries before creating nodes.
func NewWorkspace(opts ...WorkspaceOption) *Workspace {
	w := &Workspace{
		nextNode:  1,
		nextLink:  1,
		libraries: make(map[string]Library),
		classes:   make(map[string]*classRecord),
		nodes:     make(map[NodeID]*nodeRecord),
		links:     make(map[LinkID]*linkRecord),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Close marks the workspace closed and inactivates all live objects.
//
// Close calls BeforeInactive for active nodes, commits inactive state, then
// sends AfterInactive, AfterLinkInactive, and Close notifications outside the
// workspace lock. Once closed, mutating methods return ErrClosed.
func (w *Workspace) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	inactiveNodes := make(map[NodeID]bool)
	for id, node := range w.nodes {
		if node.state == StateActive {
			inactiveNodes[id] = true
		}
	}
	nodeEvents, linkEvents := w.inactiveEventsForNodesLocked(inactiveNodes)
	w.mu.Unlock()
	if err := w.callBeforeInactiveEvents(nodeEvents, InactiveWorkspaceClose); err != nil {
		return opErr("close workspace", "hook", err)
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	for _, n := range w.nodes {
		n.state = StateInactive
		n.runtime = nil
	}
	for _, l := range w.links {
		l.state = StateInactive
	}
	w.mu.Unlock()
	w.callAfterInactiveEvents(nodeEvents, InactiveWorkspaceClose)
	w.callLinkInactiveEvents(linkEvents, InactiveWorkspaceClose)
	if err := w.callCloseEvents(nodeEvents); err != nil {
		return opErr("close workspace", "hook", err)
	}
	return nil
}

// RegisterLibrary registers a library and asks it to define its classes.
//
// Registration is transactional. The library's DefineClasses hook runs outside
// the workspace lock through a LibraryScope. If the hook fails or panics, all
// classes, nodes, links, and runtimes created by the registration attempt are
// rolled back.
func (w *Workspace) RegisterLibrary(lib Library) (err error) {
	if lib == nil {
		return opErr("register library", "validate", ErrNotFound)
	}
	name := lib.Name()
	if !ValidLibraryName(name) {
		return opErr("register library", "validate", ErrInvalidName)
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return opErr("register library", "validate", ErrClosed)
	}
	if _, ok := w.libraries[name]; ok {
		w.mu.Unlock()
		return opErr("register library", "validate", ErrDuplicate)
	}
	oldLibraries := cloneLibraries(w.libraries)
	oldClasses := cloneClassRecords(w.classes)
	oldNodes := cloneNodeRecords(w.nodes)
	oldLinks := cloneLinkRecords(w.links)
	w.libraries[name] = lib
	w.mu.Unlock()

	cleanupRuntimes := make(map[NodeID]NodeRuntime)
	rollback := func() error {
		w.mu.Lock()
		w.libraries = oldLibraries
		w.classes = oldClasses
		w.nodes = oldNodes
		w.links = oldLinks
		w.mu.Unlock()
		return w.cleanupInitializedRuntimes(cleanupRuntimes, nil)
	}
	defer func() {
		if r := recover(); r != nil {
			w.logPanic("register library hook", r)
			err = errors.Join(opErr("register library", "hook", fmt.Errorf("panic: %v", r)), rollback())
		}
	}()
	var detachEvents []linkDetachEvent
	if err := lib.DefineClasses(&libraryScope{w: w, library: name, detachEvents: &detachEvents, cleanupRuntimes: cleanupRuntimes}); err != nil {
		w.logError("register library hook", err)
		return errors.Join(opErr("register library", "hook", err), rollback())
	}
	w.callAfterLinkDetachEvents(detachEvents)
	return nil
}

// UnregisterLibrary unregisters a library and inactivates its classes, nodes, and links.
func (w *Workspace) UnregisterLibrary(name string) error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return opErr("unregister library", "validate", ErrClosed)
	}
	if _, ok := w.libraries[name]; !ok {
		w.mu.Unlock()
		return opErr("unregister library", "validate", ErrNotFound)
	}
	inactiveNodes := make(map[NodeID]bool)
	for id, node := range w.nodes {
		if node.library == name && node.state == StateActive {
			inactiveNodes[id] = true
		}
	}
	nodeEvents, linkEvents := w.inactiveEventsForNodesLocked(inactiveNodes)
	w.mu.Unlock()
	if err := w.callBeforeInactiveEvents(nodeEvents, InactiveLibraryUnregister); err != nil {
		return opErr("unregister library", "hook", err)
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return opErr("unregister library", "validate", ErrClosed)
	}
	if _, ok := w.libraries[name]; !ok {
		w.mu.Unlock()
		return opErr("unregister library", "validate", ErrNotFound)
	}
	delete(w.libraries, name)
	for _, class := range w.classes {
		if class.library == name {
			class.active = false
		}
	}
	w.refreshActivityLocked()
	w.clearInactiveRuntimesLocked()
	w.mu.Unlock()
	w.callAfterInactiveEvents(nodeEvents, InactiveLibraryUnregister)
	w.callLinkInactiveEvents(linkEvents, InactiveLibraryUnregister)
	if err := w.callCloseEvents(nodeEvents); err != nil {
		return opErr("unregister library", "hook", err)
	}
	return nil
}

// DefineClass defines or replaces an active class for a registered library.
//
// Replacing a class updates existing nodes of that class to the new default port
// set, removes links whose endpoints or types are now broken, and reactivates
// preserved inactive nodes when their endpoints remain valid.
func (w *Workspace) DefineClass(library string, spec ClassSpec) error {
	return w.defineClass(library, spec, nil, nil)
}

func (w *Workspace) defineClass(library string, spec ClassSpec, deferDetachEvents *[]linkDetachEvent, cleanupRuntimes map[NodeID]NodeRuntime) error {
	w.mu.Lock()
	oldClasses := cloneClassRecords(w.classes)
	oldNodes := cloneNodeRecords(w.nodes)
	oldLinks := cloneLinkRecords(w.links)
	detachEvents, err := w.defineClassLocked(library, spec)
	if err != nil {
		w.mu.Unlock()
		return err
	}
	initNodes := w.reactivatedInitNodesLocked(spec.Name, oldNodes)
	w.mu.Unlock()

	runtimes := make(map[NodeID]NodeRuntime, len(initNodes))
	scopes := make(map[NodeID]*nodeScope, len(initNodes))
	for _, initNode := range initNodes {
		runtime, scope, err := w.initNodeRuntime(initNode.class, initNode.record, InitRestore)
		if err != nil {
			cleanupErr := w.cleanupInitializedRuntimes(runtimes, scopes)
			w.mu.Lock()
			w.classes = oldClasses
			w.nodes = oldNodes
			w.links = oldLinks
			w.mu.Unlock()
			return errors.Join(err, cleanupErr)
		}
		runtimes[initNode.record.id] = runtime
		scopes[initNode.record.id] = scope
	}

	w.mu.Lock()
	if err := w.checkOpenLocked("define class"); err != nil {
		w.classes = oldClasses
		w.nodes = oldNodes
		w.links = oldLinks
		w.mu.Unlock()
		return errors.Join(err, w.cleanupInitializedRuntimes(runtimes, scopes))
	}
	for id, runtime := range runtimes {
		node := w.nodes[id]
		if node == nil || node.class != spec.Name || node.state != StateActive {
			w.classes = oldClasses
			w.nodes = oldNodes
			w.links = oldLinks
			w.mu.Unlock()
			return errors.Join(opErr("define class", "validate", ErrInactive), w.cleanupInitializedRuntimes(runtimes, scopes))
		}
		node.runtime = runtime
		if scope := scopes[id]; scope != nil {
			scope.finishInit()
		}
		if cleanupRuntimes != nil {
			cleanupRuntimes[id] = runtime
		}
	}
	w.mu.Unlock()
	if deferDetachEvents != nil {
		*deferDetachEvents = append(*deferDetachEvents, detachEvents...)
	} else {
		w.callAfterLinkDetachEvents(detachEvents)
	}
	return nil
}

// RecallClass marks a class inactive and inactivates dependent nodes and links.
func (w *Workspace) RecallClass(library, className string) error {
	w.mu.Lock()
	if err := w.checkOpenLocked("recall class"); err != nil {
		w.mu.Unlock()
		return err
	}
	rec, ok := w.classes[className]
	if !ok {
		w.mu.Unlock()
		return opErr("recall class", "validate", ErrNotFound)
	}
	if rec.library != library {
		w.mu.Unlock()
		return opErr("recall class", "validate", ErrOwnership)
	}
	inactiveNodes := make(map[NodeID]bool)
	for id, node := range w.nodes {
		if node.class == className && node.state == StateActive {
			inactiveNodes[id] = true
		}
	}
	nodeEvents, linkEvents := w.inactiveEventsForNodesLocked(inactiveNodes)
	w.mu.Unlock()
	if err := w.callBeforeInactiveEvents(nodeEvents, InactiveClassRecall); err != nil {
		return opErr("recall class", "hook", err)
	}
	w.mu.Lock()
	if err := w.checkOpenLocked("recall class"); err != nil {
		w.mu.Unlock()
		return err
	}
	rec, ok = w.classes[className]
	if !ok {
		w.mu.Unlock()
		return opErr("recall class", "validate", ErrNotFound)
	}
	if rec.library != library {
		w.mu.Unlock()
		return opErr("recall class", "validate", ErrOwnership)
	}
	rec.active = false
	w.refreshActivityLocked()
	w.clearInactiveRuntimesLocked()
	w.mu.Unlock()
	w.callAfterInactiveEvents(nodeEvents, InactiveClassRecall)
	w.callLinkInactiveEvents(linkEvents, InactiveClassRecall)
	if err := w.callCloseEvents(nodeEvents); err != nil {
		return opErr("recall class", "hook", err)
	}
	return nil
}

// CreateNode creates an active node from a registered active class.
//
// Runtime initialization happens outside the workspace lock. The node becomes
// visible only after InitNode and ImportPrivateState have completed and the
// class is revalidated as active.
func (w *Workspace) CreateNode(className string, opts NodeOptions) (NodeID, error) {
	w.mu.Lock()
	id, rec, runtimeClass, err := w.prepareCreateNodeLocked(className, opts, InitNew)
	w.mu.Unlock()
	if err != nil {
		return 0, err
	}
	runtime, scope, err := w.initNodeRuntime(runtimeClass, rec, InitNew)
	if err != nil {
		w.mu.Lock()
		w.rollbackPreparedNodeLocked(id)
		w.mu.Unlock()
		return 0, err
	}
	w.mu.Lock()
	if err := w.checkOpenLocked("create node"); err != nil {
		if scope != nil {
			scope.finishInit()
		}
		w.rollbackPreparedNodeLocked(id)
		w.mu.Unlock()
		return 0, err
	}
	class := w.classes[className]
	if class == nil || !class.active {
		if scope != nil {
			scope.finishInit()
		}
		w.rollbackPreparedNodeLocked(id)
		w.mu.Unlock()
		return 0, opErr("create node", "validate", ErrInactive)
	}
	rec.runtime = runtime
	w.nodes[id] = rec
	if scope != nil {
		scope.finishInit()
	}
	w.mu.Unlock()
	return id, nil
}

// DeleteNode deletes a node and immediately removes all attached links.
func (w *Workspace) DeleteNode(id NodeID) error {
	w.mu.RLock()
	node, ok := w.nodes[id]
	var runtime NodeRuntime
	if ok {
		runtime = node.runtime
	}
	w.mu.RUnlock()
	if !ok {
		return opErr("delete node", "validate", ErrNotFound)
	}
	if err := w.callBeforeDelete(runtime); err != nil {
		return opErr("delete node", "hook", err)
	}
	snapshot := w.Snapshot()
	for _, link := range snapshot.Links {
		if link.Input.Node == id || link.Output.Node == id {
			if err := w.DeleteLink(link.ID); err != nil {
				return err
			}
		}
	}
	w.mu.Lock()
	if err := w.checkOpenLocked("delete node"); err != nil {
		w.mu.Unlock()
		return err
	}
	if _, ok := w.nodes[id]; !ok {
		w.mu.Unlock()
		return opErr("delete node", "validate", ErrNotFound)
	}
	delete(w.nodes, id)
	for linkID, link := range w.links {
		if link.input.Node == id || link.output.Node == id {
			delete(w.links, linkID)
		}
	}
	w.mu.Unlock()
	w.callAfterDelete(runtime)
	if err := w.callNodeClose(runtime); err != nil {
		return opErr("delete node", "hook", err)
	}
	return nil
}

// SetNodeCoordinate stores an opaque coordinate string on a node.
func (w *Workspace) SetNodeCoordinate(id NodeID, coordinate string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpenLocked("set node coordinate"); err != nil {
		return err
	}
	node, ok := w.nodes[id]
	if !ok {
		return opErr("set node coordinate", "validate", ErrNotFound)
	}
	node.dynamic.Coordinate = coordinate
	return nil
}

// SetNodeMetadata replaces editable public metadata on a node.
func (w *Workspace) SetNodeMetadata(id NodeID, metadata map[string]string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpenLocked("set node metadata"); err != nil {
		return err
	}
	node, ok := w.nodes[id]
	if !ok {
		return opErr("set node metadata", "validate", ErrNotFound)
	}
	node.dynamic.Metadata = cloneStringMap(metadata)
	return nil
}

// SetNodeMetadataValue sets one editable public metadata value on a node.
func (w *Workspace) SetNodeMetadataValue(id NodeID, key, value string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpenLocked("set node metadata value"); err != nil {
		return err
	}
	node, ok := w.nodes[id]
	if !ok {
		return opErr("set node metadata value", "validate", ErrNotFound)
	}
	if node.dynamic.Metadata == nil {
		node.dynamic.Metadata = make(map[string]string, 1)
	}
	node.dynamic.Metadata[key] = value
	return nil
}

// DeleteNodeMetadataValue removes one editable public metadata value from a node.
func (w *Workspace) DeleteNodeMetadataValue(id NodeID, key string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpenLocked("delete node metadata value"); err != nil {
		return err
	}
	node, ok := w.nodes[id]
	if !ok {
		return opErr("delete node metadata value", "validate", ErrNotFound)
	}
	delete(node.dynamic.Metadata, key)
	if len(node.dynamic.Metadata) == 0 {
		node.dynamic.Metadata = nil
	}
	return nil
}

// SetNodeState replaces editable public/private node state while preserving class and ports.
func (w *Workspace) SetNodeState(id NodeID, state NodeState) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpenLocked("set node state"); err != nil {
		return err
	}
	node, ok := w.nodes[id]
	if !ok {
		return opErr("set node state", "validate", ErrNotFound)
	}
	state.Metadata = cloneStringMap(state.Metadata)
	node.dynamic = state
	return nil
}

// SetNodePrivate replaces the application-owned private state for a node.
func (w *Workspace) SetNodePrivate(id NodeID, private any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpenLocked("set node private"); err != nil {
		return err
	}
	node, ok := w.nodes[id]
	if !ok {
		return opErr("set node private", "validate", ErrNotFound)
	}
	node.dynamic.Private = private
	return nil
}

// SetNodePorts replaces a node's public ports if every existing link remains valid.
func (w *Workspace) SetNodePorts(id NodeID, inputs, outputs []PortSpec) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpenLocked("set node ports"); err != nil {
		return err
	}
	if err := w.canSetNodePortsLocked(id, inputs, outputs); err != nil {
		return opErr("set node ports", "validate", err)
	}
	node := w.nodes[id]
	node.inputs, node.outputs = clonePorts(inputs), clonePorts(outputs)
	return nil
}

// CreateLink creates a directed link from output to input.
//
// The input and output arguments are named by endpoint role: input must identify
// an input port and output must identify an output port. Creation reserves an ID,
// validates direction, type compatibility, multiplicity, and DAG safety, obtains
// or accepts the link object, calls attach hooks, revalidates, commits, then
// sends after-attach notifications.
func (w *Workspace) CreateLink(input, output FullPortID, opts LinkOptions) (LinkID, error) {
	w.mu.Lock()
	pending, err := w.prepareCreateLinkLocked(input, output, opts)
	w.mu.Unlock()
	if err != nil {
		return 0, err
	}
	object := opts.Object
	if object == nil {
		object, err = w.callLinkObject(pending.inputRuntime, pending.inputEndpoint)
		if err != nil {
			w.mu.Lock()
			w.rollbackPreparedLinkLocked(pending.link.id)
			w.mu.Unlock()
			return 0, opErr("create link", "hook", err)
		}
		pending.link.object = object
	}
	if err := w.callBeforeLinkAttach(pending.inputRuntime, pending.inputEndpoint, object); err != nil {
		w.mu.Lock()
		w.rollbackPreparedLinkLocked(pending.link.id)
		w.mu.Unlock()
		return 0, opErr("create link", "hook", err)
	}
	if err := w.callBeforeLinkAttach(pending.outputRuntime, pending.outputEndpoint, object); err != nil {
		w.mu.Lock()
		w.rollbackPreparedLinkLocked(pending.link.id)
		w.mu.Unlock()
		return 0, opErr("create link", "hook", err)
	}
	w.mu.Lock()
	err = w.commitPreparedLinkLocked(pending)
	w.mu.Unlock()
	if err != nil {
		w.mu.Lock()
		w.rollbackPreparedLinkLocked(pending.link.id)
		w.mu.Unlock()
		return 0, err
	}
	w.callAfterLinkAttach(pending.inputRuntime, pending.inputEndpoint, object)
	w.callAfterLinkAttach(pending.outputRuntime, pending.outputEndpoint, object)
	return pending.link.id, nil
}

// DeleteLink deletes one link.
func (w *Workspace) DeleteLink(id LinkID) error {
	w.mu.Lock()
	if err := w.checkOpenLocked("delete link"); err != nil {
		w.mu.Unlock()
		return err
	}
	link, ok := w.links[id]
	if !ok {
		w.mu.Unlock()
		return opErr("delete link", "validate", ErrNotFound)
	}
	inputRuntime, outputRuntime := w.linkRuntimesLocked(link)
	inputEndpoint, outputEndpoint := linkEndpoints(link)
	w.mu.Unlock()
	if err := w.callBeforeLinkDetach(inputRuntime, inputEndpoint); err != nil {
		return opErr("delete link", "hook", err)
	}
	if err := w.callBeforeLinkDetach(outputRuntime, outputEndpoint); err != nil {
		return opErr("delete link", "hook", err)
	}
	w.mu.Lock()
	if _, ok := w.links[id]; !ok {
		w.mu.Unlock()
		return opErr("delete link", "validate", ErrNotFound)
	}
	delete(w.links, id)
	w.mu.Unlock()
	w.callAfterLinkDetach(inputRuntime, inputEndpoint)
	w.callAfterLinkDetach(outputRuntime, outputEndpoint)
	return nil
}

// SetLinkWaypoints replaces the opaque waypoint coordinate array on a link.
func (w *Workspace) SetLinkWaypoints(id LinkID, waypoints []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpenLocked("set link waypoints"); err != nil {
		return err
	}
	link, ok := w.links[id]
	if !ok {
		return opErr("set link waypoints", "validate", ErrNotFound)
	}
	link.waypoints = append([]string(nil), waypoints...)
	return nil
}

// Copy serializes selected nodes and internal links between them.
func (w *Workspace) Copy(ids []NodeID) (Clipboard, error) {
	selected := make(map[NodeID]bool, len(ids))
	for _, id := range ids {
		selected[id] = true
	}
	w.mu.RLock()
	for _, id := range ids {
		if _, ok := w.nodes[id]; !ok {
			w.mu.RUnlock()
			return Clipboard{}, opErr("copy", "validate", ErrNotFound)
		}
	}
	w.mu.RUnlock()
	exports, err := w.exportPrivateStates(selected)
	if err != nil {
		return Clipboard{}, err
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	var clip Clipboard
	copied := make(map[NodeID]bool, len(ids))
	for _, id := range ids {
		node, ok := w.nodes[id]
		if !ok {
			return Clipboard{}, opErr("copy", "validate", ErrNotFound)
		}
		if copied[id] {
			continue
		}
		copied[id] = true
		state := cloneNodeState(node.dynamic)
		if private, ok := exports[id]; ok {
			state.Private = clonePrivateState(private)
		}
		clip.Nodes = append(clip.Nodes, SaveNode{
			ID:      id.String(),
			Class:   node.class,
			State:   state,
			Inputs:  clonePorts(node.inputs),
			Outputs: clonePorts(node.outputs),
		})
	}
	sort.Slice(clip.Nodes, func(i, j int) bool { return clip.Nodes[i].ID < clip.Nodes[j].ID })
	for _, link := range w.links {
		if copied[link.input.Node] && copied[link.output.Node] {
			clip.Links = append(clip.Links, SaveLink{
				Name:      FullLinkName{Link: link.id, Input: link.input, Output: link.output}.String(),
				Type:      link.typ,
				Waypoints: append([]string(nil), link.waypoints...),
			})
		}
	}
	sort.Slice(clip.Links, func(i, j int) bool { return clip.Links[i].Name < clip.Links[j].Name })
	return clip, nil
}

// Paste creates new nodes and remapped internal links from Clipboard.
func (w *Workspace) Paste(clip Clipboard) ([]NodeID, []LinkID, error) {
	w.mu.Lock()
	if err := w.checkOpenLocked("paste"); err != nil {
		w.mu.Unlock()
		return nil, nil, err
	}
	w.mu.Unlock()
	nodeMap := make(map[NodeID]NodeID, len(clip.Nodes))
	newNodes := make([]NodeID, 0, len(clip.Nodes))
	for _, saved := range clip.Nodes {
		oldID, err := ParseNodeID(saved.ID)
		if err != nil {
			return nil, nil, opErr("paste", "validate", err)
		}
		w.mu.Lock()
		id, rec, runtimeClass, err := w.prepareCreateNodeLocked(saved.Class, NodeOptions{State: saved.State, UseState: true}, InitRestore)
		if err == nil {
			err = w.applySavedNodePortsLocked(rec, saved.Inputs, saved.Outputs)
			if err != nil {
				w.rollbackPreparedNodeLocked(id)
				err = opErr("paste", "validate", err)
			}
		}
		w.mu.Unlock()
		if err != nil {
			return nil, nil, err
		}
		runtime, scope, err := w.initNodeRuntime(runtimeClass, rec, InitRestore)
		if err != nil {
			w.mu.Lock()
			w.rollbackPreparedNodeLocked(id)
			w.mu.Unlock()
			return nil, nil, err
		}
		w.mu.Lock()
		if err := w.checkOpenLocked("paste"); err != nil {
			if scope != nil {
				scope.finishInit()
			}
			w.rollbackPreparedNodeLocked(id)
			w.mu.Unlock()
			return nil, nil, err
		}
		class := w.classes[saved.Class]
		if class == nil || !class.active {
			if scope != nil {
				scope.finishInit()
			}
			w.rollbackPreparedNodeLocked(id)
			w.mu.Unlock()
			return nil, nil, opErr("paste", "validate", ErrInactive)
		}
		rec.runtime = runtime
		w.nodes[id] = rec
		if scope != nil {
			scope.finishInit()
		}
		node := w.nodes[id]
		node.runtime = runtime
		w.mu.Unlock()
		nodeMap[oldID] = id
		newNodes = append(newNodes, id)
	}
	var newLinks []LinkID
	for _, saved := range clip.Links {
		full, err := ParseFullLinkName(saved.Name)
		if err != nil {
			return nil, nil, opErr("paste", "validate", err)
		}
		newInputNode, inputOK := nodeMap[full.Input.Node]
		newOutputNode, outputOK := nodeMap[full.Output.Node]
		if !inputOK || !outputOK {
			continue
		}
		input := FullPortID{Node: newInputNode, Port: full.Input.Port}
		output := FullPortID{Node: newOutputNode, Port: full.Output.Port}
		id, err := w.CreateLink(input, output, LinkOptions{Type: saved.Type, Waypoints: saved.Waypoints})
		if err != nil {
			return nil, nil, err
		}
		newLinks = append(newLinks, id)
	}
	w.mu.Lock()
	w.refreshActivityLocked()
	w.mu.Unlock()
	return newNodes, newLinks, nil
}

// CanCreateNode validates node creation without mutating the workspace.
func (w *Workspace) CanCreateNode(className string) error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if err := w.checkOpenLocked("can create node"); err != nil {
		return err
	}
	class, ok := w.classes[className]
	if !ok {
		return opErr("can create node", "validate", ErrNotFound)
	}
	if !class.active {
		return opErr("can create node", "validate", ErrInactive)
	}
	return nil
}

// CanDeleteNode validates node deletion without mutating the workspace.
func (w *Workspace) CanDeleteNode(id NodeID) error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if err := w.checkOpenLocked("can delete node"); err != nil {
		return err
	}
	if _, ok := w.nodes[id]; !ok {
		return opErr("can delete node", "validate", ErrNotFound)
	}
	return nil
}

// CanCreateLink validates a proposed link without mutating the workspace.
func (w *Workspace) CanCreateLink(input, output FullPortID, typ string) error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if err := w.checkOpenLocked("can create link"); err != nil {
		return err
	}
	_, err := w.validateLinkLocked(input, output, typ, 0)
	if err != nil {
		return opErr("can create link", "validate", err)
	}
	return nil
}

// CanSetNodePorts validates a proposed port replacement without mutating the workspace.
func (w *Workspace) CanSetNodePorts(id NodeID, inputs, outputs []PortSpec) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpenLocked("can set node ports"); err != nil {
		return err
	}
	if err := w.canSetNodePortsLocked(id, inputs, outputs); err != nil {
		return opErr("can set node ports", "validate", err)
	}
	return nil
}

// CanSetLinkWaypoints validates link waypoint replacement without mutating the workspace.
func (w *Workspace) CanSetLinkWaypoints(id LinkID) error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if err := w.checkOpenLocked("can set link waypoints"); err != nil {
		return err
	}
	if _, ok := w.links[id]; !ok {
		return opErr("can set link waypoints", "validate", ErrNotFound)
	}
	return nil
}

// CanDeleteLink validates link deletion without mutating the workspace.
func (w *Workspace) CanDeleteLink(id LinkID) error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if err := w.checkOpenLocked("can delete link"); err != nil {
		return err
	}
	if _, ok := w.links[id]; !ok {
		return opErr("can delete link", "validate", ErrNotFound)
	}
	return nil
}

// Snapshot returns a deterministic defensive copy of the workspace.
func (w *Workspace) Snapshot() Snapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.snapshotLocked()
}

// Class returns one class snapshot.
func (w *Workspace) Class(name string) (ClassSnapshot, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	class, ok := w.classes[name]
	if !ok {
		return ClassSnapshot{}, false
	}
	return snapshotClass(class), true
}

// Classes returns deterministic defensive class snapshots.
func (w *Workspace) Classes() []ClassSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.classesByLibraryLocked("")
}

// ClassesByLibrary returns deterministic defensive class snapshots for one library.
func (w *Workspace) ClassesByLibrary(library string) []ClassSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.classesByLibraryLocked(library)
}

// Node returns one node snapshot.
func (w *Workspace) Node(id NodeID) (NodeSnapshot, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	node, ok := w.nodes[id]
	if !ok {
		return NodeSnapshot{}, false
	}
	return snapshotNode(node), true
}

// Link returns one link snapshot.
func (w *Workspace) Link(id LinkID) (LinkSnapshot, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	link, ok := w.links[id]
	if !ok {
		return LinkSnapshot{}, false
	}
	return snapshotLink(link), true
}

func (w *Workspace) defineClassLocked(library string, spec ClassSpec) ([]linkDetachEvent, error) {
	if err := w.checkOpenLocked("define class"); err != nil {
		return nil, err
	}
	if _, ok := w.libraries[library]; !ok {
		return nil, opErr("define class", "validate", ErrNotFound)
	}
	if !ValidClassName(library, spec.Name) {
		return nil, opErr("define class", "validate", ErrInvalidName)
	}
	if err := validatePorts(spec.Inputs, InputPort); err != nil {
		return nil, opErr("define class", "validate", err)
	}
	if err := validatePorts(spec.Outputs, OutputPort); err != nil {
		return nil, opErr("define class", "validate", err)
	}
	w.classes[spec.Name] = &classRecord{spec: cloneClassSpec(spec), library: library, active: true}
	for _, node := range w.nodes {
		if node.class == spec.Name {
			node.library = library
			node.inputs = clonePorts(spec.Inputs)
			node.outputs = clonePorts(spec.Outputs)
		}
	}
	detachEvents := w.removeBrokenLinksLocked()
	w.refreshActivityLocked()
	return detachEvents, nil
}

func (w *Workspace) reactivatedInitNodesLocked(className string, oldNodes map[NodeID]*nodeRecord) []restoreInitNode {
	class := w.classes[className]
	if class == nil || class.spec.Runtime == nil {
		return nil
	}
	var initNodes []restoreInitNode
	for id, node := range w.nodes {
		if node.class != className || node.state != StateActive {
			continue
		}
		oldNode := oldNodes[id]
		if oldNode != nil && oldNode.state == StateActive {
			continue
		}
		initNodes = append(initNodes, restoreInitNode{record: node, class: class.spec.Runtime})
	}
	w.sortRestoreInitNodesLocked(initNodes)
	return initNodes
}

func (w *Workspace) prepareCreateNodeLocked(className string, opts NodeOptions, _ InitMode) (NodeID, *nodeRecord, NodeClass, error) {
	if err := w.checkOpenLocked("create node"); err != nil {
		return 0, nil, nil, err
	}
	class, ok := w.classes[className]
	if !ok {
		return 0, nil, nil, opErr("create node", "validate", ErrNotFound)
	}
	if !class.active {
		return 0, nil, nil, opErr("create node", "validate", ErrInactive)
	}
	id := w.nextNode
	w.nextNode++
	state := cloneNodeState(class.spec.Default)
	if opts.UseState || !reflect.DeepEqual(opts.State, NodeState{}) {
		state = cloneNodeState(opts.State)
	}
	rec := &nodeRecord{
		id:      id,
		class:   className,
		library: class.library,
		state:   StateActive,
		dynamic: state,
		inputs:  clonePorts(class.spec.Inputs),
		outputs: clonePorts(class.spec.Outputs),
	}
	return id, rec, class.spec.Runtime, nil
}

func (w *Workspace) applySavedNodePortsLocked(rec *nodeRecord, inputs, outputs []PortSpec) error {
	if len(inputs) > 0 {
		if err := validatePorts(inputs, InputPort); err != nil {
			return err
		}
		rec.inputs = clonePorts(inputs)
	}
	if len(outputs) > 0 {
		if err := validatePorts(outputs, OutputPort); err != nil {
			return err
		}
		rec.outputs = clonePorts(outputs)
	}
	return nil
}

func (w *Workspace) rollbackPreparedNodeLocked(id NodeID) {
	delete(w.nodes, id)
	if id+1 == w.nextNode {
		w.nextNode = id
	}
}

type pendingLinkCreate struct {
	link           *linkRecord
	inputRuntime   NodeRuntime
	outputRuntime  NodeRuntime
	inputEndpoint  LinkEndpoint
	outputEndpoint LinkEndpoint
}

func (w *Workspace) prepareCreateLinkLocked(input, output FullPortID, opts LinkOptions) (pendingLinkCreate, error) {
	if err := w.checkOpenLocked("create link"); err != nil {
		return pendingLinkCreate{}, err
	}
	typ, err := w.validateLinkLocked(input, output, opts.Type, 0)
	if err != nil {
		return pendingLinkCreate{}, opErr("create link", "validate", err)
	}
	id := w.nextLink
	w.nextLink++
	link := &linkRecord{
		id:        id,
		input:     input,
		output:    output,
		typ:       typ,
		state:     StateActive,
		waypoints: append([]string(nil), opts.Waypoints...),
		object:    opts.Object,
	}
	inputRuntime, outputRuntime := w.linkRuntimesLocked(link)
	inputEndpoint, outputEndpoint := linkEndpoints(link)
	return pendingLinkCreate{
		link:           link,
		inputRuntime:   inputRuntime,
		outputRuntime:  outputRuntime,
		inputEndpoint:  inputEndpoint,
		outputEndpoint: outputEndpoint,
	}, nil
}

func (w *Workspace) rollbackPreparedLinkLocked(id LinkID) {
	if _, exists := w.links[id]; !exists && id+1 == w.nextLink {
		w.nextLink = id
	}
}

func (w *Workspace) commitPreparedLinkLocked(pending pendingLinkCreate) error {
	if err := w.checkOpenLocked("create link"); err != nil {
		return err
	}
	if _, exists := w.links[pending.link.id]; exists {
		return opErr("create link", "validate", ErrDuplicate)
	}
	typ, err := w.validateLinkLocked(pending.link.input, pending.link.output, pending.link.typ, 0)
	if err != nil {
		return opErr("create link", "validate", err)
	}
	if typ != pending.link.typ {
		return opErr("create link", "validate", ErrTypeMismatch)
	}
	if w.nextLink <= pending.link.id {
		w.nextLink = pending.link.id + 1
	}
	w.links[pending.link.id] = pending.link
	w.refreshActivityLocked()
	return nil
}

func (w *Workspace) validateLinkLocked(input, output FullPortID, requested string, ignore LinkID) (string, error) {
	if input.Node == output.Node {
		return "", ErrInvalidPort
	}
	inNode, ok := w.nodes[input.Node]
	if !ok {
		return "", ErrNotFound
	}
	outNode, ok := w.nodes[output.Node]
	if !ok {
		return "", ErrNotFound
	}
	inPort, ok := findPort(inNode.inputs, input.Port)
	if !ok || inPort.Direction != InputPort {
		return "", ErrInvalidPort
	}
	outPort, ok := findPort(outNode.outputs, output.Port)
	if !ok || outPort.Direction != OutputPort {
		return "", ErrInvalidPort
	}
	if !inPort.Multiple {
		for id, link := range w.links {
			if id != ignore && link.input == input {
				return "", ErrMultiplicity
			}
		}
	}
	typ, err := chooseLinkType(*inPort, *outPort, requested)
	if err != nil {
		return "", err
	}
	if w.pathExistsLocked(input.Node, output.Node, ignore) {
		return "", ErrCycle
	}
	return typ, nil
}

func (w *Workspace) validateAttachedLinksLocked(nodeID NodeID) error {
	inputCounts := map[FullPortID]int{}
	for _, link := range w.links {
		inputCounts[link.input]++
		if link.input.Node != nodeID && link.output.Node != nodeID {
			continue
		}
		inNode := w.nodes[link.input.Node]
		outNode := w.nodes[link.output.Node]
		if inNode == nil || outNode == nil {
			return ErrNotFound
		}
		inPort, ok := findPort(inNode.inputs, link.input.Port)
		if !ok {
			return ErrInvalidPort
		}
		outPort, ok := findPort(outNode.outputs, link.output.Port)
		if !ok {
			return ErrInvalidPort
		}
		if !portAccepts(*inPort, link.typ) || !portAccepts(*outPort, link.typ) {
			return ErrTypeMismatch
		}
	}
	for input, count := range inputCounts {
		node := w.nodes[input.Node]
		if node == nil {
			return ErrNotFound
		}
		port, ok := findPort(node.inputs, input.Port)
		if !ok {
			return ErrInvalidPort
		}
		if !port.Multiple && count > 1 {
			return ErrMultiplicity
		}
	}
	return nil
}

func (w *Workspace) canSetNodePortsLocked(id NodeID, inputs, outputs []PortSpec) error {
	if err := validatePorts(inputs, InputPort); err != nil {
		return err
	}
	if err := validatePorts(outputs, OutputPort); err != nil {
		return err
	}
	node, ok := w.nodes[id]
	if !ok {
		return ErrNotFound
	}
	oldInputs, oldOutputs := node.inputs, node.outputs
	node.inputs, node.outputs = clonePorts(inputs), clonePorts(outputs)
	err := w.validateAttachedLinksLocked(id)
	node.inputs, node.outputs = oldInputs, oldOutputs
	return err
}

func chooseLinkType(input, output PortSpec, requested string) (string, error) {
	switch {
	case requested != "":
		if !ValidTypeName(requested) || !portAccepts(input, requested) || !portAccepts(output, requested) {
			return "", ErrTypeMismatch
		}
		return requested, nil
	case output.FixedType != "":
		if !portAccepts(input, output.FixedType) {
			return "", ErrTypeMismatch
		}
		return output.FixedType, nil
	case input.FixedType != "":
		if !portAccepts(output, input.FixedType) {
			return "", ErrTypeMismatch
		}
		return input.FixedType, nil
	default:
		return "", ErrTypeMismatch
	}
}

func portAccepts(port PortSpec, typ string) bool {
	if port.FixedType != "" {
		return port.FixedType == typ
	}
	return slices.Contains(port.AcceptedTypes, typ)
}

func (w *Workspace) pathExistsLocked(from, to NodeID, ignore LinkID) bool {
	seen := map[NodeID]bool{}
	var walk func(NodeID) bool
	walk = func(cur NodeID) bool {
		if cur == to {
			return true
		}
		if seen[cur] {
			return false
		}
		seen[cur] = true
		for id, link := range w.links {
			if id == ignore || link.output.Node != cur {
				continue
			}
			if walk(link.input.Node) {
				return true
			}
		}
		return false
	}
	return walk(from)
}

func (w *Workspace) refreshActivityLocked() {
	for _, node := range w.nodes {
		class := w.classes[node.class]
		if class != nil && class.active {
			node.state = StateActive
		} else {
			node.state = StateInactive
		}
	}
	w.removeInvalidLinksLocked()
	for _, link := range w.links {
		inNode := w.nodes[link.input.Node]
		outNode := w.nodes[link.output.Node]
		if inNode != nil && outNode != nil && inNode.state == StateActive && outNode.state == StateActive {
			link.state = StateActive
		} else {
			link.state = StateInactive
		}
	}
}

func (w *Workspace) clearInactiveRuntimesLocked() {
	for _, node := range w.nodes {
		if node.state == StateInactive {
			node.runtime = nil
		}
	}
}

func (w *Workspace) removeBrokenLinksLocked() []linkDetachEvent {
	return w.removeInvalidLinksLocked()
}

func (w *Workspace) removeInvalidLinksLocked() []linkDetachEvent {
	ids := make([]LinkID, 0, len(w.links))
	for id, link := range w.links {
		if link != nil {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	inputCounts := map[FullPortID]int{}
	var detachEvents []linkDetachEvent
	for _, id := range ids {
		link := w.links[id]
		inNode := w.nodes[link.input.Node]
		outNode := w.nodes[link.output.Node]
		if inNode == nil || outNode == nil {
			detachEvents = append(detachEvents, w.linkDetachEventLocked(link))
			delete(w.links, id)
			continue
		}
		inPort, ok := findPort(inNode.inputs, link.input.Port)
		if !ok {
			detachEvents = append(detachEvents, w.linkDetachEventLocked(link))
			delete(w.links, id)
			continue
		}
		outPort, ok := findPort(outNode.outputs, link.output.Port)
		if !ok {
			detachEvents = append(detachEvents, w.linkDetachEventLocked(link))
			delete(w.links, id)
			continue
		}
		if !portAccepts(*inPort, link.typ) || !portAccepts(*outPort, link.typ) {
			detachEvents = append(detachEvents, w.linkDetachEventLocked(link))
			delete(w.links, id)
			continue
		}
		if !inPort.Multiple && inputCounts[link.input] > 0 {
			detachEvents = append(detachEvents, w.linkDetachEventLocked(link))
			delete(w.links, id)
			continue
		}
		inputCounts[link.input]++
	}
	return detachEvents
}

func (w *Workspace) checkOpenLocked(op string) error {
	if w.closed {
		return opErr(op, "validate", ErrClosed)
	}
	return nil
}

func (w *Workspace) snapshotLocked() Snapshot {
	s := Snapshot{}
	for name := range w.libraries {
		s.Libraries = append(s.Libraries, LibrarySnapshot{Name: name, Active: true})
	}
	sort.Slice(s.Libraries, func(i, j int) bool { return s.Libraries[i].Name < s.Libraries[j].Name })
	s.Classes = w.classesByLibraryLocked("")
	for _, node := range w.nodes {
		s.Nodes = append(s.Nodes, snapshotNode(node))
	}
	sort.Slice(s.Nodes, func(i, j int) bool { return s.Nodes[i].ID < s.Nodes[j].ID })
	for _, link := range w.links {
		s.Links = append(s.Links, snapshotLink(link))
	}
	sort.Slice(s.Links, func(i, j int) bool { return s.Links[i].ID < s.Links[j].ID })
	return s
}

func (w *Workspace) classesByLibraryLocked(library string) []ClassSnapshot {
	classes := make([]ClassSnapshot, 0, len(w.classes))
	for _, class := range w.classes {
		if library != "" && class.library != library {
			continue
		}
		classes = append(classes, snapshotClass(class))
	}
	sort.Slice(classes, func(i, j int) bool { return classes[i].Spec.Name < classes[j].Spec.Name })
	return classes
}

func snapshotClass(class *classRecord) ClassSnapshot {
	return ClassSnapshot{
		Spec:    cloneClassSpec(class.spec),
		Library: class.library,
		Active:  class.active,
	}
}

func snapshotNode(node *nodeRecord) NodeSnapshot {
	return NodeSnapshot{
		ID:      node.id,
		Class:   node.class,
		Library: node.library,
		State:   node.state,
		Dynamic: cloneNodeState(node.dynamic),
		Inputs:  clonePorts(node.inputs),
		Outputs: clonePorts(node.outputs),
	}
}

func snapshotLink(link *linkRecord) LinkSnapshot {
	return LinkSnapshot{
		ID:        link.id,
		Input:     link.input,
		Output:    link.output,
		Type:      link.typ,
		State:     link.state,
		Waypoints: append([]string(nil), link.waypoints...),
	}
}

func validatePorts(ports []PortSpec, direction PortDirection) error {
	seen := map[PortID]bool{}
	for _, port := range ports {
		if port.ID.Number <= 0 || port.ID.Kind != direction || port.Direction != direction {
			return ErrInvalidPort
		}
		if seen[port.ID] {
			return ErrDuplicate
		}
		seen[port.ID] = true
		if port.FixedType != "" && !ValidTypeName(port.FixedType) {
			return ErrInvalidName
		}
		for _, typ := range port.AcceptedTypes {
			if !ValidTypeName(typ) {
				return ErrInvalidName
			}
		}
	}
	return nil
}

func findPort(ports []PortSpec, id PortID) (*PortSpec, bool) {
	for i := range ports {
		if ports[i].ID == id {
			return &ports[i], true
		}
	}
	return nil, false
}

func (w *Workspace) logPanic(op string, r any) {
	if w.logger != nil {
		w.logger.Errf("%s panic: %v", op, r)
	}
}

func (w *Workspace) logError(op string, err error) {
	if w.logger != nil && err != nil {
		w.logger.Errf("%s error: %v", op, err)
	}
}

type libraryScope struct {
	w               *Workspace
	library         string
	detachEvents    *[]linkDetachEvent
	cleanupRuntimes map[NodeID]NodeRuntime
}

func (s *libraryScope) DefineClass(spec ClassSpec) error {
	return s.w.defineClass(s.library, spec, s.detachEvents, s.cleanupRuntimes)
}

func (s *libraryScope) RecallClass(className string) error {
	return s.w.RecallClass(s.library, className)
}

func (s *libraryScope) Classes() []ClassSnapshot {
	return s.w.ClassesByLibrary(s.library)
}

func (s *libraryScope) CanCreateNode(className string) error {
	if !ValidClassName(s.library, className) {
		return opErr("scope can create node", "validate", ErrOwnership)
	}
	return s.w.CanCreateNode(className)
}

func (s *libraryScope) CreateNode(className string, opts NodeOptions) (NodeID, error) {
	if !ValidClassName(s.library, className) {
		return 0, opErr("scope create node", "validate", ErrOwnership)
	}
	id, err := s.w.CreateNode(className, opts)
	if err != nil || s.cleanupRuntimes == nil {
		return id, err
	}
	s.w.mu.RLock()
	if node := s.w.nodes[id]; node != nil && node.runtime != nil {
		s.cleanupRuntimes[id] = node.runtime
	}
	s.w.mu.RUnlock()
	return id, nil
}

func (s *libraryScope) CanDeleteNode(id NodeID) error {
	s.w.mu.RLock()
	owned := s.ownsNodeLocked(id)
	s.w.mu.RUnlock()
	if !owned {
		return opErr("scope can delete node", "validate", ErrOwnership)
	}
	return s.w.CanDeleteNode(id)
}

func (s *libraryScope) DeleteNode(id NodeID) error {
	s.w.mu.RLock()
	owned := s.ownsNodeLocked(id)
	s.w.mu.RUnlock()
	if !owned {
		return opErr("scope delete node", "validate", ErrOwnership)
	}
	return s.w.DeleteNode(id)
}

func (s *libraryScope) SetNodeState(id NodeID, state NodeState) error {
	s.w.mu.RLock()
	owned := s.ownsNodeLocked(id)
	s.w.mu.RUnlock()
	if !owned {
		return opErr("scope set node state", "validate", ErrOwnership)
	}
	return s.w.SetNodeState(id, state)
}

func (s *libraryScope) SetNodePrivate(id NodeID, private any) error {
	s.w.mu.RLock()
	owned := s.ownsNodeLocked(id)
	s.w.mu.RUnlock()
	if !owned {
		return opErr("scope set node private", "validate", ErrOwnership)
	}
	return s.w.SetNodePrivate(id, private)
}

func (s *libraryScope) SetNodeCoordinate(id NodeID, coordinate string) error {
	s.w.mu.RLock()
	owned := s.ownsNodeLocked(id)
	s.w.mu.RUnlock()
	if !owned {
		return opErr("scope set node coordinate", "validate", ErrOwnership)
	}
	return s.w.SetNodeCoordinate(id, coordinate)
}

func (s *libraryScope) SetNodeMetadata(id NodeID, metadata map[string]string) error {
	s.w.mu.RLock()
	owned := s.ownsNodeLocked(id)
	s.w.mu.RUnlock()
	if !owned {
		return opErr("scope set node metadata", "validate", ErrOwnership)
	}
	return s.w.SetNodeMetadata(id, metadata)
}

func (s *libraryScope) SetNodeMetadataValue(id NodeID, key, value string) error {
	s.w.mu.RLock()
	owned := s.ownsNodeLocked(id)
	s.w.mu.RUnlock()
	if !owned {
		return opErr("scope set node metadata value", "validate", ErrOwnership)
	}
	return s.w.SetNodeMetadataValue(id, key, value)
}

func (s *libraryScope) DeleteNodeMetadataValue(id NodeID, key string) error {
	s.w.mu.RLock()
	owned := s.ownsNodeLocked(id)
	s.w.mu.RUnlock()
	if !owned {
		return opErr("scope delete node metadata value", "validate", ErrOwnership)
	}
	return s.w.DeleteNodeMetadataValue(id, key)
}

func (s *libraryScope) CanSetNodePorts(id NodeID, inputs, outputs []PortSpec) error {
	s.w.mu.RLock()
	owned := s.ownsNodeLocked(id)
	s.w.mu.RUnlock()
	if !owned {
		return opErr("scope can set node ports", "validate", ErrOwnership)
	}
	return s.w.CanSetNodePorts(id, inputs, outputs)
}

func (s *libraryScope) SetNodePorts(id NodeID, inputs, outputs []PortSpec) error {
	s.w.mu.RLock()
	owned := s.ownsNodeLocked(id)
	s.w.mu.RUnlock()
	if !owned {
		return opErr("scope set node ports", "validate", ErrOwnership)
	}
	return s.w.SetNodePorts(id, inputs, outputs)
}

func (s *libraryScope) CanCreateLink(input, output FullPortID, typ string) error {
	s.w.mu.RLock()
	inNode, inOK := s.w.nodes[input.Node]
	outNode, outOK := s.w.nodes[output.Node]
	owned := inOK && outOK && inNode.library == s.library && outNode.library == s.library
	s.w.mu.RUnlock()
	if !owned {
		return opErr("scope can create link", "validate", ErrOwnership)
	}
	return s.w.CanCreateLink(input, output, typ)
}

func (s *libraryScope) CreateLink(input, output FullPortID, opts LinkOptions) (LinkID, error) {
	s.w.mu.RLock()
	inNode, inOK := s.w.nodes[input.Node]
	outNode, outOK := s.w.nodes[output.Node]
	owned := inOK && outOK && inNode.library == s.library && outNode.library == s.library
	s.w.mu.RUnlock()
	if !owned {
		return 0, opErr("scope create link", "validate", ErrOwnership)
	}
	return s.w.CreateLink(input, output, opts)
}

func (s *libraryScope) CanSetLinkWaypoints(id LinkID) error {
	s.w.mu.RLock()
	owned := s.ownsLinkLocked(id)
	s.w.mu.RUnlock()
	if !owned {
		return opErr("scope can set link waypoints", "validate", ErrOwnership)
	}
	return s.w.CanSetLinkWaypoints(id)
}

func (s *libraryScope) SetLinkWaypoints(id LinkID, waypoints []string) error {
	s.w.mu.RLock()
	owned := s.ownsLinkLocked(id)
	s.w.mu.RUnlock()
	if !owned {
		return opErr("scope set link waypoints", "validate", ErrOwnership)
	}
	return s.w.SetLinkWaypoints(id, waypoints)
}

func (s *libraryScope) CanDeleteLink(id LinkID) error {
	s.w.mu.RLock()
	owned := s.ownsLinkLocked(id)
	s.w.mu.RUnlock()
	if !owned {
		return opErr("scope can delete link", "validate", ErrOwnership)
	}
	return s.w.CanDeleteLink(id)
}

func (s *libraryScope) DeleteLink(id LinkID) error {
	s.w.mu.RLock()
	owned := s.ownsLinkLocked(id)
	s.w.mu.RUnlock()
	if !owned {
		return opErr("scope delete link", "validate", ErrOwnership)
	}
	return s.w.DeleteLink(id)
}

func (s *libraryScope) ownsNodeLocked(id NodeID) bool {
	node, ok := s.w.nodes[id]
	return ok && node.library == s.library
}

func (s *libraryScope) ownsLinkLocked(id LinkID) bool {
	link, ok := s.w.links[id]
	if !ok {
		return false
	}
	inNode := s.w.nodes[link.input.Node]
	outNode := s.w.nodes[link.output.Node]
	return inNode != nil && outNode != nil && inNode.library == s.library && outNode.library == s.library
}

func (s *libraryScope) ReadOnly() WorkspaceRO { return s.w }

type nodeScope struct {
	w        *Workspace
	id       NodeID
	initMu   sync.Mutex
	initRec  *nodeRecord
	initDone bool
}

func (s *nodeScope) ID() NodeID { return s.id }

func (s *nodeScope) ReadOnly() WorkspaceRO { return s.w }

func (s *nodeScope) Snapshot() (NodeSnapshot, bool) {
	if snap, ok := s.w.Node(s.id); ok {
		return snap, true
	}
	s.initMu.Lock()
	defer s.initMu.Unlock()
	if s.initDone || s.initRec == nil {
		return NodeSnapshot{}, false
	}
	return snapshotNode(s.initRec), true
}

func (s *nodeScope) SetState(state NodeState) error {
	err := s.w.SetNodeState(s.id, state)
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.updateInitRecord(func(rec *nodeRecord) error {
		rec.dynamic = cloneNodeState(state)
		return nil
	})
}

func (s *nodeScope) SetPrivate(private any) error {
	err := s.w.SetNodePrivate(s.id, private)
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.updateInitRecord(func(rec *nodeRecord) error {
		rec.dynamic.Private = private
		return nil
	})
}

func (s *nodeScope) SetCoordinate(coordinate string) error {
	err := s.w.SetNodeCoordinate(s.id, coordinate)
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.updateInitRecord(func(rec *nodeRecord) error {
		rec.dynamic.Coordinate = coordinate
		return nil
	})
}

func (s *nodeScope) SetMetadata(metadata map[string]string) error {
	err := s.w.SetNodeMetadata(s.id, metadata)
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.updateInitRecord(func(rec *nodeRecord) error {
		rec.dynamic.Metadata = cloneStringMap(metadata)
		return nil
	})
}

func (s *nodeScope) SetMetadataValue(key, value string) error {
	err := s.w.SetNodeMetadataValue(s.id, key, value)
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.updateInitRecord(func(rec *nodeRecord) error {
		if rec.dynamic.Metadata == nil {
			rec.dynamic.Metadata = make(map[string]string, 1)
		}
		rec.dynamic.Metadata[key] = value
		return nil
	})
}

func (s *nodeScope) DeleteMetadataValue(key string) error {
	err := s.w.DeleteNodeMetadataValue(s.id, key)
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.updateInitRecord(func(rec *nodeRecord) error {
		delete(rec.dynamic.Metadata, key)
		if len(rec.dynamic.Metadata) == 0 {
			rec.dynamic.Metadata = nil
		}
		return nil
	})
}

func (s *nodeScope) SetPorts(inputs, outputs []PortSpec) error {
	err := s.w.SetNodePorts(s.id, inputs, outputs)
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.updateInitRecord(func(rec *nodeRecord) error {
		rec.inputs = clonePorts(inputs)
		rec.outputs = clonePorts(outputs)
		return nil
	})
}

func (s *nodeScope) updateInitRecord(fn func(*nodeRecord) error) error {
	s.initMu.Lock()
	defer s.initMu.Unlock()
	if s.initDone || s.initRec == nil {
		return opErr("node scope", "validate", ErrNotFound)
	}
	return fn(s.initRec)
}

func (s *nodeScope) finishInit() {
	s.initMu.Lock()
	defer s.initMu.Unlock()
	s.initDone = true
	s.initRec = nil
}

func cloneClassSpec(spec ClassSpec) ClassSpec {
	spec.Default = cloneNodeState(spec.Default)
	spec.Inputs = clonePorts(spec.Inputs)
	spec.Outputs = clonePorts(spec.Outputs)
	spec.Metadata = cloneStringMap(spec.Metadata)
	return spec
}

func cloneLibraries(records map[string]Library) map[string]Library {
	out := make(map[string]Library, len(records))
	maps.Copy(out, records)
	return out
}

func cloneClassRecords(records map[string]*classRecord) map[string]*classRecord {
	out := make(map[string]*classRecord, len(records))
	for name, rec := range records {
		if rec == nil {
			continue
		}
		copy := *rec
		copy.spec = cloneClassSpec(rec.spec)
		out[name] = &copy
	}
	return out
}

func cloneNodeRecords(records map[NodeID]*nodeRecord) map[NodeID]*nodeRecord {
	out := make(map[NodeID]*nodeRecord, len(records))
	for id, rec := range records {
		if rec == nil {
			continue
		}
		copy := *rec
		copy.dynamic = cloneNodeState(rec.dynamic)
		copy.inputs = clonePorts(rec.inputs)
		copy.outputs = clonePorts(rec.outputs)
		out[id] = &copy
	}
	return out
}

func cloneLinkRecords(records map[LinkID]*linkRecord) map[LinkID]*linkRecord {
	out := make(map[LinkID]*linkRecord, len(records))
	for id, rec := range records {
		if rec == nil {
			continue
		}
		copy := *rec
		copy.waypoints = append([]string(nil), rec.waypoints...)
		out[id] = &copy
	}
	return out
}

func cloneNodeState(state NodeState) NodeState {
	state.Metadata = cloneStringMap(state.Metadata)
	state.Private = clonePrivateState(state.Private)
	return state
}

func clonePrivateState(private any) any {
	switch value := private.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for k, v := range value {
			out[k] = clonePrivateState(v)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, v := range value {
			out[i] = clonePrivateState(v)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(value))
		maps.Copy(out, value)
		return out
	case []string:
		return append([]string(nil), value...)
	default:
		return private
	}
}

func clonePorts(ports []PortSpec) []PortSpec {
	out := make([]PortSpec, len(ports))
	for i, port := range ports {
		out[i] = port
		out[i].AcceptedTypes = append([]string(nil), port.AcceptedTypes...)
		out[i].Metadata = cloneStringMap(port.Metadata)
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}
