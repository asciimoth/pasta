package pasta

import (
	"errors"
	"slices"
)

var (
	ErrNoPortTypes   = errors.New("port have no types")
	ErrPortDirection = errors.New("port direction")
)

// Port is owned by node and acts as a point for links attaching.
// Each Port can be "left" of "right".
// Each link can connect one left port to one right.
// Left-left or right-right links are not allowed.
type Port struct {
	Direction string // left | right
	ID        uint64 // must be Workspace scope uinique
	Node      uint64 // ID of owner node

	Name uint64

	// List of supported link types.
	// There must be at least one type.
	Types []string

	Links []uint64
}

func (p *Port) RemoveLink(link uint64) {
	if len(p.Links) < 1 {
		return
	}
	_ = slices.DeleteFunc(p.Links, func(e uint64) bool {
		return e == link
	})
}

func (p *Port) CopyLinks() []uint64 {
	links := make([]uint64, 0, len(p.Links))
	links = append(links, p.Links...)
	return links
}

func (p *Port) CopyTypes() []string {
	types := make([]string, 0, len(p.Types))
	types = append(types, p.Types...)
	return types
}

func (p *Port) Copy() Port {
	return Port{
		Direction: p.Direction,
		ID:        p.ID,
		Node:      p.Node,
		Types:     p.CopyTypes(),
		Links:     p.CopyLinks(),
	}
}

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
