package pasta

import "github.com/asciimoth/configer/configer"

const undoLogLimit = 64

type undoEntry interface{}

type undoAddedNode struct {
	ID uint64
}

type undoRemovedNode struct {
	ID       uint64
	Name     string
	Root     bool
	Config   map[string]any
	Ports    []undoPort
	Links    []undoLink
	Position string
	NextNode uint64
}

type undoPort struct {
	ID       uint64
	Snapshot PortSnapshot
}

type undoLink struct {
	ID         uint64
	Snapshot   LinkSnapshot
	LeftIndex  int
	RightIndex int
}

type undoAddedLink struct {
	ID uint64
}

type undoRemovedLink struct {
	ID         uint64
	Link       LinkSnapshot
	LeftIndex  int
	RightIndex int
}

// Undo rolls back the latest undoable node/link add or remove operation.
//
// Failed rollback entries are silently dropped.
func (w *Workspace) Undo() {
	w.applyHistory(true)
}

// Redo reapplies the latest operation rolled back by Undo.
//
// Failed rollback entries are silently dropped.
func (w *Workspace) Redo() {
	w.applyHistory(false)
}

func (w *Workspace) applyHistory(undo bool) {
	w.Lock()
	if w.closed {
		w.Unlock()
		return
	}
	var entry undoEntry
	if undo {
		if len(w.undoLog) == 0 {
			w.Unlock()
			return
		}
		entry = w.undoLog[len(w.undoLog)-1]
		w.undoLog = w.undoLog[:len(w.undoLog)-1]
	} else {
		if len(w.redoLog) == 0 {
			w.Unlock()
			return
		}
		entry = w.redoLog[len(w.redoLog)-1]
		w.redoLog = w.redoLog[:len(w.redoLog)-1]
	}
	w.undoRecordingDisabled += 1
	w.Unlock()

	mirror, ok := w.applyUndoEntry(entry)

	w.Lock()
	w.undoRecordingDisabled -= 1
	if ok {
		if undo {
			w.redoLog = appendLimitedUndo(w.redoLog, mirror)
		} else {
			w.undoLog = appendLimitedUndo(w.undoLog, mirror)
		}
	}
	w.Unlock()
}

func (w *Workspace) pushUndoEntry(entry undoEntry) {
	if entry == nil || w.undoRecordingDisabled > 0 {
		return
	}
	w.undoLog = appendLimitedUndo(w.undoLog, entry)
	w.redoLog = w.redoLog[:0]
}

func appendLimitedUndo(log []undoEntry, entry undoEntry) []undoEntry {
	if entry == nil {
		return log
	}
	if len(log) >= undoLogLimit {
		copy(log, log[1:])
		log[len(log)-1] = entry
		return log
	}
	return append(log, entry)
}

func uniqueUndoIDs(ids []uint64) bool {
	seen := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if id < 1 {
			return false
		}
		if _, ok := seen[id]; ok {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

func (w *Workspace) applyUndoEntry(entry undoEntry) (undoEntry, bool) {
	switch e := entry.(type) {
	case undoAddedNode:
		removed := w.captureNodeByID(e.ID)
		if removed == nil {
			return nil, false
		}
		w.RemoveNode(e.ID)
		if _, ok := w.NodeSnapshot(e.ID); ok {
			return nil, false
		}
		return removed, true
	case undoRemovedNode:
		if !w.restoreUndoNode(e) {
			return nil, false
		}
		return undoAddedNode{ID: e.ID}, true
	case undoAddedLink:
		removed := w.captureLinkByID(e.ID)
		if removed == nil {
			return nil, false
		}
		w.RemoveLink(e.ID)
		if _, ok := w.LinkSnapshot(e.ID); ok {
			return nil, false
		}
		return removed, true
	case undoRemovedLink:
		if !w.restoreUndoLink(e) {
			return nil, false
		}
		return undoAddedLink{ID: e.ID}, true
	default:
		return nil, false
	}
}

func (w *Workspace) captureNodeByID(id uint64) undoEntry {
	w.Lock()
	defer w.Unlock()
	record, ok := w.nodes.Get(id)
	if w.closed || !ok || record == nil {
		return nil
	}
	return w.undoRemovedNodeEntry(id, record)
}

func (w *Workspace) undoRemovedNodeEntry(id uint64, record *nodeRecord) undoEntry {
	if record == nil {
		return nil
	}
	cfg := configer.NewMemory(map[string]any{})
	nodeCfg := cfg.View(configer.Path{record.Name})
	if record.Node != nil {
		if err := record.OnSave(nodeCfg); err != nil {
			return nil
		}
	}
	if err := saveWorkspaceNodeState(w, nodeCfg, record); err != nil {
		return nil
	}
	root, _ := cfg.Snapshot().(map[string]any)
	nodeObject, _ := root[record.Name].(map[string]any)
	entry := undoRemovedNode{
		ID:       id,
		Name:     record.Name,
		Root:     record.Root,
		Config:   cloneAnyMap(nodeObject),
		Position: record.Position,
	}
	if pair := w.nodes.GetPair(id); pair != nil {
		if next := pair.Next(); next != nil {
			entry.NextNode = next.Key
		}
	}
	for _, portID := range record.LeftPorts {
		if port, ok := w.ports.Get(portID); ok && port != nil {
			entry.Ports = append(entry.Ports, undoPort{ID: portID, Snapshot: portSnapshot(port)})
		}
	}
	for _, portID := range record.RightPorts {
		if port, ok := w.ports.Get(portID); ok && port != nil {
			entry.Ports = append(entry.Ports, undoPort{ID: portID, Snapshot: portSnapshot(port)})
		}
	}
	for _, link := range w.linksForNode(id) {
		if link != nil {
			entry.Links = append(entry.Links, undoLink{
				ID:         link.ID,
				Snapshot:   linkSnapshot(link),
				LeftIndex:  linkIndex(w, link.LeftPort, link.ID),
				RightIndex: linkIndex(w, link.RightPort, link.ID),
			})
		}
	}
	return entry
}

func (w *Workspace) captureLinkByID(id uint64) undoEntry {
	w.Lock()
	defer w.Unlock()
	link, ok := w.links.Get(id)
	if w.closed || !ok || link == nil {
		return nil
	}
	return undoRemovedLink{
		ID:         id,
		Link:       linkSnapshot(link),
		LeftIndex:  linkIndex(w, link.LeftPort, id),
		RightIndex: linkIndex(w, link.RightPort, id),
	}
}

func (w *Workspace) restoreUndoNode(entry undoRemovedNode) bool {
	if entry.ID < 1 || entry.Name == "" {
		return false
	}
	cfg := configer.NewMemory(map[string]any{entry.Name: cloneAnyMap(entry.Config)})
	nodes, _ := parseRestoreConfig(cfg.Snapshot())
	node := nodes[entry.Name]
	if node == nil {
		return false
	}
	node.position = entry.Position

	ports := make([]Port, 0, len(entry.Ports))
	portIDs := make([]uint64, 0, len(entry.Ports))
	for _, saved := range entry.Ports {
		ports = append(ports, Port{
			Direction: saved.Snapshot.Direction,
			Node:      entry.ID,
			Name:      saved.Snapshot.Name,
			Types:     append([]string{}, saved.Snapshot.Types...),
		})
		portIDs = append(portIDs, saved.ID)
	}

	var (
		class NodeClass
	)
	w.Lock()
	if w.closed || !w.idAvailableLocked(entry.ID) || !uniqueUndoIDs(portIDs) {
		w.Unlock()
		return false
	}
	for _, id := range portIDs {
		if !w.idAvailableLocked(id) {
			w.Unlock()
			return false
		}
	}
	if c, ok := w.classes.Get(node.class); ok && c != nil {
		class = c
	}
	if err := w.rejectUniqueNodeDuplicateLocked(node.class, 0); err != nil {
		w.Unlock()
		return false
	}
	w.Unlock()

	state := NodeClassState{
		Root:        entry.Root,
		PrimaryType: node.primary,
		Name:        entry.Name,
	}
	if !node.hasPrimary {
		if class != nil {
			state.PrimaryType = class.DefaultNodeParams().PrimaryType
		}
	}
	for _, port := range ports {
		if port.Direction == "left" {
			state.LeftPorts = append(state.LeftPorts, port)
		} else {
			state.RightPorts = append(state.RightPorts, port)
		}
	}

	var restoredID uint64
	if factory, ok := class.(NodeClassFactory); ok {
		impl, err := factory.NewNode(cfg.View(configer.Path{entry.Name}), &state)
		if err != nil {
			return false
		}
		if impl != nil {
			initData := &NodeInitData{PrimaryType: state.PrimaryType}
			w.Lock()
			restoredID, err = w.addNodeLockedWithIDs(impl, node.class, entry.Root, entry.Name, state.PrimaryType, ports, portIDs, entry.ID, initData, true, true)
			w.Unlock()
			if err != nil {
				return false
			}
		}
	}
	if restoredID == 0 {
		var err error
		restoredID, err = w.addPlaceholderNodeWithRoot(node.class, entry.Root, ports, portIDs, entry.ID, entry.Name)
		if err != nil {
			return false
		}
		if state.PrimaryType != "" {
			_ = w.SetNodePrimary(restoredID, state.PrimaryType)
		}
	}
	if restoredID != entry.ID {
		return false
	}
	if entry.Position != "" {
		_ = w.SetNodePosition(entry.ID, entry.Position)
	}
	w.Lock()
	if entry.NextNode > 0 {
		_ = w.nodes.MoveBefore(entry.ID, entry.NextNode)
	}
	w.Unlock()
	for _, link := range entry.Links {
		if !w.restoreUndoLink(undoRemovedLink{
			ID:         link.ID,
			Link:       link.Snapshot,
			LeftIndex:  link.LeftIndex,
			RightIndex: link.RightIndex,
		}) {
			continue
		}
	}
	return true
}

func (w *Workspace) restoreUndoLink(entry undoRemovedLink) bool {
	w.Lock()
	defer w.Unlock()
	if w.closed || !w.idAvailableLocked(entry.ID) {
		return false
	}
	left, leftOK := w.ports.Get(entry.Link.LeftPort)
	right, rightOK := w.ports.Get(entry.Link.RightPort)
	if !leftOK || !rightOK || left == nil || right == nil {
		return false
	}
	if left.Node != entry.Link.LeftPortNode || right.Node != entry.Link.RightPortNode {
		return false
	}
	if w.portsConnected(left, right) {
		return false
	}
	if !portsSupportLinkType(left, right, entry.Link.Type) {
		return false
	}
	leftNode, leftNodeOK := w.nodes.Get(entry.Link.LeftPortNode)
	rightNode, rightNodeOK := w.nodes.Get(entry.Link.RightPortNode)
	if !leftNodeOK || !rightNodeOK || leftNode == nil || rightNode == nil {
		return false
	}
	placeholder := leftNode.Node == nil || rightNode.Node == nil
	if !placeholder {
		if err := leftNode.PreLinkAdd(entry.Link.LeftPort, entry.Link.Type, "left"); err != nil {
			return false
		}
		if err := rightNode.PreLinkAdd(entry.Link.RightPort, entry.Link.Type, "right"); err != nil {
			return false
		}
	}
	link := Link{
		ID:            entry.ID,
		Type:          entry.Link.Type,
		Placeholder:   placeholder,
		LeftPort:      entry.Link.LeftPort,
		LeftPortNode:  entry.Link.LeftPortNode,
		RightPort:     entry.Link.RightPort,
		RightPortNode: entry.Link.RightPortNode,
	}
	if err := link.Validate(); err != nil {
		return false
	}
	if !w.reserveIDLocked(entry.ID) {
		return false
	}
	w.links.Set(link.ID, &link)
	if !w.verifyDAG() {
		w.links.Delete(link.ID)
		return false
	}
	if !link.Placeholder {
		if err := leftNode.OnLinkAdd(link.ID, link.LeftPort, link.Type, "left"); err != nil {
			w.links.Delete(link.ID)
			return false
		}
		if err := rightNode.OnLinkAdd(link.ID, link.RightPort, link.Type, "right"); err != nil {
			w.links.Delete(link.ID)
			w.nodeEvLinkRemoved(leftNode.ID, link.ID, link.LeftPort, link.Type, "left")
			return false
		}
	}
	left.Links = insertLinkID(left.Links, link.ID, entry.LeftIndex)
	right.Links = insertLinkID(right.Links, link.ID, entry.RightIndex)
	w.enqueueLinkNotification(NotificationLinkAdded, link.ID, linkSnapshot(&link))
	w.enqueuePortNotification(NotificationPortUpdated, left.ID, portSnapshot(left))
	w.enqueuePortNotification(NotificationPortUpdated, right.ID, portSnapshot(right))
	w.recomputeRootPaths(true)
	return true
}

func linkIndex(w *Workspace, portID, linkID uint64) int {
	port, ok := w.ports.Get(portID)
	if !ok || port == nil {
		return -1
	}
	for i, id := range port.Links {
		if id == linkID {
			return i
		}
	}
	return -1
}

func insertLinkID(links []uint64, linkID uint64, index int) []uint64 {
	for _, id := range links {
		if id == linkID {
			return links
		}
	}
	if index < 0 || index >= len(links) {
		return append(links, linkID)
	}
	links = append(links, 0)
	copy(links[index+1:], links[index:])
	links[index] = linkID
	return links
}

func cloneAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneAny(value)
	}
	return out
}

func cloneAny(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return cloneAnyMap(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneAny(item)
		}
		return out
	case []string:
		return append([]string{}, v...)
	default:
		return v
	}
}
