package std

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeStringFormat is the class name for StringFormatClass.
const NodeTypeStringFormat = "pasta/StringFormat"

// StringFormatClass creates template-driven string formatting nodes.
type StringFormatClass struct{}

func (StringFormatClass) ClassName() string        { return NodeTypeStringFormat }
func (StringFormatClass) ShortDescription() string { return "Format string" }
func (StringFormatClass) LongDescription() string {
	return "Builds a string from text and typed placeholder parts. Placeholder parts create matching input ports."
}
func (StringFormatClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: TypeString, InitialPorts: []pasta.Port{rightPort(TypeString)}}
}
func (StringFormatClass) NewNode(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
	parts := readFormatParts(cfg)
	if state := firstState(previous); state != nil {
		reconcileStringFormatState(state, parts)
	}
	return newStringFormatNode(parts), nil
}

type stringFormatPart struct {
	ID   string
	Kind string
	Text string
	Name string
	Type string
}

type stringFormatNode struct {
	pasta.BasicNode

	parts  []stringFormatPart
	inputs map[uint64]string
	value  string

	w     *pasta.Workspace
	id    uint64
	out   uint64
	lefts map[string]uint64
}

func newStringFormatNode(parts []stringFormatPart) *stringFormatNode {
	n := &stringFormatNode{parts: normalizeFormatParts(parts), inputs: map[uint64]string{}, lefts: map[string]uint64{}}
	n.recalculate(false)
	return n
}

func (n *stringFormatNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	n.lefts = map[string]uint64{}
	if restored != nil {
		if len(restored.RightPorts) > 0 {
			n.out = restored.RightPorts[0]
		}
		n.refreshLefts()
	}
	if err := n.w.SetNodePrimary(n.id, TypeString); err != nil {
		return err
	}
	n.recalculate(false)
	if err := n.updatePorts(); err != nil {
		return err
	}
	if err := n.updateLabel(); err != nil {
		return err
	}
	n.sendMenuSnapshot()
	return nil
}

func (n *stringFormatNode) OnReady() error {
	n.requestAll()
	n.sendAll()
	return nil
}

func (n *stringFormatNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	if portDirection == "right" {
		if port == n.out && linkType == TypeString {
			return nil
		}
		return errUnsupportedType(linkType)
	}
	if !stringFormatTypeSupported(linkType) {
		return errUnsupportedType(linkType)
	}
	snapshot, ok := n.w.PortSnapshot(port)
	if ok && len(snapshot.Links) > 0 {
		return pasta.ErrLinkDup
	}
	return nil
}

func (n *stringFormatNode) OnLinkAdd(link, port uint64, _ string, portDirection string) error {
	if portDirection == "left" {
		n.requestLink(link, port)
		return nil
	}
	if port == n.out {
		n.sendToLink(link)
	}
	return nil
}

func (n *stringFormatNode) OnLinkRemoved(_ uint64, port uint64, _ string, portDirection string) error {
	if _, ok := n.inputs[port]; ok {
		delete(n.inputs, port)
		n.recalculate(true)
	}
	return nil
}

func (n *stringFormatNode) OnPortAdd(port uint64, direction string, _ []string) error {
	if direction == "left" {
		n.refreshLefts()
	}
	return nil
}

func (n *stringFormatNode) OnPortRemoved(port uint64, direction string) error {
	if direction == "left" {
		delete(n.inputs, port)
		n.refreshLefts()
		n.recalculate(true)
	}
	return nil
}

func (n *stringFormatNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if receiverPortDirection == "right" {
		if linkType == TypeString && isValueRequest(event.Payload) {
			n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: event.SenderNode, ReceiverPort: event.SenderPort, Payload: String(n.value)})
		}
		return nil
	}
	value, ok := stringFormatPayloadString(linkType, event.Payload)
	if !ok {
		return nil
	}
	n.inputs[event.ReceiverPort] = value
	n.recalculate(true)
	return nil
}

func (n *stringFormatNode) OnFormularMsg(message any) error {
	msg, ok := message.(formular.FieldUpdateMessage)
	if !ok || msg.MenuID != pasta.NodeMenuID(n.id) || msg.Field.BlockID != "template" || msg.Field.FieldID != "parts" {
		return nil
	}
	parts, ok := parseFormatPartValue(msg.Value)
	if !ok {
		return nil
	}
	parts = normalizeFormatParts(parts)
	if stringFormatPartsEqual(parts, n.parts) {
		return nil
	}
	n.parts = parts
	if err := n.updatePorts(); err != nil {
		return err
	}
	n.recalculate(true)
	n.sendMenuBlock()
	return nil
}

func (n *stringFormatNode) OnSave(cfg configer.Config) error {
	if err := pasta.DeleteNodeOwnedConfigKeys(cfg); err != nil {
		return err
	}
	return cfg.Set(configer.Path{"template"}, formatPartsConfig(n.parts))
}

func (n *stringFormatNode) recalculate(broadcast bool) {
	old := n.value
	var b strings.Builder
	for _, part := range n.parts {
		if part.Kind == "text" {
			b.WriteString(part.Text)
			continue
		}
		b.WriteString(n.inputs[n.lefts[part.ID]])
	}
	n.value = b.String()
	_ = n.updateLabel()
	if broadcast && old != n.value {
		n.sendAll()
	}
}

func (n *stringFormatNode) updatePorts() error {
	desired := stringFormatDesiredPorts(n.parts)
	byName := map[string]uint64{}
	byPart := map[string]uint64{}
	for part, port := range n.lefts {
		if port > 0 {
			byPart[part] = port
		}
	}
	current := []uint64{}
	snapshot, ok := n.w.NodeSnapshot(n.id)
	if ok {
		current = append([]uint64{}, snapshot.LeftPorts...)
		for _, port := range current {
			if ps, ok := n.w.PortSnapshot(port); ok {
				byName[ps.Name] = port
			}
		}
	}

	keep := map[uint64]struct{}{}
	ordered := make([]uint64, 0, len(desired))
	for _, desired := range desired {
		port := desired.Port
		if existing := byPart[desired.PartID]; existing > 0 {
			keep[existing] = struct{}{}
			ordered = append(ordered, existing)
			_ = n.w.SetPortName(existing, port.Name)
			_ = n.w.SetPortTypes(existing, port.Types)
			continue
		}
		if existing := byName[port.Name]; existing > 0 {
			keep[existing] = struct{}{}
			ordered = append(ordered, existing)
			_ = n.w.SetPortTypes(existing, port.Types)
			continue
		}
		port.Node = n.id
		id, err := n.w.AddPort(port)
		if err != nil {
			return err
		}
		keep[id] = struct{}{}
		ordered = append(ordered, id)
	}
	for _, port := range current {
		if _, ok := keep[port]; !ok {
			n.w.RemovePort(port)
		}
	}
	n.refreshLefts()
	if len(ordered) > 0 {
		if err := n.w.SetNodePortOrder(n.id, "left", ordered); err != nil {
			return err
		}
	}
	n.requestAll()
	return nil
}

func (n *stringFormatNode) refreshLefts() {
	n.lefts = map[string]uint64{}
	if n.w == nil || n.id == 0 {
		return
	}
	snapshot, ok := n.w.NodeSnapshot(n.id)
	if !ok {
		return
	}
	byName := map[string]uint64{}
	for _, port := range snapshot.LeftPorts {
		if ps, ok := n.w.PortSnapshot(port); ok {
			byName[ps.Name] = port
		}
	}
	for _, part := range n.parts {
		if part.Kind == "value" {
			n.lefts[part.ID] = byName[part.Name]
		}
	}
}

func (n *stringFormatNode) requestAll() {
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

func (n *stringFormatNode) requestLink(link, port uint64) {
	snapshot, ok := n.w.LinkSnapshot(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, port)
	n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: port, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: RequestValue{}})
}

func (n *stringFormatNode) sendAll() {
	port, ok := n.w.PortSnapshot(n.out)
	if !ok {
		return
	}
	for _, link := range port.Links {
		n.sendToLink(link)
	}
}

func (n *stringFormatNode) sendToLink(link uint64) {
	snapshot, ok := n.w.LinkSnapshot(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, n.out)
	n.w.SendEvent(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: String(n.value)})
}

func (n *stringFormatNode) updateLabel() error {
	if n.w == nil || n.id == 0 {
		return nil
	}
	return n.w.SetNodeLabel(n.id, n.value)
}

func (n *stringFormatNode) sendMenuSnapshot() {
	n.w.SendNodeMenuMsg(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageMenuSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1},
		Blocks:      []formular.Block{n.menuBlock()},
	})
}

func (n *stringFormatNode) sendMenuBlock() {
	if n.w == nil || n.id == 0 {
		return
	}
	n.w.SendNodeMenuMsg(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageBlockSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1, BlockGeneration: 1},
		Block:       n.menuBlock(),
	})
}

func (n *stringFormatNode) menuBlock() formular.Block {
	return formular.Block{
		ID: "template", Order: 10, Generation: 1,
		Items: []formular.Item{{
			Type:  formular.ItemField,
			ID:    "parts",
			Label: "Template",
			Field: &formular.Field{
				Kind:      formular.FieldArray,
				Value:     formatPartsArrayValue(n.parts),
				Templates: formatPartTemplates(),
				Elements:  formatPartElements(n.parts),
			},
		}},
	}
}

func reconcileStringFormatState(state *pasta.NodeClassState, parts []stringFormatPart) {
	state.PrimaryType = TypeString
	state.RightPorts = []pasta.Port{rightPort(TypeString)}
	previous := map[string]pasta.Port{}
	for _, port := range state.LeftPorts {
		previous[port.Name] = port
	}
	state.LeftPorts = nil
	for _, desired := range stringFormatDesiredPorts(parts) {
		port := desired.Port
		if kept, ok := previous[port.Name]; ok {
			kept.Direction = "left"
			kept.Types = slices.Clone(port.Types)
			state.LeftPorts = append(state.LeftPorts, kept)
			continue
		}
		state.LeftPorts = append(state.LeftPorts, port)
	}
}

type stringFormatDesiredPort struct {
	PartID string
	Port   pasta.Port
}

func stringFormatDesiredPorts(parts []stringFormatPart) []stringFormatDesiredPort {
	ports := []stringFormatDesiredPort{}
	seen := map[string]struct{}{}
	for _, part := range normalizeFormatParts(parts) {
		if part.Kind != "value" {
			continue
		}
		name := uniquePortName(part.Name, seen)
		ports = append(ports, stringFormatDesiredPort{
			PartID: part.ID,
			Port:   pasta.Port{Direction: "left", Name: name, Types: []string{part.Type}},
		})
		seen[name] = struct{}{}
	}
	return ports
}

func uniquePortName(name string, seen map[string]struct{}) string {
	base := sanitizePortName(name)
	if _, ok := seen[base]; !ok {
		return base
	}
	for i := 2; ; i++ {
		next := fmt.Sprintf("%s %d", base, i)
		if _, ok := seen[next]; !ok {
			return next
		}
	}
}

func sanitizePortName(name string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	space := false
	for i := 0; i < len(name); i++ {
		c := name[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == ' ' || c == '\t'
		if !ok {
			continue
		}
		if c == '\t' {
			c = ' '
		}
		if c == ' ' {
			if space {
				continue
			}
			space = true
		} else {
			space = false
		}
		b.WriteByte(c)
	}
	cleaned := strings.Trim(b.String(), " -_\t\r\n")
	if pasta.ValidatePortName(cleaned) == nil {
		return cleaned
	}
	return "Value"
}

func normalizeFormatParts(parts []stringFormatPart) []stringFormatPart {
	normalized := make([]stringFormatPart, 0, len(parts))
	seenIDs := map[string]struct{}{}
	seenPorts := map[string]struct{}{}
	for i, part := range parts {
		if part.Kind != "value" {
			part.Kind = "text"
		}
		if part.ID == "" {
			part.ID = part.Kind + "-" + strconv.Itoa(i+1)
		}
		if _, ok := seenIDs[part.ID]; ok {
			part.ID = part.Kind + "-" + strconv.Itoa(i+1)
		}
		seenIDs[part.ID] = struct{}{}
		if part.Kind == "value" {
			if !stringFormatTypeSupported(part.Type) {
				part.Type = TypeString
			}
			part.Name = uniquePortName(part.Name, seenPorts)
			seenPorts[part.Name] = struct{}{}
		}
		normalized = append(normalized, part)
	}
	return normalized
}

func stringFormatTypeSupported(typ string) bool {
	return typ == TypeString || typ == TypeInt || typ == TypeFloat || typ == TypeBool
}

func stringFormatPayloadString(linkType string, payload any) (string, bool) {
	switch linkType {
	case TypeString:
		return parseStringAny(payload)
	case TypeInt:
		v, ok := valueFromPayload(TypeInt, payload)
		if !ok {
			return "", false
		}
		return v.label(), true
	case TypeFloat:
		v, ok := valueFromPayload(TypeFloat, payload)
		if !ok {
			return "", false
		}
		return v.label(), true
	case TypeBool:
		v, ok := parseBoolAny(payload)
		if !ok {
			return "", false
		}
		return boolLabel(v), true
	default:
		return "", false
	}
}

func readFormatParts(cfg configer.Config) []stringFormatPart {
	if cfg == nil {
		return nil
	}
	raw, err := cfg.Get(configer.Path{"template"})
	if err != nil {
		return nil
	}
	parts, _ := parseFormatPartValue(raw)
	return parts
}

func parseFormatPartValue(value any) ([]stringFormatPart, bool) {
	switch v := value.(type) {
	case []formular.ArrayElementValue:
		parts := make([]stringFormatPart, 0, len(v))
		for _, element := range v {
			parts = append(parts, formatPartFromValues(element.ID, element.Template, element.Values))
		}
		return parts, true
	case []any:
		parts := make([]stringFormatPart, 0, len(v))
		for _, item := range v {
			element, ok := parseArrayElementMap(item)
			if !ok {
				continue
			}
			parts = append(parts, formatPartFromValues(element.ID, element.Template, element.Values))
		}
		return parts, true
	default:
		return nil, false
	}
}

func parseArrayElementMap(value any) (formular.ArrayElementValue, bool) {
	switch v := value.(type) {
	case formular.ArrayElementValue:
		return v, true
	case map[string]any:
		id, _ := parseStringAny(v["id"])
		template, _ := parseStringAny(v["template"])
		values, _ := v["values"].(map[string]any)
		return formular.ArrayElementValue{ID: id, Template: template, Values: values}, true
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return formular.ArrayElementValue{}, false
		}
		var element formular.ArrayElementValue
		if err := json.Unmarshal(data, &element); err != nil {
			return formular.ArrayElementValue{}, false
		}
		return element, true
	}
}

func formatPartFromValues(id, template string, values map[string]any) stringFormatPart {
	if template == "value" {
		name, _ := parseStringAny(values["name"])
		typ, _ := parseStringAny(values["type"])
		return stringFormatPart{ID: id, Kind: "value", Name: name, Type: typ}
	}
	text, _ := parseStringAny(values["text"])
	return stringFormatPart{ID: id, Kind: "text", Text: text}
}

func formatPartsConfig(parts []stringFormatPart) []any {
	values := formatPartsArrayValue(parts)
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, map[string]any{"id": value.ID, "template": value.Template, "values": value.Values})
	}
	return out
}

func formatPartsArrayValue(parts []stringFormatPart) []formular.ArrayElementValue {
	values := make([]formular.ArrayElementValue, 0, len(parts))
	for _, part := range parts {
		element := formular.ArrayElementValue{ID: part.ID, Template: part.Kind, Values: map[string]any{}}
		if part.Kind == "value" {
			element.Values["name"] = part.Name
			element.Values["type"] = part.Type
		} else {
			element.Values["text"] = part.Text
		}
		values = append(values, element)
	}
	return values
}

func formatPartTemplates() []formular.ArrayTemplate {
	return []formular.ArrayTemplate{
		{
			Name:  "text",
			Label: "Text",
			Items: []formular.Item{{Type: formular.ItemField, ID: "text", Label: "Text", Field: &formular.Field{Kind: formular.FieldText, Multiline: true}}},
		},
		{
			Name:  "value",
			Label: "Value",
			Items: []formular.Item{
				{Type: formular.ItemField, ID: "name", Label: "Name", Field: &formular.Field{Kind: formular.FieldText, Value: "Value"}},
				{Type: formular.ItemField, ID: "type", Label: "Type", Field: &formular.Field{Kind: formular.FieldRadio, Value: TypeString, AllowedValues: []any{TypeString, TypeInt, TypeFloat, TypeBool}}},
			},
		},
	}
}

func formatPartElements(parts []stringFormatPart) []formular.ArrayElement {
	elements := make([]formular.ArrayElement, 0, len(parts))
	for _, part := range parts {
		element := formular.ArrayElement{ID: part.ID, Template: part.Kind}
		if part.Kind == "value" {
			element.Items = []formular.Item{
				{Type: formular.ItemField, ID: "name", Label: "Name", Field: &formular.Field{Kind: formular.FieldText, Value: part.Name}},
				{Type: formular.ItemField, ID: "type", Label: "Type", Field: &formular.Field{Kind: formular.FieldRadio, Value: part.Type, AllowedValues: []any{TypeString, TypeInt, TypeFloat, TypeBool}}},
			}
		} else {
			element.Items = []formular.Item{{Type: formular.ItemField, ID: "text", Label: "Text", Field: &formular.Field{Kind: formular.FieldText, Value: part.Text, Multiline: true}}}
		}
		elements = append(elements, element)
	}
	return elements
}

func stringFormatPartsEqual(a, b []stringFormatPart) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
