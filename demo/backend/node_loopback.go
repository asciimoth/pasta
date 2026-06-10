package main

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/gonnect"
	"github.com/asciimoth/pasta/pasta"
	"github.com/asciimoth/pasta/pasta/std"
)

const nodeTypeLoopback = "demo.pasta/Loopback"

type loopbackClass struct{}

func (loopbackClass) ClassName() string        { return nodeTypeLoopback }
func (loopbackClass) ShortDescription() string { return "Loopback network" }
func (loopbackClass) LongDescription() string {
	return "Provides one detached demo.pasta/network wrapper per connected peer over an in-memory gonnect loopback network."
}
func (loopbackClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: typeNetwork, InitialPorts: []pasta.Port{
		networkPort("left", "Network"),
	}}
}
func (loopbackClass) NewNode(_ configer.Config, _ ...*pasta.NodeClassState) (pasta.Node, error) {
	network := gonnect.NewLoopbackNetwok()
	network.AllowAnyHost = true
	return &loopbackNode{base: network}, nil
}

type loopbackNode struct {
	pasta.BasicNode

	base *gonnect.LoopbackNetwork
	w    *pasta.Workspace
	id   uint64
	out  uint64
}

func (n *loopbackNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if n.base == nil {
		n.base = gonnect.NewLoopbackNetwok()
	}
	if restored != nil && len(restored.LeftPorts) > 0 {
		n.out = restored.LeftPorts[0]
	}
	if err := n.w.SetNodePrimary(n.id, typeNetwork); err != nil {
		return err
	}
	_ = n.w.SetNodeLabel(n.id, "loopback")
	return n.w.AddNodeResource(n.id, n.base)
}

func (n *loopbackNode) OnReady() error {
	n.sendAll()
	return nil
}

func (n *loopbackNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	if port != n.out || portDirection != "left" || linkType != typeNetwork {
		return pasta.LinkTypeErr(linkType)
	}
	return nil
}

func (n *loopbackNode) OnLinkAdd(link, port uint64, _, _ string) error {
	if port == n.out {
		n.sendToLink(link)
	}
	return nil
}

func (n *loopbackNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if event.ReceiverPort != n.out || receiverPortDirection != "left" || linkType != typeNetwork || !std.IsRequest(event.Payload) {
		return nil
	}
	n.sendToLink(event.Link)
	return nil
}

func (n *loopbackNode) sendAll() {
	port, ok := n.w.PortSnapshot(n.out)
	if !ok {
		return
	}
	for _, link := range port.Links {
		n.sendToLink(link)
	}
}

func (n *loopbackNode) sendToLink(link uint64) {
	wrapper := gonnect.DetachNetwork(n.base, nil)
	bindNetworkResource(n.w, n.id, link, wrapper)
	n.w.EmitEvent(n.id, link, networkPayload{Network: wrapper})
}
