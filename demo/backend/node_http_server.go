package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
	"github.com/asciimoth/pasta/pasta/std"
)

const nodeTypeHTTPServer = "demo.pasta/HttpServer"

type httpServerClass struct{}

func (httpServerClass) ClassName() string        { return nodeTypeHTTPServer }
func (httpServerClass) ShortDescription() string { return "HTTP server" }
func (httpServerClass) LongDescription() string {
	return "Runs a Go HTTP server over a received demo.pasta/network and responds to every request with the configured string."
}
func (httpServerClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: typeNetwork, InitialPorts: []pasta.Port{
		networkPort("right", "Network"),
		{Direction: "left", Name: "Response", Types: []string{std.TypeString}},
	}}
}
func (httpServerClass) NewNode(cfg configer.Config, _ ...*pasta.NodeClassState) (pasta.Node, error) {
	return &httpServerNode{
		host:     readConfigString(cfg, "host", "127.0.0.1"),
		port:     readConfigInt(cfg, "port", 8080),
		response: readConfigString(cfg, "response", ""),
	}, nil
}

type httpServerNode struct {
	pasta.BasicNode

	w    *pasta.Workspace
	id   uint64
	netp uint64
	in   uint64

	host     string
	port     int
	response string
	network  networkCloser

	worker    *httpServerWorker
	restartID uint64
	logs      []formular.LogLine
}

type httpServerLog struct {
	level string
	text  string
}

type httpServerStart struct {
	id  uint64
	cfg httpServerConfig
}

type httpServerWorker struct {
	stop chan struct{}
	done chan struct{}
}

func (n *httpServerNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if n.host == "" {
		n.host = "127.0.0.1"
	}
	if n.port == 0 {
		n.port = 8080
	}
	if restored != nil {
		if len(restored.RightPorts) > 0 {
			n.netp = restored.RightPorts[0]
		}
		if len(restored.LeftPorts) > 0 {
			n.in = restored.LeftPorts[0]
		}
	}
	if err := n.w.SetNodePrimary(n.id, typeNetwork); err != nil {
		return err
	}
	_ = n.updateLabel()
	n.sendMenuSnapshot()
	return nil
}

func (n *httpServerNode) OnReady() error {
	n.requestNetwork()
	n.requestResponse()
	n.restartWorker()
	return nil
}

func (n *httpServerNode) OnStop() {
	n.stopWorker()
}

func (n *httpServerNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	switch port {
	case n.netp:
		if portDirection != "right" || linkType != typeNetwork {
			return pasta.LinkTypeErr(linkType)
		}
		snapshot, ok := n.w.PortSnapshot(port)
		if ok && len(snapshot.Links) > 0 {
			return pasta.ErrLinkDup
		}
	case n.in:
		if portDirection != "left" || linkType != std.TypeString {
			return pasta.LinkTypeErr(linkType)
		}
		snapshot, ok := n.w.PortSnapshot(port)
		if ok && len(snapshot.Links) > 0 {
			return pasta.ErrLinkDup
		}
	default:
		return pasta.LinkTypeErr(linkType)
	}
	return nil
}

func (n *httpServerNode) OnLinkAdd(link, port uint64, _, _ string) error {
	switch port {
	case n.netp:
		n.requestLink(link, port)
	case n.in:
		n.requestLink(link, port)
	}
	return nil
}

func (n *httpServerNode) OnLinkRemoved(_ uint64, port uint64, _, _ string) error {
	if port == n.netp {
		n.network = nil
		n.restartWorker()
	}
	return nil
}

func (n *httpServerNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if event.ReceiverPort == n.netp && receiverPortDirection == "right" && linkType == typeNetwork {
		payload, ok := event.Payload.(networkPayload)
		if !ok || payload.Network == nil {
			return nil
		}
		link := linkIDForEvent(n.w, event)
		bindNetworkResource(n.w, n.id, link, payload.Network)
		n.network = payload.Network
		n.restartWorker()
		return nil
	}
	if event.ReceiverPort == n.in && receiverPortDirection == "left" && linkType == std.TypeString {
		if value, ok := stringFromPayload(event.Payload); ok {
			n.response = value
			n.restartWorker()
		}
	}
	return nil
}

func (n *httpServerNode) OnInbox(message pasta.InboxMessage) error {
	switch payload := message.Payload.(type) {
	case httpServerLog:
		n.appendLog(payload.level, payload.text)
		n.sendLogsBlock()
	case httpServerStart:
		if payload.id == n.restartID && n.worker == nil && n.network != nil {
			n.startWorker(payload.cfg)
		}
	}
	return nil
}

func (n *httpServerNode) OnFormularMsg(message any) error {
	msg, ok := message.(formular.FormApplyMessage)
	if !ok || msg.MenuID != pasta.NodeMenuID(n.id) || msg.BlockID != "listen" {
		return nil
	}
	if value, ok := stringFromPayload(msg.Values["host"]); ok && value != "" {
		n.host = value
	}
	if value, ok := intFromPayload(msg.Values["port"]); ok && value > 0 {
		n.port = value
	}
	_ = n.updateLabel()
	n.sendListenBlock()
	n.restartWorker()
	return nil
}

func (n *httpServerNode) OnSave(cfg configer.Config) error {
	if err := pasta.DeleteNodeOwnedConfigKeys(cfg); err != nil {
		return err
	}
	if err := cfg.Set(configer.Path{"host"}, n.host); err != nil {
		return err
	}
	if err := cfg.Set(configer.Path{"port"}, n.port); err != nil {
		return err
	}
	return cfg.Set(configer.Path{"response"}, n.response)
}

func (n *httpServerNode) restartWorker() {
	n.restartID++
	id := n.restartID
	n.appendLog(formular.LogInfo, "worker restarting")
	n.sendLogsBlock()
	old := n.worker
	if old != nil {
		n.worker = nil
		close(old.stop)
	}
	if n.network == nil {
		n.appendLog(formular.LogWarn, "waiting for network")
		n.sendLogsBlock()
		return
	}
	cfg := httpServerConfig{host: n.host, port: n.port, response: n.response, network: n.network}
	if old != nil {
		go func() {
			<-old.done
			n.w.SendInbox(pasta.InboxMessage{ReceiverNode: n.id, Payload: httpServerStart{id: id, cfg: cfg}})
		}()
		return
	}
	n.startWorker(cfg)
}

func (n *httpServerNode) startWorker(cfg httpServerConfig) {
	stop := make(chan struct{})
	worker := &httpServerWorker{stop: stop, done: make(chan struct{})}
	n.worker = worker
	go func() {
		defer close(worker.done)
		runHTTPServerWorker(n.w, n.id, stop, cfg)
	}()
}

func (n *httpServerNode) stopWorker() {
	if n.worker != nil {
		close(n.worker.stop)
		n.worker = nil
	}
}

type httpServerConfig struct {
	host     string
	port     int
	response string
	network  networkCloser
}

func runHTTPServerWorker(w *pasta.Workspace, node uint64, stop <-chan struct{}, cfg httpServerConfig) {
	addr := net.JoinHostPort(cfg.host, strconv.Itoa(cfg.port))
	for {
		select {
		case <-stop:
			return
		default:
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			select {
			case <-stop:
				cancel()
			case <-done:
			}
		}()
		ln, err := cfg.network.Listen(ctx, "tcp", addr)
		cancel()
		close(done)
		if err != nil {
			sendHTTPServerLog(w, node, formular.LogWarn, "listen failed: "+err.Error())
			if !sleepOrStopped(stop, time.Second) {
				return
			}
			continue
		}
		body := cfg.response
		if body == "" {
			body = "Debug http server response"
		}
		handler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			sendHTTPServerLog(w, node, formular.LogInfo, fmt.Sprintf("%s %s", r.Method, r.URL.String()))
			_, _ = rw.Write([]byte(body))
		})
		server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
		serveDone := make(chan error, 1)
		go func() { serveDone <- server.Serve(ln) }()
		sendHTTPServerLog(w, node, formular.LogInfo, "listening on "+addr)
		select {
		case <-stop:
			_ = ln.Close()
			_ = server.Shutdown(context.Background())
			<-serveDone
			return
		case err := <-serveDone:
			_ = ln.Close()
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				sendHTTPServerLog(w, node, formular.LogWarn, "server stopped: "+err.Error())
			}
			if !sleepOrStopped(stop, time.Second) {
				return
			}
		}
	}
}

func sendHTTPServerLog(w *pasta.Workspace, node uint64, level, text string) {
	w.SendInbox(pasta.InboxMessage{ReceiverNode: node, Payload: httpServerLog{level: level, text: text}})
}

func (n *httpServerNode) requestNetwork() {
	n.requestPort(n.netp)
}

func (n *httpServerNode) requestResponse() {
	n.requestPort(n.in)
}

func (n *httpServerNode) requestPort(port uint64) {
	snapshot, ok := n.w.PortSnapshot(port)
	if !ok {
		return
	}
	for _, link := range snapshot.Links {
		n.requestLink(link, port)
	}
}

func (n *httpServerNode) requestLink(link, port uint64) {
	snapshot, ok := n.w.LinkSnapshot(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, port)
	n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: port, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: std.RequestValue{}})
}

func (n *httpServerNode) updateLabel() error {
	return n.w.SetNodeLabel(n.id, net.JoinHostPort(n.host, strconv.Itoa(n.port)))
}

func (n *httpServerNode) sendMenuSnapshot() {
	n.w.SendNodeMenuMsg(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageMenuSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1},
		Blocks:      []formular.Block{n.listenBlock(), n.logsBlock()},
	})
}

func (n *httpServerNode) sendListenBlock() {
	n.w.SendNodeMenuMsg(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageBlockSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1, BlockGeneration: 1},
		Block:       n.listenBlock(),
	})
}

func (n *httpServerNode) sendLogsBlock() {
	if n.w == nil || n.id == 0 {
		return
	}
	n.w.SendNodeMenuMsg(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageBlockSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1, BlockGeneration: 1},
		Block:       n.logsBlock(),
	})
}

func (n *httpServerNode) listenBlock() formular.Block {
	return formular.Block{ID: "listen", Order: 10, Generation: 1, Form: true, Items: []formular.Item{
		{Type: formular.ItemField, ID: "host", Label: "Host", Field: &formular.Field{Kind: formular.FieldText, Value: n.host}},
		{Type: formular.ItemField, ID: "port", Label: "Port", Field: &formular.Field{Kind: formular.FieldInt, Value: n.port}},
	}}
}

func (n *httpServerNode) logsBlock() formular.Block {
	return formular.Block{ID: "logs", Order: 20, Generation: 1, Items: []formular.Item{
		{Type: formular.ItemLogs, ID: "worker", Label: "Worker", Logs: append([]formular.LogLine(nil), n.logs...)},
	}}
}

func (n *httpServerNode) appendLog(level, text string) {
	n.logs = append(n.logs, formular.LogLine{Level: level, Text: text})
	if len(n.logs) > 40 {
		n.logs = append([]formular.LogLine(nil), n.logs[len(n.logs)-40:]...)
	}
}
