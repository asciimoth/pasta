package std

import (
	"bytes"
	"encoding/json"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
	"github.com/asciimoth/persist"
)

// NodeTypeObjectToString is the class name for ObjectToStringClass.
const NodeTypeObjectToString = "pasta/ObjectToString"

// ObjectToStringClass creates nodes that stringify pasta/object values.
type ObjectToStringClass struct{}

func (ObjectToStringClass) ClassName() string        { return NodeTypeObjectToString }
func (ObjectToStringClass) ShortDescription() string { return "Object to string" }
func (ObjectToStringClass) LongDescription() string {
	return "Formats one pasta/object input as compact or pretty JSON on a pasta/string output."
}
func (ObjectToStringClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: TypeString, InitialPorts: []pasta.Port{
		rightPort(TypeString),
		{Direction: "left", Name: "input", Types: []string{TypeObject}},
	}}
}
func (ObjectToStringClass) NewNode(cfg configer.Config, _ ...*pasta.NodeClassState) (pasta.Node, error) {
	return newObjectToStringNode(readBoolAt(cfg, "pretty", false), readBoolAt(cfg, "omit_empty", false)), nil
}

type objectToStringNode struct {
	pasta.BasicNode

	pretty    bool
	omitEmpty bool
	input     Object
	value     string

	w   *pasta.Workspace
	id  uint64
	in  uint64
	out uint64
}

func newObjectToStringNode(pretty, omitEmpty bool) *objectToStringNode {
	n := &objectToStringNode{pretty: pretty, omitEmpty: omitEmpty, input: NilObject()}
	n.recalculate(false)
	return n
}

func (n *objectToStringNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if restored != nil {
		if len(restored.LeftPorts) > 0 {
			n.in = restored.LeftPorts[0]
		}
		if len(restored.RightPorts) > 0 {
			n.out = restored.RightPorts[0]
		}
	}
	if err := n.w.SetNodePrimaryLocked(n.id, TypeString); err != nil {
		return err
	}
	n.recalculate(false)
	if err := n.clearLabel(); err != nil {
		return err
	}
	n.sendMenuSnapshot(false)
	return nil
}

func (n *objectToStringNode) OnReady() error {
	n.requestInput()
	n.sendAll()
	return nil
}

func (n *objectToStringNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	if portDirection == "left" {
		if port != n.in || linkType != TypeObject {
			return pasta.LinkTypeErr(linkType)
		}
		snapshot, ok := n.w.PortSnapshotLocked(port)
		if ok && len(snapshot.Links) > 0 {
			return pasta.ErrLinkDup
		}
		return nil
	}
	if port == n.out && linkType == TypeString {
		return nil
	}
	return pasta.LinkTypeErr(linkType)
}

func (n *objectToStringNode) OnLinkAdd(link, port uint64, _, portDirection string) error {
	if portDirection == "left" {
		n.requestLink(link, port)
		return nil
	}
	if port == n.out {
		n.sendToLink(link)
	}
	return nil
}

func (n *objectToStringNode) OnLinkRemoved(_ uint64, port uint64, _ string, portDirection string) error {
	if portDirection == "left" && port == n.in {
		n.input = NilObject()
		n.recalculate(true)
	}
	return nil
}

func (n *objectToStringNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if receiverPortDirection == "right" {
		if event.ReceiverPort == n.out && linkType == TypeString && isValueRequest(event.Payload) {
			n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: event.SenderNode, ReceiverPort: event.SenderPort, Payload: String(n.value)})
		}
		return nil
	}
	if event.ReceiverPort != n.in || linkType != TypeObject {
		return nil
	}
	object, ok := ObjectFromPayload(event.Payload)
	if !ok {
		return nil
	}
	n.input = object
	n.recalculate(true)
	return nil
}

func (n *objectToStringNode) OnFormularMsg(message any) error {
	msg, ok := message.(formular.FieldUpdateMessage)
	if !ok || msg.MenuID != pasta.NodeMenuID(n.id) || msg.Field.BlockID != "state" {
		return nil
	}
	changed := false
	switch msg.Field.FieldID {
	case "pretty":
		value, ok := parseBoolAny(msg.Value)
		if ok && value != n.pretty {
			n.pretty = value
			changed = true
		}
	case "omit_empty":
		value, ok := parseBoolAny(msg.Value)
		if ok && value != n.omitEmpty {
			n.omitEmpty = value
			changed = true
		}
	}
	if !changed {
		return nil
	}
	n.recalculate(true)
	n.sendMenuBlock()
	return nil
}

func (n *objectToStringNode) OnSave(cfg configer.Config) error {
	if err := pasta.DeleteNodeOwnedConfigKeys(cfg); err != nil {
		return err
	}
	if err := cfg.Set(configer.Path{"pretty"}, n.pretty); err != nil {
		return err
	}
	return cfg.Set(configer.Path{"omit_empty"}, n.omitEmpty)
}

func (n *objectToStringNode) recalculate(broadcast bool) {
	old := n.value
	n.value = formatObjectString(n.input, n.pretty, n.omitEmpty)
	_ = n.clearLabel()
	n.sendMenuBlock()
	if broadcast && old != n.value {
		n.sendAll()
	}
}

func (n *objectToStringNode) clearLabel() error {
	if n.w == nil || n.id == 0 {
		return nil
	}
	return n.w.SetNodeLabelLocked(n.id, "")
}

func (n *objectToStringNode) requestInput() {
	snapshot, ok := n.w.PortSnapshotLocked(n.in)
	if !ok {
		return
	}
	for _, link := range snapshot.Links {
		n.requestLink(link, n.in)
	}
}

func (n *objectToStringNode) requestLink(link, port uint64) {
	snapshot, ok := n.w.LinkSnapshotLocked(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, port)
	n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: port, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: RequestValue{}})
}

func (n *objectToStringNode) sendAll() {
	port, ok := n.w.PortSnapshotLocked(n.out)
	if !ok {
		return
	}
	for _, link := range port.Links {
		n.sendToLink(link)
	}
}

func (n *objectToStringNode) sendToLink(link uint64) {
	snapshot, ok := n.w.LinkSnapshotLocked(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, n.out)
	n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: String(n.value)})
}

func (n *objectToStringNode) sendMenuSnapshot(force bool) {
	if n.w == nil || n.id == 0 {
		return
	}
	n.w.SendNodeMenuMsgLocked(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageMenuSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1},
		Force:       force,
		Blocks:      []formular.Block{n.menuBlock()},
	})
}

func (n *objectToStringNode) sendMenuBlock() {
	if n.w == nil || n.id == 0 {
		return
	}
	n.w.SendNodeMenuMsgLocked(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageBlockSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1, BlockGeneration: 1},
		Block:       n.menuBlock(),
	})
}

func (n *objectToStringNode) menuBlock() formular.Block {
	return formular.Block{
		ID: "state", Order: 10, Generation: 1,
		Items: []formular.Item{
			{Type: formular.ItemField, ID: "pretty", Label: "Pretty print", Field: &formular.Field{Kind: formular.FieldCheckbox, Value: n.pretty}},
			{Type: formular.ItemField, ID: "omit_empty", Label: "Omit empty/nil", Field: &formular.Field{Kind: formular.FieldCheckbox, Value: n.omitEmpty}},
			{Type: formular.ItemField, ID: "value", Label: "Result", Field: &formular.Field{Kind: formular.FieldText, Value: n.value, Readonly: true, Multiline: true, Placeholder: `{"name":"Ada"}`}},
		},
	}
}

func formatObjectString(object Object, pretty, omitEmpty bool) string {
	value := object.Value()
	if omitEmpty {
		if omitted, keep := omitEmptyObjectValue(value); keep {
			value = omitted
		} else {
			value = persist.Nil()
		}
	}
	data, err := value.ToJSON()
	if err != nil {
		return "null"
	}
	if !pretty {
		var out bytes.Buffer
		if err := json.Compact(&out, data); err == nil {
			return out.String()
		}
		return string(data)
	}
	var out bytes.Buffer
	if err := json.Indent(&out, data, "", "  "); err != nil {
		return string(data)
	}
	return out.String()
}

func omitEmptyObjectValue(value persist.Value) (persist.Value, bool) {
	switch value.Kind() {
	case persist.KindNil:
		return persist.Nil(), false
	case persist.KindMap:
		m, _ := value.Map()
		out := persist.NewMap()
		m.Range(func(k persist.Key, v persist.Value) bool {
			if kept, ok := omitEmptyObjectValue(v); ok {
				out = out.Assoc(k, kept)
			}
			return true
		})
		if out.Len() == 0 {
			return persist.Nil(), false
		}
		return persist.MapValue(out), true
	case persist.KindVector:
		v, _ := value.Vector()
		out := make([]persist.Value, 0, v.Len())
		v.Range(func(_ int, item persist.Value) bool {
			if kept, ok := omitEmptyObjectValue(item); ok {
				out = append(out, kept)
			}
			return true
		})
		if len(out) == 0 {
			return persist.Nil(), false
		}
		return persist.VectorValue(persist.NewVector(out...)), true
	default:
		return value, true
	}
}

func readBoolAt(cfg configer.Config, key string, fallback bool) bool {
	if cfg == nil {
		return fallback
	}
	raw, err := cfg.Get(configer.Path{key})
	if err != nil {
		return fallback
	}
	if value, ok := parseBoolAny(raw); ok {
		return value
	}
	return fallback
}
