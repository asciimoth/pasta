package pasta

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/asciimoth/configer/configer"
)

// SaveData is the deterministic JSON-like persistence shape for a workspace.
//
// It contains model state only: IDs, class names, dynamic node state, ports,
// link names, link types, waypoints, and ID generator progress. It does not
// store Go runtime values or link objects.
type SaveData struct {
	NextNode int64      `json:"nextNode"`
	NextLink int64      `json:"nextLink"`
	Nodes    []SaveNode `json:"nodes"`
	Links    []SaveLink `json:"links"`
}

// SaveNode is a persisted node record.
//
// ID is the canonical node ID string. Class is the qualified class name used to
// recover the node when that class is available. State stores public editor data
// and private application data. Inputs and Outputs preserve the node's current
// port definitions, including port metadata annotations.
type SaveNode struct {
	ID      string     `json:"id"`
	Class   string     `json:"class"`
	State   NodeState  `json:"state"`
	Inputs  []PortSpec `json:"inputs"`
	Outputs []PortSpec `json:"outputs"`
}

// SaveLink is a persisted link record.
//
// Name is the canonical full link name containing link ID, input full port ID,
// and output full port ID. Waypoints are opaque editor coordinate strings.
type SaveLink struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Waypoints []string `json:"waypoints,omitempty"`
}

// Save returns deterministic workspace data suitable for JSON/config storage.
//
// Save uses the private state already stored in NodeState. It does not call
// runtime export hooks; use SaveWithRuntimeState when runtimes own newer
// volatile private values.
func (w *Workspace) Save() SaveData {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.saveLocked(nil)
}

// SaveWithRuntimeState returns deterministic workspace data after asking active
// node runtimes to export their current private state.
func (w *Workspace) SaveWithRuntimeState() (SaveData, error) {
	exports, err := w.exportPrivateStates(nil)
	if err != nil {
		return SaveData{}, err
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.saveLocked(exports), nil
}

// SaveConfig returns deterministic workspace data as an in-memory configer tree.
func (w *Workspace) SaveConfig() (configer.Config, error) {
	return saveDataConfig(w.Save())
}

// SaveToConfig saves deterministic workspace data into cfg.
//
// Existing comments are preserved by Config implementations that support
// configer.Commenter when the commented value still maps to the compact config
// format. Void values are omitted unless a comment is already attached to the
// same compact-format path.
func (w *Workspace) SaveToConfig(cfg configer.Config) error {
	_, err := saveDataConfig(w.Save(), cfg)
	return err
}

// SaveConfigWithRuntimeState returns deterministic configer data after asking
// active node runtimes to export their current private state.
func (w *Workspace) SaveConfigWithRuntimeState() (configer.Config, error) {
	data, err := w.SaveWithRuntimeState()
	if err != nil {
		return nil, err
	}
	return saveDataConfig(data)
}

// SaveToConfigWithRuntimeState saves deterministic workspace data into cfg after
// asking active node runtimes to export their current private state.
func (w *Workspace) SaveToConfigWithRuntimeState(cfg configer.Config) error {
	data, err := w.SaveWithRuntimeState()
	if err != nil {
		return err
	}
	_, err = saveDataConfig(data, cfg)
	return err
}

func saveDataConfig(data SaveData, targets ...configer.Config) (configer.Config, error) {
	cfg := configer.Config(configer.NewMemory(nil))
	if len(targets) > 0 {
		cfg = targets[0]
		if cfg == nil {
			return nil, opErr("save config", "validate", ErrNotFound)
		}
	}
	value := configValueFromSaveData(data, cfg)
	if _, err := json.Marshal(value); err != nil {
		return nil, opErr("save config", "marshal", err)
	}
	if err := cfg.Set(nil, value); err != nil {
		return nil, opErr("save config", "marshal", err)
	}
	return cfg, nil
}

func configValueFromSaveData(data SaveData, cfg configer.Config) map[string]any {
	root := map[string]any{}
	linksByInput := make(map[string][]SaveLink)
	for _, link := range data.Links {
		full, err := ParseFullLinkName(link.Name)
		if err != nil {
			continue
		}
		linksByInput[full.Input.String()] = append(linksByInput[full.Input.String()], link)
	}
	nodes := make([]any, 0, len(data.Nodes))
	for nodeIndex, node := range data.Nodes {
		nodePath := configer.Path{"nodes", fmt.Sprint(nodeIndex)}
		out := map[string]any{
			"id":    node.ID,
			"class": node.Class,
		}
		if state := configStateValue(node.State, cfg, append(nodePath, "state")); len(state) > 0 {
			out["state"] = state
		}
		ports := append(clonePorts(node.Inputs), node.Outputs...)
		if len(ports) > 0 {
			portValues := make([]any, 0, len(ports))
			for portIndex, port := range ports {
				portPath := append(nodePath, "ports", fmt.Sprint(portIndex))
				portValue := configPortValue(node.ID, port, linksByInput, cfg, portPath)
				portValues = append(portValues, portValue)
			}
			out["ports"] = portValues
		}
		nodes = append(nodes, out)
	}
	if len(nodes) > 0 {
		root["nodes"] = nodes
	}
	return root
}

func configStateValue(state NodeState, cfg configer.Config, path configer.Path) map[string]any {
	out := map[string]any{}
	setStringConfigField(out, cfg, path, "DisplayName", state.DisplayName)
	setStringConfigField(out, cfg, path, "Description", state.Description)
	setStringConfigField(out, cfg, path, "PrimaryType", state.PrimaryType)
	setStringConfigField(out, cfg, path, "Coordinate", state.Coordinate)
	if len(state.Metadata) > 0 || hasConfigComment(cfg, append(path, "Metadata")) {
		out["Metadata"] = cloneStringMap(state.Metadata)
	}
	if state.Private != nil || hasConfigComment(cfg, append(path, "Private")) {
		out["Private"] = clonePrivateState(state.Private)
	}
	return out
}

func setStringConfigField(out map[string]any, cfg configer.Config, path configer.Path, key, value string) {
	if value != "" || hasConfigComment(cfg, append(path, key)) {
		out[key] = value
	}
}

func configPortValue(nodeID string, port PortSpec, linksByInput map[string][]SaveLink, cfg configer.Config, path configer.Path) map[string]any {
	out := map[string]any{"id": port.ID.String()}
	setStringConfigField(out, cfg, path, "Name", port.Name)
	setStringConfigField(out, cfg, path, "FixedType", port.FixedType)
	if len(port.AcceptedTypes) > 0 || hasConfigComment(cfg, append(path, "AcceptedTypes")) {
		out["AcceptedTypes"] = append([]string(nil), port.AcceptedTypes...)
	}
	if port.Multiple || hasConfigComment(cfg, append(path, "Multiple")) {
		out["Multiple"] = port.Multiple
	}
	if len(port.Metadata) > 0 || hasConfigComment(cfg, append(path, "Metadata")) {
		out["Metadata"] = cloneStringMap(port.Metadata)
	}
	if port.ID.Kind == InputPort {
		fullPort := nodeID + port.ID.String()
		if links := linksByInput[fullPort]; len(links) > 0 {
			linkValues := map[string]any{}
			for _, link := range links {
				value := map[string]any{}
				if link.Type != "" {
					value["type"] = link.Type
				}
				if len(link.Waypoints) > 0 {
					value["waypoints"] = append([]string(nil), link.Waypoints...)
				}
				linkValues[link.Name] = value
			}
			out["Links"] = linkValues
		}
	}
	return out
}

func hasConfigComment(cfg configer.Config, path configer.Path) bool {
	commenter, ok := cfg.(configer.Commenter)
	if !ok {
		return false
	}
	comment, err := commenter.GetComment(path)
	return err == nil && comment != ""
}

func (w *Workspace) saveLocked(exports map[NodeID]any) SaveData {
	data := SaveData{
		NextNode: int64(w.nextNode),
		NextLink: int64(w.nextLink),
	}
	for _, node := range w.nodes {
		state := cloneNodeState(node.dynamic)
		if private, ok := exports[node.id]; ok {
			state.Private = clonePrivateState(private)
		}
		data.Nodes = append(data.Nodes, SaveNode{
			ID:      node.id.String(),
			Class:   node.class,
			State:   state,
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

func (w *Workspace) exportPrivateStates(selected map[NodeID]bool) (map[NodeID]any, error) {
	w.mu.RLock()
	runtimes := make(map[NodeID]NodeRuntime)
	for id, node := range w.nodes {
		if selected != nil && !selected[id] {
			continue
		}
		if node.state == StateActive && node.runtime != nil {
			runtimes[id] = node.runtime
		}
	}
	w.mu.RUnlock()
	exports := make(map[NodeID]any)
	for id, runtime := range runtimes {
		private, ok, err := w.callExportPrivateState(runtime)
		if err != nil {
			return nil, opErr("export private state", "hook", err)
		}
		if ok {
			exports[id] = private
		}
	}
	return exports, nil
}

// Restore replaces workspace model state from SaveData.
//
// Registered libraries and classes are preserved. Nodes whose classes are not
// currently active are restored as inactive. Links whose endpoint nodes or
// ports are missing are skipped as broken; links that reference existing
// endpoints but violate type, multiplicity, duplicate-ID, or DAG constraints
// reject the restore and roll the workspace back to its previous state.
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
		if !validPersistedClassName(saved.Class) {
			return opErr("restore", "validate", ErrInvalidName)
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
	w.pruneSingleNodeClassDuplicatesLocked(nodes)
	oldNodes, oldLinks, oldMessages := w.nodes, w.links, cloneMessageRecords(w.messages)
	oldNextNode, oldNextLink, oldNextMessage := w.nextNode, w.nextLink, w.nextMessage
	resourceEvents := w.collectAllResourceEventsLocked()
	restoreMessageEvents := w.removeAllMessagesLocked()
	restoreMenuEvents := w.removeAllMenusLocked()
	w.nextMessage = 1
	rollback := func() {
		w.nodes, w.links = oldNodes, oldLinks
		w.messages = oldMessages
		w.nextNode, w.nextLink, w.nextMessage = oldNextNode, oldNextLink, oldNextMessage
	}
	w.nodes, w.links = nodes, links
	seenLinkIDs := map[LinkID]bool{}
	for _, saved := range data.Links {
		full, err := ParseFullLinkName(saved.Name)
		if err != nil {
			rollback()
			return opErr("restore", "validate", err)
		}
		if seenLinkIDs[full.Link] {
			rollback()
			return opErr("restore", "validate", ErrDuplicate)
		}
		seenLinkIDs[full.Link] = true
		typ, err := w.validateLinkLocked(full.Input, full.Output, saved.Type, 0)
		if err != nil {
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalidPort) {
				continue
			}
			rollback()
			return opErr("restore", "validate", err)
		}
		w.links[full.Link] = &linkRecord{
			id:        full.Link,
			input:     full.Input,
			output:    full.Output,
			typ:       typ,
			state:     StateActive,
			waypoints: append([]string(nil), saved.Waypoints...),
		}
		if full.Link > maxLink {
			maxLink = full.Link
		}
	}
	w.nextNode = NodeID(data.NextNode)
	if w.nextNode <= 0 {
		w.nextNode = 1
	}
	if w.nextNode <= maxNode {
		w.nextNode = maxNode + 1
	}
	w.nextLink = LinkID(data.NextLink)
	if w.nextLink <= 0 {
		w.nextLink = 1
	}
	if w.nextLink <= maxLink {
		w.nextLink = maxLink + 1
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
			cleanupErr := w.cleanupInitializedRuntimes(runtimes, scopes)
			w.mu.Lock()
			locked = true
			w.nodes, w.links = oldNodes, oldLinks
			w.messages = oldMessages
			w.nextNode, w.nextLink, w.nextMessage = oldNextNode, oldNextLink, oldNextMessage
			w.mu.Unlock()
			locked = false
			return errors.Join(err, cleanupErr)
		}
		runtimes[initNode.record.id] = runtime
		scopes[initNode.record.id] = scope
	}

	w.mu.Lock()
	locked = true
	if err := w.checkOpenLocked("restore"); err != nil {
		w.nodes, w.links = oldNodes, oldLinks
		w.messages = oldMessages
		w.nextNode, w.nextLink, w.nextMessage = oldNextNode, oldNextLink, oldNextMessage
		w.mu.Unlock()
		locked = false
		return errors.Join(err, w.cleanupInitializedRuntimes(runtimes, scopes))
	}
	for id, runtime := range runtimes {
		if node := w.nodes[id]; node != nil {
			node.runtime = runtime
		}
		if scope := scopes[id]; scope != nil {
			scope.finishInit()
		}
	}
	initIDs := make([]NodeID, 0, len(runtimes))
	for id := range runtimes {
		initIDs = append(initIDs, id)
	}
	keyEvents := w.keyAccessEventsForNodesLocked(initIDs)
	watchers := w.messageWatchersLocked()
	menuWatchers := w.menuWatchersLocked()
	workspaceWatchers := w.workspaceWatchersLocked()
	w.mu.Unlock()
	locked = false
	w.notifyMessageWatchers(watchers, restoreMessageEvents)
	w.notifyMenuWatchers(menuWatchers, restoreMenuEvents)
	w.notifyWorkspaceWatchers(workspaceWatchers, []WorkspaceEvent{{Kind: WorkspaceChanged}})
	w.callNodeKeyAccessEvents(keyEvents)
	if err := w.callResourceDestroyEvents(resourceEvents); err != nil {
		return opErr("restore", "hook", err)
	}
	return nil
}

// RestoreConfig replaces workspace model state from configer-backed SaveData.
func (w *Workspace) RestoreConfig(cfg configer.Config) error {
	if cfg == nil {
		return opErr("restore config", "validate", ErrNotFound)
	}
	data, err := saveDataFromConfig(cfg)
	if err != nil {
		return opErr("restore config", "unmarshal", err)
	}
	return w.Restore(data)
}

type configSaveData struct {
	Nodes []configSaveNode `json:"nodes"`
}

type configSaveNode struct {
	ID    string           `json:"id"`
	Class string           `json:"class"`
	State NodeState        `json:"state"`
	Ports []configPortSpec `json:"ports"`
}

type configPortSpec struct {
	ID            string                    `json:"id"`
	Name          string                    `json:"Name"`
	FixedType     string                    `json:"FixedType"`
	AcceptedTypes []string                  `json:"AcceptedTypes"`
	Multiple      bool                      `json:"Multiple"`
	Metadata      map[string]string         `json:"Metadata"`
	Links         map[string]configLinkSpec `json:"Links"`
}

type configLinkSpec struct {
	Type      string   `json:"type"`
	Waypoints []string `json:"waypoints"`
}

func saveDataFromConfig(cfg configer.Config) (SaveData, error) {
	snapshot := cfg.Snapshot()
	if usesLegacyConfigShape(snapshot) {
		var data SaveData
		if err := configer.Unmarshal(cfg, &data); err != nil {
			return SaveData{}, err
		}
		return data, nil
	}
	var compact configSaveData
	if err := configer.Unmarshal(cfg, &compact); err != nil {
		return SaveData{}, err
	}
	data := SaveData{Nodes: make([]SaveNode, 0, len(compact.Nodes))}
	for _, node := range compact.Nodes {
		saved := SaveNode{
			ID:    node.ID,
			Class: node.Class,
			State: cloneNodeState(node.State),
		}
		nodeID, err := ParseNodeID(node.ID)
		if err != nil {
			return SaveData{}, err
		}
		for _, port := range node.Ports {
			portID, err := ParsePortID(port.ID)
			if err != nil {
				return SaveData{}, err
			}
			spec := PortSpec{
				ID:            portID,
				Name:          port.Name,
				Direction:     portID.Kind,
				FixedType:     port.FixedType,
				AcceptedTypes: append([]string(nil), port.AcceptedTypes...),
				Multiple:      port.Multiple,
				Metadata:      cloneStringMap(port.Metadata),
			}
			switch portID.Kind {
			case InputPort:
				saved.Inputs = append(saved.Inputs, spec)
				for name, link := range port.Links {
					full, err := ParseFullLinkName(name)
					if err != nil {
						return SaveData{}, err
					}
					if full.Input != (FullPortID{Node: nodeID, Port: portID}) {
						return SaveData{}, fmt.Errorf("%w: link stored on non-input endpoint", ErrInvalidID)
					}
					data.Links = append(data.Links, SaveLink{
						Name:      name,
						Type:      link.Type,
						Waypoints: append([]string(nil), link.Waypoints...),
					})
				}
			case OutputPort:
				saved.Outputs = append(saved.Outputs, spec)
				if len(port.Links) > 0 {
					return SaveData{}, fmt.Errorf("%w: output port links", ErrInvalidID)
				}
			}
		}
		data.Nodes = append(data.Nodes, saved)
	}
	sort.Slice(data.Links, func(i, j int) bool { return data.Links[i].Name < data.Links[j].Name })
	return data, nil
}

func usesLegacyConfigShape(snapshot any) bool {
	root, ok := snapshot.(map[string]any)
	if !ok {
		return false
	}
	if _, ok := root["links"]; ok {
		return true
	}
	nodes, ok := root["nodes"].([]any)
	if !ok {
		return false
	}
	for _, rawNode := range nodes {
		node, ok := rawNode.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := node["inputs"]; ok {
			return true
		}
		if _, ok := node["outputs"]; ok {
			return true
		}
	}
	return false
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
