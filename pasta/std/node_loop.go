package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

const (
	// NodeTypeForLoop is the class name for ForLoopClass.
	NodeTypeForLoop = "pasta/ForLoop"
	// NodeTypeWhileLoop is the class name for WhileLoopClass.
	NodeTypeWhileLoop = "pasta/WhileLoop"
)

const (
	loopLabelWaiting = "waiting"
	loopLabelLooping = "looping"
)

// ForLoopClass creates counted trigger loops.
//
// A ForLoop captures Start index, End index, and Step when Trigger receives a
// pasta/trigger event. It then emits LoopStartIteration, the current Index, and
// Body for each iteration. The matching Iter node must send LoopEndIteration
// back over the pasta/loop link before the next iteration can start.
type ForLoopClass struct{}

func (ForLoopClass) ClassName() string        { return NodeTypeForLoop }
func (ForLoopClass) ShortDescription() string { return "Run counted loop" }
func (ForLoopClass) LongDescription() string {
	return "Runs a counted loop from Start index toward End index by Step. Each iteration emits Body and waits for the associated Iter node to send break or continue over the pasta/loop link."
}
func (ForLoopClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: TypeLoop, InitialPorts: []pasta.Port{
		{Direction: "left", Name: "Trigger", Types: []string{TypeTrigger}},
		{Direction: "left", Name: "Start index", Types: []string{TypeInt}},
		{Direction: "left", Name: "End index", Types: []string{TypeInt}},
		{Direction: "left", Name: "Step", Types: []string{TypeInt}},
		{Direction: "right", Name: "Loop", Types: []string{TypeLoop}},
		{Direction: "right", Name: "Body", Types: []string{TypeTrigger}},
		{Direction: "right", Name: "Index", Types: []string{TypeInt}},
		{Direction: "right", Name: "Completed", Types: []string{TypeTrigger}},
	}}
}
func (ForLoopClass) NewNode(_ configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
	if state := firstState(previous); state != nil {
		state.PrimaryType = TypeLoop
		state.Label = loopLabelWaiting
	}
	return newForLoopNode(), nil
}

// WhileLoopClass creates trigger loops that run until Iter sends break.
//
// A WhileLoop starts when Trigger receives a pasta/trigger event. Each
// iteration emits Body and waits for the associated Iter node. Continue starts
// the next iteration; break emits Completed and ends the loop.
type WhileLoopClass struct{}

func (WhileLoopClass) ClassName() string        { return NodeTypeWhileLoop }
func (WhileLoopClass) ShortDescription() string { return "Run loop until break" }
func (WhileLoopClass) LongDescription() string {
	return "Runs Body repeatedly until the associated Iter node sends break over the pasta/loop link. Continue starts the next iteration."
}
func (WhileLoopClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: TypeLoop, InitialPorts: []pasta.Port{
		{Direction: "left", Name: "Trigger", Types: []string{TypeTrigger}},
		{Direction: "right", Name: "Loop", Types: []string{TypeLoop}},
		{Direction: "right", Name: "Body", Types: []string{TypeTrigger}},
		{Direction: "right", Name: "Completed", Types: []string{TypeTrigger}},
	}}
}
func (WhileLoopClass) NewNode(_ configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
	if state := firstState(previous); state != nil {
		state.PrimaryType = TypeLoop
		state.Label = loopLabelWaiting
	}
	return &whileLoopNode{}, nil
}

type forLoopConfig struct {
	start int
	end   int
	step  int
}

type forLoopRun struct {
	config    forLoopConfig
	index     int
	iteration uint32
	active    bool
}

type forLoopNode struct {
	pasta.BasicNode

	w  *pasta.Workspace
	id uint64

	triggerIn    uint64
	startIn      uint64
	endIn        uint64
	stepIn       uint64
	loopOut      uint64
	bodyOut      uint64
	indexOut     uint64
	completedOut uint64

	latest    forLoopConfig
	run       forLoopRun
	queued    []forLoopConfig
	loopLink  uint64
	iteration uint32
}

func newForLoopNode() *forLoopNode {
	return &forLoopNode{latest: forLoopConfig{step: 1}}
}

func (n *forLoopNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if n.latest.step == 0 {
		n.latest.step = 1
	}
	n.findPorts(restored)
	if err := n.w.SetNodePrimaryLocked(n.id, TypeLoop); err != nil {
		return err
	}
	return n.setWaiting()
}

func (n *forLoopNode) OnReady() error {
	n.requestInputs()
	return nil
}

func (n *forLoopNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	switch port {
	case n.triggerIn:
		if portDirection == "left" && linkType == TypeTrigger {
			return nil
		}
	case n.startIn, n.endIn, n.stepIn:
		if portDirection != "left" || linkType != TypeInt {
			break
		}
		snapshot, ok := n.w.PortSnapshotLocked(port)
		if ok && len(snapshot.Links) > 0 {
			return pasta.ErrLinkDup
		}
		return nil
	case n.loopOut:
		if portDirection != "right" || linkType != TypeLoop {
			break
		}
		snapshot, ok := n.w.PortSnapshotLocked(port)
		if ok && len(snapshot.Links) > 0 {
			return pasta.ErrLinkDup
		}
		return nil
	case n.bodyOut, n.completedOut:
		if portDirection == "right" && linkType == TypeTrigger {
			return nil
		}
	case n.indexOut:
		if portDirection == "right" && linkType == TypeInt {
			return nil
		}
	}
	return pasta.LinkTypeErr(linkType)
}

func (n *forLoopNode) OnLinkAdd(link, port uint64, linkType, portDirection string) error {
	if port == n.loopOut && linkType == TypeLoop {
		n.loopLink = link
	}
	n.clearLoopState()
	if portDirection == "left" && linkType == TypeInt {
		n.requestLink(link, port)
	}
	return nil
}

func (n *forLoopNode) OnLinkRemoved(link, port uint64, linkType, _ string) error {
	n.clearLoopState()
	if port == n.loopOut && linkType == TypeLoop && link == n.loopLink {
		n.loopLink = 0
	}
	switch port {
	case n.startIn, n.endIn:
		n.setInput(port, 0)
	case n.stepIn:
		n.setInput(port, 1)
	}
	return nil
}

func (n *forLoopNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	switch event.ReceiverPort {
	case n.triggerIn:
		if receiverPortDirection == "left" && linkType == TypeTrigger && !IsRequest(event.Payload) {
			n.triggerLoop()
		}
	case n.startIn, n.endIn, n.stepIn:
		if receiverPortDirection == "left" && linkType == TypeInt {
			if value, ok := parseIntAny(event.Payload); ok {
				n.setInput(event.ReceiverPort, value)
			}
		}
	case n.loopOut:
		if receiverPortDirection == "right" && linkType == TypeLoop {
			n.handleLoopEnd(event)
		}
	case n.indexOut:
		if receiverPortDirection == "right" && linkType == TypeInt && IsRequest(event.Payload) && n.run.active {
			n.w.SendEventLocked(pasta.Event{
				SenderNode:   n.id,
				SenderPort:   n.indexOut,
				ReceiverNode: event.SenderNode,
				ReceiverPort: event.SenderPort,
				Payload:      Int(n.run.index),
			})
		}
	}
	return nil
}

func (n *forLoopNode) OnTrigger() error {
	n.triggerLoop()
	return nil
}

func (n *forLoopNode) findPorts(restored *pasta.NodeInitData) {
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
		case "Start index":
			n.startIn = port
		case "End index":
			n.endIn = port
		case "Step":
			n.stepIn = port
		}
	}
	for _, port := range restored.RightPorts {
		snapshot, ok := n.w.PortSnapshotLocked(port)
		if !ok {
			continue
		}
		switch snapshot.Name {
		case "Loop":
			n.loopOut = port
			n.loopLink = firstLoopLink(n.w, port)
		case "Body":
			n.bodyOut = port
		case "Index":
			n.indexOut = port
		case "Completed":
			n.completedOut = port
		}
	}
}

func (n *forLoopNode) requestInputs() {
	for _, port := range []uint64{n.startIn, n.endIn, n.stepIn} {
		snapshot, ok := n.w.PortSnapshotLocked(port)
		if !ok {
			continue
		}
		for _, link := range snapshot.Links {
			n.requestLink(link, port)
		}
	}
}

func (n *forLoopNode) requestLink(link, port uint64) {
	linkSnapshot, ok := n.w.LinkSnapshotLocked(link)
	if !ok || linkSnapshot.Type != TypeInt {
		return
	}
	receiverNode, receiverPort := otherEndpoint(linkSnapshot, port)
	n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: port, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: RequestValue{}})
}

func (n *forLoopNode) setInput(port uint64, value int) {
	switch port {
	case n.startIn:
		n.latest.start = value
	case n.endIn:
		n.latest.end = value
	case n.stepIn:
		n.latest.step = value
	}
}

func (n *forLoopNode) triggerLoop() {
	if !n.hasLoopLink() {
		return
	}
	config := n.latest
	if n.run.active {
		n.queued = append(n.queued, config)
		return
	}
	n.startConfig(config)
}

func (n *forLoopNode) startConfig(config forLoopConfig) {
	if !n.hasLoopLink() {
		n.clearLoopState()
		return
	}
	if !forLoopHasIteration(config.start, config.end, config.step) {
		n.emitCompleted()
		n.startQueued()
		return
	}
	n.run = forLoopRun{config: config, index: config.start, active: true}
	n.startIteration()
}

func (n *forLoopNode) startIteration() {
	if !n.run.active {
		return
	}
	link := n.validLoopLink()
	if link == 0 {
		n.clearLoopState()
		return
	}
	n.iteration++
	n.run.iteration = n.iteration
	_ = n.setLooping()
	sendToLink(n.w, n.id, n.loopOut, link, LoopStartIteration{Iteration: n.run.iteration})
	sendToPort(n.w, n.id, n.indexOut, Int(n.run.index))
	sendToPort(n.w, n.id, n.bodyOut, Trigger{})
}

func (n *forLoopNode) handleLoopEnd(event pasta.Event) {
	if !n.run.active || event.Link != n.loopLink {
		return
	}
	msg, ok := event.Payload.(LoopEndIteration)
	if !ok || msg.Iteration != n.run.iteration {
		return
	}
	if msg.Break {
		n.finishLoop()
		return
	}
	n.run.index += n.run.config.step
	if !forLoopHasIteration(n.run.index, n.run.config.end, n.run.config.step) {
		n.finishLoop()
		return
	}
	n.startIteration()
}

func (n *forLoopNode) finishLoop() {
	n.run = forLoopRun{}
	_ = n.setWaiting()
	n.emitCompleted()
	n.startQueued()
}

func (n *forLoopNode) startQueued() {
	for len(n.queued) > 0 && !n.run.active {
		config := n.queued[0]
		copy(n.queued, n.queued[1:])
		n.queued[len(n.queued)-1] = forLoopConfig{}
		n.queued = n.queued[:len(n.queued)-1]
		n.startConfig(config)
	}
}

func (n *forLoopNode) clearLoopState() {
	n.run = forLoopRun{}
	n.queued = nil
	_ = n.setWaiting()
}

func (n *forLoopNode) emitCompleted() {
	sendToPort(n.w, n.id, n.completedOut, Trigger{})
}

func (n *forLoopNode) hasLoopLink() bool {
	return n.validLoopLink() != 0
}

func (n *forLoopNode) validLoopLink() uint64 {
	if n.loopLink != 0 {
		if link, ok := n.w.LinkSnapshotLocked(n.loopLink); ok && link.Type == TypeLoop {
			return n.loopLink
		}
		n.loopLink = 0
	}
	n.loopLink = firstLoopLink(n.w, n.loopOut)
	return n.loopLink
}

func (n *forLoopNode) setLooping() error {
	return n.w.SetNodeLabelLocked(n.id, loopLabelLooping)
}

func (n *forLoopNode) setWaiting() error {
	return n.w.SetNodeLabelLocked(n.id, loopLabelWaiting)
}

func forLoopHasIteration(index, end, step int) bool {
	switch {
	case step > 0:
		return index < end
	case step < 0:
		return index > end
	default:
		return false
	}
}

type whileLoopNode struct {
	pasta.BasicNode

	w  *pasta.Workspace
	id uint64

	triggerIn    uint64
	loopOut      uint64
	bodyOut      uint64
	completedOut uint64

	loopLink         uint64
	active           bool
	queued           int
	iteration        uint32
	currentIteration uint32
}

func (n *whileLoopNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	n.findPorts(restored)
	if err := n.w.SetNodePrimaryLocked(n.id, TypeLoop); err != nil {
		return err
	}
	return n.setWaiting()
}

func (n *whileLoopNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	switch port {
	case n.triggerIn:
		if portDirection == "left" && linkType == TypeTrigger {
			return nil
		}
	case n.loopOut:
		if portDirection != "right" || linkType != TypeLoop {
			break
		}
		snapshot, ok := n.w.PortSnapshotLocked(port)
		if ok && len(snapshot.Links) > 0 {
			return pasta.ErrLinkDup
		}
		return nil
	case n.bodyOut, n.completedOut:
		if portDirection == "right" && linkType == TypeTrigger {
			return nil
		}
	}
	return pasta.LinkTypeErr(linkType)
}

func (n *whileLoopNode) OnLinkAdd(link, port uint64, linkType, _ string) error {
	if port == n.loopOut && linkType == TypeLoop {
		n.loopLink = link
	}
	n.clearLoopState()
	return nil
}

func (n *whileLoopNode) OnLinkRemoved(link, port uint64, linkType, _ string) error {
	n.clearLoopState()
	if port == n.loopOut && linkType == TypeLoop && link == n.loopLink {
		n.loopLink = 0
	}
	return nil
}

func (n *whileLoopNode) OnEvent(event pasta.Event, linkType string, _ []string, receiverPortDirection string) error {
	switch event.ReceiverPort {
	case n.triggerIn:
		if receiverPortDirection == "left" && linkType == TypeTrigger && !IsRequest(event.Payload) {
			n.triggerLoop()
		}
	case n.loopOut:
		if receiverPortDirection == "right" && linkType == TypeLoop {
			n.handleLoopEnd(event)
		}
	}
	return nil
}

func (n *whileLoopNode) OnTrigger() error {
	n.triggerLoop()
	return nil
}

func (n *whileLoopNode) findPorts(restored *pasta.NodeInitData) {
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
		case "Loop":
			n.loopOut = port
			n.loopLink = firstLoopLink(n.w, port)
		case "Body":
			n.bodyOut = port
		case "Completed":
			n.completedOut = port
		}
	}
}

func (n *whileLoopNode) triggerLoop() {
	if !n.hasLoopLink() {
		return
	}
	if n.active {
		n.queued++
		return
	}
	n.startIteration()
}

func (n *whileLoopNode) startIteration() {
	link := n.validLoopLink()
	if link == 0 {
		n.clearLoopState()
		return
	}
	n.active = true
	n.iteration++
	n.currentIteration = n.iteration
	_ = n.setLooping()
	sendToLink(n.w, n.id, n.loopOut, link, LoopStartIteration{Iteration: n.currentIteration})
	sendToPort(n.w, n.id, n.bodyOut, Trigger{})
}

func (n *whileLoopNode) handleLoopEnd(event pasta.Event) {
	if !n.active || event.Link != n.loopLink {
		return
	}
	msg, ok := event.Payload.(LoopEndIteration)
	if !ok || msg.Iteration != n.currentIteration {
		return
	}
	if msg.Break {
		n.finishLoop()
		return
	}
	n.startIteration()
}

func (n *whileLoopNode) finishLoop() {
	n.active = false
	_ = n.setWaiting()
	sendToPort(n.w, n.id, n.completedOut, Trigger{})
	n.startQueued()
}

func (n *whileLoopNode) startQueued() {
	if n.queued < 1 || n.active {
		return
	}
	if !n.hasLoopLink() {
		n.clearLoopState()
		return
	}
	n.queued--
	n.startIteration()
}

func (n *whileLoopNode) clearLoopState() {
	n.active = false
	n.queued = 0
	_ = n.setWaiting()
}

func (n *whileLoopNode) hasLoopLink() bool {
	return n.validLoopLink() != 0
}

func (n *whileLoopNode) validLoopLink() uint64 {
	if n.loopLink != 0 {
		if link, ok := n.w.LinkSnapshotLocked(n.loopLink); ok && link.Type == TypeLoop {
			return n.loopLink
		}
		n.loopLink = 0
	}
	n.loopLink = firstLoopLink(n.w, n.loopOut)
	return n.loopLink
}

func (n *whileLoopNode) setLooping() error {
	return n.w.SetNodeLabelLocked(n.id, loopLabelLooping)
}

func (n *whileLoopNode) setWaiting() error {
	return n.w.SetNodeLabelLocked(n.id, loopLabelWaiting)
}

func firstLoopLink(w *pasta.Workspace, port uint64) uint64 {
	if port == 0 {
		return 0
	}
	snapshot, ok := w.PortSnapshotLocked(port)
	if !ok {
		return 0
	}
	for _, linkID := range snapshot.Links {
		link, ok := w.LinkSnapshotLocked(linkID)
		if ok && link.Type == TypeLoop {
			return linkID
		}
	}
	return 0
}

func sendToPort(w *pasta.Workspace, node, port uint64, payload any) {
	snapshot, ok := w.PortSnapshotLocked(port)
	if !ok {
		return
	}
	for _, link := range snapshot.Links {
		sendToLink(w, node, port, link, payload)
	}
}

func sendToLink(w *pasta.Workspace, node, port, link uint64, payload any) {
	snapshot, ok := w.LinkSnapshotLocked(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, port)
	w.SendEventLocked(pasta.Event{
		SenderNode:   node,
		SenderPort:   port,
		ReceiverNode: receiverNode,
		ReceiverPort: receiverPort,
		Payload:      payload,
	})
}
