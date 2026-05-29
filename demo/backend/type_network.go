package main

import (
	"io"
	"time"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/gonnect"
	"github.com/asciimoth/pasta/pasta"
)

// typeNetwork is the Pasta link type carrying Network values backed by
// gonnect.Network+io.Closer.
//
// Values flow from left-directed ports to right-directed ports, which
// corresponds to right-to-left graph data flow (opposite of standard pasta/std
// types like int/float/bool/string). A node owning a left-directed
// pasta/network port sends its current Network instance, whose underlying type
// implements gonnect.Network+io.Closer, on every connected link when a new
// link connects, when OnReady runs, when the owned Network instance is
// replaced, and when the peer sends RequestValue from the right-directed side.
// If a left-directed port does not yet have a ready Network instance, it
// defers sending until a value becomes available, then broadcasts to all
// connected peers. A node owning a right-directed pasta/network port may send
// RequestValue when it needs the current Network object again. Both sides must
// register any received Network object with the Workspace resource tracking
// system, bound to the owning node and link. The event payload implements
// std.ClosablePayload, so middleware nodes such as std.SelectClass can close a
// previously-routed wrapper when switching paths. Peers that only implement
// gonnect.Network without io.Closer should wrap their instance via
// gonnect.DetachNetwork before using it with this link type. Left-directed
// pasta/network ports allow an arbitrary count of incoming links; right-directed
// pasta/network ports allow at most one outgoing link.
const typeNetwork = "demo.pasta/network"

type networkCloser interface {
	gonnect.Network
	io.Closer
}

type networkPayload struct {
	Network networkCloser
}

func (p networkPayload) Close() error {
	if p.Network == nil {
		return nil
	}
	return p.Network.Close()
}

func networkPort(direction, name string) pasta.Port {
	return pasta.Port{Direction: direction, Name: name, Types: []string{typeNetwork}}
}

func bindNetworkResource(w *pasta.Workspace, node, link uint64, n networkCloser) {
	if w == nil || n == nil {
		return
	}
	_ = w.AddNodeResource(node, n)
	if link != 0 {
		_ = w.AddLinkResource(link, n)
	}
}

func readConfigString(cfg configer.Config, key, fallback string) string {
	if cfg == nil {
		return fallback
	}
	raw, err := cfg.Get(configer.Path{key})
	if err != nil {
		return fallback
	}
	if value, ok := raw.(string); ok {
		return value
	}
	return fallback
}

func readConfigInt(cfg configer.Config, key string, fallback int) int {
	if cfg == nil {
		return fallback
	}
	raw, err := cfg.Get(configer.Path{key})
	if err != nil {
		return fallback
	}
	switch value := raw.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return fallback
	}
}

func sleepOrStopped(stop <-chan struct{}, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-stop:
		return false
	case <-timer.C:
		return true
	}
}
