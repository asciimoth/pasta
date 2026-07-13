package std

import (
	"reflect"
	"slices"
	"strconv"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
	"github.com/asciimoth/persist"
)

// NodeTypeObjectPacker is the class name for ObjectPackerClass.
const NodeTypeObjectPacker = "pasta/ObjectPacker"

const objectPackerBasePort = "Base"

const (
	objectPackerOperationSet    = "set"
	objectPackerOperationAppend = "append"
)

var objectPackerFieldOperations = []any{objectPackerOperationSet, objectPackerOperationAppend}

// ObjectPackerClass creates nodes that pack typed inputs into one object.
type ObjectPackerClass struct{}

func (ObjectPackerClass) ClassName() string        { return NodeTypeObjectPacker }
func (ObjectPackerClass) ShortDescription() string { return "Pack object" }
func (ObjectPackerClass) LongDescription() string {
	return "Builds a JSON-like pasta/object value from an optional base object, configured base delete paths, typed input fields, and map/vector paths."
}
func (ObjectPackerClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: TypeObject, InitialPorts: []pasta.Port{
		rightPort(TypeObject),
		{Direction: "left", Name: objectPackerBasePort, Types: []string{TypeObject}},
	}}
}
func (ObjectPackerClass) NewNode(cfg configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
	config := readObjectPackerConfig(cfg)
	if state := firstState(previous); state != nil {
		reconcileObjectPackerState(state, config)
	}
	return newObjectPackerNode(config), nil
}

type objectPackerConfig struct {
	RootKind    string
	Fields      []objectPackerField
	Containers  []objectContainerConfig
	DeletePaths []objectDeletePathConfig
}

type objectPackerField struct {
	ID        string
	Name      string
	Type      string
	Path      []objectPathStep
	Operation string
}

type objectPackerNode struct {
	pasta.BasicNode

	config objectPackerConfig
	inputs map[uint64]persist.Value
	value  Object

	w      *pasta.Workspace
	id     uint64
	out    uint64
	base   uint64
	lefts  map[string]uint64
	fields map[uint64]string
}

func newObjectPackerNode(config objectPackerConfig) *objectPackerNode {
	config = normalizeObjectPackerConfig(config)
	n := &objectPackerNode{
		config: config,
		inputs: map[uint64]persist.Value{},
		value:  NilObject(),
		lefts:  map[string]uint64{},
		fields: map[uint64]string{},
	}
	n.recalculate(false)
	return n
}

func (n *objectPackerNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if n.inputs == nil {
		n.inputs = map[uint64]persist.Value{}
	}
	if restored != nil {
		if len(restored.RightPorts) > 0 {
			n.out = restored.RightPorts[0]
		}
		n.refreshLefts()
	}
	if err := n.w.SetNodePrimaryLocked(n.id, TypeObject); err != nil {
		return err
	}
	if err := n.updatePorts(); err != nil {
		return err
	}
	n.recalculate(false)
	if err := n.clearLabel(); err != nil {
		return err
	}
	n.sendMenuSnapshot(false)
	return nil
}

func (n *objectPackerNode) OnReady() error {
	n.requestAll()
	n.sendAll()
	return nil
}

func (n *objectPackerNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	if portDirection == "right" {
		if port == n.out && linkType == TypeObject {
			return nil
		}
		return pasta.LinkTypeErr(linkType)
	}
	if port == n.base {
		if linkType != TypeObject {
			return pasta.LinkTypeErr(linkType)
		}
		snapshot, ok := n.w.PortSnapshotLocked(port)
		if ok && len(snapshot.Links) > 0 {
			return pasta.ErrLinkDup
		}
		return nil
	}
	fieldID, ok := n.fields[port]
	if !ok {
		return pasta.LinkTypeErr(linkType)
	}
	field, ok := n.fieldByID(fieldID)
	if !ok || linkType != field.Type {
		return pasta.LinkTypeErr(linkType)
	}
	snapshot, ok := n.w.PortSnapshotLocked(port)
	if ok && len(snapshot.Links) > 0 {
		return pasta.ErrLinkDup
	}
	return nil
}

func (n *objectPackerNode) OnLinkAdd(link, port uint64, _, portDirection string) error {
	if portDirection == "left" {
		n.requestLink(link, port)
		return nil
	}
	if port == n.out {
		n.sendToLink(link)
	}
	return nil
}

func (n *objectPackerNode) OnLinkRemoved(_ uint64, port uint64, _ string, portDirection string) error {
	if portDirection == "left" {
		delete(n.inputs, port)
		n.recalculate(true)
	}
	return nil
}

func (n *objectPackerNode) OnPortAdd(port uint64, direction string, _ []string) error {
	if direction == "left" {
		n.refreshLefts()
	}
	return nil
}

func (n *objectPackerNode) OnPortRemoved(port uint64, direction string) error {
	if direction == "left" {
		delete(n.inputs, port)
		n.refreshLefts()
		n.recalculate(true)
	}
	return nil
}

func (n *objectPackerNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	if receiverPortDirection == "right" {
		if event.ReceiverPort == n.out && linkType == TypeObject && isValueRequest(event.Payload) {
			n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: event.SenderNode, ReceiverPort: event.SenderPort, Payload: n.value})
		}
		return nil
	}
	value, ok := payloadToPersistValue(linkType, event.Payload)
	if !ok {
		return nil
	}
	n.inputs[event.ReceiverPort] = value
	n.recalculate(true)
	return nil
}

func (n *objectPackerNode) OnFormularMsg(message any) error {
	msg, ok := message.(formular.FormApplyMessage)
	if !ok || msg.MenuID != pasta.NodeMenuID(n.id) || msg.BlockID != "object" {
		return nil
	}
	next, ok := parseObjectPackerForm(msg.Values)
	if !ok {
		n.sendMenuSnapshot(true)
		return nil
	}
	if objectPackerConfigsEqual(next, n.config) {
		n.sendMenuBlock()
		return nil
	}
	n.config = next
	if err := n.updatePorts(); err != nil {
		return err
	}
	n.recalculate(true)
	n.sendMenuSnapshot(true)
	return nil
}

func (n *objectPackerNode) OnSave(cfg configer.Config) error {
	if err := pasta.DeleteNodeOwnedConfigKeys(cfg); err != nil {
		return err
	}
	if err := cfg.Set(configer.Path{"root"}, n.config.RootKind); err != nil {
		return err
	}
	if err := cfg.Set(configer.Path{"fields"}, objectPackerFieldsConfig(n.config.Fields)); err != nil {
		return err
	}
	if err := cfg.Set(configer.Path{"containers"}, objectContainersConfig(n.config.Containers)); err != nil {
		return err
	}
	return cfg.Set(configer.Path{"delete_paths"}, objectDeletePathsConfig(n.config.DeletePaths))
}

func (n *objectPackerNode) fieldByID(id string) (objectPackerField, bool) {
	for _, field := range n.config.Fields {
		if field.ID == id {
			return field, true
		}
	}
	return objectPackerField{}, false
}

func (n *objectPackerNode) recalculate(broadcast bool) {
	old := n.value
	buildFields := make([]objectBuildField, 0, len(n.config.Fields))
	for _, field := range n.config.Fields {
		value := persist.Nil()
		if port := n.lefts[field.ID]; port > 0 {
			if input, ok := n.inputs[port]; ok {
				value = input
			}
		}
		buildFields = append(buildFields, objectBuildField{Path: field.Path, Value: value, Operation: field.Operation})
	}
	base := persist.Nil()
	if n.base > 0 {
		if input, ok := n.inputs[n.base]; ok {
			base = input
		}
	}
	n.value = buildObjectValueWithBase(base, n.config.RootKind, n.config.Containers, n.config.DeletePaths, buildFields)
	_ = n.clearLabel()
	if broadcast && !old.Equal(n.value) {
		n.sendAll()
	}
}

func (n *objectPackerNode) updatePorts() error {
	desired := objectPackerDesiredPorts(n.config.Fields)
	byName := map[string]uint64{}
	byField := map[string]uint64{}
	if n.base > 0 {
		byField[objectPackerBasePort] = n.base
	}
	for fieldID, port := range n.lefts {
		if port > 0 {
			byField[fieldID] = port
		}
	}
	current := []uint64{}
	snapshot, ok := n.w.NodeSnapshotLocked(n.id)
	if ok {
		current = append([]uint64{}, snapshot.LeftPorts...)
		for _, port := range current {
			if ps, ok := n.w.PortSnapshotLocked(port); ok {
				byName[ps.Name] = port
			}
		}
	}

	keep := map[uint64]struct{}{}
	ordered := make([]uint64, 0, len(desired)+1)
	basePort := pasta.Port{Direction: "left", Name: objectPackerBasePort, Types: []string{TypeObject}}
	if existing := byField[objectPackerBasePort]; existing > 0 {
		keep[existing] = struct{}{}
		ordered = append(ordered, existing)
		_ = n.w.SetPortNameLocked(existing, basePort.Name)
		_ = n.w.SetPortTypesLocked(existing, basePort.Types)
	} else if existing := byName[basePort.Name]; existing > 0 {
		keep[existing] = struct{}{}
		ordered = append(ordered, existing)
		_ = n.w.SetPortTypesLocked(existing, basePort.Types)
	} else {
		basePort.Node = n.id
		id, err := n.w.AddPortLocked(basePort)
		if err != nil {
			return err
		}
		keep[id] = struct{}{}
		ordered = append(ordered, id)
	}
	for _, desired := range desired {
		port := desired.Port
		if existing := byField[desired.FieldID]; existing > 0 {
			keep[existing] = struct{}{}
			ordered = append(ordered, existing)
			_ = n.w.SetPortNameLocked(existing, port.Name)
			_ = n.w.SetPortTypesLocked(existing, port.Types)
			continue
		}
		if existing := byName[port.Name]; existing > 0 {
			keep[existing] = struct{}{}
			ordered = append(ordered, existing)
			_ = n.w.SetPortTypesLocked(existing, port.Types)
			continue
		}
		port.Node = n.id
		id, err := n.w.AddPortLocked(port)
		if err != nil {
			return err
		}
		keep[id] = struct{}{}
		ordered = append(ordered, id)
	}
	for _, port := range current {
		if _, ok := keep[port]; !ok {
			n.w.RemovePortLocked(port)
		}
	}
	n.refreshLefts()
	if len(ordered) > 0 {
		if err := n.w.SetNodePortOrderLocked(n.id, "left", ordered); err != nil {
			return err
		}
	}
	n.requestAll()
	return nil
}

func (n *objectPackerNode) refreshLefts() {
	n.lefts = map[string]uint64{}
	n.fields = map[uint64]string{}
	if n.w == nil || n.id == 0 {
		return
	}
	snapshot, ok := n.w.NodeSnapshotLocked(n.id)
	if !ok {
		return
	}
	byName := map[string]uint64{}
	for _, port := range snapshot.LeftPorts {
		if ps, ok := n.w.PortSnapshotLocked(port); ok {
			byName[ps.Name] = port
		}
	}
	n.base = byName[objectPackerBasePort]
	for _, field := range n.config.Fields {
		port := byName[objectInputPortName(field.Name)]
		n.lefts[field.ID] = port
		if port > 0 {
			n.fields[port] = field.ID
		}
	}
}

func (n *objectPackerNode) requestAll() {
	n.requestPort(n.base)
	for _, port := range n.lefts {
		n.requestPort(port)
	}
}

func (n *objectPackerNode) requestPort(port uint64) {
	snapshot, ok := n.w.PortSnapshotLocked(port)
	if !ok {
		return
	}
	for _, link := range snapshot.Links {
		n.requestLink(link, port)
	}
}

func (n *objectPackerNode) requestLink(link, port uint64) {
	snapshot, ok := n.w.LinkSnapshotLocked(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, port)
	n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: port, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: RequestValue{}})
}

func (n *objectPackerNode) sendAll() {
	port, ok := n.w.PortSnapshotLocked(n.out)
	if !ok {
		return
	}
	for _, link := range port.Links {
		n.sendToLink(link)
	}
}

func (n *objectPackerNode) sendToLink(link uint64) {
	snapshot, ok := n.w.LinkSnapshotLocked(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, n.out)
	n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: n.value})
}

func (n *objectPackerNode) clearLabel() error {
	if n.w == nil || n.id == 0 {
		return nil
	}
	return n.w.SetNodeLabelLocked(n.id, "")
}

func (n *objectPackerNode) sendMenuSnapshot(force bool) {
	n.w.SendNodeMenuMsgLocked(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageMenuSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1},
		Force:       force,
		Blocks:      []formular.Block{n.menuBlock()},
	})
}

func (n *objectPackerNode) sendMenuBlock() {
	n.w.SendNodeMenuMsgLocked(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageBlockSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1, BlockGeneration: 1},
		Block:       n.menuBlock(),
	})
}

func (n *objectPackerNode) menuBlock() formular.Block {
	return formular.Block{
		ID: "object", Order: 10, Generation: 1, Form: true,
		Items: []formular.Item{
			{Type: formular.ItemField, ID: "root", Label: "Root", Field: &formular.Field{Kind: formular.FieldRadio, Value: n.config.RootKind, AllowedValues: []any{objectKindMap, objectKindVector}}},
			{Type: formular.ItemField, ID: "fields", Label: "Fields", Field: &formular.Field{
				Kind:      formular.FieldArray,
				Value:     objectPackerFieldsArrayValue(n.config.Fields),
				Templates: objectPackerFieldTemplates(),
				Elements:  objectPackerFieldElements(n.config.Fields),
			}},
			{Type: formular.ItemField, ID: "delete_paths", Label: "Delete paths", Field: &formular.Field{
				Kind:      formular.FieldArray,
				Value:     objectDeletePathsArrayValue(n.config.DeletePaths),
				Templates: objectDeletePathTemplates(),
				Elements:  objectDeletePathElements(n.config.DeletePaths),
			}},
			{Type: formular.ItemField, ID: "containers", Label: "Containers", Field: &formular.Field{
				Kind:      formular.FieldArray,
				Value:     objectContainersArrayValue(n.config.Containers),
				Templates: objectContainerTemplates(),
				Elements:  objectContainerElements(n.config.Containers),
			}},
		},
	}
}

func readObjectPackerConfig(cfg configer.Config) objectPackerConfig {
	config := objectPackerConfig{RootKind: objectKindMap}
	if cfg == nil {
		return normalizeObjectPackerConfig(config)
	}
	if raw, err := cfg.Get(configer.Path{"root"}); err == nil {
		if root, ok := objectContainerKind(raw); ok {
			config.RootKind = root
		}
	}
	if raw, err := cfg.Get(configer.Path{"fields"}); err == nil {
		if fields, ok := parseObjectPackerFields(raw); ok {
			config.Fields = fields
		}
	}
	if raw, err := cfg.Get(configer.Path{"containers"}); err == nil {
		if containers, ok := parseObjectContainers(raw); ok {
			config.Containers = containers
		}
	}
	if raw, err := cfg.Get(configer.Path{"delete_paths"}); err == nil {
		if deletePaths, ok := parseObjectDeletePaths(raw); ok {
			config.DeletePaths = deletePaths
		}
	}
	config = normalizeObjectPackerConfig(config)
	if err := validateObjectPackerConfig(config); err != nil {
		return objectPackerConfig{RootKind: objectKindMap}
	}
	return config
}

func parseObjectPackerForm(values map[string]any) (objectPackerConfig, bool) {
	root, ok := objectContainerKind(values["root"])
	if !ok {
		return objectPackerConfig{}, false
	}
	fields, ok := parseObjectPackerFields(values["fields"])
	if !ok {
		return objectPackerConfig{}, false
	}
	containers := []objectContainerConfig{}
	if raw, ok := values["containers"]; ok {
		var parsed bool
		containers, parsed = parseObjectContainers(raw)
		if !parsed {
			return objectPackerConfig{}, false
		}
	}
	deletePaths := []objectDeletePathConfig{}
	if raw, ok := values["delete_paths"]; ok {
		var parsed bool
		deletePaths, parsed = parseObjectDeletePaths(raw)
		if !parsed {
			return objectPackerConfig{}, false
		}
	}
	config := normalizeObjectPackerConfig(objectPackerConfig{RootKind: root, Fields: fields, Containers: containers, DeletePaths: deletePaths})
	if err := validateObjectPackerConfig(config); err != nil {
		return objectPackerConfig{}, false
	}
	return config, true
}

func normalizeObjectPackerConfig(config objectPackerConfig) objectPackerConfig {
	if config.RootKind != objectKindVector {
		config.RootKind = objectKindMap
	}
	config.Fields = normalizeObjectPackerFields(config.Fields)
	config.Containers = normalizeObjectContainers(config.Containers)
	config.DeletePaths = normalizeObjectDeletePaths(config.DeletePaths)
	return config
}

func normalizeObjectPackerFields(fields []objectPackerField) []objectPackerField {
	out := make([]objectPackerField, 0, len(fields))
	seenIDs := map[string]struct{}{}
	seenPorts := map[string]struct{}{}
	for i, field := range fields {
		if field.ID == "" {
			field.ID = "field-" + strconv.Itoa(i+1)
		}
		if _, seen := seenIDs[field.ID]; seen {
			field.ID = "field-" + strconv.Itoa(i+1)
		}
		seenIDs[field.ID] = struct{}{}
		if !objectTypeSupported(field.Type) {
			field.Type = TypeObject
		}
		if !objectPackerFieldOperationSupported(field.Operation) {
			field.Operation = objectPackerOperationSet
		}
		field.Name = uniquePortName(field.Name, seenPorts)
		seenPorts[field.Name] = struct{}{}
		out = append(out, field)
	}
	return out
}

func validateObjectPackerConfig(config objectPackerConfig) error {
	paths := make([]objectLayoutPath, 0, len(config.Fields))
	for _, field := range config.Fields {
		if !objectTypeSupported(field.Type) {
			return pasta.LinkTypeErr(field.Type)
		}
		paths = append(paths, objectLayoutPath{Path: field.Path, Append: field.Operation == objectPackerOperationAppend})
	}
	for _, deletePath := range config.DeletePaths {
		paths = append(paths, objectLayoutPath{Path: deletePath.Path})
	}
	return validateObjectLayout(config.RootKind, paths, config.Containers)
}

func parseObjectPackerFields(value any) ([]objectPackerField, bool) {
	items, ok := parseObjectArray(value)
	if !ok {
		return nil, false
	}
	fields := make([]objectPackerField, 0, len(items))
	for i, item := range items {
		field, ok := objectPackerFieldFromValues(item.ID, item.Values)
		if !ok {
			return nil, false
		}
		if field.ID == "" {
			field.ID = "field-" + strconv.Itoa(i+1)
		}
		fields = append(fields, field)
	}
	return normalizeObjectPackerFields(fields), true
}

func objectPackerFieldFromValues(id string, values map[string]any) (objectPackerField, bool) {
	name, _ := parseStringAny(values["name"])
	typ, ok := objectTypeFromMenu(values["type"])
	if !ok {
		return objectPackerField{}, false
	}
	operation := objectPackerOperationSet
	if raw, hasOperation := values["operation"]; hasOperation {
		operation, ok = objectPackerFieldOperation(raw)
		if !ok {
			return objectPackerField{}, false
		}
	}
	path, ok := parseObjectPath(values["path"], operation == objectPackerOperationAppend)
	if !ok {
		return objectPackerField{}, false
	}
	return objectPackerField{ID: id, Name: name, Type: typ, Path: path, Operation: operation}, true
}

func objectPackerFieldsConfig(fields []objectPackerField) []any {
	out := make([]any, 0, len(fields))
	for _, field := range normalizeObjectPackerFields(fields) {
		out = append(out, map[string]any{
			"id":        field.ID,
			"name":      field.Name,
			"type":      field.Type,
			"path":      objectPathConfigValue(field.Path),
			"operation": field.Operation,
		})
	}
	return out
}

func objectPackerFieldsArrayValue(fields []objectPackerField) []formular.ArrayElementValue {
	values := make([]formular.ArrayElementValue, 0, len(fields))
	for _, field := range fields {
		values = append(values, formular.ArrayElementValue{
			ID:       field.ID,
			Template: "field",
			Values: map[string]any{
				"name":      field.Name,
				"type":      objectTypeMenuName(field.Type),
				"path":      objectPathText(field.Path),
				"operation": field.Operation,
			},
		})
	}
	return values
}

func objectPackerFieldTemplates() []formular.ArrayTemplate {
	return []formular.ArrayTemplate{{
		Name:  "field",
		Label: "Field",
		Items: []formular.Item{
			{Type: formular.ItemField, ID: "name", Label: "Name", Field: &formular.Field{Kind: formular.FieldText, Placeholder: "Total"}},
			{Type: formular.ItemField, ID: "type", Label: "Type", Field: &formular.Field{Kind: formular.FieldRadio, Value: objectTypeMenuName(TypeObject), AllowedValues: objectBasicTypes}},
			{Type: formular.ItemField, ID: "operation", Label: "Operation", Field: &formular.Field{Kind: formular.FieldRadio, Value: objectPackerOperationSet, AllowedValues: objectPackerFieldOperations}},
			{Type: formular.ItemField, ID: "path", Label: "Path", Field: &formular.Field{Kind: formular.FieldText, Placeholder: `["summary","total"]`}},
		},
	}}
}

func objectPackerFieldElements(fields []objectPackerField) []formular.ArrayElement {
	elements := make([]formular.ArrayElement, 0, len(fields))
	for _, field := range fields {
		elements = append(elements, formular.ArrayElement{
			ID:       field.ID,
			Template: "field",
			Items: []formular.Item{
				{Type: formular.ItemField, ID: "name", Label: "Name", Field: &formular.Field{Kind: formular.FieldText, Value: field.Name, Placeholder: "Total"}},
				{Type: formular.ItemField, ID: "type", Label: "Type", Field: &formular.Field{Kind: formular.FieldRadio, Value: objectTypeMenuName(field.Type), AllowedValues: objectBasicTypes}},
				{Type: formular.ItemField, ID: "operation", Label: "Operation", Field: &formular.Field{Kind: formular.FieldRadio, Value: field.Operation, AllowedValues: objectPackerFieldOperations}},
				{Type: formular.ItemField, ID: "path", Label: "Path", Field: &formular.Field{Kind: formular.FieldText, Value: objectPathText(field.Path), Placeholder: `["summary","total"]`}},
			},
		})
	}
	return elements
}

func objectPackerFieldOperation(value any) (string, bool) {
	operation, ok := parseStringAny(value)
	if !ok {
		return "", false
	}
	if !objectPackerFieldOperationSupported(operation) {
		return "", false
	}
	return operation, true
}

func objectPackerFieldOperationSupported(operation string) bool {
	return operation == objectPackerOperationSet || operation == objectPackerOperationAppend
}

type objectPackerDesiredPort struct {
	FieldID string
	Port    pasta.Port
}

func objectPackerDesiredPorts(fields []objectPackerField) []objectPackerDesiredPort {
	ports := make([]objectPackerDesiredPort, 0, len(fields))
	for _, field := range normalizeObjectPackerFields(fields) {
		ports = append(ports, objectPackerDesiredPort{
			FieldID: field.ID,
			Port:    pasta.Port{Direction: "left", Name: objectInputPortName(field.Name), Types: []string{field.Type}},
		})
	}
	return ports
}

func objectInputPortName(name string) string {
	return "In " + name
}

func reconcileObjectPackerState(state *pasta.NodeClassState, config objectPackerConfig) {
	state.PrimaryType = TypeObject
	state.RightPorts = []pasta.Port{rightPort(TypeObject)}
	previous := map[string]pasta.Port{}
	for _, port := range state.LeftPorts {
		previous[port.Name] = port
	}
	state.LeftPorts = []pasta.Port{{Direction: "left", Name: objectPackerBasePort, Types: []string{TypeObject}}}
	if kept, ok := previous[objectPackerBasePort]; ok {
		kept.Direction = "left"
		kept.Name = objectPackerBasePort
		kept.Types = []string{TypeObject}
		state.LeftPorts[0] = kept
	}
	for _, desired := range objectPackerDesiredPorts(config.Fields) {
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

func objectPackerConfigsEqual(a, b objectPackerConfig) bool {
	return a.RootKind == b.RootKind &&
		reflect.DeepEqual(a.Fields, b.Fields) &&
		reflect.DeepEqual(a.Containers, b.Containers) &&
		reflect.DeepEqual(a.DeletePaths, b.DeletePaths)
}
