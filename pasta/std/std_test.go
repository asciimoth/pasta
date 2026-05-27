// nolint
package std

import (
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

type testLogFactory struct{}
type testLogger struct{}

func (testLogFactory) WorkspaceLogger() pasta.Logger          { return testLogger{} }
func (testLogFactory) NodeLogger(uint64, string) pasta.Logger { return testLogger{} }
func (testLogger) Debug(...any)                               {}
func (testLogger) Debugf(string, ...any)                      {}
func (testLogger) Info(...any)                                {}
func (testLogger) Infof(string, ...any)                       {}
func (testLogger) Warn(...any)                                {}
func (testLogger) Warnf(string, ...any)                       {}
func (testLogger) Err(...any)                                 {}
func (testLogger) Errf(string, ...any)                        {}
func (testLogger) Fatal(...any)                               { panic("fatal log") }
func (testLogger) Fatalf(string, ...any)                      { panic("fatal log") }

func TestStdMathChoosesPrimaryTypeConvertsAndUpdatesMenus(t *testing.T) {
	w := newStdWorkspace(t)
	i10 := addByClass(t, w, NodeTypeIntConstant, "i10")
	f25 := addByClass(t, w, NodeTypeFloatConstant, "f25")
	sum := addByClass(t, w, NodeTypeSum, "sum")
	setConstant(t, w, i10, 10)
	setConstant(t, w, f25, 2.5)

	linkByPortName(t, w, i10, "output", sum, "input 1")
	linkByPortName(t, w, f25, "output", sum, "input 2")

	snapshot := w.Snapshot()
	if got := snapshot.Nodes[sum].PrimaryType; got != TypeInt {
		t.Fatalf("sum primary type = %q, want %q", got, TypeInt)
	}
	if got := snapshot.Nodes[sum].Label; got != "12" {
		t.Fatalf("sum label = %q, want 12", got)
	}
	out := portByName(t, snapshot, sum, "right", "output")
	if got := snapshot.Ports[out].Types; !reflect.DeepEqual(got, []string{TypeInt}) {
		t.Fatalf("sum output types = %#v, want pasta/int", got)
	}
	assertMenuValue(t, w, sum, 12, true)

	setConstant(t, w, i10, 20)
	if got := w.Snapshot().Nodes[sum].Label; got != "22" {
		t.Fatalf("sum label after constant edit = %q, want 22", got)
	}
	assertMenuValue(t, w, i10, 20, false)
}

func TestStdMathAnyOutputCanBeLinkedBeforeTypeDecision(t *testing.T) {
	w := newStdWorkspace(t)
	a := addByClass(t, w, NodeTypeIntConstant, "a")
	b := addByClass(t, w, NodeTypeIntConstant, "b")
	sum := addByClass(t, w, NodeTypeSum, "sum")
	sub := addByClass(t, w, NodeTypeSub, "sub")
	setConstant(t, w, a, 6)
	setConstant(t, w, b, 2)

	staged := linkByPortName(t, w, sum, "output", sub, "input 1")
	if got := w.Snapshot().Links[staged].Type; got != pasta.AnyType {
		t.Fatalf("staged math link type = %q, want any/any", got)
	}
	linkByPortName(t, w, a, "output", sum, "input 1")
	linkByPortName(t, w, b, "output", sum, "input 2")

	snapshot := w.Snapshot()
	if got := snapshot.Links[staged].Type; got != TypeInt {
		t.Fatalf("retagged math link type = %q, want pasta/int", got)
	}
	if got := snapshot.Nodes[sub].PrimaryType; got != TypeInt {
		t.Fatalf("downstream primary = %q, want pasta/int", got)
	}
	if got := snapshot.Nodes[sub].Label; got != "8" {
		t.Fatalf("downstream label = %q, want 8", got)
	}
}

func TestStdMathAttachDetachReattachAndDivisionByZero(t *testing.T) {
	w := newStdWorkspace(t)
	a := addByClass(t, w, NodeTypeIntConstant, "a")
	b := addByClass(t, w, NodeTypeIntConstant, "b")
	div := addByClass(t, w, NodeTypeDiv, "div")
	setConstant(t, w, a, 9)
	setConstant(t, w, b, 3)

	linkA := linkByPortName(t, w, a, "output", div, "input 1")
	linkByPortName(t, w, b, "output", div, "input 2")
	if got := w.Snapshot().Nodes[div].Label; got != "3" {
		t.Fatalf("div label = %q, want 3", got)
	}

	setConstant(t, w, b, 0)
	if got := w.Snapshot().Nodes[div].Label; got != "0" {
		t.Fatalf("div by zero label = %q, want 0", got)
	}

	w.RemoveLink(linkA)
	if got := w.Snapshot().Nodes[div].Label; got != "0" {
		t.Fatalf("div label after detach = %q, want 0", got)
	}
	linkByPortName(t, w, a, "output", div, "input 1")
	setConstant(t, w, b, 3)
	if got := w.Snapshot().Nodes[div].Label; got != "3" {
		t.Fatalf("div label after reattach = %q, want 3", got)
	}
}

func TestStdVariadicInputsAreBalancedAndSorted(t *testing.T) {
	w := newStdWorkspace(t)
	a := addByClass(t, w, NodeTypeIntConstant, "a")
	b := addByClass(t, w, NodeTypeIntConstant, "b")
	sum := addByClass(t, w, NodeTypeSum, "sum")
	setConstant(t, w, a, 1)
	setConstant(t, w, b, 2)

	assertLeftPortNames(t, w, sum, []string{"input 1"})
	link1 := linkByPortName(t, w, a, "output", sum, "input 1")
	assertLeftPortNames(t, w, sum, []string{"input 1", "input 2"})
	linkByPortName(t, w, b, "output", sum, "input 2")
	assertLeftPortNames(t, w, sum, []string{"input 1", "input 2", "input 3"})
	if got := w.Snapshot().Nodes[sum].Label; got != "3" {
		t.Fatalf("sum label = %q, want 3", got)
	}
	w.RemoveLink(link1)
	assertLeftPortNames(t, w, sum, []string{"input 1", "input 2"})
	if got := w.Snapshot().Nodes[sum].Label; got != "2" {
		t.Fatalf("sum label after detach = %q, want 2", got)
	}
}

func TestStdComplexFanoutMixedTypeGraphStepByStep(t *testing.T) {
	w := newStdWorkspace(t)
	nodes := map[string]uint64{
		"i2":       addByClass(t, w, NodeTypeIntConstant, "i2"),
		"i3":       addByClass(t, w, NodeTypeIntConstant, "i3"),
		"i4":       addByClass(t, w, NodeTypeIntConstant, "i4"),
		"f15":      addByClass(t, w, NodeTypeFloatConstant, "f15"),
		"f25":      addByClass(t, w, NodeTypeFloatConstant, "f25"),
		"sumInt":   addByClass(t, w, NodeTypeSum, "sumInt"),
		"sumFloat": addByClass(t, w, NodeTypeSum, "sumFloat"),
		"mulInt":   addByClass(t, w, NodeTypeMul, "mulInt"),
		"subMixed": addByClass(t, w, NodeTypeSub, "subMixed"),
		"divMixed": addByClass(t, w, NodeTypeDiv, "divMixed"),
	}
	menus := subscribeStdMenus(t, w, nodes)
	setConstant(t, w, nodes["i2"], 2)
	setConstant(t, w, nodes["i3"], 3)
	setConstant(t, w, nodes["i4"], 4)
	setConstant(t, w, nodes["f15"], 1.5)
	setConstant(t, w, nodes["f25"], 2.5)

	expectStdGraph(t, w, menus, nodes, stdGraphExpect{
		labels: map[string]string{
			"i2": "2", "i3": "3", "i4": "4", "f15": "1.5", "f25": "2.5",
			"sumInt": "0", "sumFloat": "0", "mulInt": "0", "subMixed": "0", "divMixed": "0",
		},
		menuValues: map[string]float64{
			"i2": 2, "i3": 3, "i4": 4, "f15": 1.5, "f25": 2.5,
			"sumInt": 0, "sumFloat": 0, "mulInt": 0, "subMixed": 0, "divMixed": 0,
		},
		leftPorts: map[string][]string{
			"sumInt": {"input 1"}, "sumFloat": {"input 1"}, "mulInt": {"input 1"},
			"subMixed": {"input 1", "input 2"}, "divMixed": {"input 1", "input 2"},
		},
		rightLinks: map[string]int{"i2": 0, "i3": 0, "i4": 0, "f15": 0, "f25": 0},
	})

	linkByPortName(t, w, nodes["i2"], "output", nodes["sumInt"], "input 1")
	expectStdGraph(t, w, menus, nodes, stdGraphExpect{
		labels:     map[string]string{"sumInt": "2"},
		menuValues: map[string]float64{"sumInt": 2},
		primary:    map[string]string{"sumInt": TypeInt},
		leftPorts:  map[string][]string{"sumInt": {"input 1", "input 2"}},
		rightLinks: map[string]int{"i2": 1},
	})

	linkByPortName(t, w, nodes["i3"], "output", nodes["sumInt"], "input 2")
	expectStdGraph(t, w, menus, nodes, stdGraphExpect{
		labels:     map[string]string{"sumInt": "5"},
		menuValues: map[string]float64{"sumInt": 5},
		leftPorts:  map[string][]string{"sumInt": {"input 1", "input 2", "input 3"}},
		rightLinks: map[string]int{"i3": 1},
	})

	linkByPortName(t, w, nodes["i4"], "output", nodes["sumInt"], "input 3")
	expectStdGraph(t, w, menus, nodes, stdGraphExpect{
		labels:     map[string]string{"sumInt": "9"},
		menuValues: map[string]float64{"sumInt": 9},
		leftPorts:  map[string][]string{"sumInt": {"input 1", "input 2", "input 3", "input 4"}},
		rightLinks: map[string]int{"i4": 1},
	})

	linkByPortName(t, w, nodes["i2"], "output", nodes["mulInt"], "input 1")
	expectStdGraph(t, w, menus, nodes, stdGraphExpect{
		labels:     map[string]string{"mulInt": "0"},
		menuValues: map[string]float64{"mulInt": 0},
		primary:    map[string]string{"mulInt": TypeInt},
		leftPorts:  map[string][]string{"mulInt": {"input 1", "input 2"}},
		rightLinks: map[string]int{"i2": 2},
	})

	linkByPortName(t, w, nodes["i3"], "output", nodes["mulInt"], "input 2")
	expectStdGraph(t, w, menus, nodes, stdGraphExpect{
		labels:     map[string]string{"mulInt": "0"},
		menuValues: map[string]float64{"mulInt": 0},
		leftPorts:  map[string][]string{"mulInt": {"input 1", "input 2", "input 3"}},
		rightLinks: map[string]int{"i3": 2},
	})

	linkByPortName(t, w, nodes["f15"], "output", nodes["sumFloat"], "input 1")
	expectStdGraph(t, w, menus, nodes, stdGraphExpect{
		labels:     map[string]string{"sumFloat": "1.5"},
		menuValues: map[string]float64{"sumFloat": 1.5},
		primary:    map[string]string{"sumFloat": TypeFloat},
		leftPorts:  map[string][]string{"sumFloat": {"input 1", "input 2"}},
		rightLinks: map[string]int{"f15": 1},
	})

	linkByPortName(t, w, nodes["i2"], "output", nodes["sumFloat"], "input 2")
	expectStdGraph(t, w, menus, nodes, stdGraphExpect{
		labels:     map[string]string{"sumFloat": "3.5"},
		menuValues: map[string]float64{"sumFloat": 3.5},
		leftPorts:  map[string][]string{"sumFloat": {"input 1", "input 2", "input 3"}},
		rightLinks: map[string]int{"i2": 3},
	})

	linkByPortName(t, w, nodes["f25"], "output", nodes["sumFloat"], "input 3")
	expectStdGraph(t, w, menus, nodes, stdGraphExpect{
		labels:     map[string]string{"sumFloat": "6"},
		menuValues: map[string]float64{"sumFloat": 6},
		leftPorts:  map[string][]string{"sumFloat": {"input 1", "input 2", "input 3", "input 4"}},
		rightLinks: map[string]int{"f25": 1},
	})

	linkByPortName(t, w, nodes["i3"], "output", nodes["subMixed"], "input 1")
	expectStdGraph(t, w, menus, nodes, stdGraphExpect{
		labels:     map[string]string{"subMixed": "3"},
		menuValues: map[string]float64{"subMixed": 3},
		primary:    map[string]string{"subMixed": TypeInt},
		rightLinks: map[string]int{"i3": 3},
	})

	linkByPortName(t, w, nodes["f15"], "output", nodes["subMixed"], "input 2")
	expectStdGraph(t, w, menus, nodes, stdGraphExpect{
		labels:     map[string]string{"subMixed": "2"},
		menuValues: map[string]float64{"subMixed": 2},
		rightLinks: map[string]int{"f15": 2},
	})

	linkByPortName(t, w, nodes["f25"], "output", nodes["divMixed"], "input 1")
	expectStdGraph(t, w, menus, nodes, stdGraphExpect{
		labels:     map[string]string{"divMixed": "0"},
		menuValues: map[string]float64{"divMixed": 0},
		primary:    map[string]string{"divMixed": TypeFloat},
		rightLinks: map[string]int{"f25": 2},
	})

	linkByPortName(t, w, nodes["i2"], "output", nodes["divMixed"], "input 2")
	expectStdGraph(t, w, menus, nodes, stdGraphExpect{
		labels:     map[string]string{"divMixed": "1.25"},
		menuValues: map[string]float64{"divMixed": 1.25},
		rightLinks: map[string]int{"i2": 4},
	})

	setConstant(t, w, nodes["i2"], 4)
	expectStdGraph(t, w, menus, nodes, stdGraphExpect{
		labels: map[string]string{
			"i2": "4", "sumInt": "11", "sumFloat": "8", "mulInt": "0", "divMixed": "0.625",
		},
		menuValues: map[string]float64{
			"i2": 4, "sumInt": 11, "sumFloat": 8, "mulInt": 0, "divMixed": 0.625,
		},
		rightLinks: map[string]int{"i2": 4},
	})
}

func TestStdBoolNodesPropagateFanoutAndMenus(t *testing.T) {
	w := newStdWorkspace(t)
	nodes := map[string]uint64{
		"true":  addByClass(t, w, NodeTypeTrueConstant, "true"),
		"false": addByClass(t, w, NodeTypeFalseConstant, "false"),
		"and":   addByClass(t, w, NodeTypeBoolAnd, "and"),
		"or":    addByClass(t, w, NodeTypeBoolOr, "or"),
		"not":   addByClass(t, w, NodeTypeBoolNot, "not"),
	}
	menus := subscribeStdMenus(t, w, nodes)
	expectStdBoolGraph(t, w, menus, nodes, stdBoolExpect{
		labels: map[string]string{"true": "true", "false": "false", "and": "false", "or": "false", "not": "true"},
		menus:  map[string]bool{"true": true, "false": false, "and": false, "or": false, "not": true},
		primary: map[string]string{
			"true": TypeBool, "false": TypeBool, "and": TypeBool, "or": TypeBool, "not": TypeBool,
		},
		rightLinks: map[string]int{"true": 0, "false": 0},
	})

	linkByPortName(t, w, nodes["true"], "output", nodes["and"], "input 1")
	expectStdBoolGraph(t, w, menus, nodes, stdBoolExpect{
		labels:     map[string]string{"and": "false"},
		menus:      map[string]bool{"and": false},
		rightLinks: map[string]int{"true": 1},
	})

	linkByPortName(t, w, nodes["true"], "output", nodes["or"], "input 1")
	expectStdBoolGraph(t, w, menus, nodes, stdBoolExpect{
		labels:     map[string]string{"or": "true"},
		menus:      map[string]bool{"or": true},
		rightLinks: map[string]int{"true": 2},
	})

	linkByPortName(t, w, nodes["true"], "output", nodes["not"], "input 1")
	expectStdBoolGraph(t, w, menus, nodes, stdBoolExpect{
		labels:     map[string]string{"not": "false"},
		menus:      map[string]bool{"not": false},
		rightLinks: map[string]int{"true": 3},
	})

	linkByPortName(t, w, nodes["false"], "output", nodes["and"], "input 2")
	expectStdBoolGraph(t, w, menus, nodes, stdBoolExpect{
		labels:     map[string]string{"and": "false"},
		menus:      map[string]bool{"and": false},
		rightLinks: map[string]int{"false": 1},
	})

	linkByPortName(t, w, nodes["false"], "output", nodes["or"], "input 2")
	expectStdBoolGraph(t, w, menus, nodes, stdBoolExpect{
		labels:     map[string]string{"or": "true"},
		menus:      map[string]bool{"or": true},
		rightLinks: map[string]int{"false": 2},
	})

	_, _, err := w.AddLink(
		portByName(t, w.Snapshot(), nodes["false"], "right", "output"),
		portByName(t, w.Snapshot(), nodes["not"], "left", "input 1"),
	)
	if !errors.Is(err, pasta.ErrLinkDup) {
		t.Fatalf("second bool input link error = %v, want %v", err, pasta.ErrLinkDup)
	}
	expectStdBoolGraph(t, w, menus, nodes, stdBoolExpect{
		labels:     map[string]string{"not": "false"},
		menus:      map[string]bool{"not": false},
		rightLinks: map[string]int{"false": 2, "true": 3},
	})

	cfg := configer.NewMemory(nil)
	if err := w.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig bool graph: %v", err)
	}
	restored, err := pasta.WorkspaceFromConfig(allStdClasses(), cfg, testLogFactory{})
	if err != nil {
		t.Fatalf("WorkspaceFromConfig bool graph: %v", err)
	}
	restoredNodes := map[string]uint64{}
	for name := range nodes {
		id, ok := restored.NodeIDByName(name)
		if !ok {
			t.Fatalf("restored node %q missing", name)
		}
		restoredNodes[name] = id
	}
	restoredMenus := subscribeStdMenus(t, restored, restoredNodes)
	expectStdBoolGraph(t, restored, restoredMenus, restoredNodes, stdBoolExpect{
		labels:     map[string]string{"true": "true", "false": "false", "and": "false", "or": "true", "not": "false"},
		menus:      map[string]bool{"true": true, "false": false, "and": false, "or": true, "not": false},
		rightLinks: map[string]int{"true": 3, "false": 2},
	})
}

func TestStdComparisonNodesUseComparableAnyInputs(t *testing.T) {
	w := newStdWorkspace(t)
	nodes := map[string]uint64{
		"i3":       addByClass(t, w, NodeTypeIntConstant, "i3"),
		"i4":       addByClass(t, w, NodeTypeIntConstant, "i4"),
		"f25":      addByClass(t, w, NodeTypeFloatConstant, "f25"),
		"f3":       addByClass(t, w, NodeTypeFloatConstant, "f3"),
		"more":     addByClass(t, w, NodeTypeMore, "more"),
		"less":     addByClass(t, w, NodeTypeLess, "less"),
		"equal":    addByClass(t, w, NodeTypeEqual, "equal"),
		"notEqual": addByClass(t, w, NodeTypeNotEqual, "notEqual"),
		"and":      addByClass(t, w, NodeTypeBoolAnd, "and"),
	}
	menus := subscribeStdMenus(t, w, nodes)
	setConstant(t, w, nodes["i3"], 3)
	setConstant(t, w, nodes["i4"], 4)
	setConstant(t, w, nodes["f25"], 2.5)
	setConstant(t, w, nodes["f3"], 3.0)

	expectStdBoolGraph(t, w, menus, nodes, stdBoolExpect{
		labels: map[string]string{
			"more": "false", "less": "false", "equal": "true", "notEqual": "false", "and": "false",
		},
		menus: map[string]bool{
			"more": false, "less": false, "equal": true, "notEqual": false, "and": false,
		},
		primary: map[string]string{
			"more": TypeBool, "less": TypeBool, "equal": TypeBool, "notEqual": TypeBool,
		},
	})
	expectAnyInputs(t, w, nodes, "more", "less", "equal", "notEqual")

	linkByPortName(t, w, nodes["i3"], "output", nodes["more"], "input 1")
	linkByPortName(t, w, nodes["f25"], "output", nodes["more"], "input 2")
	expectStdBoolGraph(t, w, menus, nodes, stdBoolExpect{
		labels:     map[string]string{"more": "true"},
		menus:      map[string]bool{"more": true},
		rightLinks: map[string]int{"i3": 1, "f25": 1},
	})

	linkByPortName(t, w, nodes["f25"], "output", nodes["less"], "input 1")
	linkByPortName(t, w, nodes["i4"], "output", nodes["less"], "input 2")
	expectStdBoolGraph(t, w, menus, nodes, stdBoolExpect{
		labels:     map[string]string{"less": "true"},
		menus:      map[string]bool{"less": true},
		rightLinks: map[string]int{"f25": 2, "i4": 1},
	})

	linkByPortName(t, w, nodes["i3"], "output", nodes["equal"], "input 1")
	linkByPortName(t, w, nodes["f3"], "output", nodes["equal"], "input 2")
	expectStdBoolGraph(t, w, menus, nodes, stdBoolExpect{
		labels:     map[string]string{"equal": "true"},
		menus:      map[string]bool{"equal": true},
		rightLinks: map[string]int{"i3": 2, "f3": 1},
	})

	linkByPortName(t, w, nodes["i3"], "output", nodes["notEqual"], "input 1")
	linkByPortName(t, w, nodes["f25"], "output", nodes["notEqual"], "input 2")
	expectStdBoolGraph(t, w, menus, nodes, stdBoolExpect{
		labels:     map[string]string{"notEqual": "true"},
		menus:      map[string]bool{"notEqual": true},
		rightLinks: map[string]int{"i3": 3, "f25": 3},
	})

	linkByPortName(t, w, nodes["more"], "output", nodes["and"], "input 1")
	linkByPortName(t, w, nodes["less"], "output", nodes["and"], "input 2")
	expectStdBoolGraph(t, w, menus, nodes, stdBoolExpect{
		labels:     map[string]string{"and": "true"},
		menus:      map[string]bool{"and": true},
		rightLinks: map[string]int{"more": 1, "less": 1},
	})

	setConstant(t, w, nodes["i3"], 2)
	expectStdBoolGraph(t, w, menus, nodes, stdBoolExpect{
		labels: map[string]string{
			"i3": "2", "more": "false", "equal": "false", "notEqual": "true", "and": "false",
		},
		menus: map[string]bool{
			"more": false, "equal": false, "notEqual": true, "and": false,
		},
		rightLinks: map[string]int{"i3": 3},
	})

	_, _, err := w.AddLink(
		portByName(t, w.Snapshot(), nodes["i4"], "right", "output"),
		portByName(t, w.Snapshot(), nodes["more"], "left", "input 1"),
	)
	if !errors.Is(err, pasta.ErrLinkDup) {
		t.Fatalf("second comparison input link error = %v, want %v", err, pasta.ErrLinkDup)
	}
}

func TestStdSaveRestoreCopyPasteAndPlaceholderReplacement(t *testing.T) {
	w := newStdWorkspace(t)
	a := addByClass(t, w, NodeTypeIntConstant, "a")
	b := addByClass(t, w, NodeTypeIntConstant, "b")
	sum := addByClass(t, w, NodeTypeSum, "sum")
	setConstant(t, w, a, 4)
	setConstant(t, w, b, 5)
	linkByPortName(t, w, a, "output", sum, "input 1")
	linkByPortName(t, w, b, "output", sum, "input 2")

	clip := w.Copy([]uint64{a})
	pasted := w.Paste(clip)
	if len(pasted) != 1 {
		t.Fatalf("Paste single node returned %v, want one node", pasted)
	}
	if got := w.Snapshot().Nodes[pasted[0]].Label; got != "4" {
		t.Fatalf("pasted constant label = %q, want 4", got)
	}

	groupClip := w.Copy([]uint64{a, b, sum})
	group := w.Paste(groupClip)
	if len(group) != 3 {
		t.Fatalf("Paste linked group returned %v, want three nodes", group)
	}
	groupSnapshot := w.Snapshot()
	var pastedSum uint64
	for _, id := range group {
		if groupSnapshot.Nodes[id].Class == NodeTypeSum {
			pastedSum = id
		}
	}
	if got := groupSnapshot.Nodes[pastedSum].Label; got != "9" {
		t.Fatalf("pasted linked sum label = %q, want 9", got)
	}

	cfg := configer.NewMemory(nil)
	if err := w.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	restored, err := pasta.WorkspaceFromConfig(allStdClasses(), cfg, testLogFactory{})
	if err != nil {
		t.Fatalf("WorkspaceFromConfig: %v", err)
	}
	restoredSum, ok := restored.NodeIDByName("sum")
	if !ok {
		t.Fatal("restored sum missing")
	}
	if got := restored.Snapshot().Nodes[restoredSum].Label; got != "9" {
		t.Fatalf("restored sum label = %q, want 9", got)
	}

	placeholder := pasta.NewWorkspace(testLogFactory{})
	ph, err := placeholder.AddPlaceholderNode(NodeTypeIntConstant, []pasta.Port{rightPort(TypeInt)}, "late")
	if err != nil {
		t.Fatalf("AddPlaceholderNode: %v", err)
	}
	if err := placeholder.SetNodeLabel(ph, "7"); err != nil {
		t.Fatalf("SetNodeLabel placeholder: %v", err)
	}
	if err := placeholder.AddNodeClass(IntConstantClass{}); err != nil {
		t.Fatalf("AddNodeClass placeholder replacement: %v", err)
	}
	node := placeholder.Snapshot().Nodes[ph]
	if node.Placeholder || node.Class != NodeTypeIntConstant {
		t.Fatalf("placeholder replacement node = %#v", node)
	}
}

func newStdWorkspace(t *testing.T) *pasta.Workspace {
	t.Helper()
	w := pasta.NewWorkspace(testLogFactory{})
	if err := Register(w); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return w
}

func allStdClasses() []pasta.NodeClass {
	return []pasta.NodeClass{
		IntConstantClass{}, FloatConstantClass{}, SubClass{}, DivClass{}, MulClass{}, SumClass{},
		TrueConstantClass{}, FalseConstantClass{}, BoolAndClass{}, BoolNotClass{}, BoolOrClass{},
		MoreClass{}, LessClass{}, EqualClass{}, NotEqualClass{},
	}
}

func addByClass(t *testing.T, w *pasta.Workspace, class, name string) uint64 {
	t.Helper()
	id, err := w.AddNodeByClass(class, name)
	if err != nil {
		t.Fatalf("AddNodeByClass %s: %v", class, err)
	}
	return id
}

func setConstant(t *testing.T, w *pasta.Workspace, node uint64, value any) {
	t.Helper()
	w.SendNodeFormularMsg(node, formular.FieldUpdateMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageFieldUpdate, MenuID: pasta.NodeMenuID(node), MenuGeneration: 1},
		Field:       formular.FieldRef{BlockID: "state", FieldID: "value"},
		Value:       value,
	})
}

func linkByPortName(t *testing.T, w *pasta.Workspace, rightNode uint64, rightName string, leftNode uint64, leftName string) uint64 {
	t.Helper()
	snapshot := w.Snapshot()
	right := portByName(t, snapshot, rightNode, "right", rightName)
	left := portByName(t, snapshot, leftNode, "left", leftName)
	link, _, err := w.AddLink(right, left)
	if err != nil {
		t.Fatalf("AddLink %s -> %s: %v", rightName, leftName, err)
	}
	return link
}

func portByName(t *testing.T, snapshot pasta.WorkspaceSnapshot, node uint64, direction, name string) uint64 {
	t.Helper()
	var ports []uint64
	if direction == "left" {
		ports = snapshot.Nodes[node].LeftPorts
	} else {
		ports = snapshot.Nodes[node].RightPorts
	}
	for _, port := range ports {
		if snapshot.Ports[port].Name == name {
			return port
		}
	}
	t.Fatalf("missing %s port %q on node %d in %#v", direction, name, node, snapshot.Nodes[node])
	return 0
}

func assertLeftPortNames(t *testing.T, w *pasta.Workspace, node uint64, want []string) {
	t.Helper()
	snapshot := w.Snapshot()
	got := make([]string, 0, len(snapshot.Nodes[node].LeftPorts))
	for _, port := range snapshot.Nodes[node].LeftPorts {
		got = append(got, snapshot.Ports[port].Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("left port names = %#v, want %#v", got, want)
	}
}

func assertMenuValue(t *testing.T, w *pasta.Workspace, node uint64, want any, readonly bool) {
	t.Helper()
	state := formular.NewMenuSnapshotState()
	sub := w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		if notification.Kind == pasta.NotificationNodeMenu {
			state.Apply(notification.Formular)
		}
	})
	if !w.SubscribeNodeMenu(node, sub) {
		t.Fatalf("SubscribeNodeMenu(%d) returned false", node)
	}
	snapshot, ok := state.Snapshot(pasta.NodeMenuID(node))
	if !ok {
		t.Fatalf("missing menu snapshot for node %d", node)
	}
	for _, block := range snapshot.Blocks {
		for _, item := range block.Items {
			if item.ID == "value" && item.Field != nil {
				if item.Field.Value != want || item.Field.Readonly != readonly {
					t.Fatalf("menu field = value %#v readonly %v, want %#v %v", item.Field.Value, item.Field.Readonly, want, readonly)
				}
				return
			}
		}
	}
	t.Fatalf("missing state.value field in %#v", snapshot)
}

type stdGraphExpect struct {
	labels     map[string]string
	menuValues map[string]float64
	primary    map[string]string
	leftPorts  map[string][]string
	rightLinks map[string]int
}

func subscribeStdMenus(t *testing.T, w *pasta.Workspace, nodes map[string]uint64) *formular.MenuSnapshotState {
	t.Helper()
	state := formular.NewMenuSnapshotState()
	sub := w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		if notification.Kind == pasta.NotificationNodeMenu {
			state.Apply(notification.Formular)
		}
	})
	for name, id := range nodes {
		if !w.SubscribeNodeMenu(id, sub) {
			t.Fatalf("SubscribeNodeMenu %s (%d) returned false", name, id)
		}
	}
	return state
}

func expectStdGraph(t *testing.T, w *pasta.Workspace, menus *formular.MenuSnapshotState, nodes map[string]uint64, expect stdGraphExpect) {
	t.Helper()
	snapshot := w.Snapshot()
	for name, want := range expect.labels {
		id := nodes[name]
		if got := snapshot.Nodes[id].Label; got != want {
			t.Fatalf("%s label = %q, want %q", name, got, want)
		}
	}
	for name, want := range expect.menuValues {
		got, readonly := stdMenuValue(t, menus, nodes[name])
		if got != want {
			t.Fatalf("%s menu value = %g, want %g", name, got, want)
		}
		if wantReadonly := !isConstantClass(snapshot.Nodes[nodes[name]].Class); readonly != wantReadonly {
			t.Fatalf("%s menu readonly = %v, want %v", name, readonly, wantReadonly)
		}
	}
	for name, want := range expect.primary {
		id := nodes[name]
		if got := snapshot.Nodes[id].PrimaryType; got != want {
			t.Fatalf("%s primary = %q, want %q", name, got, want)
		}
		out := portByName(t, snapshot, id, "right", "output")
		if got := snapshot.Ports[out].Types; !reflect.DeepEqual(got, []string{want}) {
			t.Fatalf("%s output types = %#v, want [%s]", name, got, want)
		}
	}
	for name, want := range expect.leftPorts {
		id := nodes[name]
		got := make([]string, 0, len(snapshot.Nodes[id].LeftPorts))
		for _, port := range snapshot.Nodes[id].LeftPorts {
			got = append(got, snapshot.Ports[port].Name)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s left ports = %#v, want %#v", name, got, want)
		}
	}
	for name, want := range expect.rightLinks {
		id := nodes[name]
		out := portByName(t, snapshot, id, "right", "output")
		if got := len(snapshot.Ports[out].Links); got != want {
			t.Fatalf("%s output link count = %d, want %d", name, got, want)
		}
	}
}

func stdMenuValue(t *testing.T, state *formular.MenuSnapshotState, node uint64) (float64, bool) {
	t.Helper()
	snapshot, ok := state.Snapshot(pasta.NodeMenuID(node))
	if !ok {
		t.Fatalf("missing menu snapshot for node %d", node)
	}
	for _, block := range snapshot.Blocks {
		if block.ID != "state" {
			continue
		}
		for _, item := range block.Items {
			if item.ID != "value" || item.Field == nil {
				continue
			}
			value, ok := parseFloatAny(item.Field.Value)
			if !ok {
				if s, ok := item.Field.Value.(string); ok {
					parsed, err := strconv.ParseFloat(s, 64)
					if err == nil {
						return parsed, item.Field.Readonly
					}
				}
				t.Fatalf("menu value for node %d has type %T", node, item.Field.Value)
			}
			return value, item.Field.Readonly
		}
	}
	t.Fatalf("missing state.value field in node %d menu %#v", node, snapshot)
	return 0, false
}

func isConstantClass(class string) bool {
	return class == NodeTypeIntConstant || class == NodeTypeFloatConstant ||
		class == NodeTypeTrueConstant || class == NodeTypeFalseConstant
}

func expectAnyInputs(t *testing.T, w *pasta.Workspace, nodes map[string]uint64, names ...string) {
	t.Helper()
	snapshot := w.Snapshot()
	for _, name := range names {
		id := nodes[name]
		for _, port := range snapshot.Nodes[id].LeftPorts {
			if got := snapshot.Ports[port].Types; !reflect.DeepEqual(got, []string{pasta.AnyType}) {
				t.Fatalf("%s input %s types = %#v, want [any/any]", name, snapshot.Ports[port].Name, got)
			}
		}
	}
}

type stdBoolExpect struct {
	labels     map[string]string
	menus      map[string]bool
	primary    map[string]string
	rightLinks map[string]int
}

func expectStdBoolGraph(t *testing.T, w *pasta.Workspace, menus *formular.MenuSnapshotState, nodes map[string]uint64, expect stdBoolExpect) {
	t.Helper()
	snapshot := w.Snapshot()
	for name, want := range expect.labels {
		id := nodes[name]
		if got := snapshot.Nodes[id].Label; got != want {
			t.Fatalf("%s label = %q, want %q", name, got, want)
		}
	}
	for name, want := range expect.menus {
		got, readonly := stdBoolMenuValue(t, menus, nodes[name])
		if got != want {
			t.Fatalf("%s menu value = %v, want %v", name, got, want)
		}
		if !readonly {
			t.Fatalf("%s bool menu readonly = false, want true", name)
		}
	}
	for name, want := range expect.primary {
		id := nodes[name]
		if got := snapshot.Nodes[id].PrimaryType; got != want {
			t.Fatalf("%s primary = %q, want %q", name, got, want)
		}
		out := portByName(t, snapshot, id, "right", "output")
		if got := snapshot.Ports[out].Types; !reflect.DeepEqual(got, []string{want}) {
			t.Fatalf("%s output types = %#v, want [%s]", name, got, want)
		}
	}
	for name, want := range expect.rightLinks {
		id := nodes[name]
		out := portByName(t, snapshot, id, "right", "output")
		if got := len(snapshot.Ports[out].Links); got != want {
			t.Fatalf("%s output link count = %d, want %d", name, got, want)
		}
	}
}

func stdBoolMenuValue(t *testing.T, state *formular.MenuSnapshotState, node uint64) (bool, bool) {
	t.Helper()
	snapshot, ok := state.Snapshot(pasta.NodeMenuID(node))
	if !ok {
		t.Fatalf("missing menu snapshot for node %d", node)
	}
	for _, block := range snapshot.Blocks {
		if block.ID != "state" {
			continue
		}
		for _, item := range block.Items {
			if item.ID != "value" || item.Field == nil {
				continue
			}
			value, ok := item.Field.Value.(bool)
			if !ok {
				t.Fatalf("bool menu value for node %d has type %T", node, item.Field.Value)
			}
			return value, item.Field.Readonly
		}
	}
	t.Fatalf("missing state.value field in node %d menu %#v", node, snapshot)
	return false, false
}
