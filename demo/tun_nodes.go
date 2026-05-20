package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/asciimoth/gonnect"
	"github.com/asciimoth/gonnect-netstack/spoofer"
	"github.com/asciimoth/gonnect-netstack/vtun"
	"github.com/asciimoth/gonnect/tun"
	"github.com/asciimoth/pasta/pasta"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

const (
	TunLibraryName = "tun.pasta.demo"

	TunVTunClass    = TunLibraryName + "/VTun"
	TunSpooferClass = TunLibraryName + "/Spoofer"
	TunCopyClass    = TunLibraryName + "/Copy"

	// TunType link objects are TunSetter callbacks supplied by input-port
	// owners. Output-port owners call the setter with a tun.DetachedTun wrapper
	// around the Tun they expose. Outputs may call the setter at attach time, from
	// background goroutines, and more than once for one link. Output owners must
	// track each passed DetachedTun as related to themselves and the link. Input
	// owners must track each accepted Tun as related to themselves and the link.
	//
	// A stale TunSetter, for example one whose node or link was deleted, must
	// close the passed Tun and return an error. If a TunSetter returns any error,
	// the output owner must delete the related link if it still exists.
	TunType = TunLibraryName + "/tun"
)

var (
	TunInputA = pasta.PortID{Number: 1, Kind: pasta.InputPort}
	TunInputB = pasta.PortID{Number: 2, Kind: pasta.InputPort}
	TunOutput = pasta.PortID{Number: 1, Kind: pasta.OutputPort}
)

// TunSetter is the runtime contract carried by tun.pasta.demo/tun links.
// The input-side node owns the callback and receives DetachedTun values from
// the output-side node. The callback takes ownership of accepted Tuns until the
// link or node lifecycle closes the tracked resource, or until it replaces the
// Tun with a newer call.
type TunSetter func(tun.Tun) error

type TunLibrary struct{}

func (TunLibrary) Name() string { return TunLibraryName }

func (TunLibrary) DefineClasses(scope pasta.LibraryScope) error {
	for _, class := range TunClasses() {
		if err := scope.DefineClass(class); err != nil {
			return err
		}
	}
	return nil
}

func TunClasses() []pasta.ClassSpec {
	return []pasta.ClassSpec{
		{
			Name:        TunVTunClass,
			DisplayName: "VTun",
			Description: "Adapts a gonnect-netstack VTun network side into a Tun output.",
			Default:     tunDefault("VTun", tunNodeState{Status: "initializing", LocalAddrs: "192.168.80.2", DNSServers: "192.168.80.1", Name: "vtun", NoLoopbackAddr: true}),
			Inputs:      []pasta.PortSpec{networkInput(NetworkInput, "network")},
			Outputs:     []pasta.PortSpec{tunOutput(TunOutput, "tun")},
			Runtime:     tunNodeClass{kind: "vtun"},
		},
		{
			Name:        TunSpooferClass,
			DisplayName: "Spoofer",
			Description: "Consumes a Tun and forwards intercepted TCP/UDP flows through a network output.",
			Default:     tunDefault("Spoofer", tunNodeState{Status: "waiting", Queue: 1024, TCPForwardAttempts: 2048}),
			Inputs:      []pasta.PortSpec{tunInput(TunInputA, "tun")},
			Outputs:     []pasta.PortSpec{networkOutput(NetworkOutput, "network")},
			Runtime:     tunNodeClass{kind: "spoofer"},
		},
		{
			Name:        TunCopyClass,
			DisplayName: "Tun Copy",
			Description: "Bidirectionally copies packets between two live Tun inputs.",
			Default:     tunDefault("Tun Copy", tunNodeState{Status: "waiting"}),
			Inputs:      []pasta.PortSpec{tunInput(TunInputA, "tun A"), tunInput(TunInputB, "tun B")},
			Runtime:     tunNodeClass{kind: "copy"},
		},
	}
}

func tunDefault(display string, state tunNodeState) pasta.NodeState {
	return pasta.NodeState{DisplayName: display, PrimaryType: TunType, Private: state}
}

func tunInput(id pasta.PortID, name string) pasta.PortSpec {
	return pasta.PortSpec{ID: id, Name: name, Direction: pasta.InputPort, FixedType: TunType}
}

func tunOutput(id pasta.PortID, name string) pasta.PortSpec {
	return pasta.PortSpec{ID: id, Name: name, Direction: pasta.OutputPort, FixedType: TunType}
}

type tunNodeClass struct {
	kind string
}

func (c tunNodeClass) InitNode(ctx pasta.NodeContext, state pasta.NodeState, _ pasta.InitMode) (pasta.NodeRuntime, error) {
	runCtx, cancel := context.WithCancel(context.Background())
	node := &tunNode{
		ctx:        ctx,
		runCtx:     runCtx,
		cancel:     cancel,
		kind:       c.kind,
		state:      tunStateFromAny(state.Private),
		outputGens: make(map[pasta.LinkID]uint64),
		inputs:     make(map[pasta.PortID]tunInputState),
	}
	node.normalizeDefaults()
	if err := ctx.Node.SetMenu(node.menu()); err != nil {
		cancel()
		return nil, err
	}
	if c.kind == "vtun" {
		if err := node.launchVTun(); err != nil {
			cancel()
			return nil, err
		}
	}
	return node, nil
}

type tunNodeState struct {
	Status             string `json:"status,omitempty"`
	Error              string `json:"error,omitempty"`
	LocalAddrs         string `json:"localAddrs,omitempty"`
	DNSServers         string `json:"dnsServers,omitempty"`
	Name               string `json:"name,omitempty"`
	NoLoopbackAddr     bool   `json:"noLoopbackAddr,omitempty"`
	Queue              int64  `json:"queue,omitempty"`
	TCPForwardAttempts int64  `json:"tcpForwardAttempts,omitempty"`
	Connections        int64  `json:"connections,omitempty"`
	Copies             int64  `json:"copies,omitempty"`
}

type tunInputState struct {
	link pasta.LinkID
	tun  tun.Tun
}

type tunOutputState struct {
	link   pasta.LinkID
	setter TunSetter
}

type tunNode struct {
	mu         sync.Mutex
	ctx        pasta.NodeContext
	runCtx     context.Context
	cancel     context.CancelFunc
	kind       string
	state      tunNodeState
	vtun       *vtun.VTun
	network    trackedNetwork
	networkLnk pasta.LinkID
	spoofer    *stack.Stack
	inputs     map[pasta.PortID]tunInputState
	output     tunOutputState
	outputGens map[pasta.LinkID]uint64
	copyCancel context.CancelFunc
	copyActive bool
}

func (n *tunNode) LinkObject(endpoint pasta.LinkEndpoint) (any, error) {
	switch endpoint.Type {
	case NetworkType:
		if n.kind != "vtun" || endpoint.Direction != pasta.InputPort {
			return nil, nil
		}
		n.mu.Lock()
		vt := n.vtun
		n.mu.Unlock()
		if vt == nil {
			return nil, fmt.Errorf("vtun is not running")
		}
		return gonnect.DetachNetwork(vt), nil
	case TunType:
		if endpoint.Direction != pasta.InputPort {
			return nil, nil
		}
		return TunSetter(func(tn tun.Tun) error {
			return n.acceptTun(endpoint.Link, endpoint.Self.Port, tn)
		}), nil
	default:
		return nil, nil
	}
}

func (n *tunNode) BeforeLinkAttach(endpoint pasta.LinkEndpoint, object any) error {
	switch endpoint.Type {
	case NetworkType:
		_, ok := object.(trackedNetwork)
		if !ok {
			return fmt.Errorf("network link object has type %T, want gonnect.Network+io.Closer", object)
		}
		if endpoint.Direction == pasta.OutputPort && n.kind == "spoofer" {
			n.mu.Lock()
			defer n.mu.Unlock()
			if n.networkLnk != 0 && n.networkLnk != endpoint.Link {
				return pasta.ErrMultiplicity
			}
		}
	case TunType:
		if _, ok := object.(TunSetter); !ok {
			return fmt.Errorf("tun link object has type %T, want TunSetter", object)
		}
		if endpoint.Direction == pasta.OutputPort && n.kind == "vtun" {
			n.mu.Lock()
			defer n.mu.Unlock()
			if n.output.link != 0 && n.output.link != endpoint.Link {
				return pasta.ErrMultiplicity
			}
		}
	}
	return nil
}

func (n *tunNode) AfterLinkAttach(endpoint pasta.LinkEndpoint, object any) {
	switch endpoint.Type {
	case NetworkType:
		netw, ok := object.(trackedNetwork)
		if !ok {
			return
		}
		if endpoint.Direction == pasta.InputPort && n.kind == "vtun" {
			if err := n.ctx.Node.TrackResource(netw, []pasta.LinkID{endpoint.Link}, closeTrackedNetwork); err != nil {
				n.setError(err)
			}
			return
		}
		if endpoint.Direction == pasta.OutputPort && n.kind == "spoofer" {
			if err := n.ctx.Node.TrackResource(netw, nil, closeTrackedNetwork); err != nil {
				n.setError(err)
				return
			}
			n.mu.Lock()
			n.network = netw
			n.networkLnk = endpoint.Link
			n.mu.Unlock()
			n.launchSpoofer()
		}
	case TunType:
		setter, ok := object.(TunSetter)
		if !ok || endpoint.Direction != pasta.OutputPort || n.kind != "vtun" {
			return
		}
		n.mu.Lock()
		n.output = tunOutputState{link: endpoint.Link, setter: setter}
		n.mu.Unlock()
		n.provideOutputTun(endpoint.Link)
	}
}

func (n *tunNode) BeforeLinkDetach(pasta.LinkEndpoint) error { return nil }

func (n *tunNode) AfterLinkDetach(endpoint pasta.LinkEndpoint) {
	n.detach(endpoint)
}

func (n *tunNode) AfterLinkInactive(endpoint pasta.LinkEndpoint, _ pasta.InactiveReason) {
	n.detach(endpoint)
}

func (n *tunNode) BeforeInactive(pasta.InactiveReason) error { return nil }

func (n *tunNode) AfterInactive(pasta.InactiveReason) {
	_ = n.Close()
}

func (n *tunNode) BeforeDelete() error { return nil }

func (n *tunNode) AfterDelete() {
	_ = n.Close()
}

func (n *tunNode) Close() error {
	n.cancel()
	n.mu.Lock()
	vt := n.vtun
	sp := n.spoofer
	copyCancel := n.copyCancel
	inputs := n.inputs
	n.vtun = nil
	n.spoofer = nil
	n.copyCancel = nil
	n.copyActive = false
	n.network = nil
	n.networkLnk = 0
	n.output = tunOutputState{}
	n.inputs = make(map[pasta.PortID]tunInputState)
	n.mu.Unlock()
	if copyCancel != nil {
		copyCancel()
	}
	for _, input := range inputs {
		closeTun(input.tun)
	}
	if sp != nil {
		sp.Close()
	}
	if vt != nil {
		_ = vt.Close()
	}
	return nil
}

func (n *tunNode) ApplyMenuUpdate(update pasta.MenuStateUpdate) (pasta.MenuStateUpdate, error) {
	n.mu.Lock()
	for _, field := range update.Fields {
		if field.Block != "main" {
			continue
		}
		switch field.Field {
		case "localAddrs":
			n.state.LocalAddrs = stringFromAny(field.Value)
		case "dnsServers":
			n.state.DNSServers = stringFromAny(field.Value)
		case "name":
			n.state.Name = stringFromAny(field.Value)
		case "noLoopbackAddr":
			n.state.NoLoopbackAddr = boolFromAny(field.Value)
		case "queue":
			n.state.Queue = int64(menuNumberValue(field.Value))
		case "tcpForwardAttempts":
			n.state.TCPForwardAttempts = int64(menuNumberValue(field.Value))
		}
	}
	n.normalizeDefaultsLocked()
	state := n.state
	n.mu.Unlock()
	_ = n.ctx.Node.SetPrivate(state)
	if n.kind == "vtun" {
		if err := n.launchVTun(); err != nil {
			return pasta.MenuStateUpdate{}, err
		}
	}
	if n.kind == "spoofer" {
		n.launchSpoofer()
	}
	return update, nil
}

func (n *tunNode) ImportPrivateState(private any) error {
	n.mu.Lock()
	n.state = tunStateFromAny(private)
	n.normalizeDefaultsLocked()
	n.mu.Unlock()
	n.publish()
	if n.kind == "vtun" {
		return n.launchVTun()
	}
	if n.kind == "spoofer" {
		n.launchSpoofer()
	}
	return nil
}

func (n *tunNode) ExportPrivateState() (any, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.state, nil
}

func (n *tunNode) acceptTun(link pasta.LinkID, port pasta.PortID, tn tun.Tun) error {
	if tn == nil {
		return fmt.Errorf("nil Tun")
	}
	n.mu.Lock()
	if n.runCtx.Err() != nil {
		n.mu.Unlock()
		closeTun(tn)
		return fmt.Errorf("tun setter for closed node")
	}
	snap, ok := n.ctx.Node.ReadOnly().Link(link)
	if !ok || snap.State != pasta.StateActive || snap.Input.Node != n.ctx.ID || snap.Input.Port != port {
		n.mu.Unlock()
		closeTun(tn)
		return fmt.Errorf("stale tun setter for link %s", link)
	}
	old := n.inputs[port]
	n.inputs[port] = tunInputState{link: link, tun: tn}
	n.mu.Unlock()

	if old.tun != nil && old.tun != tn {
		_ = n.ctx.Node.UntrackResource(old.tun)
		closeTun(old.tun)
	}
	if err := n.ctx.Node.TrackResource(tn, []pasta.LinkID{link}, closeTrackedTun); err != nil {
		closeTun(tn)
		return err
	}
	if n.kind == "spoofer" {
		n.launchSpoofer()
	}
	if n.kind == "copy" {
		n.maybeLaunchCopy()
	}
	n.setStatus("linked")
	return nil
}

func (n *tunNode) provideOutputTun(link pasta.LinkID) {
	n.mu.Lock()
	if n.kind != "vtun" || n.output.link != link || n.output.setter == nil || n.vtun == nil || n.runCtx.Err() != nil {
		n.mu.Unlock()
		return
	}
	setter := n.output.setter
	wrapped := tun.Detach(n.vtun)
	n.outputGens[link]++
	gen := n.outputGens[link]
	n.mu.Unlock()

	if err := n.ctx.Node.TrackResource(wrapped, []pasta.LinkID{link}, closeTrackedTun); err != nil {
		closeTun(wrapped)
		n.setError(err)
		return
	}
	if err := setter(wrapped); err != nil {
		_ = n.ctx.Node.UntrackResource(wrapped)
		closeTun(wrapped)
		n.setError(err)
		_ = n.ctx.Node.DeleteLink(link)
		return
	}
	n.setStatus("tun linked")
	go n.watchOutputTun(link, gen, wrapped)
}

func (n *tunNode) watchOutputTun(link pasta.LinkID, gen uint64, tn *tun.DetachedTun) {
	events := tn.Events()
	for range events {
	}
	select {
	case <-n.runCtx.Done():
		return
	case <-time.After(25 * time.Millisecond):
	}
	n.mu.Lock()
	current := n.output.link == link && n.outputGens[link] == gen
	n.mu.Unlock()
	if current {
		_ = n.ctx.Node.UntrackResource(tn)
		n.provideOutputTun(link)
	}
}

func (n *tunNode) launchVTun() error {
	n.mu.Lock()
	old := n.vtun
	state := n.state
	opts, err := vtunOptsFromState(state)
	if err != nil {
		n.mu.Unlock()
		n.setError(err)
		return err
	}
	n.vtun = nil
	n.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	vt, err := opts.Build()
	if err != nil {
		n.setError(err)
		return err
	}
	n.mu.Lock()
	n.vtun = vt
	n.state.Status = "running"
	n.state.Error = ""
	link := n.output.link
	n.mu.Unlock()
	n.publish()
	if link != 0 {
		n.provideOutputTun(link)
	}
	return nil
}

func (n *tunNode) launchSpoofer() {
	if n.kind != "spoofer" {
		return
	}
	n.mu.Lock()
	old := n.spoofer
	input := n.inputs[TunInputA]
	netw := n.network
	state := n.state
	n.spoofer = nil
	n.mu.Unlock()
	if old != nil {
		old.Close()
	}
	if input.tun == nil || netw == nil {
		n.setStatus("waiting")
		return
	}
	opts := &spoofer.Opts{
		TCPForwardAttempts: int(state.TCPForwardAttempts),
		OnTCPConn:          n.forwardTCP,
		OnUDPConn:          n.forwardUDP,
	}
	opts.WithTunEndpoint(input.tun, int(state.Queue))
	sp, err := opts.Launch()
	if err != nil {
		n.setError(err)
		return
	}
	n.mu.Lock()
	n.spoofer = sp
	n.state.Status = "spoofing"
	n.state.Error = ""
	n.mu.Unlock()
	n.publish()
}

func (n *tunNode) forwardTCP(c net.Conn, ep stack.TransportEndpointID) {
	n.mu.Lock()
	netw := n.network
	n.state.Connections++
	n.mu.Unlock()
	n.publish()
	if netw == nil {
		_ = c.Close()
		return
	}
	dst, err := endpointAddrPort(ep)
	if err != nil {
		n.setError(err)
		_ = c.Close()
		return
	}
	upstream, err := netw.Dial(n.runCtx, "tcp", dst.String())
	if err != nil {
		n.setError(err)
		_ = c.Close()
		return
	}
	go copyConnPair(c, upstream)
}

func (n *tunNode) forwardUDP(c gonnect.PacketConn, ep stack.TransportEndpointID) {
	n.mu.Lock()
	netw := n.network
	n.state.Connections++
	n.mu.Unlock()
	n.publish()
	if netw == nil {
		_ = c.Close()
		return
	}
	dst, err := endpointAddrPort(ep)
	if err != nil {
		n.setError(err)
		_ = c.Close()
		return
	}
	upstream, err := netw.ListenPacket(n.runCtx, "udp", "")
	if err != nil {
		n.setError(err)
		_ = c.Close()
		return
	}
	go copyPacketPair(c, upstream, net.UDPAddrFromAddrPort(dst))
}

func (n *tunNode) maybeLaunchCopy() {
	if n.kind != "copy" {
		return
	}
	n.mu.Lock()
	if n.copyActive {
		n.mu.Unlock()
		return
	}
	a := n.inputs[TunInputA].tun
	b := n.inputs[TunInputB].tun
	if a == nil || b == nil {
		n.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(n.runCtx)
	n.copyCancel = cancel
	n.copyActive = true
	n.state.Status = "copying"
	n.state.Error = ""
	n.state.Copies++
	n.mu.Unlock()
	n.publish()
	go n.runCopy(ctx, a, b)
}

func (n *tunNode) runCopy(ctx context.Context, a, b tun.Tun) {
	errCh := make(chan error, 1)
	go func() { errCh <- tun.Copy(a, b) }()
	var err error
	select {
	case <-ctx.Done():
		closeTun(a)
		closeTun(b)
		err = ctx.Err()
	case err = <-errCh:
	}
	n.mu.Lock()
	for port, input := range n.inputs {
		if input.tun == a || input.tun == b {
			_ = n.ctx.Node.UntrackResource(input.tun)
			delete(n.inputs, port)
		}
	}
	n.copyActive = false
	n.copyCancel = nil
	if err != nil && !errors.Is(err, context.Canceled) && !tun.IsTunTermError(err) {
		n.state.Status = "error"
		n.state.Error = err.Error()
	} else {
		n.state.Status = "waiting"
		n.state.Error = ""
	}
	n.mu.Unlock()
	n.publish()
	n.maybeLaunchCopy()
}

func (n *tunNode) detach(endpoint pasta.LinkEndpoint) {
	n.mu.Lock()
	if endpoint.Type == TunType && endpoint.Direction == pasta.OutputPort && n.output.link == endpoint.Link {
		n.output = tunOutputState{}
		delete(n.outputGens, endpoint.Link)
		n.mu.Unlock()
		n.setStatus("running")
		return
	}
	if endpoint.Type == TunType && endpoint.Direction == pasta.InputPort {
		input := n.inputs[endpoint.Self.Port]
		if input.link == endpoint.Link {
			delete(n.inputs, endpoint.Self.Port)
		}
		copyCancel := n.copyCancel
		n.mu.Unlock()
		if input.tun != nil {
			closeTun(input.tun)
		}
		if copyCancel != nil {
			copyCancel()
		}
		if n.kind == "spoofer" {
			n.launchSpoofer()
		}
		n.setStatus("waiting")
		return
	}
	if endpoint.Type == NetworkType && endpoint.Direction == pasta.OutputPort && n.networkLnk == endpoint.Link {
		n.network = nil
		n.networkLnk = 0
		sp := n.spoofer
		n.spoofer = nil
		n.mu.Unlock()
		if sp != nil {
			sp.Close()
		}
		n.setStatus("waiting")
		return
	}
	n.mu.Unlock()
}

func (n *tunNode) normalizeDefaults() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.normalizeDefaultsLocked()
}

func (n *tunNode) normalizeDefaultsLocked() {
	if n.kind == "vtun" {
		if strings.TrimSpace(n.state.LocalAddrs) == "" {
			n.state.LocalAddrs = "192.168.80.2"
		}
		if strings.TrimSpace(n.state.Name) == "" {
			n.state.Name = "vtun"
		}
	}
	if n.kind == "spoofer" {
		if n.state.Queue < 1 {
			n.state.Queue = 1024
		}
		if n.state.TCPForwardAttempts < 1 {
			n.state.TCPForwardAttempts = 2048
		}
	}
	if n.state.Status == "" {
		n.state.Status = "waiting"
	}
}

func (n *tunNode) setStatus(status string) {
	n.mu.Lock()
	n.state.Status = status
	n.state.Error = ""
	n.mu.Unlock()
	n.publish()
}

func (n *tunNode) setError(err error) {
	n.mu.Lock()
	n.state.Status = "error"
	n.state.Error = err.Error()
	n.mu.Unlock()
	n.publish()
}

func (n *tunNode) publish() {
	n.mu.Lock()
	state := n.state
	n.mu.Unlock()
	_ = n.ctx.Node.SetPrivate(state)
	_ = n.ctx.Node.SetMenu(n.menu())
}

func (n *tunNode) menu() pasta.NodeMenu {
	n.mu.Lock()
	kind := n.kind
	state := n.state
	n.mu.Unlock()
	fields := []pasta.MenuField{
		{ID: "status", Label: "Status", Kind: pasta.MenuFieldReadOnly, ReadOnly: true, Value: state.Status},
		{ID: "error", Label: "Error", Kind: pasta.MenuFieldReadOnly, ReadOnly: true, Value: state.Error},
	}
	switch kind {
	case "vtun":
		fields = append([]pasta.MenuField{
			{ID: "localAddrs", Label: "Local addresses", Kind: pasta.MenuFieldString, Value: state.LocalAddrs},
			{ID: "dnsServers", Label: "DNS servers", Kind: pasta.MenuFieldString, Value: state.DNSServers},
			{ID: "name", Label: "Name", Kind: pasta.MenuFieldString, Value: state.Name},
			{ID: "noLoopbackAddr", Label: "No loopback address", Kind: pasta.MenuFieldBool, Render: pasta.MenuRenderCheckbox, Value: state.NoLoopbackAddr},
		}, fields...)
	case "spoofer":
		fields = append([]pasta.MenuField{
			{ID: "queue", Label: "Queue", Kind: pasta.MenuFieldInt64, Value: state.Queue},
			{ID: "tcpForwardAttempts", Label: "TCP attempts", Kind: pasta.MenuFieldInt64, Value: state.TCPForwardAttempts},
			{ID: "connections", Label: "Connections", Kind: pasta.MenuFieldReadOnly, ReadOnly: true, Value: state.Connections},
		}, fields...)
	case "copy":
		fields = append([]pasta.MenuField{
			{ID: "copies", Label: "Copies", Kind: pasta.MenuFieldReadOnly, ReadOnly: true, Value: state.Copies},
		}, fields...)
	}
	return pasta.NodeMenu{Committable: kind == "vtun" || kind == "spoofer", Blocks: []pasta.MenuBlock{{ID: "main", Title: "Tun", Fields: fields}}}
}

func vtunOptsFromState(state tunNodeState) (*vtun.Opts, error) {
	local, err := parseAddrList(state.LocalAddrs)
	if err != nil {
		return nil, err
	}
	dns, err := parseAddrList(state.DNSServers)
	if err != nil {
		return nil, err
	}
	return &vtun.Opts{
		LocalAddrs:     local,
		DnsServers:     dns,
		Name:           firstNonEmptyString(strings.TrimSpace(state.Name), "vtun"),
		NoLoopbackAddr: state.NoLoopbackAddr,
	}, nil
}

func parseAddrList(raw string) ([]netip.Addr, error) {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\t' || r == ' '
	})
	addrs := make([]netip.Addr, 0, len(fields))
	for _, field := range fields {
		if field == "" {
			continue
		}
		addr, err := netip.ParseAddr(field)
		if err != nil {
			return nil, err
		}
		addrs = append(addrs, addr)
	}
	return addrs, nil
}

func endpointAddrPort(ep stack.TransportEndpointID) (netip.AddrPort, error) {
	addr, ok := netip.AddrFromSlice(ep.LocalAddress.AsSlice())
	if !ok {
		return netip.AddrPort{}, fmt.Errorf("invalid endpoint address %s", ep.LocalAddress)
	}
	return netip.AddrPortFrom(addr, uint16(ep.LocalPort)), nil
}

func copyConnPair(a, b net.Conn) {
	var once sync.Once
	closeBoth := func() {
		_ = a.Close()
		_ = b.Close()
	}
	go func() {
		_, _ = io.Copy(a, b)
		once.Do(closeBoth)
	}()
	go func() {
		_, _ = io.Copy(b, a)
		once.Do(closeBoth)
	}()
}

func copyPacketPair(local gonnect.PacketConn, upstream net.PacketConn, dst net.Addr) {
	var once sync.Once
	closeBoth := func() {
		_ = local.Close()
		_ = upstream.Close()
	}
	go func() {
		defer once.Do(closeBoth)
		buf := make([]byte, 64*1024)
		for {
			n, _, err := local.ReadFrom(buf)
			if err != nil {
				return
			}
			if _, err := upstream.WriteTo(buf[:n], dst); err != nil {
				return
			}
		}
	}()
	go func() {
		defer once.Do(closeBoth)
		buf := make([]byte, 64*1024)
		for {
			n, _, err := upstream.ReadFrom(buf)
			if err != nil {
				return
			}
			if _, err := local.Write(buf[:n]); err != nil {
				return
			}
		}
	}()
}

func closeTun(tn tun.Tun) {
	if tn != nil {
		_ = tn.Close()
	}
}

func closeTrackedTun(resource any) error {
	tn, ok := resource.(tun.Tun)
	if !ok || tn == nil {
		return nil
	}
	return tn.Close()
}

func tunStateFromAny(v any) tunNodeState {
	switch x := v.(type) {
	case tunNodeState:
		return x
	case map[string]any:
		return tunNodeState{
			Status:             stringFromAny(x["status"]),
			Error:              stringFromAny(x["error"]),
			LocalAddrs:         stringFromAny(x["localAddrs"]),
			DNSServers:         stringFromAny(x["dnsServers"]),
			Name:               stringFromAny(x["name"]),
			NoLoopbackAddr:     boolFromAny(x["noLoopbackAddr"]),
			Queue:              int64(menuNumberValue(x["queue"])),
			TCPForwardAttempts: int64(menuNumberValue(x["tcpForwardAttempts"])),
			Connections:        int64(menuNumberValue(x["connections"])),
			Copies:             int64(menuNumberValue(x["copies"])),
		}
	default:
		return tunNodeState{}
	}
}
