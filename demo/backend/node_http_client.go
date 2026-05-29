package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
	"github.com/asciimoth/pasta/pasta/std"
)

const nodeTypeHTTPClient = "demo.pasta/HttpClient"

type httpClientClass struct{}

func (httpClientClass) ClassName() string        { return nodeTypeHTTPClient }
func (httpClientClass) ShortDescription() string { return "HTTP client" }
func (httpClientClass) LongDescription() string {
	return "Runs HTTP requests over a received demo.pasta/network in a background worker."
}
func (httpClientClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: typeNetwork, InitialPorts: []pasta.Port{
		networkPort("right", "Network"),
	}}
}
func (httpClientClass) NewNode(cfg configer.Config, _ ...*pasta.NodeClassState) (pasta.Node, error) {
	return &httpClientNode{
		url:    readConfigString(cfg, "url", "http://127.0.0.1:8080/"),
		method: readConfigString(cfg, "method", http.MethodGet),
		body:   readConfigString(cfg, "body", ""),
	}, nil
}

type httpClientNode struct {
	pasta.BasicNode

	w    *pasta.Workspace
	id   uint64
	netp uint64

	network networkCloser
	url     string
	method  string
	body    string

	pending bool
	status  int
	resp    string
	errText string

	jobs chan httpClientRequest
	stop chan struct{}
}

type httpClientRequest struct {
	network networkCloser
	url     string
	method  string
	body    string
}

type httpClientResult struct {
	status int
	body   string
	err    string
}

func (n *httpClientNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if n.method == "" {
		n.method = http.MethodGet
	}
	if n.url == "" {
		n.url = "http://127.0.0.1:8080/"
	}
	if restored != nil && len(restored.RightPorts) > 0 {
		n.netp = restored.RightPorts[0]
	}
	if err := n.w.SetNodePrimary(n.id, typeNetwork); err != nil {
		return err
	}
	_ = n.w.SetNodeLabel(n.id, n.method)
	n.jobs = make(chan httpClientRequest, 1)
	n.stop = make(chan struct{})
	go runHTTPClientWorker(n.w, n.id, n.jobs, n.stop)
	n.sendMenuSnapshot()
	return nil
}

func (n *httpClientNode) OnReady() error {
	n.requestNetwork()
	return nil
}

func (n *httpClientNode) OnStop() {
	if n.stop != nil {
		close(n.stop)
		n.stop = nil
	}
}

func (n *httpClientNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	if port != n.netp || portDirection != "right" || linkType != typeNetwork {
		return pasta.LinkTypeErr(linkType)
	}
	snapshot, ok := n.w.PortSnapshot(port)
	if ok && len(snapshot.Links) > 0 {
		return pasta.ErrLinkDup
	}
	return nil
}

func (n *httpClientNode) OnLinkAdd(link, port uint64, _, _ string) error {
	if port == n.netp {
		n.requestLink(link, port)
	}
	return nil
}

func (n *httpClientNode) OnLinkRemoved(_ uint64, port uint64, _, _ string) error {
	if port == n.netp {
		n.network = nil
	}
	return nil
}

func (n *httpClientNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if event.ReceiverPort != n.netp || receiverPortDirection != "right" || linkType != typeNetwork {
		return nil
	}
	payload, ok := event.Payload.(networkPayload)
	if !ok || payload.Network == nil {
		return nil
	}
	link := linkIDForEvent(n.w, event)
	bindNetworkResource(n.w, n.id, link, payload.Network)
	n.network = payload.Network
	return nil
}

func (n *httpClientNode) OnInbox(message pasta.InboxMessage) error {
	result, ok := message.Payload.(httpClientResult)
	if !ok {
		return nil
	}
	n.pending = false
	n.status = result.status
	n.resp = result.body
	n.errText = result.err
	n.sendRequestBlock()
	n.sendResultBlock()
	return nil
}

func (n *httpClientNode) OnFormularMsg(message any) error {
	msg, ok := message.(formular.FormApplyMessage)
	if !ok || msg.MenuID != pasta.NodeMenuID(n.id) || msg.BlockID != "request" || n.pending {
		return nil
	}
	if value, ok := stringFromPayload(msg.Values["url"]); ok && value != "" {
		n.url = value
	}
	if value, ok := stringFromPayload(msg.Values["method"]); ok && value != "" {
		n.method = strings.ToUpper(value)
	}
	if value, ok := stringFromPayload(msg.Values["body"]); ok {
		n.body = value
	}
	n.status = 0
	n.resp = ""
	n.errText = ""
	n.pending = true
	_ = n.w.SetNodeLabel(n.id, n.method)
	n.sendRequestBlock()
	n.sendResultBlock()
	req := httpClientRequest{network: n.network, url: n.url, method: n.method, body: n.body}
	select {
	case n.jobs <- req:
	default:
		n.w.SendInbox(pasta.InboxMessage{ReceiverNode: n.id, Payload: httpClientResult{err: "worker is busy"}})
	}
	return nil
}

func (n *httpClientNode) OnSave(cfg configer.Config) error {
	if err := pasta.DeleteNodeOwnedConfigKeys(cfg); err != nil {
		return err
	}
	if err := cfg.Set(configer.Path{"url"}, n.url); err != nil {
		return err
	}
	if err := cfg.Set(configer.Path{"method"}, n.method); err != nil {
		return err
	}
	return cfg.Set(configer.Path{"body"}, n.body)
}

func runHTTPClientWorker(w *pasta.Workspace, node uint64, jobs <-chan httpClientRequest, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case req := <-jobs:
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				select {
				case <-stop:
					cancel()
				case <-done:
				}
			}()
			result := doHTTPRequest(ctx, req)
			cancel()
			close(done)
			w.SendInbox(pasta.InboxMessage{ReceiverNode: node, Payload: result})
		}
	}
}

func doHTTPRequest(parent context.Context, req httpClientRequest) httpClientResult {
	if req.network == nil {
		return httpClientResult{err: "network is not ready"}
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	body := io.Reader(nil)
	if req.body != "" {
		body = strings.NewReader(req.body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.method, req.url, body)
	if err != nil {
		return httpClientResult{err: err.Error()}
	}
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return req.network.Dial(ctx, network, address)
		},
	}}
	resp, err := client.Do(httpReq)
	if err != nil {
		return httpClientResult{err: err.Error()}
	}
	defer resp.Body.Close() //nolint
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return httpClientResult{status: resp.StatusCode, err: err.Error()}
	}
	return httpClientResult{status: resp.StatusCode, body: string(data)}
}

func (n *httpClientNode) requestNetwork() {
	snapshot, ok := n.w.PortSnapshot(n.netp)
	if !ok {
		return
	}
	for _, link := range snapshot.Links {
		n.requestLink(link, n.netp)
	}
}

func (n *httpClientNode) requestLink(link, port uint64) {
	snapshot, ok := n.w.LinkSnapshot(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, port)
	n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: port, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: std.RequestValue{}})
}

func (n *httpClientNode) sendMenuSnapshot() {
	n.w.SendNodeMenuMsg(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageMenuSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1},
		Blocks:      []formular.Block{n.requestBlock(), n.resultBlock()},
	})
}

func (n *httpClientNode) sendRequestBlock() {
	n.w.SendNodeMenuMsg(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageBlockSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1, BlockGeneration: 1},
		Block:       n.requestBlock(),
	})
}

func (n *httpClientNode) sendResultBlock() {
	n.w.SendNodeMenuMsg(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageBlockSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1, BlockGeneration: 1},
		Block:       n.resultBlock(),
	})
}

func (n *httpClientNode) requestBlock() formular.Block {
	return formular.Block{ID: "request", Order: 10, Generation: 1, Form: true, Inactive: n.pending, Items: []formular.Item{
		{Type: formular.ItemField, ID: "url", Label: "URL", Field: &formular.Field{Kind: formular.FieldText, Value: n.url}},
		{Type: formular.ItemField, ID: "method", Label: "Method", Field: &formular.Field{Kind: formular.FieldText, Value: n.method}},
		{Type: formular.ItemField, ID: "body", Label: "Body", Field: &formular.Field{Kind: formular.FieldText, Value: n.body, Multiline: true}},
	}}
}

func (n *httpClientNode) resultBlock() formular.Block {
	return formular.Block{ID: "result", Order: 20, Generation: 1, Items: []formular.Item{
		{Type: formular.ItemField, ID: "status", Label: "Status", Field: &formular.Field{Kind: formular.FieldInt, Value: n.status, Readonly: true}},
		{Type: formular.ItemField, ID: "error", Label: "Error", Field: &formular.Field{Kind: formular.FieldText, Value: n.errText, Readonly: true}},
		{Type: formular.ItemField, ID: "response", Label: "Response", Field: &formular.Field{Kind: formular.FieldText, Value: n.resp, Readonly: true, Multiline: true}},
	}}
}
