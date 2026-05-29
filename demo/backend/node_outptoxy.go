package main

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/gonnect"
	"github.com/asciimoth/pasta/pasta"
	"github.com/asciimoth/pasta/pasta/std"
	"github.com/asciimoth/socksgo"
)

// TODO: Node menu

const nodeTypeOutproxy = "demo.pasta/OutProxy"

type outproxyClass struct{}

func (outproxyClass) ClassName() string { return nodeTypeOutproxy }
func (outproxyClass) ShortDescription() string {
	return "Gateway to real network via socks-over-websocket"
}
func (outproxyClass) LongDescription() string {
	return "Provides access to real internet by using external socks-over-websocket proxy."
}
func (outproxyClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: typeNetwork, InitialPorts: []pasta.Port{
		networkPort("left", "Network"),
	}}
}

func (outproxyClass) NewNode(cfg configer.Config, _ ...*pasta.NodeClassState) (pasta.Node, error) {
	o := &outproxyNode{
		url: readConfigString(cfg, "url", "socks5+ws://localhost:1080"),
	}
	o.rebuildClient()
	return o, nil
}

type outproxyNode struct {
	pasta.BasicNode

	url  string
	base networkCloser

	w   *pasta.Workspace
	id  uint64
	out uint64
}

func (o *outproxyNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	o.w = w
	o.id = id
	if restored != nil && len(restored.LeftPorts) > 0 {
		o.out = restored.LeftPorts[0]
	}
	if err := o.w.SetNodePrimary(o.id, typeNetwork); err != nil {
		return err
	}
	_ = o.w.SetNodeLabel(o.id, o.url)
	return nil
}

func (o *outproxyNode) OnReady() error {
	o.sendAll()
	return nil
}

func (o *outproxyNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	if port != o.out || portDirection != "left" || linkType != typeNetwork {
		return pasta.LinkTypeErr(linkType)
	}
	return nil
}

func (o *outproxyNode) OnLinkAdd(link, port uint64, _, _ string) error {
	if port == o.out {
		o.sendToLink(link)
	}
	return nil
}

func (o *outproxyNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if event.ReceiverPort != o.out || receiverPortDirection != "left" || linkType != typeNetwork || !std.IsRequest(event.Payload) {
		return nil
	}
	o.sendToLink(event.Link)
	return nil
}

func (o *outproxyNode) rebuildClient() {
	if o.w != nil && o.id != 0 {
		_ = o.w.SetNodeLabel(o.id, o.url)
	}
	if o.base != nil {
		_ = o.base.Close()
	}
	client, err := socksgo.ClientFromURL(o.url)
	if err != nil || client.WebSocketURL == "" {
		o.base = gonnect.DetachNetwork(&gonnect.RejectNetwork{})
	}
	client.Filter = gonnect.FalseFilter
	o.base = gonnect.DetachNetwork(client)
	o.sendAll()
}

func (o *outproxyNode) sendAll() {
	if o.w == nil || o.out == 0 || o.id == 0 {
		return
	}
	port, ok := o.w.PortSnapshot(o.out)
	if !ok {
		return
	}
	for _, link := range port.Links {
		o.sendToLink(link)
	}
}

func (o *outproxyNode) sendToLink(link uint64) {
	if o.w == nil || o.out == 0 || o.id == 0 {
		return
	}
	wrapper := gonnect.DetachNetwork(o.base)
	bindNetworkResource(o.w, o.id, link, wrapper)
	o.w.EmitEvent(o.id, link, networkPayload{Network: wrapper})
}
