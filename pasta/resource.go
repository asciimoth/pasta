package pasta

import (
	"errors"
	"io"
	"reflect"
)

var (
	// ErrNoResource reports that a resource registration was requested without
	// a usable io.Closer instance.
	ErrNoResource = errors.New("resource not found")
)

type resourceKey struct {
	typ   reflect.Type
	ptr   uintptr
	value io.Closer
}

type resourceState struct {
	closer io.Closer
}

// AddNodeResource binds resource to a node lifecycle.
//
// The workspace closes the resource when the node is removed, when the node
// implementation is replaced, when a node callback failure or panic replaces
// the implementation with a placeholder, or when the workspace is closed.
//
// The same resource may be bound to multiple nodes and links. Close is called
// only once, when any bound owner is removed or replaced, or when the workspace
// closes.
func (w *Workspace) AddNodeResource(id uint64, resource io.Closer) error {
	w.Lock()
	defer w.Unlock()
	if w.closed {
		return ErrWorkspaceClosed
	}
	if record, present := w.nodes.Get(id); id < 1 || !present || record == nil {
		return ErrNoNode
	}
	return w.addResourceBindingLocked(w.nodeResources, id, resource)
}

// AddLinkResource binds resource to a link lifecycle.
//
// The workspace closes the resource when the link is removed, including links
// removed as a side effect of node or port removal, or when the workspace is
// closed. See AddNodeResource for deduplication semantics.
func (w *Workspace) AddLinkResource(id uint64, resource io.Closer) error {
	w.Lock()
	defer w.Unlock()
	if w.closed {
		return ErrWorkspaceClosed
	}
	if link, present := w.links.Get(id); id < 1 || !present || link == nil {
		return ErrNoLink
	}
	return w.addResourceBindingLocked(w.linkResources, id, resource)
}

func (w *Workspace) addResourceBindingLocked(owners map[uint64]map[resourceKey]struct{}, owner uint64, resource io.Closer) error {
	key, ok := newResourceKey(resource)
	if !ok {
		return ErrNoResource
	}
	if owners[owner] == nil {
		owners[owner] = make(map[resourceKey]struct{})
	}
	if _, present := owners[owner][key]; present {
		return nil
	}
	owners[owner][key] = struct{}{}
	if w.resources[key] == nil {
		w.resources[key] = &resourceState{closer: resource}
	}
	return nil
}

func (w *Workspace) closeNodeResourcesLocked(id uint64) {
	w.closeOwnerResourcesLocked(w.nodeResources, id)
}

func (w *Workspace) closeLinkResourcesLocked(id uint64) {
	w.closeOwnerResourcesLocked(w.linkResources, id)
}

func (w *Workspace) closeOwnerResourcesLocked(owners map[uint64]map[resourceKey]struct{}, owner uint64) {
	keys := owners[owner]
	if len(keys) < 1 {
		return
	}
	delete(owners, owner)
	for key := range keys {
		w.closeResourceLocked(key)
	}
}

func (w *Workspace) closeResourceLocked(key resourceKey) {
	state := w.resources[key]
	if state == nil {
		return
	}
	delete(w.resources, key)
	w.detachResourceLocked(key)
	CloseBackground(state.closer)
}

func (w *Workspace) detachResourceLocked(key resourceKey) {
	detachResourceOwnerLocked(w.nodeResources, key)
	detachResourceOwnerLocked(w.linkResources, key)
}

func detachResourceOwnerLocked(owners map[uint64]map[resourceKey]struct{}, key resourceKey) {
	for owner, keys := range owners {
		delete(keys, key)
		if len(keys) < 1 {
			delete(owners, owner)
		}
	}
}

func (w *Workspace) closeAllResourcesLocked() {
	for key, state := range w.resources {
		if state == nil || state.closer == nil {
			continue
		}
		CloseBackground(state.closer)
		delete(w.resources, key)
	}
	w.nodeResources = make(map[uint64]map[resourceKey]struct{})
	w.linkResources = make(map[uint64]map[resourceKey]struct{})
}

func newResourceKey(resource io.Closer) (resourceKey, bool) {
	if resource == nil {
		return resourceKey{}, false
	}
	value := reflect.ValueOf(resource)
	if !value.IsValid() {
		return resourceKey{}, false
	}
	typ := value.Type()
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		if value.IsNil() {
			return resourceKey{}, false
		}
		return resourceKey{typ: typ, ptr: value.Pointer()}, true
	default:
		if typ.Comparable() {
			return resourceKey{typ: typ, value: resource}, true
		}
		return resourceKey{}, false
	}
}
