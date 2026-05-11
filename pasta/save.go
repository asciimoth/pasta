package pasta

import "sort"

// SaveData is the deterministic JSON-like persistence shape for a workspace.
type SaveData struct {
	NextNode int64      `json:"nextNode"`
	NextLink int64      `json:"nextLink"`
	Nodes    []SaveNode `json:"nodes"`
	Links    []SaveLink `json:"links"`
}

// SaveNode is a persisted node record.
type SaveNode struct {
	ID      string     `json:"id"`
	Class   string     `json:"class"`
	State   NodeState  `json:"state"`
	Inputs  []PortSpec `json:"inputs"`
	Outputs []PortSpec `json:"outputs"`
}

// SaveLink is a persisted link record.
type SaveLink struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Waypoints []string `json:"waypoints,omitempty"`
}

// Save returns deterministic workspace data suitable for JSON/config storage.
func (w *Workspace) Save() SaveData {
	w.mu.RLock()
	defer w.mu.RUnlock()
	data := SaveData{
		NextNode: int64(w.nextNode),
		NextLink: int64(w.nextLink),
	}
	for _, node := range w.nodes {
		data.Nodes = append(data.Nodes, SaveNode{
			ID:      node.id.String(),
			Class:   node.class,
			State:   cloneNodeState(node.dynamic),
			Inputs:  clonePorts(node.inputs),
			Outputs: clonePorts(node.outputs),
		})
	}
	sort.Slice(data.Nodes, func(i, j int) bool { return data.Nodes[i].ID < data.Nodes[j].ID })
	for _, link := range w.links {
		data.Links = append(data.Links, SaveLink{
			Name:      FullLinkName{Link: link.id, Input: link.input, Output: link.output}.String(),
			Type:      link.typ,
			Waypoints: append([]string(nil), link.waypoints...),
		})
	}
	sort.Slice(data.Links, func(i, j int) bool { return data.Links[i].Name < data.Links[j].Name })
	return data
}

// Restore replaces workspace model state from SaveData.
//
// Registered libraries and classes are preserved. Nodes whose classes are not
// currently active are restored as inactive. Broken links are skipped.
func (w *Workspace) Restore(data SaveData) error {
	w.mu.Lock()
	locked := true
	defer func() {
		if locked {
			w.mu.Unlock()
		}
	}()
	if err := w.checkOpenLocked("restore"); err != nil {
		return err
	}
	nodes := make(map[NodeID]*nodeRecord)
	links := make(map[LinkID]*linkRecord)
	var maxNode NodeID
	var maxLink LinkID
	for _, saved := range data.Nodes {
		id, err := ParseNodeID(saved.ID)
		if err != nil {
			return opErr("restore", "validate", err)
		}
		if _, exists := nodes[id]; exists {
			return opErr("restore", "validate", ErrDuplicate)
		}
		class := w.classes[saved.Class]
		library := ""
		state := StateInactive
		if class != nil {
			library = class.library
			if class.active {
				state = StateActive
			}
		}
		inputs := saved.Inputs
		outputs := saved.Outputs
		if len(inputs) == 0 && class != nil {
			inputs = class.spec.Inputs
		}
		if len(outputs) == 0 && class != nil {
			outputs = class.spec.Outputs
		}
		if err := validatePorts(inputs, InputPort); err != nil {
			return opErr("restore", "validate", err)
		}
		if err := validatePorts(outputs, OutputPort); err != nil {
			return opErr("restore", "validate", err)
		}
		nodes[id] = &nodeRecord{
			id:      id,
			class:   saved.Class,
			library: library,
			state:   state,
			dynamic: cloneNodeState(saved.State),
			inputs:  clonePorts(inputs),
			outputs: clonePorts(outputs),
		}
		if id > maxNode {
			maxNode = id
		}
	}
	oldNodes, oldLinks := w.nodes, w.links
	oldNextNode, oldNextLink := w.nextNode, w.nextLink
	w.nodes, w.links = nodes, links
	for _, saved := range data.Links {
		full, err := ParseFullLinkName(saved.Name)
		if err != nil {
			w.nodes, w.links = oldNodes, oldLinks
			w.nextNode, w.nextLink = oldNextNode, oldNextLink
			return opErr("restore", "validate", err)
		}
		if _, err := w.validateLinkLocked(full.Input, full.Output, saved.Type, full.Link); err != nil {
			continue
		}
		w.links[full.Link] = &linkRecord{
			id:        full.Link,
			input:     full.Input,
			output:    full.Output,
			typ:       saved.Type,
			state:     StateActive,
			waypoints: append([]string(nil), saved.Waypoints...),
		}
		if full.Link > maxLink {
			maxLink = full.Link
		}
	}
	w.nextNode = NodeID(data.NextNode)
	if w.nextNode <= maxNode {
		w.nextNode = maxNode + 1
	}
	w.nextLink = LinkID(data.NextLink)
	if w.nextLink <= maxLink {
		w.nextLink = maxLink + 1
	}
	if w.nextNode <= 0 {
		w.nextNode = 1
	}
	if w.nextLink <= 0 {
		w.nextLink = 1
	}
	w.refreshActivityLocked()
	initNodes := make([]restoreInitNode, 0, len(w.nodes))
	for _, node := range w.nodes {
		class := w.classes[node.class]
		if node.state == StateActive && class != nil && class.spec.Runtime != nil {
			initNodes = append(initNodes, restoreInitNode{record: node, class: class.spec.Runtime})
		}
	}
	w.sortRestoreInitNodesLocked(initNodes)
	w.mu.Unlock()
	locked = false

	runtimes := make(map[NodeID]NodeRuntime, len(initNodes))
	scopes := make(map[NodeID]*nodeScope, len(initNodes))
	for _, initNode := range initNodes {
		runtime, scope, err := w.initNodeRuntime(initNode.class, initNode.record, InitRestore)
		if err != nil {
			for _, scope := range scopes {
				if scope != nil {
					scope.finishInit()
				}
			}
			w.mu.Lock()
			locked = true
			w.nodes, w.links = oldNodes, oldLinks
			w.nextNode, w.nextLink = oldNextNode, oldNextLink
			w.mu.Unlock()
			locked = false
			return err
		}
		runtimes[initNode.record.id] = runtime
		scopes[initNode.record.id] = scope
	}

	w.mu.Lock()
	locked = true
	if err := w.checkOpenLocked("restore"); err != nil {
		for _, scope := range scopes {
			if scope != nil {
				scope.finishInit()
			}
		}
		w.nodes, w.links = oldNodes, oldLinks
		w.nextNode, w.nextLink = oldNextNode, oldNextLink
		w.mu.Unlock()
		locked = false
		return err
	}
	for id, runtime := range runtimes {
		if node := w.nodes[id]; node != nil {
			node.runtime = runtime
		}
		if scope := scopes[id]; scope != nil {
			scope.finishInit()
		}
	}
	w.mu.Unlock()
	locked = false
	return nil
}

type restoreInitNode struct {
	record *nodeRecord
	class  NodeClass
}

func (w *Workspace) sortRestoreInitNodesLocked(nodes []restoreInitNode) {
	remaining := make(map[NodeID]restoreInitNode, len(nodes))
	for _, node := range nodes {
		remaining[node.record.id] = node
	}
	ordered := nodes[:0]
	for len(remaining) > 0 {
		var picked NodeID
		for id := range remaining {
			if picked != 0 && id >= picked {
				continue
			}
			if !w.hasOutgoingLinkToRemainingLocked(id, remaining) {
				picked = id
			}
		}
		if picked == 0 {
			for id := range remaining {
				if picked == 0 || id < picked {
					picked = id
				}
			}
		}
		ordered = append(ordered, remaining[picked])
		delete(remaining, picked)
	}
	copy(nodes, ordered)
}

func (w *Workspace) hasOutgoingLinkToRemainingLocked(id NodeID, remaining map[NodeID]restoreInitNode) bool {
	for _, link := range w.links {
		if link.output.Node == id {
			if _, ok := remaining[link.input.Node]; ok {
				return true
			}
		}
	}
	return false
}
