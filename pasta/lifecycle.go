package pasta

import (
	"fmt"
	"slices"
	"sort"
)

func (w *Workspace) initNodeRuntime(class NodeClass, rec *nodeRecord, mode InitMode) (runtime NodeRuntime, scope *nodeScope, err error) {
	if class == nil {
		return nil, nil, nil
	}
	scope = &nodeScope{w: w, id: rec.id, initRec: rec}
	ctx := NodeContext{
		ID:       rec.id,
		Class:    rec.class,
		Library:  rec.library,
		ReadOnly: w,
		Node:     scope,
	}
	defer func() {
		if err != nil {
			scope.finishInit()
		}
	}()
	err = w.recoverHook("init node", func() error {
		var initErr error
		runtime, initErr = class.InitNode(ctx, cloneNodeState(rec.dynamic), mode)
		return initErr
	})
	if err != nil {
		return nil, scope, opErr("create node", "hook", err)
	}
	err = w.callImportPrivateState(runtime, clonePrivateState(rec.dynamic.Private))
	if err != nil {
		return nil, scope, opErr("create node", "hook", err)
	}
	return runtime, scope, nil
}

func (w *Workspace) callExportPrivateState(runtime NodeRuntime) (private any, ok bool, err error) {
	hook, ok := runtime.(NodePrivateExportHook)
	if !ok {
		return nil, false, nil
	}
	err = w.recoverHook("export private state", func() error {
		var exportErr error
		private, exportErr = hook.ExportPrivateState()
		return exportErr
	})
	return private, true, err
}

func (w *Workspace) callImportPrivateState(runtime NodeRuntime, private any) error {
	hook, ok := runtime.(NodePrivateImportHook)
	if !ok {
		return nil
	}
	return w.recoverHook("import private state", func() error {
		return hook.ImportPrivateState(private)
	})
}

func (w *Workspace) callMenuUpdateHook(hook NodeMenuUpdateHook, update MenuStateUpdate) (out MenuStateUpdate, err error) {
	err = w.recoverHook("apply menu update", func() error {
		var updateErr error
		out, updateErr = hook.ApplyMenuUpdate(update)
		return updateErr
	})
	return out, err
}

func (w *Workspace) callMenuButtonHook(hook NodeMenuButtonHook, ref MenuButtonRef) error {
	return w.recoverHook("trigger menu button", func() error {
		return hook.TriggerMenuButton(ref)
	})
}

func (w *Workspace) callLinkObject(runtime NodeRuntime, endpoint LinkEndpoint) (object any, err error) {
	provider, ok := runtime.(LinkObjectProvider)
	if !ok {
		return nil, nil
	}
	err = w.recoverHook("link object", func() error {
		var objectErr error
		object, objectErr = provider.LinkObject(endpoint)
		return objectErr
	})
	return object, err
}

func (w *Workspace) callBeforeLinkAttach(runtime NodeRuntime, endpoint LinkEndpoint, object any) error {
	hook, ok := runtime.(LinkAttachHook)
	if !ok {
		return nil
	}
	return w.recoverHook("before link attach", func() error {
		return hook.BeforeLinkAttach(endpoint, object)
	})
}

func (w *Workspace) callAfterLinkAttach(runtime NodeRuntime, endpoint LinkEndpoint, object any) {
	hook, ok := runtime.(LinkAttachHook)
	if !ok {
		return
	}
	_ = w.recoverHook("after link attach", func() error {
		hook.AfterLinkAttach(endpoint, object)
		return nil
	})
}

func (w *Workspace) callBeforeLinkDetach(runtime NodeRuntime, endpoint LinkEndpoint) error {
	hook, ok := runtime.(LinkDetachHook)
	if !ok {
		return nil
	}
	return w.recoverHook("before link detach", func() error {
		return hook.BeforeLinkDetach(endpoint)
	})
}

func (w *Workspace) callAfterLinkDetach(runtime NodeRuntime, endpoint LinkEndpoint) {
	hook, ok := runtime.(LinkDetachHook)
	if !ok {
		return
	}
	_ = w.recoverHook("after link detach", func() error {
		hook.AfterLinkDetach(endpoint)
		return nil
	})
}

func (w *Workspace) callAfterLinkInactive(runtime NodeRuntime, endpoint LinkEndpoint, reason InactiveReason) {
	hook, ok := runtime.(LinkInactiveHook)
	if !ok {
		return
	}
	_ = w.recoverHook("after link inactive", func() error {
		hook.AfterLinkInactive(endpoint, reason)
		return nil
	})
}

func (w *Workspace) callBeforeInactive(runtime NodeRuntime, reason InactiveReason) error {
	hook, ok := runtime.(NodeInactiveHook)
	if !ok {
		return nil
	}
	return w.recoverHook("before inactive", func() error {
		return hook.BeforeInactive(reason)
	})
}

func (w *Workspace) callAfterInactive(runtime NodeRuntime, reason InactiveReason) {
	hook, ok := runtime.(NodeInactiveHook)
	if !ok {
		return
	}
	_ = w.recoverHook("after inactive", func() error {
		hook.AfterInactive(reason)
		return nil
	})
}

func (w *Workspace) callNodeKeyAccess(runtime NodeRuntime, hasAccess bool) {
	hook, ok := runtime.(NodeKeyAccessHook)
	if !ok {
		return
	}
	_ = w.recoverHook("key node access", func() error {
		hook.HasKeyNodeAccess(hasAccess)
		return nil
	})
}

func (w *Workspace) callBeforeDelete(runtime NodeRuntime) error {
	hook, ok := runtime.(NodeDeleteHook)
	if !ok {
		return nil
	}
	return w.recoverHook("before delete", hook.BeforeDelete)
}

func (w *Workspace) callBeforeInactiveEvents(events []nodeInactiveEvent, reason InactiveReason) error {
	for _, event := range events {
		if err := w.callBeforeInactive(event.runtime, reason); err != nil {
			return err
		}
	}
	return nil
}

func (w *Workspace) callAfterInactiveEvents(events []nodeInactiveEvent, reason InactiveReason) {
	for _, event := range events {
		w.callAfterInactive(event.runtime, reason)
	}
}

func (w *Workspace) callLinkInactiveEvents(events []linkInactiveEvent, reason InactiveReason) {
	for _, event := range events {
		w.callAfterLinkInactive(event.inputRuntime, event.inputEndpoint, reason)
		w.callAfterLinkInactive(event.outputRuntime, event.outputEndpoint, reason)
	}
}

func (w *Workspace) callAfterLinkDetachEvents(events []linkDetachEvent) {
	for _, event := range events {
		if event.inputRuntime == nil || event.outputRuntime == nil {
			w.mu.RLock()
			if event.inputRuntime == nil {
				if node := w.nodes[event.inputEndpoint.Self.Node]; node != nil {
					event.inputRuntime = node.runtime
				}
			}
			if event.outputRuntime == nil {
				if node := w.nodes[event.outputEndpoint.Self.Node]; node != nil {
					event.outputRuntime = node.runtime
				}
			}
			w.mu.RUnlock()
		}
		w.callAfterLinkDetach(event.inputRuntime, event.inputEndpoint)
		w.callAfterLinkDetach(event.outputRuntime, event.outputEndpoint)
	}
}

func (w *Workspace) callNodeKeyAccessEvents(events []nodeKeyAccessEvent) {
	for _, event := range events {
		w.callNodeKeyAccess(event.runtime, event.hasAccess)
	}
}

func (w *Workspace) callCloseEvents(events []nodeInactiveEvent) error {
	var first error
	for _, event := range events {
		if err := w.callNodeClose(event.runtime); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (w *Workspace) cleanupInitializedRuntimes(runtimes map[NodeID]NodeRuntime, scopes map[NodeID]*nodeScope) error {
	scopeIDs := make([]NodeID, 0, len(scopes))
	for id := range scopes {
		scopeIDs = append(scopeIDs, id)
	}
	slices.Sort(scopeIDs)
	for _, id := range scopeIDs {
		if scope := scopes[id]; scope != nil {
			scope.finishInit()
		}
	}

	runtimeIDs := make([]NodeID, 0, len(runtimes))
	for id := range runtimes {
		runtimeIDs = append(runtimeIDs, id)
	}
	slices.Sort(runtimeIDs)
	var first error
	for _, id := range runtimeIDs {
		if err := w.callNodeClose(runtimes[id]); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (w *Workspace) callAfterDelete(runtime NodeRuntime) {
	hook, ok := runtime.(NodeDeleteHook)
	if !ok {
		return
	}
	_ = w.recoverHook("after delete", func() error {
		hook.AfterDelete()
		return nil
	})
}

func (w *Workspace) callNodeClose(runtime NodeRuntime) error {
	hook, ok := runtime.(NodeCloseHook)
	if !ok {
		return nil
	}
	return w.recoverHook("close node", hook.Close)
}

func (w *Workspace) recoverHook(op string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			w.logPanic(op+" hook", r)
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	err = fn()
	if err != nil {
		w.logError(op+" hook", err)
	}
	return err
}

func (w *Workspace) linkRuntimesLocked(link *linkRecord) (NodeRuntime, NodeRuntime) {
	var inputRuntime NodeRuntime
	var outputRuntime NodeRuntime
	if node := w.nodes[link.input.Node]; node != nil {
		inputRuntime = node.runtime
	}
	if node := w.nodes[link.output.Node]; node != nil {
		outputRuntime = node.runtime
	}
	return inputRuntime, outputRuntime
}

func linkEndpoints(link *linkRecord) (LinkEndpoint, LinkEndpoint) {
	input := LinkEndpoint{
		Link:      link.id,
		Self:      link.input,
		Peer:      link.output,
		Type:      link.typ,
		Direction: InputPort,
	}
	output := LinkEndpoint{
		Link:      link.id,
		Self:      link.output,
		Peer:      link.input,
		Type:      link.typ,
		Direction: OutputPort,
	}
	return input, output
}

type nodeInactiveEvent struct {
	id      NodeID
	runtime NodeRuntime
}

type nodeKeyAccessEvent struct {
	id        NodeID
	runtime   NodeRuntime
	hasAccess bool
}

type linkInactiveEvent struct {
	id             LinkID
	inputRuntime   NodeRuntime
	outputRuntime  NodeRuntime
	inputEndpoint  LinkEndpoint
	outputEndpoint LinkEndpoint
}

type linkDetachEvent struct {
	id             LinkID
	inputRuntime   NodeRuntime
	outputRuntime  NodeRuntime
	inputEndpoint  LinkEndpoint
	outputEndpoint LinkEndpoint
}

func (w *Workspace) inactiveEventsForNodesLocked(nodes map[NodeID]bool) ([]nodeInactiveEvent, []linkInactiveEvent) {
	nodeEvents := make([]nodeInactiveEvent, 0, len(nodes))
	for id := range nodes {
		node := w.nodes[id]
		if node != nil && node.state == StateActive {
			nodeEvents = append(nodeEvents, nodeInactiveEvent{id: id, runtime: node.runtime})
		}
	}
	sort.Slice(nodeEvents, func(i, j int) bool { return nodeEvents[i].id < nodeEvents[j].id })
	linkEvents := make([]linkInactiveEvent, 0)
	for id, link := range w.links {
		if link.state != StateActive || (!nodes[link.input.Node] && !nodes[link.output.Node]) {
			continue
		}
		inputRuntime, outputRuntime := w.linkRuntimesLocked(link)
		inputEndpoint, outputEndpoint := linkEndpoints(link)
		linkEvents = append(linkEvents, linkInactiveEvent{
			id:             id,
			inputRuntime:   inputRuntime,
			outputRuntime:  outputRuntime,
			inputEndpoint:  inputEndpoint,
			outputEndpoint: outputEndpoint,
		})
	}
	sort.Slice(linkEvents, func(i, j int) bool { return linkEvents[i].id < linkEvents[j].id })
	return nodeEvents, linkEvents
}

func (w *Workspace) linkDetachEventLocked(link *linkRecord) linkDetachEvent {
	inputRuntime, outputRuntime := w.linkRuntimesLocked(link)
	inputEndpoint, outputEndpoint := linkEndpoints(link)
	return linkDetachEvent{
		id:             link.id,
		inputRuntime:   inputRuntime,
		outputRuntime:  outputRuntime,
		inputEndpoint:  inputEndpoint,
		outputEndpoint: outputEndpoint,
	}
}
