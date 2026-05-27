package pasta

import (
	"sort"
	"strconv"
	"strings"

	"github.com/asciimoth/configer/configer"
)

type restoreNode struct {
	name       string
	class      string
	primary    string
	hasPrimary bool
	position   string
	links      []string
	ports      restorePorts
}

type restorePorts struct {
	left  map[string]struct{}
	right map[string]struct{}
}

type restoreLink struct {
	sourceNode string
	sourcePort string
	targetNode string
	targetPort string
	raw        string
}

// WorkspaceFromConfig restores a workspace from Config.
//
// The constructor accepts a nil Config, in which case it returns an empty
// workspace with the supplied classes registered and Ready run.
// Unknown or unavailable node classes are restored as placeholders so the saved
// topology can be kept until a matching class factory is registered.
func WorkspaceFromConfig(classes []NodeClass, cfg configer.Config, logf LogFactory) (*Workspace, error) {
	var (
		nodes  map[string]*restoreNode
		links  []restoreLink
		nextID uint64 = 1
	)

	if cfg != nil {
		cfg.RLock()
		defer cfg.RUnlock()

		snapshot := cfg.Snapshot()
		nodes, links = parseRestoreConfig(snapshot)
		nextID = restoreNextID(nodes, links)
	} else {
		nodes = map[string]*restoreNode{}
	}

	w := NewWorkspace(logf)
	w.Lock()
	w.nextid = nextID
	w.isReady = false
	w.undoRecordingDisabled += 1
	w.Unlock()

	for _, class := range classes {
		if err := w.AddNodeClass(class); err != nil {
			return nil, err
		}
	}

	names := make([]string, 0, len(nodes))
	for name := range nodes {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		node := nodes[name]
		if node == nil || node.class == "" {
			continue
		}
		if _, err := restoreOneNode(w, cfg, node); err != nil {
			return nil, err
		}
	}

	sort.SliceStable(links, func(i, j int) bool {
		a := links[i].sourceNode + "\x00" + links[i].raw
		b := links[j].sourceNode + "\x00" + links[j].raw
		return a < b
	})
	for _, link := range links {
		addRestoredLink(w, link)
	}

	w.Ready()
	w.Lock()
	w.undoRecordingDisabled -= 1
	w.undoLog = w.undoLog[:0]
	w.redoLog = w.redoLog[:0]
	w.Unlock()
	return w, nil
}

func restoreOneNode(w *Workspace, cfg configer.Config, node *restoreNode) (uint64, error) {
	if err := ValidateClassName(node.class); err != nil {
		return 0, err
	}
	if err := ValidateNodeName(node.name); err != nil {
		return 0, err
	}

	var (
		params NodeClassParams
		class  NodeClass
	)
	w.Lock()
	if c, ok := w.classes.Get(node.class); ok && c != nil {
		class = c
		params = c.DefaultNodeParams()
	}
	w.Unlock()

	state := NodeClassState{
		Root:        params.Root,
		PrimaryType: params.PrimaryType,
		Name:        node.name,
		LeftPorts:   restoreInitialPorts("left", params.InitialPorts, node.ports.left),
		RightPorts:  restoreInitialPorts("right", params.InitialPorts, node.ports.right),
	}
	if node.hasPrimary {
		state.PrimaryType = node.primary
	}

	var id uint64
	if factory, ok := class.(NodeClassFactory); ok {
		var nodeCfg configer.Config
		if cfg != nil {
			nodeCfg = cfg.View(configer.Path{node.name})
		}
		impl, err := factory.NewNode(nodeCfg, &state)
		if err != nil {
			return 0, err
		}
		if impl != nil {
			ports := append([]Port{}, state.LeftPorts...)
			ports = append(ports, state.RightPorts...)
			initData := &NodeInitData{PrimaryType: state.PrimaryType}
			w.Lock()
			id, err = w.addNodeLocked(impl, node.class, state.Root, state.Name, state.PrimaryType, ports, initData, true, true)
			w.Unlock()
			if err != nil {
				return 0, err
			}
		}
	}
	if id == 0 {
		ports := append([]Port{}, state.LeftPorts...)
		ports = append(ports, state.RightPorts...)
		var err error
		id, err = w.AddPlaceholderNodeWithRoot(node.class, state.Root, ports, node.name)
		if err != nil {
			return 0, err
		}
		if state.PrimaryType != "" {
			_ = w.SetNodePrimary(id, state.PrimaryType)
		}
	}
	if node.position != "" {
		_ = w.SetNodePosition(id, node.position)
	}
	return id, nil
}

func parseRestoreConfig(snapshot any) (map[string]*restoreNode, []restoreLink) {
	root, ok := snapshot.(map[string]any)
	if !ok {
		return map[string]*restoreNode{}, nil
	}
	nodes := make(map[string]*restoreNode, len(root))
	links := make([]restoreLink, 0)
	for name, raw := range root {
		object, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		class, _ := object[saveKeyClass].(string)
		if class == "" {
			continue
		}
		node := &restoreNode{
			name:  name,
			class: class,
			ports: restorePorts{
				left:  map[string]struct{}{},
				right: map[string]struct{}{},
			},
		}
		if primary, ok := object[saveKeyPrimary].(string); ok {
			node.primary = primary
			node.hasPrimary = true
		}
		node.position, _ = object[saveKeyPos].(string)
		for _, rawLink := range restoreStringList(object[saveKeyLinks]) {
			link, ok := parseRestoreLink(name, rawLink)
			if !ok {
				continue
			}
			node.links = append(node.links, rawLink)
			links = append(links, link)
		}
		nodes[name] = node
	}
	for _, link := range links {
		if source := nodes[link.sourceNode]; source != nil {
			source.ports.right[link.sourcePort] = struct{}{}
		}
		if target := nodes[link.targetNode]; target != nil {
			target.ports.left[link.targetPort] = struct{}{}
		}
	}
	return nodes, links
}

func restoreStringList(value any) []string {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if s, ok := value.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func parseRestoreLink(sourceNode, raw string) (restoreLink, bool) {
	sourcePort, rest, ok := strings.Cut(raw, " -> [")
	if !ok {
		return restoreLink{}, false
	}
	targetNode, targetPort, ok := strings.Cut(rest, "] ")
	if !ok || sourcePort == "" || targetNode == "" || targetPort == "" {
		return restoreLink{}, false
	}
	return restoreLink{
		sourceNode: sourceNode,
		sourcePort: sourcePort,
		targetNode: targetNode,
		targetPort: targetPort,
		raw:        raw,
	}, true
}

func restoreNextID(nodes map[string]*restoreNode, links []restoreLink) uint64 {
	maxID := 0
	for name := range nodes {
		if n, ok := trailingNameNumber(name); ok && n > maxID {
			maxID = n
		}
	}
	next := maxID + len(nodes) + len(links)*3 + 1
	if next < 1 {
		return 1
	}
	return uint64(next)
}

func trailingNameNumber(name string) (int, bool) {
	i := strings.LastIndexByte(name, ' ')
	if i < 0 || i == len(name)-1 {
		return 0, false
	}
	n, err := strconv.Atoi(name[i+1:])
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func restoreInitialPorts(direction string, defaults []Port, guessed map[string]struct{}) []Port {
	ports := make([]Port, 0, len(defaults)+len(guessed))
	seen := map[string]struct{}{}
	for _, port := range defaults {
		if port.Direction != direction {
			continue
		}
		port = port.Copy()
		port.ID = 0
		port.Node = 0
		port.Links = nil
		ports = append(ports, port)
		seen[port.Name] = struct{}{}
	}
	names := make([]string, 0, len(guessed))
	for name := range guessed {
		if _, ok := seen[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ValidatePortName(name); err != nil {
			continue
		}
		ports = append(ports, Port{
			Direction: direction,
			Name:      name,
			Types:     []string{AnyType},
		})
	}
	return ports
}

func addRestoredLink(w *Workspace, link restoreLink) {
	sourceID, ok := w.NodeIDByName(link.sourceNode)
	if !ok {
		return
	}
	targetID, ok := w.NodeIDByName(link.targetNode)
	if !ok {
		return
	}
	sourcePort, ok := w.nodePortByName(sourceID, "right", link.sourcePort)
	if !ok {
		return
	}
	targetPort, ok := w.nodePortByName(targetID, "left", link.targetPort)
	if !ok {
		return
	}
	_, _, _ = w.AddLink(sourcePort, targetPort)
}

func (w *Workspace) nodePortByName(node uint64, direction, name string) (uint64, bool) {
	w.Lock()
	defer w.Unlock()
	if w.closed {
		return 0, false
	}
	record, ok := w.nodes.Get(node)
	if !ok || record == nil {
		return 0, false
	}
	ports := record.LeftPorts
	if direction == "right" {
		ports = record.RightPorts
	}
	for _, portID := range ports {
		port, ok := w.ports.Get(portID)
		if ok && port != nil && port.Name == name {
			return portID, true
		}
	}
	return 0, false
}
