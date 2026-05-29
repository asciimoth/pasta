package std

import (
	"slices"
	"strings"

	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

type stringNode struct {
	pasta.BasicNode

	op       string
	variadic bool
	inputs   map[uint64]string
	value    any

	w     *pasta.Workspace
	id    uint64
	out   uint64
	lefts []uint64
}

func newStringNode(op string, variadic bool) *stringNode {
	n := &stringNode{op: op, variadic: variadic, inputs: map[uint64]string{}}
	n.recalculate(false)
	return n
}

func (n *stringNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if restored != nil {
		if len(restored.RightPorts) > 0 {
			n.out = restored.RightPorts[0]
		}
		n.lefts = append([]uint64{}, restored.LeftPorts...)
	}
	if err := n.w.SetNodePrimary(n.id, n.outputType()); err != nil {
		return err
	}
	n.recalculate(false)
	if err := n.updateLabel(); err != nil {
		return err
	}
	n.sendMenuSnapshot()
	if n.variadic {
		n.scheduleBalanceInputs()
	}
	return nil
}

func (n *stringNode) OnReady() error {
	n.requestAll()
	n.sendAll()
	return nil
}

func (n *stringNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	switch portDirection {
	case "left":
		if linkType != TypeString {
			return pasta.LinkTypeErr(linkType)
		}
		snapshot, ok := n.w.PortSnapshot(port)
		if ok && len(snapshot.Links) > 0 {
			return pasta.ErrLinkDup
		}
		return nil
	case "right":
		if linkType != n.outputType() {
			return pasta.LinkTypeErr(linkType)
		}
		return nil
	default:
		return pasta.LinkTypeErr(linkType)
	}
}

func (n *stringNode) OnLinkAdd(link, port uint64, _, portDirection string) error {
	if portDirection == "left" {
		n.requestLink(link, port)
		if n.variadic {
			n.scheduleBalanceInputs()
		}
		return nil
	}
	if port == n.out {
		n.sendToLink(link)
	}
	return nil
}

func (n *stringNode) OnLinkRemoved(_ uint64, port uint64, _ string, portDirection string) error {
	if portDirection == "left" {
		delete(n.inputs, port)
		n.recalculate(true)
		if n.variadic {
			n.scheduleBalanceInputs()
		}
	}
	return nil
}

func (n *stringNode) OnPortAdd(port uint64, direction string, _ []string) error {
	if direction == "left" {
		n.lefts = append(n.lefts, port)
		n.sortInputs()
	}
	return nil
}

func (n *stringNode) OnPortRemoved(port uint64, direction string) error {
	if direction == "left" {
		delete(n.inputs, port)
		n.refreshLefts()
		n.recalculate(true)
	}
	return nil
}

func (n *stringNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if receiverPortDirection == "right" {
		if linkType == n.outputType() && isValueRequest(event.Payload) {
			n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: event.SenderNode, ReceiverPort: event.SenderPort, Payload: n.payload()})
		}
		return nil
	}
	if linkType != TypeString {
		return nil
	}
	value, ok := parseStringAny(event.Payload)
	if !ok {
		return nil
	}
	n.inputs[event.ReceiverPort] = value
	n.recalculate(true)
	return nil
}

func (n *stringNode) outputType() string {
	switch n.op {
	case "length":
		return TypeInt
	case "contains":
		return TypeBool
	default:
		return TypeString
	}
}

func (n *stringNode) recalculate(broadcast bool) {
	old := n.value
	switch n.op {
	case "concat":
		var b strings.Builder
		for i := range n.lefts {
			b.WriteString(n.input(i))
		}
		n.value = b.String()
	case "length":
		n.value = len(n.input(0))
	case "contains":
		n.value = strings.Contains(n.input(0), n.input(1))
	case "upper":
		n.value = strings.ToUpper(n.input(0))
	case "lower":
		n.value = strings.ToLower(n.input(0))
	case "trimSpace":
		n.value = strings.TrimSpace(n.input(0))
	default:
		n.value = ""
	}
	_ = n.updateLabel()
	n.sendMenuBlock()
	if broadcast && old != n.value {
		n.sendAll()
	}
}

func (n *stringNode) input(index int) string {
	if index >= len(n.lefts) {
		return ""
	}
	return n.inputs[n.lefts[index]]
}

func (n *stringNode) payload() any {
	switch v := n.value.(type) {
	case int:
		return Int(v)
	case bool:
		return v
	case string:
		return String(v)
	default:
		return String("")
	}
}

func (n *stringNode) label() string {
	switch v := n.value.(type) {
	case int:
		return intValue(v).label()
	case bool:
		return boolLabel(v)
	case string:
		return v
	default:
		return ""
	}
}

func (n *stringNode) menuValue() any {
	switch v := n.value.(type) {
	case int:
		return v
	case bool:
		return v
	case string:
		return v
	default:
		return ""
	}
}

func (n *stringNode) requestAll() {
	for _, port := range n.lefts {
		snapshot, ok := n.w.PortSnapshot(port)
		if !ok {
			continue
		}
		for _, link := range snapshot.Links {
			n.requestLink(link, port)
		}
	}
}

func (n *stringNode) requestLink(link, port uint64) {
	snapshot, ok := n.w.LinkSnapshot(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, port)
	n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: port, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: RequestValue{}})
}

func (n *stringNode) sendAll() {
	port, ok := n.w.PortSnapshot(n.out)
	if !ok {
		return
	}
	for _, link := range port.Links {
		n.sendToLink(link)
	}
}

func (n *stringNode) sendToLink(link uint64) {
	snapshot, ok := n.w.LinkSnapshot(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, n.out)
	n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: n.payload()})
}

func (n *stringNode) scheduleBalanceInputs() {
	n.w.SendInbox(pasta.InboxMessage{ReceiverNode: n.id, Payload: "balanceInputs"})
}

func (n *stringNode) OnInbox(message pasta.InboxMessage) error {
	if message.Payload == "balanceInputs" && n.variadic {
		n.balanceInputs()
	}
	return nil
}

func (n *stringNode) balanceInputs() {
	for len(n.freeInputPorts()) == 0 {
		_, _ = n.w.AddPort(stringInputPortForNode(n.id, len(n.lefts)+1))
		n.refreshLefts()
	}
	for {
		free := n.freeInputPorts()
		if len(free) <= 1 {
			break
		}
		trailing := n.trailingFreePort()
		if trailing == 0 {
			break
		}
		n.w.RemovePort(trailing)
		n.refreshLefts()
	}
	n.sortInputs()
}

func (n *stringNode) refreshLefts() {
	snapshot, ok := n.w.NodeSnapshot(n.id)
	if !ok {
		n.lefts = nil
		return
	}
	n.lefts = append([]uint64{}, snapshot.LeftPorts...)
	n.sortInputs()
}

func (n *stringNode) freeInputPorts() []uint64 {
	free := []uint64{}
	for _, port := range n.lefts {
		snapshot, ok := n.w.PortSnapshot(port)
		if ok && len(snapshot.Links) == 0 {
			free = append(free, port)
		}
	}
	return free
}

func (n *stringNode) trailingFreePort() uint64 {
	for i := len(n.lefts) - 1; i >= 0; i-- {
		port := n.lefts[i]
		snapshot, ok := n.w.PortSnapshot(port)
		if ok && len(snapshot.Links) == 0 {
			return port
		}
		if ok && len(snapshot.Links) > 0 {
			return 0
		}
	}
	return 0
}

func (n *stringNode) sortInputs() {
	slices.SortFunc(n.lefts, func(a, b uint64) int {
		return inputIndex(n.w, a) - inputIndex(n.w, b)
	})
	_ = n.w.SetNodePortOrder(n.id, "left", n.lefts)
}

func (n *stringNode) updateLabel() error {
	if n.w == nil || n.id == 0 {
		return nil
	}
	return n.w.SetNodeLabel(n.id, n.label())
}

func (n *stringNode) sendMenuSnapshot() {
	n.w.SendNodeMenuMsg(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageMenuSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1},
		Blocks:      []formular.Block{n.menuBlock()},
	})
}

func (n *stringNode) sendMenuBlock() {
	if n.w == nil || n.id == 0 {
		return
	}
	n.w.SendNodeMenuMsg(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageBlockSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1, BlockGeneration: 1},
		Block:       n.menuBlock(),
	})
}

func (n *stringNode) menuBlock() formular.Block {
	return formular.Block{
		ID: "state", Order: 10, Generation: 1,
		Items: []formular.Item{{
			Type:  formular.ItemField,
			ID:    "value",
			Label: "Value",
			Field: &formular.Field{Kind: menuFieldKind(n.outputType()), Value: n.menuValue(), Readonly: true},
		}},
	}
}

func stringInputPortForNode(node uint64, index int) pasta.Port {
	port := stringInputPort(index)
	port.Node = node
	return port
}
