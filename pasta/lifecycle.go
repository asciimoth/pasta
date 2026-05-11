package pasta

import (
	"fmt"
	"sort"
)

func (w *Workspace) initNodeRuntime(class NodeClass, rec *nodeRecord, mode InitMode) (runtime NodeRuntime, err error) {
	if class == nil {
		return nil, nil
	}
	ctx := NodeContext{
		ID:       rec.id,
		Class:    rec.class,
		Library:  rec.library,
		ReadOnly: w,
	}
	err = w.recoverHook("init node", func() error {
		var initErr error
		runtime, initErr = class.InitNode(ctx, cloneNodeState(rec.dynamic), mode)
		return initErr
	})
	if err != nil {
		return nil, opErr("create node", "hook", err)
	}
	return runtime, nil
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
	if err := w.recoverHook("after link attach", func() error {
		hook.AfterLinkAttach(endpoint, object)
		return nil
	}); err != nil {
		w.logPanic("after link attach", err)
	}
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
	if err := w.recoverHook("after link detach", func() error {
		hook.AfterLinkDetach(endpoint)
		return nil
	}); err != nil {
		w.logPanic("after link detach", err)
	}
}

func (w *Workspace) callAfterLinkInactive(runtime NodeRuntime, endpoint LinkEndpoint, reason InactiveReason) {
	hook, ok := runtime.(LinkInactiveHook)
	if !ok {
		return
	}
	if err := w.recoverHook("after link inactive", func() error {
		hook.AfterLinkInactive(endpoint, reason)
		return nil
	}); err != nil {
		w.logPanic("after link inactive", err)
	}
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
	if err := w.recoverHook("after inactive", func() error {
		hook.AfterInactive(reason)
		return nil
	}); err != nil {
		w.logPanic("after inactive", err)
	}
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

func (w *Workspace) callAfterDelete(runtime NodeRuntime) {
	hook, ok := runtime.(NodeDeleteHook)
	if !ok {
		return
	}
	if err := w.recoverHook("after delete", func() error {
		hook.AfterDelete()
		return nil
	}); err != nil {
		w.logPanic("after delete", err)
	}
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
			w.logPanic(op, r)
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return fn()
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

type linkInactiveEvent struct {
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
