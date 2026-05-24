package pasta

import "errors"

var (
	ErrLinkSameNode = errors.New("same node link")
	ErrLinkSamePort = errors.New("same port link")
)

// Link connects one left port to one right.
// Left-left or right-right links are not allowed.
// Link cannot connect two ports of same Node.
type Link struct {
	ID   uint64
	Type string

	// IDs of "left" type port and node owning it
	LeftPort, LeftPortNode uint64
	// IDs of "right" type port and node owning it
	RightPort, RightPortNode uint64
}

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
