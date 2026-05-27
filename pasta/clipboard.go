package pasta

import (
	"encoding/json"
	"errors"
	"slices"

	"github.com/asciimoth/configer/configer"
)

type clipboardPayload struct {
	Version int             `json:"version"`
	Nodes   []clipboardNode `json:"nodes"`
	Links   []clipboardLink `json:"links"`
}

type clipboardNode struct {
	ID          uint64          `json:"id"`
	Class       string          `json:"class"`
	Name        string          `json:"name"`
	PrimaryType string          `json:"primary_type,omitempty"`
	Label       string          `json:"label,omitempty"`
	Position    string          `json:"position,omitempty"`
	Root        bool            `json:"root,omitempty"`
	Placeholder bool            `json:"placeholder,omitempty"`
	LeftPorts   []clipboardPort `json:"left_ports,omitempty"`
	RightPorts  []clipboardPort `json:"right_ports,omitempty"`
	Config      any             `json:"config,omitempty"`
}

type clipboardPort struct {
	ID    uint64   `json:"id"`
	Name  string   `json:"name"`
	Types []string `json:"types"`
}

type clipboardLink struct {
	ID            uint64 `json:"id"`
	Type          string `json:"type"`
	LeftPort      uint64 `json:"left_port"`
	LeftPortNode  uint64 `json:"left_port_node"`
	RightPort     uint64 `json:"right_port"`
	RightPortNode uint64 `json:"right_port_node"`
}

// Copy serializes the selected nodes and links between them into an opaque
// clipboard string. Missing node IDs are ignored.
func (w *Workspace) Copy(ids []uint64) string {
	w.Lock()
	defer w.Unlock()
	if w.closed {
		return ""
	}

	selected := make(map[uint64]struct{}, len(ids))
	payload := clipboardPayload{Version: 1}
	for _, id := range ids {
		if _, seen := selected[id]; seen {
			continue
		}
		record, present := w.nodes.Get(id)
		if !present || record == nil {
			continue
		}
		selected[id] = struct{}{}
		node := clipboardNode{
			ID:          id,
			Class:       record.Class,
			Name:        record.Name,
			PrimaryType: record.PrimaryType,
			Label:       record.Label,
			Position:    record.Position,
			Root:        record.Root,
			Placeholder: record.Node == nil,
			LeftPorts:   w.copyClipboardPorts(record.LeftPorts),
			RightPorts:  w.copyClipboardPorts(record.RightPorts),
		}
		if record.Node != nil {
			cfg := configer.NewMemory(map[string]any{})
			if err := record.OnSave(cfg); err != nil {
				return ""
			}
			node.Config = cfg.Snapshot()
		}
		payload.Nodes = append(payload.Nodes, node)
	}
	if len(payload.Nodes) == 0 {
		return ""
	}

	for pair := w.links.Oldest(); pair != nil; pair = pair.Next() {
		link := pair.Value
		if link == nil {
			continue
		}
		if _, ok := selected[link.LeftPortNode]; !ok {
			continue
		}
		if _, ok := selected[link.RightPortNode]; !ok {
			continue
		}
		payload.Links = append(payload.Links, clipboardLink{
			ID:            pair.Key,
			Type:          link.Type,
			LeftPort:      link.LeftPort,
			LeftPortNode:  link.LeftPortNode,
			RightPort:     link.RightPort,
			RightPortNode: link.RightPortNode,
		})
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(data)
}

func (w *Workspace) copyClipboardPorts(ids []uint64) []clipboardPort {
	ports := make([]clipboardPort, 0, len(ids))
	for _, id := range ids {
		port, present := w.ports.Get(id)
		if !present || port == nil {
			continue
		}
		ports = append(ports, clipboardPort{
			ID:    id,
			Name:  port.Name,
			Types: port.CopyTypes(),
		})
	}
	return ports
}

// Paste restores a clipboard string as new nodes and returns their new IDs.
// Invalid clipboard data and individual node/link restore failures are ignored.
func (w *Workspace) Paste(data string) []uint64 {
	var payload clipboardPayload
	if err := json.Unmarshal([]byte(data), &payload); err != nil || payload.Version != 1 {
		return nil
	}

	w.Lock()
	if w.closed {
		w.Unlock()
		return nil
	}
	w.undoRecordingDisabled += 1
	w.Unlock()

	nodeIDs := make(map[uint64]uint64, len(payload.Nodes))
	portIDs := make(map[uint64]uint64)
	pasted := make([]uint64, 0, len(payload.Nodes))
	pastedLinks := make([]uint64, 0, len(payload.Links))
	for _, node := range payload.Nodes {
		id, ports, ok := w.pasteClipboardNode(node)
		if !ok {
			continue
		}
		nodeIDs[node.ID] = id
		pasted = append(pasted, id)
		for oldID, newID := range ports {
			portIDs[oldID] = newID
		}
	}

	for _, link := range payload.Links {
		if nodeIDs[link.LeftPortNode] == 0 || nodeIDs[link.RightPortNode] == 0 {
			continue
		}
		left := portIDs[link.LeftPort]
		right := portIDs[link.RightPort]
		if left == 0 || right == 0 {
			continue
		}
		if id := w.addClipboardLink(left, right, link.Type); id > 0 {
			pastedLinks = append(pastedLinks, id)
		}
	}

	w.Lock()
	w.undoRecordingDisabled -= 1
	w.pushUndoEntry(pasteUndoEntry(pasted, pastedLinks))
	w.Unlock()
	return pasted
}

func pasteUndoEntry(nodes, links []uint64) undoEntry {
	if len(nodes) == 0 && len(links) == 0 {
		return nil
	}
	entries := make([]undoEntry, 0, len(nodes)+len(links))
	for _, id := range links {
		entries = append(entries, undoAddedLink{ID: id})
	}
	for i := len(nodes) - 1; i >= 0; i-- {
		entries = append(entries, undoAddedNode{ID: nodes[i]})
	}
	return undoGroup{Entries: entries}
}

func (w *Workspace) pasteClipboardNode(node clipboardNode) (uint64, map[uint64]uint64, bool) {
	if err := ValidateClassName(node.Class); err != nil {
		return 0, nil, false
	}
	if node.PrimaryType != "" {
		if err := ValidateTypeName(node.PrimaryType); err != nil {
			return 0, nil, false
		}
	}

	state, ok := node.clipboardState()
	if !ok {
		return 0, nil, false
	}
	name, ok := w.clipboardNodeName(node.Class, node.Name)
	if !ok {
		return 0, nil, false
	}
	state.Name = name

	var (
		id  uint64
		err error
	)
	if !node.Placeholder {
		if impl, handled, failed := w.newClipboardNode(node, &state); failed {
			return 0, nil, false
		} else if handled && impl != nil {
			ports := append([]Port{}, state.LeftPorts...)
			ports = append(ports, state.RightPorts...)
			initData := &NodeInitData{PrimaryType: state.PrimaryType}
			w.Lock()
			id, err = w.addNodeLocked(impl, node.Class, state.Root, state.Name, state.PrimaryType, ports, initData, true, true)
			w.Unlock()
			if err != nil {
				if id > 0 {
					w.RemoveNode(id)
				}
				return 0, nil, false
			}
		}
	}
	if id == 0 {
		ports := append([]Port{}, state.LeftPorts...)
		ports = append(ports, state.RightPorts...)
		id, err = w.AddPlaceholderNodeWithRoot(node.Class, state.Root, ports, state.Name)
		if err != nil {
			return 0, nil, false
		}
		if state.PrimaryType != "" {
			_ = w.SetNodePrimary(id, state.PrimaryType)
		}
	}
	_ = w.SetNodeLabel(id, state.Label)
	_ = w.SetNodePosition(id, node.Position)

	snapshot, ok := w.NodeSnapshot(id)
	if !ok {
		return 0, nil, false
	}
	return id, w.clipboardPortIDMap(node, snapshot), true
}

func (node clipboardNode) clipboardState() (NodeClassState, bool) {
	state := NodeClassState{
		Root:        node.Root,
		PrimaryType: node.PrimaryType,
		Name:        node.Name,
		Label:       node.Label,
		LeftPorts:   make([]Port, 0, len(node.LeftPorts)),
		RightPorts:  make([]Port, 0, len(node.RightPorts)),
	}
	for _, port := range node.LeftPorts {
		if port.ID == 0 {
			return NodeClassState{}, false
		}
		state.LeftPorts = append(state.LeftPorts, Port{
			Direction: "left",
			Name:      port.Name,
			Types:     slices.Clone(port.Types),
		})
	}
	for _, port := range node.RightPorts {
		if port.ID == 0 {
			return NodeClassState{}, false
		}
		state.RightPorts = append(state.RightPorts, Port{
			Direction: "right",
			Name:      port.Name,
			Types:     slices.Clone(port.Types),
		})
	}
	return state, true
}

func (w *Workspace) clipboardPortIDMap(node clipboardNode, snapshot NodeSnapshot) map[uint64]uint64 {
	mapped := make(map[uint64]uint64, len(node.LeftPorts)+len(node.RightPorts))
	w.Lock()
	defer w.Unlock()
	w.mapClipboardPortsByNameLocked(mapped, node.LeftPorts, snapshot.LeftPorts)
	w.mapClipboardPortsByNameLocked(mapped, node.RightPorts, snapshot.RightPorts)
	return mapped
}

func (w *Workspace) mapClipboardPortsByNameLocked(mapped map[uint64]uint64, old []clipboardPort, current []uint64) {
	byName := make(map[string]uint64, len(current))
	for _, id := range current {
		port, present := w.ports.Get(id)
		if present && port != nil {
			byName[port.Name] = id
		}
	}
	for _, port := range old {
		if id := byName[port.Name]; id > 0 {
			mapped[port.ID] = id
		}
	}
}

func (w *Workspace) clipboardNodeName(class, name string) (string, bool) {
	w.Lock()
	defer w.Unlock()
	if w.closed {
		return "", false
	}
	if w.nodeClassIsUniqueLocked(class) {
		if err := w.rejectUniqueNodeDuplicateLocked(class, 0); err != nil {
			return "", false
		}
	}
	if name != "" && ValidateNodeName(name) == nil && w.nodeNameAvailableLocked(name, 0) {
		return name, true
	}
	return w.generateNodeNameLocked(w.nextid, class, 0), true
}

func (w *Workspace) newClipboardNode(node clipboardNode, state *NodeClassState) (impl Node, handled bool, failed bool) {
	w.Lock()
	class, present := w.classes.Get(node.Class)
	w.Unlock()
	if !present || class == nil {
		return nil, false, false
	}
	factory, ok := class.(NodeClassFactory)
	if !ok {
		return nil, false, false
	}

	cfg := configer.NewMemory(map[string]any{})
	if object, ok := node.Config.(map[string]any); ok {
		cfg = configer.NewMemory(object)
	}
	defer func() {
		if recover() != nil {
			impl = nil
			handled = true
			failed = true
		}
	}()
	impl, err := factory.NewNode(cfg, state)
	if err != nil {
		return nil, true, true
	}
	return impl, true, false
}

func (w *Workspace) addClipboardLink(pa, pb uint64, typ string) uint64 {
	w.Lock()
	defer w.Unlock()
	if w.closed || typ == "" {
		return 0
	}
	if err := ValidateTypeName(typ); err != nil {
		return 0
	}

	portA, present := w.ports.Get(pa)
	if !present || portA == nil {
		return 0
	}
	portB, present := w.ports.Get(pb)
	if !present || portB == nil {
		return 0
	}
	if portA.Node == portB.Node || portA.Direction == portB.Direction {
		return 0
	}

	left, right := portA, portB
	if portA.Direction == "right" {
		left, right = portB, portA
	}
	if w.portsConnected(left, right) || !portSupportsClipboardType(left, typ) || !portSupportsClipboardType(right, typ) {
		return 0
	}

	leftNode, present := w.nodes.Get(left.Node)
	if !present || leftNode == nil {
		return 0
	}
	rightNode, present := w.nodes.Get(right.Node)
	if !present || rightNode == nil {
		return 0
	}
	placeholder := leftNode.Node == nil || rightNode.Node == nil
	if !placeholder {
		if rejection := leftNode.PreLinkAdd(left.ID, typ, left.Direction); rejection != nil {
			if errors.Is(rejection, ErrNodePanic) {
				w.failNodeLocked(leftNode.ID, "PreLinkAdd", rejection, true, true)
			}
			return 0
		}
		if rejection := rightNode.PreLinkAdd(right.ID, typ, right.Direction); rejection != nil {
			if errors.Is(rejection, ErrNodePanic) {
				w.failNodeLocked(rightNode.ID, "PreLinkAdd", rejection, true, true)
			}
			return 0
		}
	}

	link := Link{
		ID:            w.nextIDLocked(),
		Type:          typ,
		Placeholder:   placeholder,
		LeftPort:      left.ID,
		LeftPortNode:  leftNode.ID,
		RightPort:     right.ID,
		RightPortNode: rightNode.ID,
	}
	w.links.Set(link.ID, &link)
	if !w.verifyDAG() {
		w.links.Delete(link.ID)
		return 0
	}
	if !link.Placeholder {
		if err := leftNode.OnLinkAdd(link.ID, left.ID, link.Type, left.Direction); err != nil {
			w.links.Delete(link.ID)
			w.failNodeLocked(leftNode.ID, "OnLinkAdd", err, true, true)
			return 0
		}
		if err := rightNode.OnLinkAdd(link.ID, right.ID, link.Type, right.Direction); err != nil {
			w.links.Delete(link.ID)
			w.failNodeLocked(rightNode.ID, "OnLinkAdd", err, true, true)
			w.nodeEvLinkRemoved(leftNode.ID, link.ID, left.ID, link.Type, left.Direction)
			return 0
		}
	}

	left.Links = append(left.Links, link.ID)
	right.Links = append(right.Links, link.ID)
	w.enqueueLinkNotification(NotificationLinkAdded, link.ID, linkSnapshot(&link))
	w.enqueuePortNotification(NotificationPortUpdated, left.ID, portSnapshot(left))
	w.enqueuePortNotification(NotificationPortUpdated, right.ID, portSnapshot(right))
	w.recomputeRootPaths(true)
	w.pushUndoEntry(undoAddedLink{ID: link.ID})
	return link.ID
}

func portSupportsClipboardType(port *Port, typ string) bool {
	return slices.Contains(port.Types, AnyType) || slices.Contains(port.Types, typ)
}
