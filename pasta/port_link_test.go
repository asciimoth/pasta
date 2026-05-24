package pasta_test

import (
	"errors"
	"testing"

	"github.com/asciimoth/pasta/pasta"
)

func TestPortRemoveLink(t *testing.T) {
	port := pasta.Port{Links: []uint64{1, 2, 3, 2}}

	port.RemoveLink(2)

	want := []uint64{1, 3}
	if !equalUint64s(port.Links, want) {
		t.Fatalf("links = %v, want %v", port.Links, want)
	}

	port.RemoveLink(99)
	if !equalUint64s(port.Links, want) {
		t.Fatalf("links after removing missing link = %v, want %v", port.Links, want)
	}
}

func TestPortCopyIsDeepCopy(t *testing.T) {
	port := pasta.Port{
		Direction: "left",
		ID:        10,
		Node:      20,
		Name:      "input",
		Types:     []string{"example.com/typeA"},
		Links:     []uint64{1, 2},
	}

	copied := port.Copy()
	copied.Types[0] = "example.com/typeB"
	copied.Links[0] = 99

	if port.Types[0] != "example.com/typeA" {
		t.Fatalf("original type changed to %q", port.Types[0])
	}
	if port.Links[0] != 1 {
		t.Fatalf("original link changed to %d", port.Links[0])
	}
	if copied.Name != port.Name {
		t.Fatalf("copied name = %q, want %q", copied.Name, port.Name)
	}
}

func TestPortValidate(t *testing.T) {
	tests := []struct {
		name string
		port pasta.Port
		want error
	}{
		{
			name: "valid left",
			port: pasta.Port{Direction: "left", Types: []string{"example.com/typeA"}},
		},
		{
			name: "valid right",
			port: pasta.Port{Direction: "right", Types: []string{"example.com/typeA"}},
		},
		{
			name: "valid any",
			port: pasta.Port{Direction: "left", Types: []string{pasta.AnyType}},
		},
		{
			name: "bad direction",
			port: pasta.Port{Direction: "top", Types: []string{"example.com/typeA"}},
			want: pasta.ErrPortDirection,
		},
		{
			name: "no types",
			port: pasta.Port{Direction: "left"},
			want: pasta.ErrNoPortTypes,
		},
		{
			name: "invalid type",
			port: pasta.Port{Direction: "left", Types: []string{"example.com/TypeA"}},
			want: pasta.ErrTypeName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.port.Validate()
			if !errors.Is(err, tt.want) {
				t.Fatalf("Validate() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestLinkValidate(t *testing.T) {
	tests := []struct {
		name string
		link pasta.Link
		want error
	}{
		{
			name: "valid",
			link: pasta.Link{
				Type:          "example.com/typeA",
				LeftPort:      1,
				LeftPortNode:  10,
				RightPort:     2,
				RightPortNode: 20,
			},
		},
		{
			name: "invalid type",
			link: pasta.Link{Type: "example.com/TypeA", LeftPort: 1, LeftPortNode: 10, RightPort: 2, RightPortNode: 20},
			want: pasta.ErrTypeName,
		},
		{
			name: "same node",
			link: pasta.Link{Type: "example.com/typeA", LeftPort: 1, LeftPortNode: 10, RightPort: 2, RightPortNode: 10},
			want: pasta.ErrLinkSameNode,
		},
		{
			name: "same port",
			link: pasta.Link{Type: "example.com/typeA", LeftPort: 1, LeftPortNode: 10, RightPort: 1, RightPortNode: 20},
			want: pasta.ErrLinkSamePort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.link.Validate()
			if !errors.Is(err, tt.want) {
				t.Fatalf("Validate() = %v, want %v", err, tt.want)
			}
		})
	}
}

func equalUint64s(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
