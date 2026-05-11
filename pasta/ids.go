package pasta

import (
	"fmt"
	"strconv"
	"strings"
)

// NodeID identifies a node within a workspace.
type NodeID int64

// PortID identifies a port within one node.
type PortID struct {
	Number int64
	Kind   PortDirection
}

// LinkID identifies a link within a workspace.
type LinkID int64

// FullPortID identifies a port globally within a workspace.
type FullPortID struct {
	Node NodeID
	Port PortID
}

// FullLinkName is the canonical persisted link name.
type FullLinkName struct {
	Link   LinkID
	Input  FullPortID
	Output FullPortID
}

// String returns the canonical node ID form, such as 123N.
func (id NodeID) String() string { return strconv.FormatInt(int64(id), 10) + "N" }

// String returns the canonical link ID form, such as 123L.
func (id LinkID) String() string { return strconv.FormatInt(int64(id), 10) + "L" }

// String returns the canonical port ID form, such as 123i or 123o.
func (id PortID) String() string {
	suffix := "?"
	switch id.Kind {
	case InputPort:
		suffix = "i"
	case OutputPort:
		suffix = "o"
	}
	return strconv.FormatInt(id.Number, 10) + suffix
}

// String returns the canonical full port ID form, such as 123N456o.
func (id FullPortID) String() string { return id.Node.String() + id.Port.String() }

// String returns the canonical full link name used by persistence.
func (n FullLinkName) String() string {
	return n.Link.String() + ":" + n.Input.String() + ":" + n.Output.String()
}

// ParseNodeID parses a canonical node ID such as 123N.
func ParseNodeID(s string) (NodeID, error) {
	n, err := parseSuffixedInt(s, 'N')
	return NodeID(n), err
}

// ParseLinkID parses a canonical link ID such as 123L.
func ParseLinkID(s string) (LinkID, error) {
	n, err := parseSuffixedInt(s, 'L')
	return LinkID(n), err
}

// ParsePortID parses a canonical port ID such as 123i or 123o.
func ParsePortID(s string) (PortID, error) {
	if len(s) < 2 {
		return PortID{}, ErrInvalidID
	}
	var kind PortDirection
	switch s[len(s)-1] {
	case 'i':
		kind = InputPort
	case 'o':
		kind = OutputPort
	default:
		return PortID{}, ErrInvalidID
	}
	n, err := strconv.ParseInt(s[:len(s)-1], 10, 64)
	if err != nil || n <= 0 {
		return PortID{}, ErrInvalidID
	}
	return PortID{Number: n, Kind: kind}, nil
}

// ParseFullPortID parses a canonical full port ID such as 123N456o.
func ParseFullPortID(s string) (FullPortID, error) {
	pos := strings.IndexByte(s, 'N')
	if pos <= 0 || pos == len(s)-1 {
		return FullPortID{}, ErrInvalidID
	}
	node, err := ParseNodeID(s[:pos+1])
	if err != nil {
		return FullPortID{}, err
	}
	port, err := ParsePortID(s[pos+1:])
	if err != nil {
		return FullPortID{}, err
	}
	return FullPortID{Node: node, Port: port}, nil
}

// ParseFullLinkName parses the canonical full link name.
func ParseFullLinkName(s string) (FullLinkName, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return FullLinkName{}, ErrInvalidID
	}
	link, err := ParseLinkID(parts[0])
	if err != nil {
		return FullLinkName{}, err
	}
	input, err := ParseFullPortID(parts[1])
	if err != nil {
		return FullLinkName{}, err
	}
	output, err := ParseFullPortID(parts[2])
	if err != nil {
		return FullLinkName{}, err
	}
	if input.Port.Kind != InputPort || output.Port.Kind != OutputPort {
		return FullLinkName{}, fmt.Errorf("%w: full link endpoint direction", ErrInvalidID)
	}
	return FullLinkName{Link: link, Input: input, Output: output}, nil
}

func parseSuffixedInt(s string, suffix byte) (int64, error) {
	if len(s) < 2 || s[len(s)-1] != suffix {
		return 0, ErrInvalidID
	}
	n, err := strconv.ParseInt(s[:len(s)-1], 10, 64)
	if err != nil || n <= 0 {
		return 0, ErrInvalidID
	}
	return n, nil
}
