package pasta

import "slices"

// WorkspaceSnapshot is a JSON-serializable copy of a workspace graph.
//
// Nodes, ports, and links are keyed by their workspace-scoped IDs. Individual
// snapshot values do not include their own IDs.
type WorkspaceSnapshot struct {
	Nodes map[uint64]NodeSnapshot `json:"nodes"`
	Ports map[uint64]PortSnapshot `json:"ports"`
	Links map[uint64]LinkSnapshot `json:"links"`
}

// NodeSnapshot is a JSON-serializable copy of node metadata and port IDs.
type NodeSnapshot struct {
	Class       string   `json:"class"`
	PrimaryType string   `json:"primary_type"`
	Label       string   `json:"label"`
	Placeholder bool     `json:"placeholder"`
	Root        bool     `json:"root"`
	HasRootPath bool     `json:"has_root_path"`
	LeftPorts   []uint64 `json:"left_ports"`
	RightPorts  []uint64 `json:"right_ports"`
}

// PortSnapshot is a JSON-serializable copy of a port.
type PortSnapshot struct {
	Direction string   `json:"direction"`
	Node      uint64   `json:"node"`
	Name      string   `json:"name"`
	Types     []string `json:"types"`
	Links     []uint64 `json:"links"`
}

// LinkSnapshot is a JSON-serializable copy of a link.
type LinkSnapshot struct {
	Type          string `json:"type"`
	Placeholder   bool   `json:"placeholder"`
	LeftPort      uint64 `json:"left_port"`
	LeftPortNode  uint64 `json:"left_port_node"`
	RightPort     uint64 `json:"right_port"`
	RightPortNode uint64 `json:"right_port_node"`
}

// Snapshot returns a JSON-serializable copy of all nodes, ports, and links.
func (w *Workspace) Snapshot() WorkspaceSnapshot {
	w.Lock()
	defer w.Unlock()
	if w.closed {
		return WorkspaceSnapshot{}
	}

	return w.snapshotLocked()
}

func (w *Workspace) snapshotLocked() WorkspaceSnapshot {
	snapshot := WorkspaceSnapshot{
		Nodes: make(map[uint64]NodeSnapshot, w.nodes.Len()),
		Ports: make(map[uint64]PortSnapshot, w.ports.Len()),
		Links: make(map[uint64]LinkSnapshot, w.links.Len()),
	}

	for pair := w.nodes.Oldest(); pair != nil; pair = pair.Next() {
		if pair.Value == nil {
			continue
		}
		snapshot.Nodes[pair.Key] = nodeSnapshot(pair.Value)
	}
	for pair := w.ports.Oldest(); pair != nil; pair = pair.Next() {
		if pair.Value == nil {
			continue
		}
		snapshot.Ports[pair.Key] = portSnapshot(pair.Value)
	}
	for pair := w.links.Oldest(); pair != nil; pair = pair.Next() {
		if pair.Value == nil {
			continue
		}
		snapshot.Links[pair.Key] = linkSnapshot(pair.Value)
	}

	return snapshot
}

// NodeSnapshot returns a JSON-serializable copy of one node.
func (w *Workspace) NodeSnapshot(id uint64) (NodeSnapshot, bool) {
	w.Lock()
	defer w.Unlock()

	record, present := w.nodes.Get(id)
	if w.closed || !present || record == nil {
		return NodeSnapshot{}, false
	}
	return nodeSnapshot(record), true
}

// PortSnapshot returns a JSON-serializable copy of one port.
func (w *Workspace) PortSnapshot(id uint64) (PortSnapshot, bool) {
	w.Lock()
	defer w.Unlock()

	port, present := w.ports.Get(id)
	if w.closed || !present || port == nil {
		return PortSnapshot{}, false
	}
	return portSnapshot(port), true
}

// LinkSnapshot returns a JSON-serializable copy of one link.
func (w *Workspace) LinkSnapshot(id uint64) (LinkSnapshot, bool) {
	w.Lock()
	defer w.Unlock()

	link, present := w.links.Get(id)
	if w.closed || !present || link == nil {
		return LinkSnapshot{}, false
	}
	return linkSnapshot(link), true
}

func nodeSnapshot(record *nodeRecord) NodeSnapshot {
	if record.Node == nil {
		return NodeSnapshot{
			Class:       record.Class,
			Label:       record.Label,
			Placeholder: true,
			LeftPorts:   slices.Clone(record.LeftPorts),
			RightPorts:  slices.Clone(record.RightPorts),
		}
	}
	return NodeSnapshot{
		Class:       record.Class,
		PrimaryType: record.PrimaryType,
		Label:       record.Label,
		Root:        record.Root,
		HasRootPath: record.HasRootPath,
		LeftPorts:   slices.Clone(record.LeftPorts),
		RightPorts:  slices.Clone(record.RightPorts),
	}
}

func portSnapshot(port *Port) PortSnapshot {
	return PortSnapshot{
		Direction: port.Direction,
		Node:      port.Node,
		Name:      port.Name,
		Types:     slices.Clone(port.Types),
		Links:     slices.Clone(port.Links),
	}
}

func linkSnapshot(link *Link) LinkSnapshot {
	return LinkSnapshot{
		Type:          link.Type,
		Placeholder:   link.Placeholder,
		LeftPort:      link.LeftPort,
		LeftPortNode:  link.LeftPortNode,
		RightPort:     link.RightPort,
		RightPortNode: link.RightPortNode,
	}
}
