package pasta

import (
	"fmt"
	"reflect"
	"slices"
)

type resourceKey struct {
	typ   reflect.Type
	value any
}

type resourceRecord struct {
	key        resourceKey
	resource   any
	relations  ResourceRelations
	destructor ResourceDestructor
	order      int64
}

type resourceDestroyEvent struct {
	resource   any
	destructor ResourceDestructor
	order      int64
}

// TrackResource tracks a resource until any related active node or link becomes inactive or is removed.
//
// Resource identity uses normal Go equality, so nil resources and values whose
// dynamic type is not comparable return ErrInvalidResource. Registering the
// same resource again merges in the new relations and replaces the previous
// destructor without calling it.
func (w *Workspace) TrackResource(resource any, relations ResourceRelations, destructor ResourceDestructor) error {
	key, err := makeResourceKey(resource)
	if err != nil {
		return opErr("track resource", "validate", err)
	}
	if destructor == nil || (len(relations.Nodes) == 0 && len(relations.Links) == 0) {
		return opErr("track resource", "validate", ErrInvalidResource)
	}
	clean := cloneResourceRelations(relations)
	w.mu.Lock()
	callNow := false
	if w.closed {
		callNow = true
	} else if !w.resourceRelationsActiveLocked(clean) {
		callNow = true
	} else {
		if existing := w.resources[key]; existing != nil {
			existing.resource = resource
			existing.relations = mergeResourceRelations(existing.relations, clean)
			existing.destructor = destructor
		} else {
			w.nextResource++
			w.resources[key] = &resourceRecord{
				key:        key,
				resource:   resource,
				relations:  clean,
				destructor: destructor,
				order:      w.nextResource,
			}
		}
	}
	w.mu.Unlock()
	if callNow {
		if err := w.callResourceDestructor(destructor, resource); err != nil {
			return opErr("track resource", "hook", err)
		}
	}
	return nil
}

// UntrackResource stops tracking a resource that has already been released by its owner.
//
// Calling UntrackResource for a comparable resource that is not currently
// tracked is a no-op.
func (w *Workspace) UntrackResource(resource any) error {
	key, err := makeResourceKey(resource)
	if err != nil {
		return opErr("untrack resource", "validate", err)
	}
	w.mu.Lock()
	delete(w.resources, key)
	w.mu.Unlock()
	return nil
}

func (s *nodeScope) TrackResource(resource any, links []LinkID, destructor ResourceDestructor) error {
	relations := ResourceRelations{
		Nodes: []NodeID{s.id},
		Links: append([]LinkID(nil), links...),
	}
	return s.w.TrackResource(resource, relations, destructor)
}

func (s *nodeScope) UntrackResource(resource any) error {
	return s.w.UntrackResource(resource)
}

func makeResourceKey(resource any) (resourceKey, error) {
	if resource == nil {
		return resourceKey{}, ErrInvalidResource
	}
	typ := reflect.TypeOf(resource)
	if typ == nil || !typ.Comparable() {
		return resourceKey{}, ErrInvalidResource
	}
	return resourceKey{typ: typ, value: resource}, nil
}

func cloneResourceRelations(rel ResourceRelations) ResourceRelations {
	nodes := append([]NodeID(nil), rel.Nodes...)
	links := append([]LinkID(nil), rel.Links...)
	slices.Sort(nodes)
	nodes = slices.Compact(nodes)
	slices.Sort(links)
	links = slices.Compact(links)
	return ResourceRelations{Nodes: nodes, Links: links}
}

func mergeResourceRelations(a, b ResourceRelations) ResourceRelations {
	return cloneResourceRelations(ResourceRelations{
		Nodes: append(append([]NodeID(nil), a.Nodes...), b.Nodes...),
		Links: append(append([]LinkID(nil), a.Links...), b.Links...),
	})
}

func (w *Workspace) resourceRelationsActiveLocked(rel ResourceRelations) bool {
	for _, id := range rel.Nodes {
		node := w.nodes[id]
		if node == nil || node.state != StateActive {
			return false
		}
	}
	for _, id := range rel.Links {
		link := w.links[id]
		if link == nil || link.state != StateActive {
			return false
		}
	}
	return true
}

func (w *Workspace) collectResourceEventsLocked(nodes map[NodeID]bool, links map[LinkID]bool) []resourceDestroyEvent {
	if len(w.resources) == 0 {
		return nil
	}
	events := make([]resourceDestroyEvent, 0)
	for key, rec := range w.resources {
		if rec == nil || !resourceTouches(rec.relations, nodes, links) {
			continue
		}
		events = append(events, resourceDestroyEvent{
			resource:   rec.resource,
			destructor: rec.destructor,
			order:      rec.order,
		})
		delete(w.resources, key)
	}
	slices.SortFunc(events, func(a, b resourceDestroyEvent) int {
		switch {
		case a.order < b.order:
			return -1
		case a.order > b.order:
			return 1
		default:
			return 0
		}
	})
	return events
}

func (w *Workspace) collectAllResourceEventsLocked() []resourceDestroyEvent {
	nodes := make(map[NodeID]bool, len(w.nodes))
	for id := range w.nodes {
		nodes[id] = true
	}
	links := make(map[LinkID]bool, len(w.links))
	for id := range w.links {
		links[id] = true
	}
	return w.collectResourceEventsLocked(nodes, links)
}

func resourceTouches(rel ResourceRelations, nodes map[NodeID]bool, links map[LinkID]bool) bool {
	for _, id := range rel.Nodes {
		if nodes[id] {
			return true
		}
	}
	for _, id := range rel.Links {
		if links[id] {
			return true
		}
	}
	return false
}

func (w *Workspace) callResourceDestroyEvents(events []resourceDestroyEvent) error {
	var first error
	for _, event := range events {
		if err := w.callResourceDestructor(event.destructor, event.resource); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (w *Workspace) callResourceDestructor(destructor ResourceDestructor, resource any) error {
	if destructor == nil {
		return nil
	}
	return w.recoverHook("destroy resource", func() error {
		if err := destructor(resource); err != nil {
			return fmt.Errorf("resource destructor: %w", err)
		}
		return nil
	})
}
