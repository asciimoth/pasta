package pasta

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
)

// Workspace owns libraries, classes, nodes, links, and ID generation.
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
func WithLogger(logger Logger) WorkspaceOption {
	return func(w *Workspace) { w.logger = logger }
}

// NewWorkspace creates an empty workspace.
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
	runtimes := make([]NodeRuntime, 0, len(w.nodes))
	for _, n := range w.nodes {
		n.state = StateInactive
		if n.runtime != nil {
			runtimes = append(runtimes, n.runtime)
		}
	}
	for _, l := range w.links {
		l.state = StateInactive
	}
	w.mu.Unlock()
	w.callAfterInactiveEvents(nodeEvents, InactiveWorkspaceClose)
	w.callLinkInactiveEvents(linkEvents, InactiveWorkspaceClose)
	var first error
	for _, runtime := range runtimes {
		if err := w.callNodeClose(runtime); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// RegisterLibrary registers a library and asks it to define its classes.
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
	w.libraries[name] = lib
	w.mu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			w.logPanic("register library", r)
			_ = w.UnregisterLibrary(name)
			err = opErr("register library", "hook", fmt.Errorf("panic: %v", r))
		}
	}()
	if err := lib.DefineClasses(&libraryScope{w: w, library: name}); err != nil {
		_ = w.UnregisterLibrary(name)
		return opErr("register library", "hook", err)
	}
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
	w.mu.Unlock()
	w.callAfterInactiveEvents(nodeEvents, InactiveLibraryUnregister)
	w.callLinkInactiveEvents(linkEvents, InactiveLibraryUnregister)
	return nil
}

// DefineClass defines or replaces an active class for a registered library.
func (w *Workspace) DefineClass(library string, spec ClassSpec) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.defineClassLocked(library, spec)
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
	w.mu.Unlock()
	w.callAfterInactiveEvents(nodeEvents, InactiveClassRecall)
	w.callLinkInactiveEvents(linkEvents, InactiveClassRecall)
	return nil
}

// CreateNode creates an active node from a registered active class.
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
	if err := validatePorts(inputs, InputPort); err != nil {
		return opErr("set node ports", "validate", err)
	}
	if err := validatePorts(outputs, OutputPort); err != nil {
		return opErr("set node ports", "validate", err)
	}
	node, ok := w.nodes[id]
	if !ok {
		return opErr("set node ports", "validate", ErrNotFound)
	}
	oldInputs, oldOutputs := node.inputs, node.outputs
	node.inputs, node.outputs = clonePorts(inputs), clonePorts(outputs)
	if err := w.validateAttachedLinksLocked(id); err != nil {
		node.inputs, node.outputs = oldInputs, oldOutputs
		return opErr("set node ports", "validate", err)
	}
	return nil
}

// CreateLink creates a directed link from output to input.
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
	w.mu.RLock()
	defer w.mu.RUnlock()
	selected := make(map[NodeID]bool, len(ids))
	var clip Clipboard
	for _, id := range ids {
		node, ok := w.nodes[id]
		if !ok {
			return Clipboard{}, opErr("copy", "validate", ErrNotFound)
		}
		if selected[id] {
			continue
		}
		selected[id] = true
		clip.Nodes = append(clip.Nodes, SaveNode{
			ID:      id.String(),
			Class:   node.class,
			State:   cloneNodeState(node.dynamic),
			Inputs:  clonePorts(node.inputs),
			Outputs: clonePorts(node.outputs),
		})
	}
	sort.Slice(clip.Nodes, func(i, j int) bool { return clip.Nodes[i].ID < clip.Nodes[j].ID })
	for _, link := range w.links {
		if selected[link.input.Node] && selected[link.output.Node] {
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
		node.inputs = clonePorts(saved.Inputs)
		node.outputs = clonePorts(saved.Outputs)
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

// CanCreateLink validates a proposed link without mutating the workspace.
func (w *Workspace) CanCreateLink(input, output FullPortID, typ string) error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	_, err := w.validateLinkLocked(input, output, typ, 0)
	if err != nil {
		return opErr("can create link", "validate", err)
	}
	return nil
}

// Snapshot returns a deterministic defensive copy of the workspace.
func (w *Workspace) Snapshot() Snapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.snapshotLocked()
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

func (w *Workspace) defineClassLocked(library string, spec ClassSpec) error {
	if err := w.checkOpenLocked("define class"); err != nil {
		return err
	}
	if _, ok := w.libraries[library]; !ok {
		return opErr("define class", "validate", ErrNotFound)
	}
	if !ValidClassName(library, spec.Name) {
		return opErr("define class", "validate", ErrInvalidName)
	}
	if err := validatePorts(spec.Inputs, InputPort); err != nil {
		return opErr("define class", "validate", err)
	}
	if err := validatePorts(spec.Outputs, OutputPort); err != nil {
		return opErr("define class", "validate", err)
	}
	w.classes[spec.Name] = &classRecord{spec: cloneClassSpec(spec), library: library, active: true}
	for _, node := range w.nodes {
		if node.class == spec.Name {
			node.inputs = clonePorts(spec.Inputs)
			node.outputs = clonePorts(spec.Outputs)
		}
	}
	w.removeBrokenLinksLocked()
	w.refreshActivityLocked()
	return nil
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
	for _, accepted := range port.AcceptedTypes {
		if accepted == typ {
			return true
		}
	}
	return false
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
	w.removeBrokenLinksLocked()
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

func (w *Workspace) removeBrokenLinksLocked() {
	for id, link := range w.links {
		inNode := w.nodes[link.input.Node]
		outNode := w.nodes[link.output.Node]
		if inNode == nil || outNode == nil {
			delete(w.links, id)
			continue
		}
		if _, ok := findPort(inNode.inputs, link.input.Port); !ok {
			delete(w.links, id)
			continue
		}
		if _, ok := findPort(outNode.outputs, link.output.Port); !ok {
			delete(w.links, id)
		}
	}
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
	for _, class := range w.classes {
		s.Classes = append(s.Classes, ClassSnapshot{
			Spec:    cloneClassSpec(class.spec),
			Library: class.library,
			Active:  class.active,
		})
	}
	sort.Slice(s.Classes, func(i, j int) bool { return s.Classes[i].Spec.Name < s.Classes[j].Spec.Name })
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

type libraryScope struct {
	w       *Workspace
	library string
}

func (s *libraryScope) DefineClass(spec ClassSpec) error {
	return s.w.DefineClass(s.library, spec)
}

func (s *libraryScope) RecallClass(className string) error {
	return s.w.RecallClass(s.library, className)
}

func (s *libraryScope) CreateNode(className string, opts NodeOptions) (NodeID, error) {
	if !ValidClassName(s.library, className) {
		return 0, opErr("scope create node", "validate", ErrOwnership)
	}
	return s.w.CreateNode(className, opts)
}

func (s *libraryScope) DeleteNode(id NodeID) error {
	s.w.mu.RLock()
	node, ok := s.w.nodes[id]
	owned := ok && node.library == s.library
	s.w.mu.RUnlock()
	if !owned {
		return opErr("scope delete node", "validate", ErrOwnership)
	}
	return s.w.DeleteNode(id)
}

func (s *libraryScope) SetNodePrivate(id NodeID, private any) error {
	s.w.mu.RLock()
	node, ok := s.w.nodes[id]
	owned := ok && node.library == s.library
	s.w.mu.RUnlock()
	if !owned {
		return opErr("scope set node private", "validate", ErrOwnership)
	}
	return s.w.SetNodePrivate(id, private)
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

func (s *libraryScope) DeleteLink(id LinkID) error {
	s.w.mu.RLock()
	link, ok := s.w.links[id]
	owned := false
	if ok {
		inNode := s.w.nodes[link.input.Node]
		outNode := s.w.nodes[link.output.Node]
		owned = inNode != nil && outNode != nil && inNode.library == s.library && outNode.library == s.library
	}
	s.w.mu.RUnlock()
	if !owned {
		return opErr("scope delete link", "validate", ErrOwnership)
	}
	return s.w.DeleteLink(id)
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

func (s *nodeScope) SetPorts(inputs, outputs []PortSpec) error {
	err := s.w.SetNodePorts(s.id, inputs, outputs)
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.updateInitRecord(func(rec *nodeRecord) error {
		if err := validatePorts(inputs, InputPort); err != nil {
			return opErr("set node ports", "validate", err)
		}
		if err := validatePorts(outputs, OutputPort); err != nil {
			return opErr("set node ports", "validate", err)
		}
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

func cloneNodeState(state NodeState) NodeState {
	state.Metadata = cloneStringMap(state.Metadata)
	return state
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
	for k, v := range in {
		out[k] = v
	}
	return out
}
