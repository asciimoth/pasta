package main

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/gonnect"
	"github.com/asciimoth/pasta/pasta"
	"github.com/asciimoth/pasta/pasta/std"
	"github.com/asciimoth/socksgo"
)

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

	w    *pasta.Workspace
	id   uint64
	out  uint64
	urlp uint64
}

func (o *outproxyNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	o.w = w
	o.id = id

	if restored != nil {
		if len(restored.LeftPorts) > 0 {
			o.out = restored.LeftPorts[0]
		}
		if len(restored.LeftPorts) > 1 {
			o.urlp = restored.LeftPorts[1]
		}
	}

	if err := o.w.SetNodePrimaryLocked(o.id, typeNetwork); err != nil {
		return err
	}

	_ = o.updateLabel()
	o.sendMenuSnapshot()
	return nil
}

func (o *outproxyNode) OnReady() error {
	o.requestURL()

	if !o.urlLinked() {
		o.sendAll()
	}

	return nil
}

func (o *outproxyNode) OnStop() {
	if o.base != nil {
		pasta.CloseBackground(o.base)
		o.base = nil
	}
}

func (o *outproxyNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	switch port {
	case o.out:
		if portDirection != "left" || linkType != typeNetwork {
			return pasta.LinkTypeErr(linkType)
		}

	case o.urlp:
		if portDirection != "left" || linkType != std.TypeString {
			return pasta.LinkTypeErr(linkType)
		}
		if o.portHasLinks(o.urlp) {
			return pasta.ErrLinkDup
		}

	default:
		return pasta.LinkTypeErr(linkType)
	}

	return nil
}

func (o *outproxyNode) OnLinkAdd(link, port uint64, _, _ string) error {
	switch port {
	case o.out:
		o.sendToLink(link)

	case o.urlp:
		o.sendURLBlock()
		std.RequestLocked(o.w, o.id, link)
	}

	return nil
}

func (o *outproxyNode) OnLinkRemoved(_ uint64, port uint64, _, _ string) error {
	if port == o.urlp {
		o.sendURLBlock()
	}
	return nil
}

func (o *outproxyNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if event.ReceiverPort == o.out && receiverPortDirection == "left" && linkType == typeNetwork && std.IsRequest(event.Payload) {
		o.sendToLink(event.Link)
		return nil
	}

	if event.ReceiverPort == o.urlp && receiverPortDirection == "left" && linkType == std.TypeString {
		if value, ok := std.StringFromPayload(event.Payload); ok {
			o.setURL(value)
		}
		return nil
	}

	return nil
}

func (o *outproxyNode) OnFormularMsg(message any) error {
	msg, ok := message.(formular.FormApplyMessage)
	if !ok || msg.MenuID != pasta.NodeMenuID(o.id) || msg.BlockID != "url" {
		return nil
	}

	if o.urlLinked() {
		o.sendURLBlock()
		return nil
	}

	if value, ok := std.StringFromPayload(msg.Values["url"]); ok {
		o.setURL(value)
		return nil
	}

	o.sendURLBlock()
	return nil
}

func (o *outproxyNode) OnSave(cfg configer.Config) error {
	// if err := pasta.DeleteNodeOwnedConfigKeys(cfg); err != nil {
	// 	return err
	// }
	return cfg.Set(configer.Path{"url"}, o.url)
}

func (o *outproxyNode) setURL(url string) {
	if o.url == url {
		o.sendURLBlock()
		return
	}

	o.url = url
	o.rebuildClient()
	o.sendURLBlock()
}

// Should be called each time url changed.
func (o *outproxyNode) rebuildClient() {
	if o.base != nil {
		pasta.CloseBackground(o.base)
		o.base = nil
	}

	client, err := socksgo.ClientFromURL(o.url)
	if err != nil || client == nil || client.WebSocketURL == "" {
		o.base = gonnect.DetachNetwork(&gonnect.RejectNetwork{}, nil)
		_ = o.updateLabel()
		o.sendAll()
		return
	}

	client.Filter = gonnect.FalseFilter
	o.base = gonnect.DetachNetwork(client, nil)

	_ = o.updateLabel()
	o.sendAll()
}

func (o *outproxyNode) requestURL() {
	o.requestPort(o.urlp)
}

func (o *outproxyNode) requestPort(port uint64) {
	if o.w == nil || o.id == 0 || port == 0 {
		return
	}

	snapshot, ok := o.w.PortSnapshotLocked(port)
	if !ok {
		return
	}

	for _, link := range snapshot.Links {
		std.RequestLocked(o.w, o.id, link)
	}
}

func (o *outproxyNode) updateLabel() error {
	if o.w == nil || o.id == 0 {
		return nil
	}
	return o.w.SetNodeLabelLocked(o.id, o.url)
}

func (o *outproxyNode) sendAll() {
	if o.w == nil || o.out == 0 || o.id == 0 {
		return
	}

	port, ok := o.w.PortSnapshotLocked(o.out)
	if !ok {
		return
	}

	for _, link := range port.Links {
		o.sendToLink(link)
	}
}

func (o *outproxyNode) sendToLink(link uint64) {
	if o.w == nil || o.out == 0 || o.id == 0 || o.base == nil {
		return
	}

	wrapper := gonnect.DetachNetwork(o.base, nil)
	bindNetworkResource(o.w, o.id, link, wrapper)
	o.w.EmitEventLocked(o.id, link, networkPayload{Network: wrapper})
}

func (o *outproxyNode) sendMenuSnapshot() {
	if o.w == nil || o.id == 0 {
		return
	}

	o.w.SendNodeMenuMsgLocked(o.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{
			Type:           formular.MessageMenuSnapshot,
			MenuID:         pasta.NodeMenuID(o.id),
			MenuGeneration: 1,
		},
		Blocks: []formular.Block{o.urlBlock()},
	})
}

func (o *outproxyNode) sendURLBlock() {
	if o.w == nil || o.id == 0 {
		return
	}

	o.w.SendNodeMenuMsgLocked(o.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{
			Type:            formular.MessageBlockSnapshot,
			MenuID:          pasta.NodeMenuID(o.id),
			MenuGeneration:  1,
			BlockGeneration: 1,
		},
		Block: o.urlBlock(),
	})
}

func (o *outproxyNode) urlBlock() formular.Block {
	readonly := o.urlLinked()

	help := "Editable while no string URL link is attached."
	if readonly {
		help = "Controlled by the attached string URL link."
	}

	return formular.Block{
		ID:         "url",
		Order:      10,
		Generation: 1,
		Form:       true,
		Items: []formular.Item{
			{
				Type:  formular.ItemField,
				ID:    "url",
				Label: "URL",
				Help:  help,
				Field: &formular.Field{
					Kind:     formular.FieldText,
					Value:    o.url,
					Readonly: readonly,
				},
			},
		},
	}
}

func (o *outproxyNode) urlLinked() bool {
	return o.portHasLinks(o.urlp)
}

func (o *outproxyNode) portHasLinks(port uint64) bool {
	if o.w == nil || port == 0 {
		return false
	}

	snapshot, ok := o.w.PortSnapshotLocked(port)
	return ok && len(snapshot.Links) > 0
}
