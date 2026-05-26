package pasta

import (
	"errors"
	"fmt"

	"github.com/asciimoth/configer/configer"
)

const (
	saveKeyClass   = "Class"
	saveKeyPrimary = "Primary"
	saveKeyPos     = "Pos"
	saveKeyLinks   = "Links"
)

// SaveConfig saves the workspace state into an existing Config.
//
// The root object is keyed by node name. Workspace-owned node keys are
// CamelCase; node implementations should use lower-case keys for node-owned
// state saved by OnSave.
func (w *Workspace) SaveConfig(cfg configer.Config) error {
	if cfg == nil {
		return errors.New("nil config")
	}
	if cfg.IsReadOnly() {
		return configer.ErrReadOnly
	}

	w.Lock()
	defer w.Unlock()
	if w.closed {
		return ErrWorkspaceClosed
	}

	nodeNames := make(map[string]*nodeRecord, w.nodes.Len())
	for pair := w.nodes.Oldest(); pair != nil; pair = pair.Next() {
		if pair.Value != nil {
			nodeNames[pair.Value.Name] = pair.Value
		}
	}

	root, err := cfg.Get(nil)
	if err != nil {
		if !errors.Is(err, configer.ErrNotFound) {
			return err
		}
		root = map[string]any{}
	}
	rootObject, ok := root.(map[string]any)
	if !ok {
		if err := cfg.Set(nil, map[string]any{}); err != nil {
			return err
		}
		rootObject = map[string]any{}
	}
	for name := range rootObject {
		if _, ok := nodeNames[name]; !ok {
			if err := deleteIfExists(cfg, configer.Path{name}); err != nil {
				return err
			}
		}
	}

	for pair := w.nodes.Oldest(); pair != nil; pair = pair.Next() {
		record := pair.Value
		if record == nil {
			continue
		}
		nodePath := configer.Path{record.Name}
		nodeCfg := cfg.View(nodePath)
		if record.Node != nil {
			if err := record.OnSave(nodeCfg); err != nil {
				return err
			}
		}
		if err := saveWorkspaceNodeState(w, nodeCfg, record); err != nil {
			return err
		}
	}

	return nil
}

// DeleteNodeOwnedConfigKeys deletes all lower-case keys from a node Config view.
//
// It is intended for simple Node.OnSave implementations that prefer to clear
// all previously saved node-owned state, including commented keys, before
// writing fresh state. Workspace-owned keys are CamelCase and are left intact.
func DeleteNodeOwnedConfigKeys(cfg configer.Config) error {
	if cfg == nil {
		return errors.New("nil config")
	}
	if cfg.IsReadOnly() {
		return configer.ErrReadOnly
	}

	snapshot, err := cfg.Get(nil)
	if err != nil {
		if errors.Is(err, configer.ErrNotFound) {
			return nil
		}
		return err
	}
	object, ok := snapshot.(map[string]any)
	if !ok {
		return nil
	}
	for key := range object {
		if key != "" && isLower(key[0]) {
			if err := deleteIfExists(cfg, configer.Path{key}); err != nil {
				return err
			}
		}
	}
	return nil
}

func saveWorkspaceNodeState(w *Workspace, cfg configer.Config, record *nodeRecord) error {
	if err := cfg.Set(configer.Path{saveKeyClass}, record.Class); err != nil {
		return err
	}

	defaultPrimary := ""
	if class, ok := w.classes.Get(record.Class); ok && class != nil {
		defaultPrimary = class.DefaultNodeParams().PrimaryType
	}
	if record.PrimaryType == defaultPrimary && !hasConfigComment(cfg, configer.Path{saveKeyPrimary}) {
		if err := deleteIfExists(cfg, configer.Path{saveKeyPrimary}); err != nil {
			return err
		}
	} else if err := cfg.Set(configer.Path{saveKeyPrimary}, record.PrimaryType); err != nil {
		return err
	}

	if record.Position == "" && !hasConfigComment(cfg, configer.Path{saveKeyPos}) {
		if err := deleteIfExists(cfg, configer.Path{saveKeyPos}); err != nil {
			return err
		}
	} else if err := cfg.Set(configer.Path{saveKeyPos}, record.Position); err != nil {
		return err
	}

	links := outgoingLinkSpecs(w, record)
	if len(links) == 0 && !hasConfigComment(cfg, configer.Path{saveKeyLinks}) {
		return deleteIfExists(cfg, configer.Path{saveKeyLinks})
	}
	return cfg.Set(configer.Path{saveKeyLinks}, links)
}

func outgoingLinkSpecs(w *Workspace, record *nodeRecord) []string {
	specs := make([]string, 0)
	for _, portID := range record.RightPorts {
		port, ok := w.ports.Get(portID)
		if !ok || port == nil {
			continue
		}
		for _, linkID := range port.Links {
			link, ok := w.links.Get(linkID)
			if !ok || link == nil || link.RightPort != port.ID {
				continue
			}
			targetNode, ok := w.nodes.Get(link.LeftPortNode)
			if !ok || targetNode == nil {
				continue
			}
			targetPort, ok := w.ports.Get(link.LeftPort)
			if !ok || targetPort == nil {
				continue
			}
			specs = append(specs, fmt.Sprintf("%s -> [%s] %s", port.Name, targetNode.Name, targetPort.Name))
		}
	}
	return specs
}

func deleteIfExists(cfg configer.Config, path configer.Path) error {
	if err := cfg.Delete(path); err != nil && !errors.Is(err, configer.ErrNotFound) {
		return err
	}
	return nil
}

func hasConfigComment(cfg configer.Config, path configer.Path) bool {
	commenter, ok := cfg.(configer.Commenter)
	if !ok {
		return false
	}
	comment, err := commenter.GetComment(path)
	return err == nil && comment != ""
}
