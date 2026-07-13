package std

import (
	"sync"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

const (
	// NodeTypeBoolGet is the class name for BoolGetClass.
	NodeTypeBoolGet = "pasta/BoolGet"
	// NodeTypeBoolSet is the class name for BoolSetClass.
	NodeTypeBoolSet = "pasta/BoolSet"
	// NodeTypeIntGet is the class name for IntGetClass.
	NodeTypeIntGet = "pasta/IntGet"
	// NodeTypeIntSet is the class name for IntSetClass.
	NodeTypeIntSet = "pasta/IntSet"
	// NodeTypeFloatGet is the class name for FloatGetClass.
	NodeTypeFloatGet = "pasta/FloatGet"
	// NodeTypeFloatSet is the class name for FloatSetClass.
	NodeTypeFloatSet = "pasta/FloatSet"
	// NodeTypeStringGet is the class name for StringGetClass.
	NodeTypeStringGet = "pasta/StringGet"
	// NodeTypeStringSet is the class name for StringSetClass.
	NodeTypeStringSet = "pasta/StringSet"
	// NodeTypeObjectGet is the class name for ObjectGetClass.
	NodeTypeObjectGet = "pasta/ObjectGet"
	// NodeTypeObjectSet is the class name for ObjectSetClass.
	NodeTypeObjectSet = "pasta/ObjectSet"
)

const (
	defaultVariableName = "var"
	variableMenuBlock   = "state"
)

// BoolGetClass creates nodes that read a named bool variable when triggered.
type BoolGetClass struct{ store *variableStore }

func (c BoolGetClass) ClassName() string        { return NodeTypeBoolGet }
func (c BoolGetClass) ShortDescription() string { return "Get boolean variable" }
func (c BoolGetClass) LongDescription() string {
	return "Reads a named pasta/bool variable only when triggered, sends that value on Value, then emits Trigger."
}
func (c BoolGetClass) DefaultNodeParams() pasta.NodeClassParams {
	return variableGetParams(TypeBool)
}
func (c BoolGetClass) NewNode(cfg configer.Config, _ ...*pasta.NodeClassState) (pasta.Node, error) {
	return newVariableGetNode(TypeBool, c.variableStore(), readVariableName(cfg)), nil
}
func (c BoolGetClass) variableStore() *variableStore { return variableClassStore(c.store, TypeBool) }

// BoolSetClass creates nodes that write a named bool variable when triggered.
type BoolSetClass struct{ store *variableStore }

func (c BoolSetClass) ClassName() string        { return NodeTypeBoolSet }
func (c BoolSetClass) ShortDescription() string { return "Set boolean variable" }
func (c BoolSetClass) LongDescription() string {
	return "Stores the latest pasta/bool input locally and writes it to a named variable only when triggered, then emits Trigger."
}
func (c BoolSetClass) DefaultNodeParams() pasta.NodeClassParams {
	return variableSetParams(TypeBool)
}
func (c BoolSetClass) NewNode(cfg configer.Config, _ ...*pasta.NodeClassState) (pasta.Node, error) {
	return newVariableSetNode(TypeBool, c.variableStore(), readVariableName(cfg), readVariableValue(cfg, TypeBool)), nil
}
func (c BoolSetClass) variableStore() *variableStore { return variableClassStore(c.store, TypeBool) }

// IntGetClass creates nodes that read a named int variable when triggered.
type IntGetClass struct{ store *variableStore }

func (c IntGetClass) ClassName() string        { return NodeTypeIntGet }
func (c IntGetClass) ShortDescription() string { return "Get integer variable" }
func (c IntGetClass) LongDescription() string {
	return "Reads a named pasta/int variable only when triggered, sends that value on Value, then emits Trigger."
}
func (c IntGetClass) DefaultNodeParams() pasta.NodeClassParams {
	return variableGetParams(TypeInt)
}
func (c IntGetClass) NewNode(cfg configer.Config, _ ...*pasta.NodeClassState) (pasta.Node, error) {
	return newVariableGetNode(TypeInt, c.variableStore(), readVariableName(cfg)), nil
}
func (c IntGetClass) variableStore() *variableStore { return variableClassStore(c.store, TypeInt) }

// IntSetClass creates nodes that write a named int variable when triggered.
type IntSetClass struct{ store *variableStore }

func (c IntSetClass) ClassName() string        { return NodeTypeIntSet }
func (c IntSetClass) ShortDescription() string { return "Set integer variable" }
func (c IntSetClass) LongDescription() string {
	return "Stores the latest pasta/int input locally and writes it to a named variable only when triggered, then emits Trigger."
}
func (c IntSetClass) DefaultNodeParams() pasta.NodeClassParams {
	return variableSetParams(TypeInt)
}
func (c IntSetClass) NewNode(cfg configer.Config, _ ...*pasta.NodeClassState) (pasta.Node, error) {
	return newVariableSetNode(TypeInt, c.variableStore(), readVariableName(cfg), readVariableValue(cfg, TypeInt)), nil
}
func (c IntSetClass) variableStore() *variableStore { return variableClassStore(c.store, TypeInt) }

// FloatGetClass creates nodes that read a named float variable when triggered.
type FloatGetClass struct{ store *variableStore }

func (c FloatGetClass) ClassName() string        { return NodeTypeFloatGet }
func (c FloatGetClass) ShortDescription() string { return "Get float variable" }
func (c FloatGetClass) LongDescription() string {
	return "Reads a named pasta/float variable only when triggered, sends that value on Value, then emits Trigger."
}
func (c FloatGetClass) DefaultNodeParams() pasta.NodeClassParams {
	return variableGetParams(TypeFloat)
}
func (c FloatGetClass) NewNode(cfg configer.Config, _ ...*pasta.NodeClassState) (pasta.Node, error) {
	return newVariableGetNode(TypeFloat, c.variableStore(), readVariableName(cfg)), nil
}
func (c FloatGetClass) variableStore() *variableStore { return variableClassStore(c.store, TypeFloat) }

// FloatSetClass creates nodes that write a named float variable when triggered.
type FloatSetClass struct{ store *variableStore }

func (c FloatSetClass) ClassName() string        { return NodeTypeFloatSet }
func (c FloatSetClass) ShortDescription() string { return "Set float variable" }
func (c FloatSetClass) LongDescription() string {
	return "Stores the latest pasta/float input locally and writes it to a named variable only when triggered, then emits Trigger."
}
func (c FloatSetClass) DefaultNodeParams() pasta.NodeClassParams {
	return variableSetParams(TypeFloat)
}
func (c FloatSetClass) NewNode(cfg configer.Config, _ ...*pasta.NodeClassState) (pasta.Node, error) {
	return newVariableSetNode(TypeFloat, c.variableStore(), readVariableName(cfg), readVariableValue(cfg, TypeFloat)), nil
}
func (c FloatSetClass) variableStore() *variableStore { return variableClassStore(c.store, TypeFloat) }

// StringGetClass creates nodes that read a named string variable when triggered.
type StringGetClass struct{ store *variableStore }

func (c StringGetClass) ClassName() string        { return NodeTypeStringGet }
func (c StringGetClass) ShortDescription() string { return "Get string variable" }
func (c StringGetClass) LongDescription() string {
	return "Reads a named pasta/string variable only when triggered, sends that value on Value, then emits Trigger."
}
func (c StringGetClass) DefaultNodeParams() pasta.NodeClassParams {
	return variableGetParams(TypeString)
}
func (c StringGetClass) NewNode(cfg configer.Config, _ ...*pasta.NodeClassState) (pasta.Node, error) {
	return newVariableGetNode(TypeString, c.variableStore(), readVariableName(cfg)), nil
}
func (c StringGetClass) variableStore() *variableStore {
	return variableClassStore(c.store, TypeString)
}

// StringSetClass creates nodes that write a named string variable when triggered.
type StringSetClass struct{ store *variableStore }

func (c StringSetClass) ClassName() string        { return NodeTypeStringSet }
func (c StringSetClass) ShortDescription() string { return "Set string variable" }
func (c StringSetClass) LongDescription() string {
	return "Stores the latest pasta/string input locally and writes it to a named variable only when triggered, then emits Trigger."
}
func (c StringSetClass) DefaultNodeParams() pasta.NodeClassParams {
	return variableSetParams(TypeString)
}
func (c StringSetClass) NewNode(cfg configer.Config, _ ...*pasta.NodeClassState) (pasta.Node, error) {
	return newVariableSetNode(TypeString, c.variableStore(), readVariableName(cfg), readVariableValue(cfg, TypeString)), nil
}
func (c StringSetClass) variableStore() *variableStore {
	return variableClassStore(c.store, TypeString)
}

// ObjectGetClass creates nodes that read a named object variable when triggered.
type ObjectGetClass struct{ store *variableStore }

func (c ObjectGetClass) ClassName() string        { return NodeTypeObjectGet }
func (c ObjectGetClass) ShortDescription() string { return "Get object variable" }
func (c ObjectGetClass) LongDescription() string {
	return "Reads a named pasta/object variable only when triggered, sends that value on Value, then emits Trigger."
}
func (c ObjectGetClass) DefaultNodeParams() pasta.NodeClassParams {
	return variableGetParams(TypeObject)
}
func (c ObjectGetClass) NewNode(cfg configer.Config, _ ...*pasta.NodeClassState) (pasta.Node, error) {
	return newVariableGetNode(TypeObject, c.variableStore(), readVariableName(cfg)), nil
}
func (c ObjectGetClass) variableStore() *variableStore {
	return variableClassStore(c.store, TypeObject)
}

// ObjectSetClass creates nodes that write a named object variable when triggered.
type ObjectSetClass struct{ store *variableStore }

func (c ObjectSetClass) ClassName() string        { return NodeTypeObjectSet }
func (c ObjectSetClass) ShortDescription() string { return "Set object variable" }
func (c ObjectSetClass) LongDescription() string {
	return "Stores the latest pasta/object input locally and writes it to a named variable only when triggered, then emits Trigger."
}
func (c ObjectSetClass) DefaultNodeParams() pasta.NodeClassParams {
	return variableSetParams(TypeObject)
}
func (c ObjectSetClass) NewNode(cfg configer.Config, _ ...*pasta.NodeClassState) (pasta.Node, error) {
	return newVariableSetNode(TypeObject, c.variableStore(), readVariableName(cfg), readVariableValue(cfg, TypeObject)), nil
}
func (c ObjectSetClass) variableStore() *variableStore {
	return variableClassStore(c.store, TypeObject)
}

type variableClassStores struct {
	boolStore   *variableStore
	intStore    *variableStore
	floatStore  *variableStore
	stringStore *variableStore
	objectStore *variableStore
}

func newVariableClassStores() *variableClassStores {
	return &variableClassStores{
		boolStore:   newVariableStore(TypeBool),
		intStore:    newVariableStore(TypeInt),
		floatStore:  newVariableStore(TypeFloat),
		stringStore: newVariableStore(TypeString),
		objectStore: newVariableStore(TypeObject),
	}
}

func (s *variableClassStores) get(typ string) *variableStore {
	if s == nil {
		return fallbackVariableClassStores.get(typ)
	}
	switch typ {
	case TypeBool:
		return s.boolStore
	case TypeInt:
		return s.intStore
	case TypeFloat:
		return s.floatStore
	case TypeString:
		return s.stringStore
	case TypeObject:
		return s.objectStore
	default:
		return nil
	}
}

var fallbackVariableClassStores = &variableClassStores{
	boolStore:   newVariableStore(TypeBool),
	intStore:    newVariableStore(TypeInt),
	floatStore:  newVariableStore(TypeFloat),
	stringStore: newVariableStore(TypeString),
	objectStore: newVariableStore(TypeObject),
}

func variableClassStore(store *variableStore, typ string) *variableStore {
	if store != nil {
		return store
	}
	return fallbackVariableClassStores.get(typ)
}

type variableStore struct {
	mu    sync.Mutex
	typ   string
	items map[string]variableEntry
}

type variableEntry struct {
	value any
	refs  int
}

func newVariableStore(typ string) *variableStore {
	return &variableStore{typ: typ, items: map[string]variableEntry{}}
}

func (s *variableStore) acquire(name string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.items[name]
	if !ok {
		entry = variableEntry{value: variableDefaultValue(s.typ)}
	}
	entry.refs++
	s.items[name] = entry
}

func (s *variableStore) release(name string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.items[name]
	if !ok {
		return
	}
	entry.refs--
	if entry.refs <= 0 {
		delete(s.items, name)
		return
	}
	s.items[name] = entry
}

func (s *variableStore) set(name string, value any) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.items[name]
	if !ok {
		entry = variableEntry{}
	}
	entry.value = value
	s.items[name] = entry
}

func (s *variableStore) get(name string) any {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.items[name]
	if !ok {
		return variableDefaultValue(s.typ)
	}
	return entry.value
}

type variableSetNode struct {
	pasta.BasicNode

	typ   string
	store *variableStore
	name  string
	value any

	w          *pasta.Workspace
	id         uint64
	triggerIn  uint64
	valueIn    uint64
	triggerOut uint64
	acquired   bool
}

func newVariableSetNode(typ string, store *variableStore, name string, value any) *variableSetNode {
	if name == "" {
		name = defaultVariableName
	}
	if value == nil {
		value = variableDefaultValue(typ)
	}
	return &variableSetNode{typ: typ, store: store, name: name, value: value}
}

func (n *variableSetNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	n.findPorts(restored)
	if err := n.w.SetNodePrimaryLocked(n.id, n.typ); err != nil {
		return err
	}
	n.acquireName(n.name)
	if err := n.updateLabel(); err != nil {
		return err
	}
	n.sendMenuSnapshot(false)
	return nil
}

func (n *variableSetNode) OnStop() {
	if n.acquired {
		n.store.release(n.name)
		n.acquired = false
	}
}

func (n *variableSetNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	switch port {
	case n.triggerIn:
		if portDirection == "left" && linkType == TypeTrigger {
			return nil
		}
	case n.valueIn:
		if portDirection == "left" && linkType == n.typ {
			snapshot, ok := n.w.PortSnapshotLocked(port)
			if ok && len(snapshot.Links) > 0 {
				return pasta.ErrLinkDup
			}
			return nil
		}
	case n.triggerOut:
		if portDirection == "right" && linkType == TypeTrigger {
			return nil
		}
	}
	return pasta.LinkTypeErr(linkType)
}

func (n *variableSetNode) OnLinkRemoved(_ uint64, port uint64, _ string, _ string) error {
	if port == n.valueIn {
		n.value = variableDefaultValue(n.typ)
		if err := n.updateLabel(); err != nil {
			return err
		}
		n.sendMenuBlock()
	}
	return nil
}

func (n *variableSetNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	switch event.ReceiverPort {
	case n.triggerIn:
		if receiverPortDirection == "left" && linkType == TypeTrigger && !IsRequest(event.Payload) {
			n.run()
		}
	case n.valueIn:
		if receiverPortDirection != "left" || linkType != n.typ || isValueRequest(event.Payload) {
			return nil
		}
		value, ok := variableValueFromPayload(n.typ, event.Payload)
		if !ok {
			return nil
		}
		n.value = value
		if err := n.updateLabel(); err != nil {
			return err
		}
		n.sendMenuBlock()
	}
	return nil
}

func (n *variableSetNode) OnFormularMsg(message any) error {
	msg, ok := message.(formular.FormApplyMessage)
	if !ok || msg.MenuID != pasta.NodeMenuID(n.id) || msg.BlockID != variableMenuBlock {
		return nil
	}
	nextName, ok := parseStringAny(msg.Values["name"])
	if !ok {
		n.sendMenuSnapshot(true)
		return nil
	}
	nextValue, ok := variableValueFromMenu(n.typ, msg.Values["value"])
	if !ok {
		n.sendMenuSnapshot(true)
		return nil
	}
	if nextName == "" {
		nextName = defaultVariableName
	}
	if nextName != n.name {
		n.acquireName(nextName)
	}
	n.value = nextValue
	if err := n.updateLabel(); err != nil {
		return err
	}
	n.sendMenuBlock()
	return nil
}

func (n *variableSetNode) OnTrigger() error {
	n.run()
	return nil
}

func (n *variableSetNode) OnSave(cfg configer.Config) error {
	if err := pasta.DeleteNodeOwnedConfigKeys(cfg); err != nil {
		return err
	}
	if err := cfg.Set(configer.Path{"name"}, n.name); err != nil {
		return err
	}
	value, err := variableValueToConfigValue(n.typ, n.value)
	if err != nil {
		return err
	}
	return cfg.Set(configer.Path{"value"}, value)
}

func (n *variableSetNode) findPorts(restored *pasta.NodeInitData) {
	if restored == nil {
		return
	}
	for _, port := range restored.LeftPorts {
		snapshot, ok := n.w.PortSnapshotLocked(port)
		if !ok {
			continue
		}
		switch snapshot.Name {
		case "Trigger":
			n.triggerIn = port
		case "Value":
			n.valueIn = port
		}
	}
	for _, port := range restored.RightPorts {
		snapshot, ok := n.w.PortSnapshotLocked(port)
		if ok && snapshot.Name == "Trigger" {
			n.triggerOut = port
		}
	}
}

func (n *variableSetNode) acquireName(next string) {
	if n.store == nil {
		n.store = variableClassStore(nil, n.typ)
	}
	if n.acquired {
		n.store.release(n.name)
	}
	n.name = next
	n.store.acquire(n.name)
	n.acquired = true
}

func (n *variableSetNode) run() {
	n.store.set(n.name, n.value)
	n.emitTrigger()
}

func (n *variableSetNode) emitTrigger() {
	port, ok := n.w.PortSnapshotLocked(n.triggerOut)
	if !ok {
		return
	}
	for _, link := range port.Links {
		snapshot, ok := n.w.LinkSnapshotLocked(link)
		if !ok {
			continue
		}
		receiverNode, receiverPort := otherEndpoint(snapshot, n.triggerOut)
		n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: n.triggerOut, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: Trigger{}})
	}
}

func (n *variableSetNode) updateLabel() error {
	return n.w.SetNodeLabelLocked(n.id, n.name)
}

func (n *variableSetNode) sendMenuSnapshot(force bool) {
	n.w.SendNodeMenuMsgLocked(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageMenuSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1},
		Force:       force,
		Blocks:      []formular.Block{n.menuBlock()},
	})
}

func (n *variableSetNode) sendMenuBlock() {
	n.w.SendNodeMenuMsgLocked(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageBlockSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1, BlockGeneration: 1},
		Block:       n.menuBlock(),
	})
}

func (n *variableSetNode) menuBlock() formular.Block {
	return formular.Block{
		ID: variableMenuBlock, Order: 10, Generation: 1, Form: true,
		Items: []formular.Item{
			{Type: formular.ItemField, ID: "name", Label: "Name", Field: &formular.Field{Kind: formular.FieldText, Value: n.name, Required: true}},
			{Type: formular.ItemField, ID: "value", Label: "Value", Field: variableField(n.typ, n.value)},
		},
	}
}

type variableGetNode struct {
	pasta.BasicNode

	typ   string
	store *variableStore
	name  string
	sent  map[uint64]any

	w          *pasta.Workspace
	id         uint64
	triggerIn  uint64
	triggerOut uint64
	valueOut   uint64
	acquired   bool
}

func newVariableGetNode(typ string, store *variableStore, name string) *variableGetNode {
	if name == "" {
		name = defaultVariableName
	}
	return &variableGetNode{typ: typ, store: store, name: name, sent: map[uint64]any{}}
}

func (n *variableGetNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	n.findPorts(restored)
	if n.sent == nil {
		n.sent = map[uint64]any{}
	}
	if err := n.w.SetNodePrimaryLocked(n.id, n.typ); err != nil {
		return err
	}
	n.acquireName(n.name)
	if err := n.updateLabel(); err != nil {
		return err
	}
	n.sendMenuSnapshot(false)
	return nil
}

func (n *variableGetNode) OnStop() {
	if n.acquired {
		n.store.release(n.name)
		n.acquired = false
	}
}

func (n *variableGetNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	switch port {
	case n.triggerIn:
		if portDirection == "left" && linkType == TypeTrigger {
			return nil
		}
	case n.triggerOut:
		if portDirection == "right" && linkType == TypeTrigger {
			return nil
		}
	case n.valueOut:
		if portDirection == "right" && linkType == n.typ {
			return nil
		}
	}
	return pasta.LinkTypeErr(linkType)
}

func (n *variableGetNode) OnLinkRemoved(link uint64, port uint64, _ string, _ string) error {
	if port == n.valueOut {
		delete(n.sent, link)
	}
	return nil
}

func (n *variableGetNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	switch event.ReceiverPort {
	case n.triggerIn:
		if receiverPortDirection == "left" && linkType == TypeTrigger && !IsRequest(event.Payload) {
			n.run()
		}
	case n.valueOut:
		if receiverPortDirection == "right" && linkType == n.typ && isValueRequest(event.Payload) {
			if value, ok := n.sent[event.Link]; ok {
				n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: n.valueOut, ReceiverNode: event.SenderNode, ReceiverPort: event.SenderPort, Payload: value})
			}
		}
	}
	return nil
}

func (n *variableGetNode) OnFormularMsg(message any) error {
	msg, ok := message.(formular.FormApplyMessage)
	if !ok || msg.MenuID != pasta.NodeMenuID(n.id) || msg.BlockID != variableMenuBlock {
		return nil
	}
	nextName, ok := parseStringAny(msg.Values["name"])
	if !ok {
		n.sendMenuSnapshot(true)
		return nil
	}
	if nextName == "" {
		nextName = defaultVariableName
	}
	if nextName != n.name {
		n.acquireName(nextName)
		n.sent = map[uint64]any{}
	}
	if err := n.updateLabel(); err != nil {
		return err
	}
	n.sendMenuBlock()
	return nil
}

func (n *variableGetNode) OnTrigger() error {
	n.run()
	return nil
}

func (n *variableGetNode) OnSave(cfg configer.Config) error {
	if err := pasta.DeleteNodeOwnedConfigKeys(cfg); err != nil {
		return err
	}
	return cfg.Set(configer.Path{"name"}, n.name)
}

func (n *variableGetNode) findPorts(restored *pasta.NodeInitData) {
	if restored == nil {
		return
	}
	for _, port := range restored.LeftPorts {
		snapshot, ok := n.w.PortSnapshotLocked(port)
		if ok && snapshot.Name == "Trigger" {
			n.triggerIn = port
		}
	}
	for _, port := range restored.RightPorts {
		snapshot, ok := n.w.PortSnapshotLocked(port)
		if !ok {
			continue
		}
		switch snapshot.Name {
		case "Trigger":
			n.triggerOut = port
		case "Value":
			n.valueOut = port
		}
	}
}

func (n *variableGetNode) acquireName(next string) {
	if n.store == nil {
		n.store = variableClassStore(nil, n.typ)
	}
	if n.acquired {
		n.store.release(n.name)
	}
	n.name = next
	n.store.acquire(n.name)
	n.acquired = true
}

func (n *variableGetNode) run() {
	value := n.store.get(n.name)
	n.sendValueAll(value)
	n.emitTrigger()
}

func (n *variableGetNode) sendValueAll(value any) {
	port, ok := n.w.PortSnapshotLocked(n.valueOut)
	if !ok {
		return
	}
	for _, link := range port.Links {
		n.sendValueToLink(link, value)
	}
}

func (n *variableGetNode) sendValueToLink(link uint64, value any) {
	snapshot, ok := n.w.LinkSnapshotLocked(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, n.valueOut)
	n.sent[link] = value
	n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: n.valueOut, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: value})
}

func (n *variableGetNode) emitTrigger() {
	port, ok := n.w.PortSnapshotLocked(n.triggerOut)
	if !ok {
		return
	}
	for _, link := range port.Links {
		snapshot, ok := n.w.LinkSnapshotLocked(link)
		if !ok {
			continue
		}
		receiverNode, receiverPort := otherEndpoint(snapshot, n.triggerOut)
		n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: n.triggerOut, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: Trigger{}})
	}
}

func (n *variableGetNode) updateLabel() error {
	return n.w.SetNodeLabelLocked(n.id, n.name)
}

func (n *variableGetNode) sendMenuSnapshot(force bool) {
	n.w.SendNodeMenuMsgLocked(n.id, formular.MenuSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageMenuSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1},
		Force:       force,
		Blocks:      []formular.Block{n.menuBlock()},
	})
}

func (n *variableGetNode) sendMenuBlock() {
	n.w.SendNodeMenuMsgLocked(n.id, formular.BlockSnapshotMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageBlockSnapshot, MenuID: pasta.NodeMenuID(n.id), MenuGeneration: 1, BlockGeneration: 1},
		Block:       n.menuBlock(),
	})
}

func (n *variableGetNode) menuBlock() formular.Block {
	return formular.Block{
		ID: variableMenuBlock, Order: 10, Generation: 1, Form: true,
		Items: []formular.Item{{Type: formular.ItemField, ID: "name", Label: "Name", Field: &formular.Field{Kind: formular.FieldText, Value: n.name, Required: true}}},
	}
}

func variableGetParams(typ string) pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: typ, InitialPorts: []pasta.Port{
		{Direction: "right", Name: "Trigger", Types: []string{TypeTrigger}},
		{Direction: "right", Name: "Value", Types: []string{typ}},
		{Direction: "left", Name: "Trigger", Types: []string{TypeTrigger}},
	}}
}

func variableSetParams(typ string) pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: typ, InitialPorts: []pasta.Port{
		{Direction: "right", Name: "Trigger", Types: []string{TypeTrigger}},
		{Direction: "left", Name: "Trigger", Types: []string{TypeTrigger}},
		{Direction: "left", Name: "Value", Types: []string{typ}},
	}}
}

func readVariableName(cfg configer.Config) string {
	if cfg == nil {
		return defaultVariableName
	}
	raw, err := cfg.Get(configer.Path{"name"})
	if err != nil {
		return defaultVariableName
	}
	name, ok := parseStringAny(raw)
	if !ok || name == "" {
		return defaultVariableName
	}
	return name
}

func readVariableValue(cfg configer.Config, typ string) any {
	if cfg == nil {
		return variableDefaultValue(typ)
	}
	raw, err := cfg.Get(configer.Path{"value"})
	if err != nil {
		return variableDefaultValue(typ)
	}
	if value, ok := variableValueFromConfig(typ, raw); ok {
		return value
	}
	return variableDefaultValue(typ)
}

func variableDefaultValue(typ string) any {
	switch typ {
	case TypeBool:
		return false
	case TypeInt:
		return Int(0)
	case TypeFloat:
		return Float(0)
	case TypeString:
		return String("")
	case TypeObject:
		return NilObject()
	default:
		return nil
	}
}

func variableValueFromPayload(typ string, payload any) (any, bool) {
	switch typ {
	case TypeBool:
		return parseBoolAny(payload)
	case TypeInt:
		value, ok := parseIntAny(payload)
		return Int(value), ok
	case TypeFloat:
		value, ok := parseFloatAny(payload)
		return Float(value), ok
	case TypeString:
		value, ok := parseStringAny(payload)
		return String(value), ok
	case TypeObject:
		return ObjectFromPayload(payload)
	default:
		return nil, false
	}
}

func variableValueFromMenu(typ string, value any) (any, bool) {
	if typ != TypeObject {
		return variableValueFromPayload(typ, value)
	}
	raw, ok := parseStringAny(value)
	if !ok {
		return nil, false
	}
	object, err := ObjectFromJSON([]byte(raw))
	return object, err == nil
}

func variableValueFromConfig(typ string, value any) (any, bool) {
	if typ != TypeObject {
		return variableValueFromPayload(typ, value)
	}
	return objectFromConfigValue(value)
}

func variableValueToConfigValue(typ string, value any) (any, error) {
	switch typ {
	case TypeBool:
		v, _ := parseBoolAny(value)
		return v, nil
	case TypeInt:
		v, _ := parseIntAny(value)
		return v, nil
	case TypeFloat:
		v, _ := parseFloatAny(value)
		return v, nil
	case TypeString:
		v, _ := parseStringAny(value)
		return v, nil
	case TypeObject:
		object, ok := ObjectFromPayload(value)
		if !ok {
			object = NilObject()
		}
		return objectToConfigValue(object)
	default:
		return nil, nil
	}
}

func variableField(typ string, value any) *formular.Field {
	field := &formular.Field{Kind: variableFieldKind(typ), Value: variableMenuValue(typ, value)}
	if typ == TypeObject {
		field.Multiline = true
		field.Placeholder = `{"name":"Ada"}`
	}
	return field
}

func variableFieldKind(typ string) string {
	switch typ {
	case TypeBool:
		return formular.FieldCheckbox
	case TypeFloat:
		return formular.FieldFloat
	case TypeString, TypeObject:
		return formular.FieldText
	default:
		return formular.FieldInt
	}
}

func variableMenuValue(typ string, value any) any {
	switch typ {
	case TypeBool:
		v, _ := parseBoolAny(value)
		return v
	case TypeInt:
		v, _ := parseIntAny(value)
		return v
	case TypeFloat:
		v, _ := parseFloatAny(value)
		return v
	case TypeString:
		v, _ := parseStringAny(value)
		return v
	case TypeObject:
		object, ok := ObjectFromPayload(value)
		if !ok {
			object = NilObject()
		}
		return object.PrettyJSONString()
	default:
		return nil
	}
}
