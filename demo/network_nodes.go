package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/asciimoth/gonnect"
	"github.com/asciimoth/pasta/pasta"
)

const (
	NetworkLibraryName = "network.pasta.demo"

	NetworkLoopbackClass = NetworkLibraryName + "/Loopback"
	NetworkLoggerClass   = NetworkLibraryName + "/Logger"
	NetworkServerClass   = NetworkLibraryName + "/HTTPServer"
	NetworkClientClass   = NetworkLibraryName + "/HTTPClient"

	NetworkType = NetworkLibraryName + "/network"
)

var (
	NetworkInput  = pasta.PortID{Number: 1, Kind: pasta.InputPort}
	NetworkOutput = pasta.PortID{Number: 1, Kind: pasta.OutputPort}
)

type trackedNetwork interface {
	gonnect.Network
	io.Closer
}

type NetworkLibrary struct{}

func (NetworkLibrary) Name() string { return NetworkLibraryName }

func (NetworkLibrary) DefineClasses(scope pasta.LibraryScope) error {
	for _, class := range NetworkClasses() {
		if err := scope.DefineClass(class); err != nil {
			return err
		}
	}
	return nil
}

func NetworkClasses() []pasta.ClassSpec {
	return []pasta.ClassSpec{
		{
			Name:        NetworkLoopbackClass,
			DisplayName: "Loopback Network",
			Description: "Creates detached virtual network handles for each attached link.",
			Default:     networkDefault("Loopback Network", networkState{Status: "ready"}),
			Inputs:      []pasta.PortSpec{networkInput(NetworkInput, "network")},
			Runtime:     networkNodeClass{kind: "loopback"},
		},
		{
			Name:        NetworkLoggerClass,
			DisplayName: "Network Logger",
			Description: "Wraps a linked network and logs network operations passing through it.",
			Default:     networkDefault("Network Logger", networkState{Status: "waiting"}),
			Inputs:      []pasta.PortSpec{networkInput(NetworkInput, "logged")},
			Outputs:     []pasta.PortSpec{networkOutput(NetworkOutput, "source")},
			Runtime:     networkNodeClass{kind: "logger"},
		},
		{
			Name:        NetworkServerClass,
			DisplayName: "HTTP Server",
			Description: "Runs an HTTP server on a linked network.",
			Default:     networkDefault("HTTP Server", networkState{Address: "127.0.0.1:8080", Status: "waiting", Response: "pasta demo response for GET /"}),
			Outputs:     []pasta.PortSpec{networkOutput(NetworkOutput, "network")},
			Runtime:     networkNodeClass{kind: "server"},
		},
		{
			Name:        NetworkClientClass,
			DisplayName: "HTTP Client",
			Description: "Sends HTTP requests through a linked network.",
			Default:     networkDefault("HTTP Client", networkState{Address: "http://127.0.0.1:8080/", Status: "waiting"}),
			Outputs:     []pasta.PortSpec{networkOutput(NetworkOutput, "network")},
			Runtime:     networkNodeClass{kind: "client"},
		},
	}
}

func networkDefault(display string, state networkState) pasta.NodeState {
	return pasta.NodeState{
		DisplayName: display,
		PrimaryType: NetworkType,
		Private:     state,
	}
}

func networkInput(id pasta.PortID, name string) pasta.PortSpec {
	return pasta.PortSpec{
		ID:        id,
		Name:      name,
		Direction: pasta.InputPort,
		FixedType: NetworkType,
		Multiple:  true,
	}
}

func networkOutput(id pasta.PortID, name string) pasta.PortSpec {
	return pasta.PortSpec{
		ID:        id,
		Name:      name,
		Direction: pasta.OutputPort,
		FixedType: NetworkType,
	}
}

type networkNodeClass struct {
	kind string
}

func (c networkNodeClass) InitNode(ctx pasta.NodeContext, state pasta.NodeState, _ pasta.InitMode) (pasta.NodeRuntime, error) {
	runCtx, cancel := context.WithCancel(context.Background())
	node := &networkNode{
		ctx:       ctx,
		runCtx:    runCtx,
		cancel:    cancel,
		kind:      c.kind,
		state:     networkStateFromAny(state.Private),
		base:      gonnect.NewLoopbackNetwok(),
		reattach:  make(chan struct{}),
		requestCh: make(chan struct{}, 1),
	}
	if node.state.Address == "" {
		if c.kind == "client" {
			node.state.Address = "http://127.0.0.1:8080/"
		} else {
			node.state.Address = "127.0.0.1:8080"
		}
	}
	if c.kind == "server" && node.state.Response == "" {
		node.state.Response = "pasta demo response for GET /"
	}
	if err := ctx.Node.SetMenu(node.menu()); err != nil {
		cancel()
		return nil, err
	}
	if c.kind == "client" {
		go node.clientLoop()
	}
	return node, nil
}

type networkState struct {
	Address  string `json:"address,omitempty"`
	Status   string `json:"status,omitempty"`
	Response string `json:"response,omitempty"`
	Error    string `json:"error,omitempty"`
	Logs     string `json:"logs,omitempty"`
	Requests int64  `json:"requests,omitempty"`
}

type networkNode struct {
	mu        sync.Mutex
	ctx       pasta.NodeContext
	runCtx    context.Context
	cancel    context.CancelFunc
	kind      string
	state     networkState
	base      gonnect.Network
	link      pasta.LinkID
	network   trackedNetwork
	server    *http.Server
	serverCan context.CancelFunc
	serverGen int64
	reattach  chan struct{}
	requestCh chan struct{}
}

func (n *networkNode) LinkObject(endpoint pasta.LinkEndpoint) (any, error) {
	if endpoint.Direction != pasta.InputPort {
		return nil, nil
	}
	switch n.kind {
	case "loopback":
		return gonnect.DetachNetwork(n.base), nil
	case "logger":
		return &loggingNetwork{node: n, log: n.logNetwork}, nil
	default:
		return nil, nil
	}
}

func (n *networkNode) BeforeLinkAttach(endpoint pasta.LinkEndpoint, object any) error {
	netw, ok := object.(trackedNetwork)
	if !ok {
		return fmt.Errorf("network link object has type %T, want gonnect.Network+io.Closer", object)
	}
	if endpoint.Direction == pasta.OutputPort {
		n.mu.Lock()
		defer n.mu.Unlock()
		if n.link != 0 && n.link != endpoint.Link {
			return pasta.ErrMultiplicity
		}
	}
	_ = netw
	return nil
}

func (n *networkNode) AfterLinkAttach(endpoint pasta.LinkEndpoint, object any) {
	netw, ok := object.(trackedNetwork)
	if !ok {
		return
	}
	if endpoint.Direction == pasta.InputPort && n.kind == "loopback" {
		if err := n.ctx.Node.TrackResource(netw, []pasta.LinkID{endpoint.Link}, closeTrackedNetwork); err != nil {
			n.setError(err)
		}
		return
	}
	if endpoint.Direction == pasta.InputPort && n.kind == "logger" {
		if err := n.ctx.Node.TrackResource(netw, []pasta.LinkID{endpoint.Link}, closeTrackedNetwork); err != nil {
			n.setError(err)
		}
		n.setStatus("logging")
		return
	}
	if endpoint.Direction != pasta.OutputPort {
		return
	}
	if err := n.ctx.Node.TrackResource(netw, nil, closeTrackedNetwork); err != nil {
		n.setError(err)
		return
	}
	n.mu.Lock()
	n.link = endpoint.Link
	n.network = netw
	n.wakeReattachLocked()
	n.mu.Unlock()
	if n.kind == "server" {
		n.startServer()
	}
	n.setStatus("linked")
}

func (n *networkNode) BeforeLinkDetach(pasta.LinkEndpoint) error { return nil }

func (n *networkNode) AfterLinkDetach(endpoint pasta.LinkEndpoint) {
	n.detach(endpoint.Link)
}

func (n *networkNode) AfterLinkInactive(endpoint pasta.LinkEndpoint, _ pasta.InactiveReason) {
	n.detach(endpoint.Link)
}

func (n *networkNode) BeforeInactive(pasta.InactiveReason) error { return nil }

func (n *networkNode) AfterInactive(pasta.InactiveReason) {
	_ = n.Close()
}

func (n *networkNode) BeforeDelete() error { return nil }

func (n *networkNode) AfterDelete() {
	_ = n.Close()
}

func (n *networkNode) Close() error {
	n.cancel()
	n.mu.Lock()
	serverCancel := n.serverCan
	server := n.server
	n.serverCan = nil
	n.server = nil
	n.link = 0
	n.network = nil
	n.wakeReattachLocked()
	n.mu.Unlock()
	if serverCancel != nil {
		serverCancel()
	}
	if server != nil {
		_ = server.Close()
	}
	return nil
}

func (n *networkNode) ApplyMenuUpdate(update pasta.MenuStateUpdate) (pasta.MenuStateUpdate, error) {
	n.mu.Lock()
	for _, field := range update.Fields {
		if field.Block == "main" && field.Field == "address" {
			n.state.Address = stringFromAny(field.Value)
		}
		if field.Block == "main" && field.Field == "response" && n.kind == "server" {
			n.state.Response = stringFromAny(field.Value)
		}
	}
	state := n.state
	n.mu.Unlock()
	if n.kind == "server" {
		n.startServer()
	}
	_ = n.ctx.Node.SetPrivate(state)
	return update, nil
}

func (n *networkNode) TriggerMenuButton(ref pasta.MenuButtonRef) error {
	if n.kind != "client" || ref.Block != "main" || ref.Button != "request" {
		return nil
	}
	select {
	case n.requestCh <- struct{}{}:
	default:
	}
	return nil
}

func (n *networkNode) ImportPrivateState(private any) error {
	n.mu.Lock()
	n.state = networkStateFromAny(private)
	n.mu.Unlock()
	n.publish()
	return nil
}

func (n *networkNode) ExportPrivateState() (any, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.state, nil
}

func (n *networkNode) startServer() {
	if n.kind != "server" {
		return
	}
	n.mu.Lock()
	if n.serverCan != nil {
		n.serverCan()
	}
	if n.server != nil {
		_ = n.server.Close()
	}
	ctx, cancel := context.WithCancel(n.runCtx)
	n.serverGen++
	generation := n.serverGen
	n.serverCan = cancel
	n.server = nil
	n.wakeReattachLocked()
	n.mu.Unlock()
	go n.serveHTTP(ctx, generation)
}

func (n *networkNode) serveHTTP(ctx context.Context, generation int64) {
	for {
		netw, addr, wake := n.currentNetwork()
		if netw == nil {
			if !waitForNetwork(ctx, wake, time.Second) {
				return
			}
			continue
		}
		if addr == "" {
			addr = "127.0.0.1:8080"
		}
		listenCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		listener, err := netw.Listen(listenCtx, "tcp", addr)
		cancel()
		if err != nil {
			n.setServerError(generation, err)
			if !waitForNetwork(ctx, wake, 250*time.Millisecond) {
				return
			}
			continue
		}
		server := &http.Server{Handler: http.HandlerFunc(n.handleHTTP)}
		n.mu.Lock()
		if n.serverGen != generation {
			n.mu.Unlock()
			_ = listener.Close()
			return
		}
		n.server = server
		n.state.Status = "listening " + addr
		n.state.Error = ""
		n.mu.Unlock()
		n.publish()
		err = server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) && ctx.Err() == nil && n.isServerGeneration(generation) {
			n.setServerError(generation, err)
		}
		if ctx.Err() != nil {
			return
		}
		if !waitForNetwork(ctx, wake, 250*time.Millisecond) {
			return
		}
	}
}

func (n *networkNode) handleHTTP(w http.ResponseWriter, r *http.Request) {
	n.mu.Lock()
	body := n.state.Response
	if body == "" {
		body = fmt.Sprintf("pasta demo response for %s %s", r.Method, r.URL.Path)
		n.state.Response = body
	}
	n.state.Requests++
	n.state.Status = fmt.Sprintf("%s %s", r.Method, r.URL.Path)
	n.state.Error = ""
	n.mu.Unlock()
	n.publish()
	_, _ = io.WriteString(w, body+"\n")
}

func (n *networkNode) clientLoop() {
	for {
		select {
		case <-n.runCtx.Done():
			return
		case <-n.requestCh:
			n.runClientRequest()
		}
	}
}

func (n *networkNode) runClientRequest() {
	netw, addr, _ := n.currentNetwork()
	if netw == nil {
		n.setError(fmt.Errorf("network is not linked"))
		return
	}
	if addr == "" {
		addr = "http://127.0.0.1:8080/"
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return netw.Dial(ctx, network, address)
		},
	}
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	resp, err := client.Get(addr)
	if err != nil {
		transport.CloseIdleConnections()
		n.setError(err)
		return
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	transport.CloseIdleConnections()
	if err != nil {
		n.setError(err)
		return
	}
	n.mu.Lock()
	n.state.Requests++
	n.state.Status = resp.Status
	n.state.Response = string(body)
	n.state.Error = ""
	n.mu.Unlock()
	n.publish()
}

func (n *networkNode) currentNetwork() (trackedNetwork, string, <-chan struct{}) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.network, n.state.Address, n.reattach
}

func (n *networkNode) detach(link pasta.LinkID) {
	n.mu.Lock()
	if n.link != link {
		n.mu.Unlock()
		return
	}
	if n.serverCan != nil {
		n.serverCan()
	}
	if n.server != nil {
		_ = n.server.Close()
	}
	n.serverCan = nil
	n.server = nil
	n.serverGen++
	n.link = 0
	n.network = nil
	n.state.Status = "waiting"
	n.wakeReattachLocked()
	n.mu.Unlock()
	n.publish()
}

func (n *networkNode) wakeReattachLocked() {
	close(n.reattach)
	n.reattach = make(chan struct{})
}

func (n *networkNode) setStatus(status string) {
	n.mu.Lock()
	n.state.Status = status
	n.state.Error = ""
	n.mu.Unlock()
	n.publish()
}

func (n *networkNode) setError(err error) {
	n.mu.Lock()
	n.state.Status = "error"
	n.state.Error = err.Error()
	n.mu.Unlock()
	n.publish()
}

func (n *networkNode) setServerError(generation int64, err error) {
	n.mu.Lock()
	if n.serverGen != generation {
		n.mu.Unlock()
		return
	}
	n.state.Status = "error"
	n.state.Error = err.Error()
	n.mu.Unlock()
	n.publish()
}

func (n *networkNode) isServerGeneration(generation int64) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.serverGen == generation
}

func (n *networkNode) publish() {
	n.mu.Lock()
	state := n.state
	n.mu.Unlock()
	_ = n.ctx.Node.SetPrivate(state)
	_ = n.ctx.Node.SetMenu(n.menu())
}

func (n *networkNode) menu() pasta.NodeMenu {
	n.mu.Lock()
	kind := n.kind
	state := n.state
	n.mu.Unlock()
	fields := []pasta.MenuField{
		{ID: "status", Label: "Status", Kind: pasta.MenuFieldReadOnly, ReadOnly: true, Value: state.Status},
		{ID: "error", Label: "Error", Kind: pasta.MenuFieldReadOnly, ReadOnly: true, Value: state.Error},
		{ID: "logs", Label: "Logs", Kind: pasta.MenuFieldReadOnly, ReadOnly: true, Value: state.Logs},
		{ID: "requests", Label: "Requests", Kind: pasta.MenuFieldReadOnly, ReadOnly: true, Value: state.Requests},
	}
	response := pasta.MenuField{ID: "response", Label: "Response", Kind: pasta.MenuFieldReadOnly, ReadOnly: true, Value: state.Response}
	if kind == "server" {
		response = pasta.MenuField{ID: "response", Label: "Response", Kind: pasta.MenuFieldString, Value: state.Response}
	}
	fields = append(fields[:1], append([]pasta.MenuField{response}, fields[1:]...)...)
	if kind == "server" || kind == "client" {
		fields = append([]pasta.MenuField{{ID: "address", Label: "Address", Kind: pasta.MenuFieldString, Value: state.Address}}, fields...)
	}
	block := pasta.MenuBlock{ID: "main", Title: "Network", Fields: fields}
	if kind == "client" {
		block.Buttons = []pasta.MenuButton{{ID: "request", Label: "Request"}}
	}
	return pasta.NodeMenu{Blocks: []pasta.MenuBlock{block}}
}

func (n *networkNode) logNetwork(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	n.mu.Lock()
	if n.state.Logs == "" {
		n.state.Logs = line
	} else {
		lines := strings.Split(n.state.Logs, "\n")
		lines = append(lines, line)
		if len(lines) > 8 {
			lines = lines[len(lines)-8:]
		}
		n.state.Logs = strings.Join(lines, "\n")
	}
	n.state.Status = line
	n.mu.Unlock()
	n.publish()
}

type loggingNetwork struct {
	node *networkNode
	log  func(string, ...any)
}

var _ trackedNetwork = (*loggingNetwork)(nil)

func (n *loggingNetwork) Close() error { return nil }

func (n *loggingNetwork) current() (trackedNetwork, error) {
	if n == nil || n.node == nil {
		return nil, fmt.Errorf("logger has no upstream network")
	}
	netw, _, _ := n.node.currentNetwork()
	if netw == nil {
		return nil, fmt.Errorf("logger has no upstream network")
	}
	return netw, nil
}

func (n *loggingNetwork) IsNative() bool {
	netw, err := n.current()
	return err == nil && netw.IsNative()
}

func (n *loggingNetwork) Interfaces() ([]gonnect.NetworkInterface, error) {
	netw, err := n.current()
	if err != nil {
		return nil, err
	}
	return netw.Interfaces()
}

func (n *loggingNetwork) InterfaceAddrs() ([]net.Addr, error) {
	netw, err := n.current()
	if err != nil {
		return nil, err
	}
	return netw.InterfaceAddrs()
}

func (n *loggingNetwork) InterfaceMulticastAddrs() ([]net.Addr, error) {
	netw, err := n.current()
	if err != nil {
		return nil, err
	}
	return netw.InterfaceMulticastAddrs()
}

func (n *loggingNetwork) InterfacesByIndex(index int) ([]gonnect.NetworkInterface, error) {
	netw, err := n.current()
	if err != nil {
		return nil, err
	}
	return netw.InterfacesByIndex(index)
}

func (n *loggingNetwork) InterfacesByName(name string) ([]gonnect.NetworkInterface, error) {
	netw, err := n.current()
	if err != nil {
		return nil, err
	}
	return netw.InterfacesByName(name)
}

func (n *loggingNetwork) LookupIP(ctx context.Context, network, address string) ([]net.IP, error) {
	netw, err := n.current()
	if err != nil {
		return nil, err
	}
	return netw.LookupIP(ctx, network, address)
}

func (n *loggingNetwork) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	netw, err := n.current()
	if err != nil {
		return nil, err
	}
	return netw.LookupIPAddr(ctx, host)
}

func (n *loggingNetwork) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	netw, err := n.current()
	if err != nil {
		return nil, err
	}
	return netw.LookupNetIP(ctx, network, host)
}

func (n *loggingNetwork) LookupHost(ctx context.Context, host string) ([]string, error) {
	netw, err := n.current()
	if err != nil {
		return nil, err
	}
	return netw.LookupHost(ctx, host)
}

func (n *loggingNetwork) LookupAddr(ctx context.Context, addr string) ([]string, error) {
	netw, err := n.current()
	if err != nil {
		return nil, err
	}
	return netw.LookupAddr(ctx, addr)
}

func (n *loggingNetwork) LookupCNAME(ctx context.Context, host string) (string, error) {
	netw, err := n.current()
	if err != nil {
		return "", err
	}
	return netw.LookupCNAME(ctx, host)
}

func (n *loggingNetwork) LookupPort(ctx context.Context, network, service string) (int, error) {
	netw, err := n.current()
	if err != nil {
		return 0, err
	}
	return netw.LookupPort(ctx, network, service)
}

func (n *loggingNetwork) LookupNS(ctx context.Context, name string) ([]*net.NS, error) {
	netw, err := n.current()
	if err != nil {
		return nil, err
	}
	return netw.LookupNS(ctx, name)
}

func (n *loggingNetwork) LookupMX(ctx context.Context, name string) ([]*net.MX, error) {
	netw, err := n.current()
	if err != nil {
		return nil, err
	}
	return netw.LookupMX(ctx, name)
}

func (n *loggingNetwork) LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error) {
	netw, err := n.current()
	if err != nil {
		return "", nil, err
	}
	return netw.LookupSRV(ctx, service, proto, name)
}

func (n *loggingNetwork) LookupTXT(ctx context.Context, name string) ([]string, error) {
	netw, err := n.current()
	if err != nil {
		return nil, err
	}
	return netw.LookupTXT(ctx, name)
}

func (n *loggingNetwork) Dial(ctx context.Context, network, address string) (net.Conn, error) {
	n.logf("dial %s %s", network, address)
	netw, err := n.current()
	if err == nil {
		conn, dialErr := netw.Dial(ctx, network, address)
		n.logResult("dial", dialErr)
		return conn, dialErr
	}
	n.logResult("dial", err)
	return nil, err
}

func (n *loggingNetwork) Listen(ctx context.Context, network, address string) (net.Listener, error) {
	n.logf("listen %s %s", network, address)
	netw, err := n.current()
	if err == nil {
		listener, listenErr := netw.Listen(ctx, network, address)
		n.logResult("listen", listenErr)
		return listener, listenErr
	}
	n.logResult("listen", err)
	return nil, err
}

func (n *loggingNetwork) PacketDial(ctx context.Context, network, address string) (gonnect.PacketConn, error) {
	n.logf("packet dial %s %s", network, address)
	netw, err := n.current()
	if err == nil {
		conn, dialErr := netw.PacketDial(ctx, network, address)
		n.logResult("packet dial", dialErr)
		return conn, dialErr
	}
	n.logResult("packet dial", err)
	return nil, err
}

func (n *loggingNetwork) ListenPacket(ctx context.Context, network, address string) (gonnect.PacketConn, error) {
	n.logf("listen packet %s %s", network, address)
	netw, err := n.current()
	if err == nil {
		conn, listenErr := netw.ListenPacket(ctx, network, address)
		n.logResult("listen packet", listenErr)
		return conn, listenErr
	}
	n.logResult("listen packet", err)
	return nil, err
}

func (n *loggingNetwork) DialTCP(ctx context.Context, network, laddr, raddr string) (gonnect.TCPConn, error) {
	n.logf("dial tcp %s %s -> %s", network, laddr, raddr)
	netw, err := n.current()
	if err == nil {
		conn, dialErr := netw.DialTCP(ctx, network, laddr, raddr)
		n.logResult("dial tcp", dialErr)
		return conn, dialErr
	}
	n.logResult("dial tcp", err)
	return nil, err
}

func (n *loggingNetwork) ListenTCP(ctx context.Context, network, laddr string) (gonnect.TCPListener, error) {
	n.logf("listen tcp %s %s", network, laddr)
	netw, err := n.current()
	if err == nil {
		listener, listenErr := netw.ListenTCP(ctx, network, laddr)
		n.logResult("listen tcp", listenErr)
		return listener, listenErr
	}
	n.logResult("listen tcp", err)
	return nil, err
}

func (n *loggingNetwork) DialUDP(ctx context.Context, network, laddr, raddr string) (gonnect.UDPConn, error) {
	n.logf("dial udp %s %s -> %s", network, laddr, raddr)
	netw, err := n.current()
	if err == nil {
		conn, dialErr := netw.DialUDP(ctx, network, laddr, raddr)
		n.logResult("dial udp", dialErr)
		return conn, dialErr
	}
	n.logResult("dial udp", err)
	return nil, err
}

func (n *loggingNetwork) ListenUDP(ctx context.Context, network, laddr string) (gonnect.UDPConn, error) {
	n.logf("listen udp %s %s", network, laddr)
	netw, err := n.current()
	if err == nil {
		conn, listenErr := netw.ListenUDP(ctx, network, laddr)
		n.logResult("listen udp", listenErr)
		return conn, listenErr
	}
	n.logResult("listen udp", err)
	return nil, err
}

func (n *loggingNetwork) ListenPacketConfig(ctx context.Context, lc *gonnect.ListenConfig, network, address string) (gonnect.PacketConn, error) {
	n.logf("listen packet config %s %s", network, address)
	netw, err := n.current()
	if err == nil {
		conn, listenErr := netw.ListenPacketConfig(ctx, lc, network, address)
		n.logResult("listen packet config", listenErr)
		return conn, listenErr
	}
	n.logResult("listen packet config", err)
	return nil, err
}

func (n *loggingNetwork) ListenUDPConfig(ctx context.Context, lc *gonnect.ListenConfig, network, laddr string) (gonnect.UDPConn, error) {
	n.logf("listen udp config %s %s", network, laddr)
	netw, err := n.current()
	if err == nil {
		conn, listenErr := netw.ListenUDPConfig(ctx, lc, network, laddr)
		n.logResult("listen udp config", listenErr)
		return conn, listenErr
	}
	n.logResult("listen udp config", err)
	return nil, err
}

func (n *loggingNetwork) ListenMulticastUDP(ctx context.Context, network, address string, opts gonnect.MulticastOptions) (gonnect.MulticastPacketConn, error) {
	n.logf("listen multicast udp %s %s", network, address)
	netw, err := n.current()
	if err == nil {
		conn, listenErr := netw.ListenMulticastUDP(ctx, network, address, opts)
		n.logResult("listen multicast udp", listenErr)
		return conn, listenErr
	}
	n.logResult("listen multicast udp", err)
	return nil, err
}

func (n *loggingNetwork) logf(format string, args ...any) {
	if n != nil && n.log != nil {
		n.log(format, args...)
	}
}

func (n *loggingNetwork) logResult(op string, err error) {
	if err != nil {
		n.logf("%s error: %v", op, err)
		return
	}
	n.logf("%s ok", op)
}

func closeTrackedNetwork(resource any) error {
	closer, ok := resource.(io.Closer)
	if !ok {
		return nil
	}
	return closer.Close()
}

func waitForNetwork(ctx context.Context, wake <-chan struct{}, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-wake:
		return true
	case <-timer.C:
		return true
	}
}

func networkStateFromAny(v any) networkState {
	switch x := v.(type) {
	case networkState:
		return x
	case map[string]any:
		return networkState{
			Address:  stringFromAny(x["address"]),
			Status:   stringFromAny(x["status"]),
			Response: stringFromAny(x["response"]),
			Error:    stringFromAny(x["error"]),
			Logs:     stringFromAny(x["logs"]),
			Requests: int64(positiveFloatFromAny(x["requests"], 0)),
		}
	default:
		return networkState{}
	}
}
