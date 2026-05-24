package pasta

import "errors"

var (
	// ErrLinkSameNode reports that a link connects ports owned by the same node.
	ErrLinkSameNode = errors.New("same node link")
	// ErrLinkSamePort reports that a link connects a port to itself.
	ErrLinkSamePort = errors.New("same port link")
)

// Link connects one left port to one right port.
//
// Left-left or right-right links are not allowed.
// A link cannot connect two ports owned by the same node.
type Link struct {
	ID   uint64
	Type string

	// LeftPort and LeftPortNode identify the left endpoint and its owner node.
	LeftPort, LeftPortNode uint64
	// RightPort and RightPortNode identify the right endpoint and its owner node.
	RightPort, RightPortNode uint64
}

// Validate reports whether the link has a valid type and endpoints.
func (l *Link) Validate() (err error) {
	if err := ValidateTypeName(l.Type); err != nil {
		return err
	}
	if l.LeftPortNode == l.RightPortNode {
		return ErrLinkSameNode
	}
	if l.LeftPort == l.RightPort {
		return ErrLinkSamePort
	}
	return
}
