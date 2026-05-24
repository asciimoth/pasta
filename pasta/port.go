package pasta

import (
	"errors"
	"slices"
)

var (
	// ErrNoPortTypes reports that a port has no supported link types.
	ErrNoPortTypes = errors.New("port have no types")
	// ErrPortDirection reports that a port direction is neither "left" nor "right".
	ErrPortDirection = errors.New("port direction")
)

// Port is owned by node and acts as a point for links attaching.
//
// Each port direction must be "left" or "right". Each link connects one left
// port to one right port.
// Left-left or right-right links are not allowed.
type Port struct {
	Direction string // left | right
	ID        uint64 // workspace-scoped unique ID
	Node      uint64 // owner node ID

	Name string

	// List of supported link types.
	// There must be at least one type.
	//
	// AnyType can be used as a wildcard placeholder type. Node implementations
	// should generally allow attached AnyType links and ignore them when they
	// have no specific handling.
	Types []string

	Links []uint64
}

// RemoveLink removes link from the port's link list.
func (p *Port) RemoveLink(link uint64) {
	if len(p.Links) < 1 {
		return
	}
	p.Links = slices.DeleteFunc(p.Links, func(e uint64) bool {
		return e == link
	})
}

// CopyLinks returns a copy of the port's link IDs.
func (p *Port) CopyLinks() []uint64 {
	links := make([]uint64, 0, len(p.Links))
	links = append(links, p.Links...)
	return links
}

// CopyTypes returns a copy of the port's supported link types.
func (p *Port) CopyTypes() []string {
	types := make([]string, 0, len(p.Types))
	types = append(types, p.Types...)
	return types
}

// Copy returns a deep copy of the port.
func (p *Port) Copy() Port {
	return Port{
		Direction: p.Direction,
		ID:        p.ID,
		Node:      p.Node,
		Name:      p.Name,
		Types:     p.CopyTypes(),
		Links:     p.CopyLinks(),
	}
}

// Validate reports whether the port has a valid direction and type list.
func (p *Port) Validate() (err error) {
	if p.Direction != "left" && p.Direction != "right" {
		return errors.Join(ErrPortDirection, errors.New(p.Direction))
	}
	if len(p.Types) < 1 {
		return ErrNoPortTypes
	}
	for _, tp := range p.Types {
		if err := ValidateTypeName(tp); err != nil {
			return err
		}
	}
	return
}
