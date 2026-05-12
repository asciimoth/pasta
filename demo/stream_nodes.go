package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/asciimoth/pasta/pasta"
)

const (
	StreamLibraryName = "stream.pasta.demo"

	StreamProviderClass  = StreamLibraryName + "/Provider"
	StreamPrefixClass    = StreamLibraryName + "/Prefix"
	StreamUppercaseClass = StreamLibraryName + "/Uppercase"
	StreamSinkClass      = StreamLibraryName + "/Sink"

	StreamType = StreamLibraryName + "/stream"
)

var (
	StreamInput  = pasta.PortID{Number: 1, Kind: pasta.InputPort}
	StreamOutput = pasta.PortID{Number: 1, Kind: pasta.OutputPort}
)

type streamReadFunc func(context.Context) (string, bool)

type streamWire struct {
	read streamReadFunc
}

func newStreamWire(read streamReadFunc) *streamWire {
	return &streamWire{read: read}
}

func (w *streamWire) Read(ctx context.Context) (string, bool) {
	if w == nil || w.read == nil {
		<-ctx.Done()
		return "", false
	}
	return w.read(ctx)
}

type StreamLibrary struct{}

func (StreamLibrary) Name() string { return StreamLibraryName }

func (StreamLibrary) DefineClasses(scope pasta.LibraryScope) error {
	for _, class := range StreamClasses() {
		if err := scope.DefineClass(class); err != nil {
			return err
		}
	}
	return nil
}

func StreamClasses() []pasta.ClassSpec {
	return []pasta.ClassSpec{
		{
			Name:        StreamSinkClass,
			DisplayName: "Stream Sink",
			Description: "Output-side consumer that pulls stream chunks from nodes to its right.",
			Default:     streamDefault("Stream Sink", streamState{Value: "waiting"}),
			Outputs:     []pasta.PortSpec{streamOutput(StreamOutput, "pull")},
			Runtime:     streamNodeClass{kind: "sink"},
		},
		{
			Name:        StreamUppercaseClass,
			DisplayName: "Stream Uppercase",
			Description: "Output-side processor that pulls stream chunks and converts them to upper case.",
			Default:     streamDefault("Stream Uppercase", streamState{Value: "waiting"}),
			Inputs:      []pasta.PortSpec{streamInput(StreamInput, "source")},
			Outputs:     []pasta.PortSpec{streamOutput(StreamOutput, "pull")},
			Runtime:     streamNodeClass{kind: "uppercase"},
		},
		{
			Name:        StreamPrefixClass,
			DisplayName: "Stream Prefix",
			Description: "Output-side processor that pulls stream chunks and prepends configured text.",
			Default:     streamDefault("Stream Prefix", streamState{Prefix: "processed: ", Value: "waiting"}),
			Inputs:      []pasta.PortSpec{streamInput(StreamInput, "source")},
			Outputs:     []pasta.PortSpec{streamOutput(StreamOutput, "pull")},
			Runtime:     streamNodeClass{kind: "prefix"},
		},
		{
			Name:        StreamProviderClass,
			DisplayName: "Stream Provider",
			Description: "Input-side provider that supplies timed stream chunks to output-side pullers.",
			Default:     streamDefault("Stream Provider", streamState{IntervalSeconds: 1, Value: "waiting"}),
			Inputs:      []pasta.PortSpec{streamInput(StreamInput, "read")},
			Runtime:     streamNodeClass{kind: "provider"},
		},
	}
}

func streamDefault(display string, state streamState) pasta.NodeState {
	return pasta.NodeState{
		DisplayName: display,
		PrimaryType: StreamType,
		Private:     state,
		Metadata:    map[string]string{"palette": "streams"},
	}
}

func streamInput(id pasta.PortID, name string) pasta.PortSpec {
	return pasta.PortSpec{
		ID:        id,
		Name:      name,
		Direction: pasta.InputPort,
		FixedType: StreamType,
		Metadata:  map[string]string{"label": name},
	}
}

func streamOutput(id pasta.PortID, name string) pasta.PortSpec {
	return pasta.PortSpec{
		ID:        id,
		Name:      name,
		Direction: pasta.OutputPort,
		FixedType: StreamType,
		Metadata:  map[string]string{"label": name},
	}
}

type streamNodeClass struct {
	kind string
}

func (c streamNodeClass) InitNode(ctx pasta.NodeContext, state pasta.NodeState, _ pasta.InitMode) (pasta.NodeRuntime, error) {
	runCtx, cancel := context.WithCancel(context.Background())
	node := &streamNode{
		ctx:         ctx,
		runCtx:      runCtx,
		ctxCancel:   cancel,
		kind:        c.kind,
		state:       streamStateFromAny(state.Private),
		sources:     make(map[pasta.LinkID]*streamSource),
		pullCancels: make(map[pasta.LinkID]context.CancelFunc),
	}
	if node.state.IntervalSeconds <= 0 {
		node.state.IntervalSeconds = 1
	}
	if err := ctx.Node.SetMenu(node.menu()); err != nil {
		cancel()
		return nil, err
	}
	return node, nil
}

type streamSource struct {
	ctx    context.Context
	cancel context.CancelFunc
	wire   *streamWire
}

type streamNode struct {
	mu          sync.RWMutex
	ctx         pasta.NodeContext
	runCtx      context.Context
	ctxCancel   context.CancelFunc
	kind        string
	state       streamState
	msgs        []pasta.MessageID
	sources     map[pasta.LinkID]*streamSource
	pullCancels map[pasta.LinkID]context.CancelFunc
}

type streamState struct {
	Value           string  `json:"value"`
	Prefix          string  `json:"prefix,omitempty"`
	IntervalSeconds float64 `json:"intervalSeconds,omitempty"`
	Count           int64   `json:"count,omitempty"`
}

func (n *streamNode) LinkObject(endpoint pasta.LinkEndpoint) (any, error) {
	if endpoint.Direction != pasta.InputPort {
		return nil, nil
	}
	return newStreamWire(n.read), nil
}

func (n *streamNode) BeforeLinkAttach(_ pasta.LinkEndpoint, object any) error {
	if _, ok := object.(*streamWire); !ok {
		return fmt.Errorf("stream link object has type %T, want *streamWire", object)
	}
	return nil
}

func (n *streamNode) AfterLinkAttach(endpoint pasta.LinkEndpoint, object any) {
	if endpoint.Direction != pasta.OutputPort {
		return
	}
	wire, ok := object.(*streamWire)
	if !ok {
		return
	}
	if n.kind == "sink" {
		ctx, cancel := context.WithCancel(n.runCtx)
		n.mu.Lock()
		n.pullCancels[endpoint.Link] = cancel
		n.mu.Unlock()
		go n.pullSink(ctx, endpoint.Link, wire)
		return
	}
	ctx, cancel := context.WithCancel(n.runCtx)
	n.mu.Lock()
	n.sources[endpoint.Link] = &streamSource{ctx: ctx, cancel: cancel, wire: wire}
	n.mu.Unlock()
}

func (n *streamNode) BeforeLinkDetach(pasta.LinkEndpoint) error { return nil }

func (n *streamNode) AfterLinkDetach(endpoint pasta.LinkEndpoint) {
	n.closeLink(endpoint.Link)
	if endpoint.Direction == pasta.OutputPort && n.kind != "sink" {
		n.setValue("waiting")
	}
}

func (n *streamNode) AfterLinkInactive(endpoint pasta.LinkEndpoint, _ pasta.InactiveReason) {
	n.closeLink(endpoint.Link)
}

func (n *streamNode) BeforeInactive(pasta.InactiveReason) error { return nil }

func (n *streamNode) AfterInactive(pasta.InactiveReason) {
	n.closeAll()
}

func (n *streamNode) BeforeDelete() error { return nil }

func (n *streamNode) AfterDelete() {
	n.closeAll()
}

func (n *streamNode) Close() error {
	n.closeAll()
	return nil
}

func (n *streamNode) ApplyMenuUpdate(update pasta.MenuStateUpdate) (pasta.MenuStateUpdate, error) {
	n.mu.RLock()
	state := n.state
	kind := n.kind
	n.mu.RUnlock()

	for _, field := range update.Fields {
		if field.Block != "main" {
			continue
		}
		switch field.Field {
		case "intervalSeconds":
			if kind == "provider" {
				state.IntervalSeconds = positiveFloatFromAny(field.Value, 1)
			}
		case "prefix":
			if kind == "prefix" {
				state.Prefix = stringFromAny(field.Value)
			}
		}
	}
	n.mu.Lock()
	n.state.IntervalSeconds = state.IntervalSeconds
	n.state.Prefix = state.Prefix
	n.mu.Unlock()
	n.updateMenu()
	return update, nil
}

func (n *streamNode) TriggerMenuButton(ref pasta.MenuButtonRef) error {
	if ref.Block != "main" {
		return nil
	}
	switch ref.Button {
	case "messages-clear":
		return n.clearMessages()
	default:
		return nil
	}
}

func (n *streamNode) ImportPrivateState(private any) error {
	n.mu.Lock()
	n.state = streamStateFromAny(private)
	if n.state.IntervalSeconds <= 0 {
		n.state.IntervalSeconds = 1
	}
	n.mu.Unlock()
	n.updateMenu()
	return nil
}

func (n *streamNode) ExportPrivateState() (any, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.state, nil
}

func (n *streamNode) pullSink(ctx context.Context, link pasta.LinkID, wire *streamWire) {
	for {
		value, ok := wire.Read(ctx)
		if !ok {
			return
		}
		n.observe(value)
		_ = link
	}
}

func (n *streamNode) read(ctx context.Context) (string, bool) {
	switch n.kind {
	case "provider":
		return n.readProvider(ctx)
	case "prefix", "uppercase":
		value, ok := n.readSource(ctx)
		if !ok {
			return "", false
		}
		return n.process(value), true
	default:
		<-ctx.Done()
		return "", false
	}
}

func (n *streamNode) readProvider(ctx context.Context) (string, bool) {
	n.mu.RLock()
	interval := n.state.IntervalSeconds
	n.mu.RUnlock()
	if interval <= 0 {
		interval = 1
	}
	timer := time.NewTimer(durationSeconds(interval))
	defer timer.Stop()
	select {
	case <-timer.C:
		return n.nextProviderValue(), true
	case <-ctx.Done():
		return "", false
	case <-n.runCtx.Done():
		return "", false
	}
}

func (n *streamNode) readSource(ctx context.Context) (string, bool) {
	source := n.firstSource()
	if source == nil {
		<-ctx.Done()
		return "", false
	}
	readCtx, cancel := context.WithCancel(source.ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			cancel()
		case <-done:
		}
	}()
	value, ok := source.wire.Read(readCtx)
	close(done)
	return value, ok
}

func (n *streamNode) firstSource() *streamSource {
	n.mu.RLock()
	defer n.mu.RUnlock()
	for _, source := range n.sources {
		return source
	}
	return nil
}

func (n *streamNode) process(value string) string {
	switch n.kind {
	case "prefix":
		n.mu.RLock()
		prefix := n.state.Prefix
		n.mu.RUnlock()
		value = prefix + value
	case "uppercase":
		value = strings.ToUpper(value)
	}
	n.observe(value)
	return value
}

func (n *streamNode) nextProviderValue() string {
	n.mu.Lock()
	n.state.Count++
	count := n.state.Count
	value := fmt.Sprintf("chunk-%03d", count)
	n.state.Value = value
	state := n.state
	n.mu.Unlock()
	_ = n.ctx.Node.SetPrivate(state)
	n.updateMenu()
	return value
}

func (n *streamNode) observe(value string) {
	n.mu.Lock()
	n.state.Count++
	n.state.Value = value
	state := n.state
	n.mu.Unlock()
	_ = n.ctx.Node.SetPrivate(state)
	n.updateMenu()
}

func (n *streamNode) setValue(value string) {
	n.mu.Lock()
	n.state.Value = value
	state := n.state
	n.mu.Unlock()
	_ = n.ctx.Node.SetPrivate(state)
	n.updateMenu()
}

func (n *streamNode) closeLink(link pasta.LinkID) {
	n.mu.Lock()
	source := n.sources[link]
	delete(n.sources, link)
	cancel := n.pullCancels[link]
	delete(n.pullCancels, link)
	n.mu.Unlock()
	if source != nil {
		source.cancel()
	}
	if cancel != nil {
		cancel()
	}
}

func (n *streamNode) closeAll() {
	n.ctxCancel()
	n.mu.Lock()
	sources := n.sources
	cancels := n.pullCancels
	n.sources = make(map[pasta.LinkID]*streamSource)
	n.pullCancels = make(map[pasta.LinkID]context.CancelFunc)
	n.mu.Unlock()
	for _, source := range sources {
		source.cancel()
	}
	for _, cancel := range cancels {
		cancel()
	}
}

func (n *streamNode) clearMessages() error {
	n.mu.Lock()
	ids := append([]pasta.MessageID(nil), n.msgs...)
	n.msgs = nil
	n.mu.Unlock()
	for _, id := range ids {
		if err := n.ctx.Node.RemoveMessage(id); err != nil && !errors.Is(err, pasta.ErrNotFound) {
			return err
		}
	}
	return nil
}

func (n *streamNode) updateMenu() {
	_ = n.ctx.Node.SetMenu(n.menu())
}

func (n *streamNode) menu() pasta.NodeMenu {
	n.mu.RLock()
	kind := n.kind
	state := n.state
	n.mu.RUnlock()

	fields := []pasta.MenuField{
		{ID: "value", Label: "Latest", Kind: pasta.MenuFieldReadOnly, ReadOnly: true, Value: state.Value},
		{ID: "count", Label: "Count", Kind: pasta.MenuFieldReadOnly, ReadOnly: true, Value: state.Count},
	}
	switch kind {
	case "provider":
		fields = append([]pasta.MenuField{
			{ID: "intervalSeconds", Label: "Interval Seconds", Kind: pasta.MenuFieldFloat64, Value: state.IntervalSeconds},
		}, fields...)
	case "prefix":
		fields = append([]pasta.MenuField{
			{ID: "prefix", Label: "Prefix", Kind: pasta.MenuFieldString, Value: state.Prefix},
		}, fields...)
	}
	return pasta.NodeMenu{Blocks: []pasta.MenuBlock{{
		ID:      "main",
		Title:   "Stream",
		Fields:  fields,
		Buttons: []pasta.MenuButton{{ID: "messages-clear", Label: "Clear Messages"}},
	}}}
}

func streamStateFromAny(v any) streamState {
	switch x := v.(type) {
	case streamState:
		return x
	case map[string]any:
		return streamState{
			Value:           stringFromAny(x["value"]),
			Prefix:          stringFromAny(x["prefix"]),
			IntervalSeconds: positiveFloatFromAny(x["intervalSeconds"], 0),
			Count:           int64(positiveFloatFromAny(x["count"], 0)),
		}
	case map[string]string:
		return streamState{
			Value:           x["value"],
			Prefix:          x["prefix"],
			IntervalSeconds: positiveFloatFromAny(x["intervalSeconds"], 0),
			Count:           int64(positiveFloatFromAny(x["count"], 0)),
		}
	case json.RawMessage:
		var state streamState
		_ = json.Unmarshal(x, &state)
		return state
	default:
		return streamState{}
	}
}

func streamTextValue(v any) (string, bool) {
	state := streamStateFromAny(v)
	if state == (streamState{}) {
		return "", false
	}
	return state.Value, true
}

func positiveFloatFromAny(v any, fallback float64) float64 {
	value := streamFloatFromAny(v)
	if value <= 0 {
		return fallback
	}
	return value
}

func streamFloatFromAny(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case int32:
		return float64(x)
	case uint:
		return float64(x)
	case uint64:
		return float64(x)
	case uint32:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	case string:
		var f float64
		_, _ = fmt.Sscanf(x, "%f", &f)
		return f
	default:
		return 0
	}
}

func durationSeconds(seconds float64) time.Duration {
	if seconds < 0.05 {
		seconds = 0.05
	}
	return time.Duration(seconds * float64(time.Second))
}
