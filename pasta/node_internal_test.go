package pasta

import (
	"errors"
	"testing"
)

type internalNode struct {
	panicOn map[string]bool
	calls   []string
}

func (n *internalNode) call(name string) {
	if n.panicOn[name] {
		panic(name)
	}
	n.calls = append(n.calls, name)
}

func (n *internalNode) OnInit(w *Workspace, l Logger, id uint64, class string) error {
	n.call("init")
	return nil
}

func (n *internalNode) OnReady() error {
	n.call("ready")
	return nil
}

func (n *internalNode) OnStop() {
	n.call("stop")
}

func (n *internalNode) OnPortAdd(port uint64, direction string, types []string) error {
	n.call("port-add")
	return nil
}

func (n *internalNode) OnPortRemoved(port uint64, direction string) error {
	n.call("port-removed")
	return nil
}

func (n *internalNode) PreLinkAdd(port uint64, linkType, portDirection string) error {
	n.call("pre-link-add")
	return nil
}

func (n *internalNode) OnLinkAdd(link, port uint64, linkType, portDirection string) error {
	n.call("link-add")
	return nil
}

func (n *internalNode) OnLinkRemoved(link, port uint64, linkType, portDirection string) error {
	n.call("link-removed")
	return nil
}

func (n *internalNode) OnEvent(event Event, linkType string, receiverPortTypes []string, receiverPortDirection string) error {
	n.call("event")
	return nil
}

func TestNodeRecordRemovePortAndPorts(t *testing.T) {
	record := nodeRecord{
		LeftPorts:  []uint64{1, 2, 3},
		RightPorts: []uint64{4, 2},
	}

	record.RemovePort(2)

	var got []uint64
	for port := range record.Ports() {
		got = append(got, port)
	}

	want := []uint64{1, 3, 4}
	if !sameUint64s(got, want) {
		t.Fatalf("ports = %v, want %v", got, want)
	}
}

func TestNodeRecordPanicStopsNode(t *testing.T) {
	node := &internalNode{panicOn: map[string]bool{"ready": true}}
	record := nodeRecord{Node: node}

	err := record.OnReady()
	if !errors.Is(err, ErrNodePanic) {
		t.Fatalf("OnReady error = %v, want %v", err, ErrNodePanic)
	}
	if !record.stopped {
		t.Fatal("record was not stopped after panic")
	}

	if err := record.OnPortAdd(1, "left", []string{"example.com/typeA"}); err != nil {
		t.Fatalf("OnPortAdd after stopped node = %v, want nil", err)
	}
	if len(node.calls) != 0 {
		t.Fatalf("stopped node received calls: %v", node.calls)
	}
}

func TestNodeStopIgnoresPanic(t *testing.T) {
	nodeStop(&internalNode{panicOn: map[string]bool{"stop": true}})
}

func TestMultiSliceIterStopsWhenYieldReturnsFalse(t *testing.T) {
	var got []int
	for n := range multiSLiceIter([]int{1, 2}, []int{3, 4}) {
		got = append(got, n)
		if n == 3 {
			break
		}
	}

	want := []int{1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("iterated values = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("iterated values = %v, want %v", got, want)
		}
	}
}

func sameUint64s(a, b []uint64) bool {
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
