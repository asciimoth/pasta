package std

import (
	"slices"
	"strconv"
	"strings"

	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

type mathNode struct {
	pasta.BasicNode

	op       string
	variadic bool
	primary  string
	result   numberValue
	inputs   map[uint64]numberValue

	w     *pasta.Workspace
	id    uint64
	out   uint64
	lefts []uint64
}

func newMathNode(op string, variadic bool, previous ...*pasta.NodeClassState) *mathNode {
	n := &mathNode{op: op, variadic: variadic, inputs: map[uint64]numberValue{}, result: intValue(0)}
	if state := firstState(previous); state != nil {
		n.primary = state.PrimaryType
	}
	if n.primary != "" {
		n.result = zeroValue(n.primary)
	}
	return n
}

func (n *mathNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, isRestored bool) error {
	n.w = w
	n.id = id
	if restored != nil {
		if restored.PrimaryType != "" {
			n.primary = restored.PrimaryType
			n.result = zeroValue(n.primary)
		}
		if len(restored.RightPorts) > 0 {
			n.out = restored.RightPorts[0]
		}
		n.lefts = append([]uint64{}, restored.LeftPorts...)
	}
	if n.primary != "" {
		if err := n.w.SetNodePrimary(n.id, n.primary); err != nil {
			return err
		}
		if n.out != 0 {
			if err := n.w.SetPortTypes(n.out, []string{n.primary}); err != nil {
				return err
			}
		}
	}
	n.recalculate(false)
	if err := n.updateLabel(); err != nil {
		return err
	}
	n.sendMenuSnapshot()
	if n.variadic && !isRestored {
		n.scheduleBalanceInputs()
	}
	return nil
}

func (n *mathNode) OnReady() error {
	n.requestAll()
	n.sendAll()
	return nil
}

func (n *mathNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	if linkType != TypeInt && linkType != TypeFloat && linkType != pasta.AnyType {
		return pasta.LinkTypeErr(linkType)
	}
	if portDirection == "left" {
		snapshot, ok := n.w.PortSnapshot(port)
		if ok && len(snapshot.Links) > 0 {
			return pasta.ErrLinkDup
		}
	}
	return nil
}

func (n *mathNode) OnLinkAdd(link, port uint64, linkType, portDirection string) error {
	if portDirection == "left" {
		if n.primary == "" && linkType != pasta.AnyType {
			if err := n.decidePrimary(linkType); err != nil {
				return err
			}
		}
		if linkType != pasta.AnyType {
			n.requestLink(link, port, linkType)
		}
		if n.variadic {
			n.scheduleBalanceInputs()
		}
		return nil
	}
	if port == n.out && n.primary != "" {
		n.sendToLink(link)
	}
	return nil
}

func (n *mathNode) OnLinkRemoved(_ uint64, port uint64, _ string, portDirection string) error {
	if portDirection == "left" {
		delete(n.inputs, port)
		n.recalculate(true)
		if n.variadic {
			n.scheduleBalanceInputs()
		}
	}
	return nil
}

func (n *mathNode) OnPortAdd(port uint64, direction string, _ []string) error {
	if direction == "left" && !slices.Contains(n.lefts, port) {
		n.lefts = append(n.lefts, port)
		n.sortInputs()
	}
	return nil
}

func (n *mathNode) OnPortRemoved(port uint64, direction string) error {
	if direction == "left" {
		n.lefts = slices.DeleteFunc(n.lefts, func(id uint64) bool { return id == port })
		delete(n.inputs, port)
		n.recalculate(true)
	}
	return nil
}

func (n *mathNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if receiverPortDirection == "right" {
		if (linkType == TypeInt || linkType == TypeFloat) && isValueRequest(event.Payload) {
			n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: event.SenderNode, ReceiverPort: event.SenderPort, Payload: n.result.as(linkType).payload()})
		}
		return nil
	}
	value, ok := valueFromPayload(linkType, event.Payload)
	if !ok {
		return nil
	}
	if n.primary == "" {
		if err := n.decidePrimary(linkType); err != nil {
			return err
		}
	}
	n.inputs[event.ReceiverPort] = value
	n.recalculate(true)
	return nil
}

func (n *mathNode) decidePrimary(typ string) error {
	n.primary = typ
	n.result = zeroValue(typ)
	if err := n.w.SetNodePrimary(n.id, typ); err != nil {
		return err
	}
	if n.out != 0 {
		return n.w.SetPortTypes(n.out, []string{typ})
	}
	return nil
}

func (n *mathNode) recalculate(broadcast bool) {
	old := n.result
	typ := n.primary
	if typ == "" {
		typ = TypeInt
	}
	switch n.op {
	case "sub":
		a, b := n.input(0, typ), n.input(1, typ)
		if typ == TypeFloat {
			n.result = floatValue(a.f - b.f)
		} else {
			n.result = intValue(a.i - b.i)
		}
	case "div":
		a, b := n.input(0, typ), n.input(1, typ)
		if typ == TypeFloat {
			if b.f == 0 {
				n.result = floatValue(0)
			} else {
				n.result = floatValue(a.f / b.f)
			}
		} else if b.i == 0 {
			n.result = intValue(0)
		} else {
			n.result = intValue(a.i / b.i)
		}
	case "mul":
		if typ == TypeFloat {
			acc := float64(1)
			for _, port := range n.lefts {
				acc *= n.valueForPort(port, typ).f
			}
			n.result = floatValue(acc)
		} else {
			acc := 1
			for _, port := range n.lefts {
				acc *= n.valueForPort(port, typ).i
			}
			n.result = intValue(acc)
		}
	default:
		if typ == TypeFloat {
			acc := float64(0)
			for _, port := range n.lefts {
				acc += n.valueForPort(port, typ).f
			}
			n.result = floatValue(acc)
		} else {
			acc := 0
			for _, port := range n.lefts {
				acc += n.valueForPort(port, typ).i
			}
			n.result = intValue(acc)
		}
	}
	_ = n.updateLabel()
	n.sendMenuBlock()
	if broadcast && old.payload() != n.result.payload() {
		n.sendAll()
	}
}

func (n *mathNode) input(index int, typ string) numberValue {
	if index >= len(n.lefts) {
		return zeroValue(typ)
	}
	return n.valueForPort(n.lefts[index], typ)
}

func (n *mathNode) valueForPort(port uint64, typ string) numberValue {
	value, ok := n.inputs[port]
	if !ok {
		if n.op == "mul" {
			return oneValue(typ)
		} else {
			return zeroValue(typ)
		}
	}
	return value.as(typ)
}

func (n *mathNode) requestAll() {
	for _, port := range n.lefts {
		snapshot, ok := n.w.PortSnapshot(port)
		if !ok {
			continue
		}
		for _, link := range snapshot.Links {
			n.requestLink(link, port, "")
		}
	}
}

func (n *mathNode) requestLink(link, port uint64, linkType string) {
	snapshot, ok := n.w.LinkSnapshot(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, port)
	n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: port, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: RequestValue{}})
}

func (n *mathNode) sendAll() {
	if n.primary == "" {
		return
	}
	port, ok := n.w.PortSnapshot(n.out)
	if !ok {
		return
	}
	for _, link := range port.Links {
		n.sendToLink(link)
	}
}

func (n *mathNode) sendToLink(link uint64) {
	snapshot, ok := n.w.LinkSnapshot(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, n.out)
	n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: n.result.as(snapshot.Type).payload()})
}

func (n *mathNode) scheduleBalanceInputs() {
	n.w.AddPendingOp(n.balanceInputs)
}

func (n *mathNode) balanceInputs() {
	if !n.variadic {
		return
	}
	n.refreshLefts()
	free := n.freeInputPorts()
	if len(free) == 0 {
		_, _ = n.w.AddPort(inputPortForNode(n.id, len(n.lefts)+1))
		n.refreshLefts()
	}
	for {
		n.refreshLefts()
		free = n.freeInputPorts()
		if len(free) <= 1 {
			break
		}
		trailing := n.trailingFreePort()
		if trailing == 0 {
			break
		}
		n.w.RemovePort(trailing)
	}
	n.sortInputs()
}

func (n *mathNode) refreshLefts() {
	snapshot, ok := n.w.NodeSnapshot(n.id)
	if !ok {
		return
	}
	n.lefts = append([]uint64{}, snapshot.LeftPorts...)
	n.sortInputs()
}

func (n *mathNode) freeInputPorts() []uint64 {
	free := []uint64{}
	for _, port := range n.lefts {
		snapshot, ok := n.w.PortSnapshot(port)
		if ok && len(snapshot.Links) == 0 {
			free = append(free, port)
		}
	}
	return free
}

func (n *mathNode) trailingFreePort() uint64 {
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

func (n *mathNode) sortInputs() {
	slices.SortFunc(n.lefts, func(a, b uint64) int {
		return inputIndex(n.w, a) - inputIndex(n.w, b)
	})
	_ = n.w.SetNodePortOrder(n.id, "left", n.lefts)
}

func (n *mathNode) updateLabel() error {
	return n.w.SetNodeLabel(n.id, n.result.label())
}

func (n *mathNode) sendMenuSnapshot() {
	n.w.SendNodeMenuMsg(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageMenuSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1},
		Blocks:      []formular.Block{n.menuBlock()},
	})
}

func (n *mathNode) sendMenuBlock() {
	if n.w == nil || n.id == 0 {
		return
	}
	n.w.SendNodeMenuMsg(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageBlockSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1, BlockGeneration: 1},
		Block:       n.menuBlock(),
	})
}

func (n *mathNode) menuBlock() formular.Block {
	return formular.Block{
		ID: "state", Order: 10, Generation: 1,
		Items: []formular.Item{{Type: formular.ItemField, ID: "value", Label: "Value", Field: &formular.Field{Kind: menuFieldKind(n.result.typ), Value: n.result.menuValue(), Readonly: true}}},
	}
}

func inputPortForNode(node uint64, index int) pasta.Port {
	port := inputPort(index)
	port.Node = node
	return port
}

func inputIndex(w *pasta.Workspace, port uint64) int {
	snapshot, ok := w.PortSnapshot(port)
	if !ok {
		return 0
	}
	_, raw, ok := strings.Cut(snapshot.Name, "input ")
	if !ok {
		return 0
	}
	index, _ := strconv.Atoi(raw)
	return index
}

func otherEndpoint(link pasta.LinkSnapshot, port uint64) (uint64, uint64) {
	if link.LeftPort == port {
		return link.RightPortNode, link.RightPort
	}
	return link.LeftPortNode, link.LeftPort
}

func isValueRequest(payload any) bool {
	_, ok := payload.(RequestValue)
	return ok
}
