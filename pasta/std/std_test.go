// nolint
package std

import (
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

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

func TestStdEditableConstantsCommitOnlyOnFormApply(t *testing.T) {
	w := newStdWorkspace(t)
	nodes := map[string]uint64{
		"int":    addByClass(t, w, NodeTypeIntConstant, "int"),
		"float":  addByClass(t, w, NodeTypeFloatConstant, "float"),
		"string": addByClass(t, w, NodeTypeStringConstant, "string"),
		"bool":   addByClass(t, w, NodeTypeBoolConstant, "bool"),
	}
	menus := subscribeStdMenus(t, w, nodes)
	assertConstantMenuForm(t, menus, nodes["int"], 0, false)
	assertConstantMenuForm(t, menus, nodes["float"], float64(0), false)
	assertConstantMenuForm(t, menus, nodes["string"], "", false)
	assertConstantMenuForm(t, menus, nodes["bool"], false, false)

	sendConstantFieldUpdate(t, w, nodes["int"], 11)
	sendConstantFieldUpdate(t, w, nodes["float"], 2.5)
	sendConstantFieldUpdate(t, w, nodes["string"], "draft")
	sendConstantFieldUpdate(t, w, nodes["bool"], true)
	assertNodeLabel(t, w, nodes["int"], "0")
	assertNodeLabel(t, w, nodes["float"], "0")
	assertNodeLabel(t, w, nodes["string"], "")
	assertNodeLabel(t, w, nodes["bool"], "false")
	assertConstantMenuForm(t, menus, nodes["int"], 0, false)
	assertConstantMenuForm(t, menus, nodes["float"], float64(0), false)
	assertConstantMenuForm(t, menus, nodes["string"], "", false)
	assertConstantMenuForm(t, menus, nodes["bool"], false, false)

	setConstant(t, w, nodes["int"], 11)
	setConstant(t, w, nodes["float"], 2.5)
	setConstant(t, w, nodes["string"], "applied")
	setConstant(t, w, nodes["bool"], true)
	assertNodeLabel(t, w, nodes["int"], "11")
	assertNodeLabel(t, w, nodes["float"], "2.5")
	assertNodeLabel(t, w, nodes["string"], "applied")
	assertNodeLabel(t, w, nodes["bool"], "true")
	assertConstantMenuForm(t, menus, nodes["int"], 11, false)
	assertConstantMenuForm(t, menus, nodes["float"], 2.5, false)
	assertConstantMenuForm(t, menus, nodes["string"], "applied", false)
	assertConstantMenuForm(t, menus, nodes["bool"], true, false)
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
		"select":   addByClass(t, w, NodeTypeSelect, "select"),
		"true":     addByClass(t, w, NodeTypeTrueConstant, "true"),
		"false":    addByClass(t, w, NodeTypeFalseConstant, "false"),
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
			"sumInt": "0", "sumFloat": "0", "mulInt": "1", "subMixed": "0", "divMixed": "0",
			"select": "in 0 -> out", "true": "true", "false": "false",
		},
		menuValues: map[string]float64{
			"i2": 2, "i3": 3, "i4": 4, "f15": 1.5, "f25": 2.5,
			"sumInt": 0, "sumFloat": 0, "mulInt": 1, "subMixed": 0, "divMixed": 0,
		},
		leftPorts: map[string][]string{
			"sumInt": {"input 1"}, "sumFloat": {"input 1"}, "mulInt": {"input 1"},
			"subMixed": {"input 1", "input 2"}, "divMixed": {"input 1", "input 2"},
		},
		rightLinks: map[string]int{"i2": 0, "i3": 0, "i4": 0, "f15": 0, "f25": 0, "true": 0, "false": 0},
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
		labels:     map[string]string{"mulInt": "2"},
		menuValues: map[string]float64{"mulInt": 2},
		primary:    map[string]string{"mulInt": TypeInt},
		leftPorts:  map[string][]string{"mulInt": {"input 1", "input 2"}},
		rightLinks: map[string]int{"i2": 2},
	})

	linkByPortName(t, w, nodes["i3"], "output", nodes["mulInt"], "input 2")
	expectStdGraph(t, w, menus, nodes, stdGraphExpect{
		labels:     map[string]string{"mulInt": "6"},
		menuValues: map[string]float64{"mulInt": 6},
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
			"i2": "4", "sumInt": "11", "sumFloat": "8", "mulInt": "12", "divMixed": "0.625",
		},
		menuValues: map[string]float64{
			"i2": 4, "sumInt": 11, "sumFloat": 8, "mulInt": 12, "divMixed": 0.625,
		},
		rightLinks: map[string]int{"i2": 4},
	})

	linkByPortName(t, w, nodes["i2"], "output", nodes["select"], "In 0")
	expectStdGraph(t, w, menus, nodes, stdGraphExpect{
		labels:     map[string]string{"select": "in 0 -> out"},
		primary:    map[string]string{"select": TypeInt},
		rightLinks: map[string]int{"i2": 5},
	})
	assertSelectDataPortTypes(t, w, nodes["select"], TypeInt)
	assertSelectMenu(t, menus, nodes["select"], false)

	linkByPortName(t, w, nodes["i3"], "output", nodes["select"], "In 1")
	expectStdGraph(t, w, menus, nodes, stdGraphExpect{
		rightLinks: map[string]int{"i3": 4},
	})

	linkByPortName(t, w, nodes["select"], "Out", nodes["sumFloat"], "input 4")
	expectStdGraph(t, w, menus, nodes, stdGraphExpect{
		labels:     map[string]string{"sumFloat": "12"},
		menuValues: map[string]float64{"sumFloat": 12},
		rightLinks: map[string]int{"select": 1},
		leftPorts:  map[string][]string{"sumFloat": {"input 1", "input 2", "input 3", "input 4", "input 5"}},
	})

	falseSelectorLink := linkByPortName(t, w, nodes["false"], "output", nodes["select"], "Selector")
	expectStdGraph(t, w, menus, nodes, stdGraphExpect{
		labels:     map[string]string{"select": "in 0 -> out", "sumFloat": "12"},
		menuValues: map[string]float64{"sumFloat": 12},
		rightLinks: map[string]int{"false": 1},
	})
	assertSelectMenu(t, menus, nodes["select"], false)

	w.RemoveLink(falseSelectorLink)
	linkByPortName(t, w, nodes["true"], "output", nodes["select"], "Selector")
	expectStdGraph(t, w, menus, nodes, stdGraphExpect{
		labels:     map[string]string{"select": "in 1 -> out", "sumFloat": "11"},
		menuValues: map[string]float64{"sumFloat": 11},
		rightLinks: map[string]int{"true": 1, "false": 0},
	})
	assertSelectMenu(t, menus, nodes["select"], true)
}

func TestSelectUsesSharedRequestValueForCustomLinkTypes(t *testing.T) {
	w := newStdWorkspace(t)
	source0Class := &customValueClass{name: "example.com/SelectSource0", value: "zero"}
	source1Class := &customValueClass{name: "example.com/SelectSource1", value: "one"}
	sinkClass := &customSinkClass{}
	for _, class := range []pasta.NodeClass{source0Class, source1Class, sinkClass} {
		if err := w.AddNodeClass(class); err != nil {
			t.Fatalf("AddNodeClass %s: %v", class.ClassName(), err)
		}
	}

	source0 := addByClass(t, w, source0Class.ClassName(), "source0")
	source1 := addByClass(t, w, source1Class.ClassName(), "source1")
	selectNode := addByClass(t, w, NodeTypeSelect, "select")
	selector := addByClass(t, w, NodeTypeTrueConstant, "selector")
	sink := addByClass(t, w, sinkClass.ClassName(), "sink")

	linkByPortName(t, w, source0, "output", selectNode, "In 0")
	linkByPortName(t, w, source1, "output", selectNode, "In 1")
	linkByPortName(t, w, selectNode, "Out", sink, "input")
	if got := sinkClass.node.value; got != "zero" {
		t.Fatalf("sink before selector = %q, want zero", got)
	}

	requestsBefore := source1Class.node.requests
	linkByPortName(t, w, selector, "output", selectNode, "Selector")
	if got := sinkClass.node.value; got != "one" {
		t.Fatalf("sink after selector = %q, want one", got)
	}
	if got := source1Class.node.requests; got <= requestsBefore {
		t.Fatalf("custom source requests = %d, want > %d", got, requestsBefore)
	}
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

func TestStdBoolConstantsSaveAndRestoreValueConfig(t *testing.T) {
	w := newStdWorkspace(t)
	addByClass(t, w, NodeTypeTrueConstant, "true")
	addByClass(t, w, NodeTypeFalseConstant, "false")

	cfg := configer.NewMemory(nil)
	if err := w.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig bool constants: %v", err)
	}
	assertConfigValue(t, cfg, configer.Path{"true", "value"}, true)
	assertConfigValue(t, cfg, configer.Path{"false", "value"}, false)

	restoredCfg := configer.NewMemory(map[string]any{
		"true":  map[string]any{"Class": NodeTypeTrueConstant, "value": false},
		"false": map[string]any{"Class": NodeTypeFalseConstant, "value": true},
	})
	restored, err := pasta.WorkspaceFromConfig(allStdClasses(), restoredCfg, testLogFactory{})
	if err != nil {
		t.Fatalf("WorkspaceFromConfig bool constants: %v", err)
	}
	restoredNodes := map[string]uint64{}
	for _, name := range []string{"true", "false"} {
		id, ok := restored.NodeIDByName(name)
		if !ok {
			t.Fatalf("restored node %q missing", name)
		}
		restoredNodes[name] = id
	}
	menus := subscribeStdMenus(t, restored, restoredNodes)
	expectStdBoolGraph(t, restored, menus, restoredNodes, stdBoolExpect{
		labels: map[string]string{"true": "false", "false": "true"},
		menus:  map[string]bool{"true": false, "false": true},
	})
}

func TestStdStringFormatBuildsPortsFromTemplateAndFormatsValues(t *testing.T) {
	w := newStdWorkspace(t)
	nodes := map[string]uint64{
		"name":   addByClass(t, w, NodeTypeStringConstant, "name"),
		"count":  addByClass(t, w, NodeTypeIntConstant, "count"),
		"ratio":  addByClass(t, w, NodeTypeFloatConstant, "ratio"),
		"format": addByClass(t, w, NodeTypeStringFormat, "format"),
		"true":   addByClass(t, w, NodeTypeTrueConstant, "true"),
	}
	menus := subscribeStdMenus(t, w, nodes)
	setConstant(t, w, nodes["name"], "Ada")
	setConstant(t, w, nodes["count"], 7)
	setConstant(t, w, nodes["ratio"], 2.5)

	setStringFormatTemplate(t, w, nodes["format"], []formatPartSpec{
		{template: "text", text: "Hello "},
		{template: "value", name: "Name", typ: TypeString},
		{template: "text", text: ", count="},
		{template: "value", name: "Count", typ: TypeInt},
		{template: "text", text: ", ratio="},
		{template: "value", name: "Ratio", typ: TypeFloat},
		{template: "text", text: ", ok="},
		{template: "value", name: "Ok", typ: TypeBool},
	})
	assertLeftPortNames(t, w, nodes["format"], []string{"Name", "Count", "Ratio", "Ok"})
	assertStringFormatMenuTemplate(t, menus, nodes["format"], []string{"text-1", "value-2", "text-3", "value-4", "text-5", "value-6", "text-7", "value-8"})

	linkByPortName(t, w, nodes["name"], "output", nodes["format"], "Name")
	linkByPortName(t, w, nodes["count"], "output", nodes["format"], "Count")
	linkByPortName(t, w, nodes["ratio"], "output", nodes["format"], "Ratio")
	linkByPortName(t, w, nodes["true"], "output", nodes["format"], "Ok")

	snapshot := w.Snapshot()
	if got := snapshot.Nodes[nodes["format"]].Label; got != "" {
		t.Fatalf("format label = %q, want empty", got)
	}
	assertStringFormatMenuResult(t, menus, nodes["format"], "Hello Ada, count=7, ratio=2.5, ok=true")
	out := portByName(t, snapshot, nodes["format"], "right", "output")
	if got := snapshot.Ports[out].Types; !reflect.DeepEqual(got, []string{TypeString}) {
		t.Fatalf("format output types = %#v, want [%s]", got, TypeString)
	}

	setStringFormatTemplate(t, w, nodes["format"], []formatPartSpec{
		{template: "text", text: "Hello "},
		{template: "value", name: "Person", typ: TypeString},
		{template: "text", text: ", count="},
		{template: "value", name: "Count", typ: TypeInt},
		{template: "text", text: ", ratio="},
		{template: "value", name: "Ratio", typ: TypeFloat},
		{template: "text", text: ", ok="},
		{template: "value", name: "Ok", typ: TypeBool},
	})
	assertLeftPortNames(t, w, nodes["format"], []string{"Person", "Count", "Ratio", "Ok"})
	if got := w.Snapshot().Nodes[nodes["format"]].Label; got != "" {
		t.Fatalf("format label after placeholder rename = %q, want empty", got)
	}
	assertStringFormatMenuResult(t, menus, nodes["format"], "Hello Ada, count=7, ratio=2.5, ok=true")
	if _, _, ok := w.LinkByPorts(
		portByName(t, w.Snapshot(), nodes["name"], "right", "output"),
		portByName(t, w.Snapshot(), nodes["format"], "left", "Person"),
	); !ok {
		t.Fatal("renamed placeholder did not keep existing link")
	}
}

func TestStdStringFormatSaveRestoreAndCopyPastePreserveDynamicPortsAndLinks(t *testing.T) {
	w := newStdWorkspace(t)
	name := addByClass(t, w, NodeTypeStringConstant, "name")
	format := addByClass(t, w, NodeTypeStringFormat, "format")
	setConstant(t, w, name, "Ada")
	setStringFormatTemplate(t, w, format, []formatPartSpec{
		{template: "text", text: "Hello "},
		{template: "value", name: "Name", typ: TypeString},
	})
	linkByPortName(t, w, name, "output", format, "Name")

	cfg := configer.NewMemory(nil)
	if err := w.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig string format: %v", err)
	}
	rawTemplate, err := cfg.Get(configer.Path{"format", "template"})
	if err != nil {
		t.Fatalf("saved format template missing: %v", err)
	}
	if parts, ok := rawTemplate.([]any); !ok || len(parts) != 2 {
		t.Fatalf("saved template = %#v, want two parts", rawTemplate)
	}

	restored, err := pasta.WorkspaceFromConfig(allStdClasses(), cfg, testLogFactory{})
	if err != nil {
		t.Fatalf("WorkspaceFromConfig string format: %v", err)
	}
	restoredFormat, ok := restored.NodeIDByName("format")
	if !ok {
		t.Fatal("restored format node missing")
	}
	assertLeftPortNames(t, restored, restoredFormat, []string{"Name"})
	restoredName, _ := restored.NodeIDByName("name")
	if _, _, ok := restored.LinkByPorts(
		portByName(t, restored.Snapshot(), restoredName, "right", "output"),
		portByName(t, restored.Snapshot(), restoredFormat, "left", "Name"),
	); !ok {
		t.Fatal("restored dynamic input link missing")
	}
	restoredMenus := subscribeStdMenus(t, restored, map[string]uint64{"format": restoredFormat})
	if got := restored.Snapshot().Nodes[restoredFormat].Label; got != "" {
		t.Fatalf("restored format label = %q, want empty", got)
	}
	assertStringFormatMenuResult(t, restoredMenus, restoredFormat, "Hello Ada")

	clip := restored.Copy([]uint64{restoredName, restoredFormat})
	pasted := restored.Paste(clip)
	if len(pasted) != 2 {
		t.Fatalf("Paste returned %v, want two nodes", pasted)
	}
	pastedFormat := pasted[1]
	assertLeftPortNames(t, restored, pastedFormat, []string{"Name"})
	pastedMenus := subscribeStdMenus(t, restored, map[string]uint64{"format": pastedFormat})
	if got := restored.Snapshot().Nodes[pastedFormat].Label; got != "" {
		t.Fatalf("pasted format label = %q, want empty", got)
	}
	assertStringFormatMenuResult(t, pastedMenus, pastedFormat, "Hello Ada")
}

const customSelectType = "example.com/selectValue"

type customValueClass struct {
	name  string
	value string
	node  *customValueNode
}

func (c *customValueClass) ClassName() string        { return c.name }
func (c *customValueClass) ShortDescription() string { return "custom value source" }
func (c *customValueClass) LongDescription() string  { return "custom value source" }
func (c *customValueClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: customSelectType, InitialPorts: []pasta.Port{rightPort(customSelectType)}}
}
func (c *customValueClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	c.node = &customValueNode{value: c.value}
	return c.node, nil
}

type customValueNode struct {
	pasta.BasicNode
	value    string
	requests int
	w        *pasta.Workspace
	id       uint64
	out      uint64
}

func (n *customValueNode) OnInit(w *pasta.Workspace, _ pasta.Logger, id uint64, _ string, restored *pasta.NodeInitData, _, _, _, _ bool) error {
	n.w = w
	n.id = id
	if restored != nil && len(restored.RightPorts) > 0 {
		n.out = restored.RightPorts[0]
	}
	return nil
}

func (n *customValueNode) PreLinkAdd(port uint64, linkType, _ string) error {
	if port != n.out || linkType != customSelectType {
		return pasta.LinkTypeErr(linkType)
	}
	return nil
}

func (n *customValueNode) OnLinkAdd(link, port uint64, _ string, _ string) error {
	if port == n.out {
		n.sendToLink(link)
	}
	return nil
}

func (n *customValueNode) OnEvent(event pasta.Event, _ string, _ []string, receiverPortDirection string) error {
	if event.ReceiverPort != n.out || receiverPortDirection != "right" || !isValueRequest(event.Payload) {
		return nil
	}
	n.requests++
	n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: event.SenderNode, ReceiverPort: event.SenderPort, Payload: n.value})
	return nil
}

func (n *customValueNode) sendToLink(link uint64) {
	snapshot, ok := n.w.LinkSnapshotLocked(link)
	if !ok {
		return
	}
	receiverNode, receiverPort := otherEndpoint(snapshot, n.out)
	n.w.SendEventLocked(pasta.Event{SenderNode: n.id, SenderPort: n.out, ReceiverNode: receiverNode, ReceiverPort: receiverPort, Payload: n.value})
}

func TestStdSelectCallbackReentryDoesNotDeadlock(t *testing.T) {
	w := newStdWorkspace(t)
	source0Class := &customValueClass{name: "example.com/DeadlockSelectSource0", value: "zero"}
	source1Class := &customValueClass{name: "example.com/DeadlockSelectSource1", value: "one"}
	sinkClass := &customSinkClass{}
	for _, class := range []pasta.NodeClass{source0Class, source1Class, sinkClass} {
		if err := w.AddNodeClass(class); err != nil {
			t.Fatalf("AddNodeClass %s: %v", class.ClassName(), err)
		}
	}

	source0 := addByClass(t, w, source0Class.ClassName(), "source0")
	source1 := addByClass(t, w, source1Class.ClassName(), "source1")
	selectNode := addByClass(t, w, NodeTypeSelect, "select")
	selector := addByClass(t, w, NodeTypeTrueConstant, "selector")
	sink := addByClass(t, w, sinkClass.ClassName(), "sink")

	linkByPortName(t, w, source0, "output", selectNode, "In 0")
	linkByPortName(t, w, source1, "output", selectNode, "In 1")
	linkByPortName(t, w, selectNode, "Out", sink, "input")

	snapshot := w.Snapshot()
	from := portByName(t, snapshot, selector, "right", "output")
	to := portByName(t, snapshot, selectNode, "left", "Selector")
	done := make(chan error, 1)
	go func() {
		_, _, err := w.AddLink(from, to)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("AddLink selector: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("selector AddLink deadlocked while std callbacks re-entered workspace")
	}
	if got := sinkClass.node.value; got != "one" {
		t.Fatalf("sink after selector = %q, want one", got)
	}
}

type customSinkClass struct {
	node *customSinkNode
}

func (c *customSinkClass) ClassName() string        { return "example.com/SelectSink" }
func (c *customSinkClass) ShortDescription() string { return "custom value sink" }
func (c *customSinkClass) LongDescription() string  { return "custom value sink" }
func (c *customSinkClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{InitialPorts: []pasta.Port{{Direction: "left", Name: "input", Types: []string{customSelectType}}}}
}
func (c *customSinkClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	c.node = &customSinkNode{}
	return c.node, nil
}

type customSinkNode struct {
	pasta.BasicNode
	value string
}

func (n *customSinkNode) PreLinkAdd(_ uint64, linkType, portDirection string) error {
	if portDirection != "left" || linkType != customSelectType {
		return pasta.LinkTypeErr(linkType)
	}
	return nil
}

func (n *customSinkNode) OnEvent(event pasta.Event, _ string, _ []string, receiverPortDirection string) error {
	if receiverPortDirection == "left" {
		n.value, _ = event.Payload.(string)
	}
	return nil
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

func TestStdStringNodesProcessAndCompare(t *testing.T) {
	w := newStdWorkspace(t)
	nodes := map[string]uint64{
		"hello":    addByClass(t, w, NodeTypeStringConstant, "hello"),
		"space":    addByClass(t, w, NodeTypeStringConstant, "space"),
		"world":    addByClass(t, w, NodeTypeStringConstant, "world"),
		"substr":   addByClass(t, w, NodeTypeStringConstant, "substr"),
		"concat":   addByClass(t, w, NodeTypeStringConcat, "concat"),
		"split":    addByClass(t, w, NodeTypeStringSplit, "split"),
		"before":   addByClass(t, w, NodeTypeStringTrimSpace, "before"),
		"after":    addByClass(t, w, NodeTypeStringUpper, "after"),
		"upper":    addByClass(t, w, NodeTypeStringUpper, "upper"),
		"lower":    addByClass(t, w, NodeTypeStringLower, "lower"),
		"trim":     addByClass(t, w, NodeTypeStringTrimSpace, "trim"),
		"length":   addByClass(t, w, NodeTypeStringLength, "length"),
		"contains": addByClass(t, w, NodeTypeStringContains, "contains"),
		"less":     addByClass(t, w, NodeTypeLess, "less"),
	}
	menus := subscribeStdMenus(t, w, nodes)
	setConstant(t, w, nodes["hello"], " Hello")
	setConstant(t, w, nodes["space"], ", ")
	setConstant(t, w, nodes["world"], "World ")
	setConstant(t, w, nodes["substr"], "WORLD")

	expectStdStringGraph(t, w, menus, nodes, stdStringExpect{
		labels: map[string]string{
			"hello": " Hello", "space": ", ", "world": "World ", "substr": "WORLD",
			"concat": "", "upper": "", "lower": "", "trim": "",
		},
		menus: map[string]string{
			"hello": " Hello", "space": ", ", "world": "World ", "substr": "WORLD",
			"concat": "", "upper": "", "lower": "", "trim": "",
		},
		primary: map[string]string{
			"hello": TypeString, "concat": TypeString, "upper": TypeString, "lower": TypeString, "trim": TypeString, "before": TypeString, "after": TypeString,
			"length": TypeInt, "contains": TypeBool,
		},
	})
	assertStringSplitPorts(t, w, nodes["split"])

	linkByPortName(t, w, nodes["hello"], "output", nodes["concat"], "input 1")
	linkByPortName(t, w, nodes["space"], "output", nodes["concat"], "input 2")
	linkByPortName(t, w, nodes["world"], "output", nodes["concat"], "input 3")
	expectStdStringGraph(t, w, menus, nodes, stdStringExpect{
		labels:    map[string]string{"concat": " Hello, World "},
		menus:     map[string]string{"concat": " Hello, World "},
		leftPorts: map[string][]string{"concat": {"input 1", "input 2", "input 3", "input 4"}},
	})

	linkByPortName(t, w, nodes["concat"], "output", nodes["split"], "Text")
	linkByPortName(t, w, nodes["space"], "output", nodes["split"], "Separator")
	linkByPortName(t, w, nodes["split"], "Before", nodes["before"], "input 1")
	linkByPortName(t, w, nodes["split"], "After", nodes["after"], "input 1")
	expectStdStringGraph(t, w, menus, nodes, stdStringExpect{
		labels: map[string]string{
			"split": " Hello | World ", "before": "Hello", "after": "WORLD ",
		},
		menus: map[string]string{
			"before": "Hello", "after": "WORLD ",
		},
	})

	linkByPortName(t, w, nodes["concat"], "output", nodes["upper"], "input 1")
	linkByPortName(t, w, nodes["upper"], "output", nodes["trim"], "input 1")
	linkByPortName(t, w, nodes["trim"], "output", nodes["lower"], "input 1")
	linkByPortName(t, w, nodes["trim"], "output", nodes["length"], "input 1")
	linkByPortName(t, w, nodes["trim"], "output", nodes["contains"], "input 1")
	linkByPortName(t, w, nodes["substr"], "output", nodes["contains"], "input 2")
	expectStdStringGraph(t, w, menus, nodes, stdStringExpect{
		labels: map[string]string{
			"upper": " HELLO, WORLD ", "trim": "HELLO, WORLD", "lower": "hello, world", "length": "12", "contains": "true",
		},
		menus: map[string]string{
			"upper": " HELLO, WORLD ", "trim": "HELLO, WORLD", "lower": "hello, world",
		},
	})
	expectStdGraph(t, w, menus, nodes, stdGraphExpect{menuValues: map[string]float64{"length": 12}})
	expectStdBoolGraph(t, w, menus, nodes, stdBoolExpect{menus: map[string]bool{"contains": true}})

	linkByPortName(t, w, nodes["hello"], "output", nodes["less"], "input 1")
	linkByPortName(t, w, nodes["world"], "output", nodes["less"], "input 2")
	expectStdBoolGraph(t, w, menus, nodes, stdBoolExpect{
		labels: map[string]string{"less": "true"},
		menus:  map[string]bool{"less": true},
	})

	setConstant(t, w, nodes["world"], "there ")
	expectStdStringGraph(t, w, menus, nodes, stdStringExpect{
		labels: map[string]string{
			"concat": " Hello, there ", "split": " Hello | there ", "before": "Hello", "after": "THERE ", "upper": " HELLO, THERE ", "trim": "HELLO, THERE", "lower": "hello, there", "length": "12", "contains": "false",
		},
		menus: map[string]string{
			"concat": " Hello, there ", "before": "Hello", "after": "THERE ", "upper": " HELLO, THERE ", "trim": "HELLO, THERE", "lower": "hello, there",
		},
	})
	expectStdBoolGraph(t, w, menus, nodes, stdBoolExpect{
		labels: map[string]string{"contains": "false", "less": "true"},
		menus:  map[string]bool{"contains": false, "less": true},
	})
}

func TestStdStringSaveRestore(t *testing.T) {
	w := newStdWorkspace(t)
	a := addByClass(t, w, NodeTypeStringConstant, "a")
	b := addByClass(t, w, NodeTypeStringConstant, "b")
	concat := addByClass(t, w, NodeTypeStringConcat, "concat")
	setConstant(t, w, a, "saved")
	setConstant(t, w, b, " string")
	linkByPortName(t, w, a, "output", concat, "input 1")
	linkByPortName(t, w, b, "output", concat, "input 2")

	cfg := configer.NewMemory(nil)
	if err := w.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig string graph: %v", err)
	}
	restored, err := pasta.WorkspaceFromConfig(allStdClasses(), cfg, testLogFactory{})
	if err != nil {
		t.Fatalf("WorkspaceFromConfig string graph: %v", err)
	}
	restoredConcat, ok := restored.NodeIDByName("concat")
	if !ok {
		t.Fatal("restored concat missing")
	}
	if got := restored.Snapshot().Nodes[restoredConcat].Label; got != "saved string" {
		t.Fatalf("restored concat label = %q, want saved string", got)
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
		IntConstantClass{}, FloatConstantClass{}, StringConstantClass{}, ObjectConstantClass{}, ObjectPackerClass{}, ObjectUnpackerClass{}, ObjectToStringClass{}, SubClass{}, DivClass{}, MulClass{}, SumClass{},
		StringConcatClass{}, StringFormatClass{}, StringLengthClass{}, StringContainsClass{}, StringSplitClass{}, StringUpperClass{}, StringLowerClass{}, StringTrimSpaceClass{},
		TrueConstantClass{}, FalseConstantClass{}, BoolAndClass{}, BoolNotClass{}, BoolOrClass{},
		MoreClass{}, LessClass{}, EqualClass{}, NotEqualClass{},
		TriggerClass{}, PopUpClass{}, GatewayClass{},
		SelectClass{}, SelectOutClass{}, BoolConstantClass{},
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
	w.SendNodeFormularMsg(node, formular.FormApplyMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageFormApply, MenuID: pasta.NodeMenuID(node), MenuGeneration: 1},
		BlockID:     "state",
		Values:      map[string]any{"value": value},
	})
}

func sendConstantFieldUpdate(t *testing.T, w *pasta.Workspace, node uint64, value any) {
	t.Helper()
	w.SendNodeFormularMsg(node, formular.FieldUpdateMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageFieldUpdate, MenuID: pasta.NodeMenuID(node), MenuGeneration: 1},
		Field:       formular.FieldRef{BlockID: "state", FieldID: "value"},
		Value:       value,
	})
}

func assertNodeLabel(t *testing.T, w *pasta.Workspace, node uint64, want string) {
	t.Helper()
	if got := w.Snapshot().Nodes[node].Label; got != want {
		t.Fatalf("node %d label = %q, want %q", node, got, want)
	}
}

type formatPartSpec struct {
	template string
	text     string
	name     string
	typ      string
}

func setStringFormatTemplate(t *testing.T, w *pasta.Workspace, node uint64, parts []formatPartSpec) {
	t.Helper()
	values := make([]formular.ArrayElementValue, 0, len(parts))
	for i, part := range parts {
		id := part.template + "-" + strconv.Itoa(i+1)
		element := formular.ArrayElementValue{
			ID:       id,
			Template: part.template,
			Values:   map[string]any{},
		}
		if part.template == "text" {
			element.Values["text"] = part.text
		} else {
			element.Values["name"] = part.name
			element.Values["type"] = part.typ
		}
		values = append(values, element)
	}
	w.SendNodeFormularMsg(node, formular.FieldUpdateMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageFieldUpdate, MenuID: pasta.NodeMenuID(node), MenuGeneration: 1},
		Field:       formular.FieldRef{BlockID: "template", FieldID: "parts"},
		Value:       values,
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

func assertStringFormatMenuTemplate(t *testing.T, state *formular.MenuSnapshotState, node uint64, wantIDs []string) {
	t.Helper()
	snapshot, ok := state.Snapshot(pasta.NodeMenuID(node))
	if !ok {
		t.Fatalf("missing format menu snapshot for node %d", node)
	}
	for _, block := range snapshot.Blocks {
		if block.ID != "template" {
			continue
		}
		for _, item := range block.Items {
			if item.ID != "parts" || item.Field == nil {
				continue
			}
			got := make([]string, 0, len(item.Field.Elements))
			for _, element := range item.Field.Elements {
				got = append(got, element.ID)
			}
			if !reflect.DeepEqual(got, wantIDs) {
				t.Fatalf("format menu element ids = %#v, want %#v", got, wantIDs)
			}
			return
		}
	}
	t.Fatalf("missing template.parts array field in %#v", snapshot)
}

func assertStringFormatMenuResult(t *testing.T, state *formular.MenuSnapshotState, node uint64, want string) {
	t.Helper()
	snapshot, ok := state.Snapshot(pasta.NodeMenuID(node))
	if !ok {
		t.Fatalf("missing format menu snapshot for node %d", node)
	}
	for _, block := range snapshot.Blocks {
		if block.ID != "result" {
			continue
		}
		for _, item := range block.Items {
			if item.ID != "value" || item.Field == nil {
				continue
			}
			got, ok := parseStringAny(item.Field.Value)
			if !ok {
				t.Fatalf("format result has type %T", item.Field.Value)
			}
			if got != want || !item.Field.Readonly || !item.Field.Multiline {
				t.Fatalf("format result = %q readonly %v multiline %v, want %q true true", got, item.Field.Readonly, item.Field.Multiline, want)
			}
			return
		}
	}
	t.Fatalf("missing result.value field in %#v", snapshot)
}

func assertConfigValue(t *testing.T, cfg configer.Config, path configer.Path, want any) {
	t.Helper()
	got, err := cfg.Get(path)
	if err != nil {
		t.Fatalf("Get(%v): %v", path, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Get(%v) = %#v, want %#v", path, got, want)
	}
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

func onlyRightPort(t *testing.T, snapshot pasta.WorkspaceSnapshot, node uint64) uint64 {
	t.Helper()
	ports := snapshot.Nodes[node].RightPorts
	if len(ports) != 1 {
		t.Fatalf("node %d right ports = %#v, want exactly one", node, ports)
	}
	return ports[0]
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

func assertConstantMenuForm(t *testing.T, state *formular.MenuSnapshotState, node uint64, want any, readonly bool) {
	t.Helper()
	snapshot, ok := state.Snapshot(pasta.NodeMenuID(node))
	if !ok {
		t.Fatalf("missing menu snapshot for node %d", node)
	}
	for _, block := range snapshot.Blocks {
		if block.ID != "state" {
			continue
		}
		if !block.Form {
			t.Fatalf("node %d state block Form = false, want true", node)
		}
		for _, item := range block.Items {
			if item.ID == "value" && item.Field != nil {
				if item.Field.Value != want || item.Field.Readonly != readonly {
					t.Fatalf("node %d menu field = value %#v readonly %v, want %#v %v", node, item.Field.Value, item.Field.Readonly, want, readonly)
				}
				return
			}
		}
	}
	t.Fatalf("missing state.value field in %#v", snapshot)
}

func assertSelectDataPortTypes(t *testing.T, w *pasta.Workspace, node uint64, want string) {
	t.Helper()
	snapshot := w.Snapshot()
	for _, spec := range []struct {
		direction string
		name      string
	}{
		{"left", "In 0"},
		{"left", "In 1"},
		{"right", "Out"},
	} {
		port := portByName(t, snapshot, node, spec.direction, spec.name)
		if got := snapshot.Ports[port].Types; !reflect.DeepEqual(got, []string{want}) {
			t.Fatalf("select %s types = %#v, want [%s]", spec.name, got, want)
		}
	}
}

func assertSelectMenu(t *testing.T, state *formular.MenuSnapshotState, node uint64, want bool) {
	t.Helper()
	snapshot, ok := state.Snapshot(pasta.NodeMenuID(node))
	if !ok {
		t.Fatalf("missing select menu snapshot for node %d", node)
	}
	for _, block := range snapshot.Blocks {
		if block.ID != "state" {
			continue
		}
		for _, item := range block.Items {
			if item.ID != "selector" || item.Field == nil {
				continue
			}
			got, ok := item.Field.Value.(bool)
			if !ok {
				t.Fatalf("select menu value has type %T", item.Field.Value)
			}
			if got != want || !item.Field.Readonly {
				t.Fatalf("select menu = %v readonly %v, want %v true", got, item.Field.Readonly, want)
			}
			return
		}
	}
	t.Fatalf("missing select selector field in %#v", snapshot)
}

func assertStringSplitPorts(t *testing.T, w *pasta.Workspace, node uint64) {
	t.Helper()
	snapshot := w.Snapshot()
	for _, spec := range []struct {
		direction string
		name      string
	}{
		{"left", "Text"},
		{"left", "Separator"},
		{"right", "Before"},
		{"right", "After"},
	} {
		port := portByName(t, snapshot, node, spec.direction, spec.name)
		if got := snapshot.Ports[port].Types; !reflect.DeepEqual(got, []string{TypeString}) {
			t.Fatalf("string split %s types = %#v, want [%s]", spec.name, got, TypeString)
		}
	}
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
		out := onlyRightPort(t, snapshot, id)
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
		out := onlyRightPort(t, snapshot, id)
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

type stdStringExpect struct {
	labels    map[string]string
	menus     map[string]string
	primary   map[string]string
	leftPorts map[string][]string
}

func expectStdStringGraph(t *testing.T, w *pasta.Workspace, menus *formular.MenuSnapshotState, nodes map[string]uint64, expect stdStringExpect) {
	t.Helper()
	snapshot := w.Snapshot()
	for name, want := range expect.labels {
		id := nodes[name]
		if got := snapshot.Nodes[id].Label; got != want {
			t.Fatalf("%s label = %q, want %q", name, got, want)
		}
	}
	for name, want := range expect.menus {
		got, readonly := stdStringMenuValue(t, menus, nodes[name])
		if got != want {
			t.Fatalf("%s menu value = %q, want %q", name, got, want)
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
		out := onlyRightPort(t, snapshot, id)
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
}

func stdStringMenuValue(t *testing.T, state *formular.MenuSnapshotState, node uint64) (string, bool) {
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
			value, ok := parseStringAny(item.Field.Value)
			if !ok {
				t.Fatalf("string menu value for node %d has type %T", node, item.Field.Value)
			}
			return value, item.Field.Readonly
		}
	}
	t.Fatalf("missing state.value field in node %d menu %#v", node, snapshot)
	return "", false
}

func isConstantClass(class string) bool {
	return class == NodeTypeIntConstant || class == NodeTypeFloatConstant ||
		class == NodeTypeStringConstant || class == NodeTypeBoolConstant ||
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
		out := onlyRightPort(t, snapshot, id)
		if got := snapshot.Ports[out].Types; !reflect.DeepEqual(got, []string{want}) {
			t.Fatalf("%s output types = %#v, want [%s]", name, got, want)
		}
	}
	for name, want := range expect.rightLinks {
		id := nodes[name]
		out := onlyRightPort(t, snapshot, id)
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
