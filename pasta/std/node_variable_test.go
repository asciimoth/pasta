package std

import (
	"testing"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

func TestVariableSetWritesOnlyOnTriggerAndGetReadsOnlyOnTrigger(t *testing.T) {
	w := newStdWorkspace(t)
	source := addByClass(t, w, NodeTypeIntConstant, "source")
	set := addByClass(t, w, NodeTypeIntSet, "set")
	get := addByClass(t, w, NodeTypeIntGet, "get")
	sum := addByClass(t, w, NodeTypeSum, "sum")
	setConstant(t, w, source, 10)
	setVariableSet(t, w, set, "answer", 0)
	setVariableGet(t, w, get, "answer")

	linkByPortName(t, w, source, "output", set, "Value")
	linkByPortName(t, w, set, "Trigger", get, "Trigger")
	linkByPortName(t, w, get, "Value", sum, "input 1")
	assertNodeLabel(t, w, sum, "0")

	triggerStdNode(t, w, get)
	assertNodeLabel(t, w, sum, "0")

	triggerStdNode(t, w, set)
	assertNodeLabel(t, w, sum, "10")

	setConstant(t, w, source, 20)
	triggerStdNode(t, w, get)
	assertNodeLabel(t, w, sum, "10")

	triggerStdNode(t, w, set)
	assertNodeLabel(t, w, sum, "20")
}

func TestVariableSetLabelShowsNameOnly(t *testing.T) {
	w := newStdWorkspace(t)
	source := addByClass(t, w, NodeTypeStringConstant, "source")
	set := addByClass(t, w, NodeTypeStringSet, "set")
	setConstant(t, w, source, "first")

	setVariableSet(t, w, set, "secret", "configured")
	assertNodeLabel(t, w, set, "secret")

	valueLink := linkByPortName(t, w, source, "output", set, "Value")
	assertNodeLabel(t, w, set, "secret")

	setConstant(t, w, source, "second")
	assertNodeLabel(t, w, set, "secret")

	w.RemoveLink(valueLink)
	assertNodeLabel(t, w, set, "secret")
}

func TestVariableNodesMirrorStdValueGraphs(t *testing.T) {
	t.Run("numeric", func(t *testing.T) {
		w := newStdWorkspace(t)
		trigger := addByClass(t, w, NodeTypeTrigger, "trigger")
		i2 := addByClass(t, w, NodeTypeIntConstant, "i2")
		i3 := addByClass(t, w, NodeTypeIntConstant, "i3")
		f15 := addByClass(t, w, NodeTypeFloatConstant, "f15")
		sumInt := addByClass(t, w, NodeTypeSum, "sumInt")
		sumFloat := addByClass(t, w, NodeTypeSum, "sumFloat")
		mulInt := addByClass(t, w, NodeTypeMul, "mulInt")
		setConstant(t, w, i2, 2)
		setConstant(t, w, i3, 3)
		setConstant(t, w, f15, 1.5)

		i2Get := linkVariablePair(t, w, trigger, i2, NodeTypeIntSet, NodeTypeIntGet, "A")
		i3Get := linkVariablePair(t, w, trigger, i3, NodeTypeIntSet, NodeTypeIntGet, "B")
		f15Get := linkVariablePair(t, w, trigger, f15, NodeTypeFloatSet, NodeTypeFloatGet, "F")
		linkByPortName(t, w, i2Get, "Value", sumInt, "input 1")
		linkByPortName(t, w, i3Get, "Value", sumInt, "input 2")
		linkByPortName(t, w, i2Get, "Value", mulInt, "input 1")
		linkByPortName(t, w, i3Get, "Value", mulInt, "input 2")
		linkByPortName(t, w, f15Get, "Value", sumFloat, "input 1")
		linkByPortName(t, w, i2Get, "Value", sumFloat, "input 2")

		assertNodeLabel(t, w, sumInt, "0")
		assertNodeLabel(t, w, sumFloat, "0")
		assertNodeLabel(t, w, mulInt, "1")

		triggerStdNode(t, w, trigger)
		assertNodeLabel(t, w, sumInt, "5")
		assertNodeLabel(t, w, sumFloat, "3.5")
		assertNodeLabel(t, w, mulInt, "6")
	})

	t.Run("string", func(t *testing.T) {
		w := newStdWorkspace(t)
		trigger := addByClass(t, w, NodeTypeTrigger, "trigger")
		hello := addByClass(t, w, NodeTypeStringConstant, "hello")
		space := addByClass(t, w, NodeTypeStringConstant, "space")
		world := addByClass(t, w, NodeTypeStringConstant, "world")
		substr := addByClass(t, w, NodeTypeStringConstant, "substr")
		concat := addByClass(t, w, NodeTypeStringConcat, "concat")
		upper := addByClass(t, w, NodeTypeStringUpper, "upper")
		trim := addByClass(t, w, NodeTypeStringTrimSpace, "trim")
		lower := addByClass(t, w, NodeTypeStringLower, "lower")
		length := addByClass(t, w, NodeTypeStringLength, "length")
		contains := addByClass(t, w, NodeTypeStringContains, "contains")
		setConstant(t, w, hello, " Hello")
		setConstant(t, w, space, ", ")
		setConstant(t, w, world, "World ")
		setConstant(t, w, substr, "WORLD")

		helloGet := linkVariablePair(t, w, trigger, hello, NodeTypeStringSet, NodeTypeStringGet, "hello")
		spaceGet := linkVariablePair(t, w, trigger, space, NodeTypeStringSet, NodeTypeStringGet, "space")
		worldGet := linkVariablePair(t, w, trigger, world, NodeTypeStringSet, NodeTypeStringGet, "world")
		substrGet := linkVariablePair(t, w, trigger, substr, NodeTypeStringSet, NodeTypeStringGet, "substr")
		linkByPortName(t, w, helloGet, "Value", concat, "input 1")
		linkByPortName(t, w, spaceGet, "Value", concat, "input 2")
		linkByPortName(t, w, worldGet, "Value", concat, "input 3")
		linkByPortName(t, w, concat, "output", upper, "input 1")
		linkByPortName(t, w, upper, "output", trim, "input 1")
		linkByPortName(t, w, trim, "output", lower, "input 1")
		linkByPortName(t, w, trim, "output", length, "input 1")
		linkByPortName(t, w, trim, "output", contains, "input 1")
		linkByPortName(t, w, substrGet, "Value", contains, "input 2")

		triggerStdNode(t, w, trigger)
		assertNodeLabel(t, w, concat, " Hello, World ")
		assertNodeLabel(t, w, upper, " HELLO, WORLD ")
		assertNodeLabel(t, w, trim, "HELLO, WORLD")
		assertNodeLabel(t, w, lower, "hello, world")
		assertNodeLabel(t, w, length, "12")
		assertNodeLabel(t, w, contains, "true")
	})

	t.Run("bool", func(t *testing.T) {
		w := newStdWorkspace(t)
		trigger := addByClass(t, w, NodeTypeTrigger, "trigger")
		trueNode := addByClass(t, w, NodeTypeTrueConstant, "true")
		falseNode := addByClass(t, w, NodeTypeFalseConstant, "false")
		andNode := addByClass(t, w, NodeTypeBoolAnd, "and")
		orNode := addByClass(t, w, NodeTypeBoolOr, "or")
		notNode := addByClass(t, w, NodeTypeBoolNot, "not")

		trueGet := linkVariablePair(t, w, trigger, trueNode, NodeTypeBoolSet, NodeTypeBoolGet, "T")
		falseGet := linkVariablePair(t, w, trigger, falseNode, NodeTypeBoolSet, NodeTypeBoolGet, "F")
		linkByPortName(t, w, trueGet, "Value", andNode, "input 1")
		linkByPortName(t, w, falseGet, "Value", andNode, "input 2")
		linkByPortName(t, w, trueGet, "Value", orNode, "input 1")
		linkByPortName(t, w, falseGet, "Value", orNode, "input 2")
		linkByPortName(t, w, trueGet, "Value", notNode, "input 1")

		triggerStdNode(t, w, trigger)
		assertNodeLabel(t, w, andNode, "false")
		assertNodeLabel(t, w, orNode, "true")
		assertNodeLabel(t, w, notNode, "false")
	})
}

func TestVariableNamespacesAreUniquePerTypeAndObjectVariablesWork(t *testing.T) {
	w := newStdWorkspace(t)
	intSet := addByClass(t, w, NodeTypeIntSet, "intSet")
	intGet := addByClass(t, w, NodeTypeIntGet, "intGet")
	sum := addByClass(t, w, NodeTypeSum, "sum")
	stringSet := addByClass(t, w, NodeTypeStringSet, "stringSet")
	stringGet := addByClass(t, w, NodeTypeStringGet, "stringGet")
	concat := addByClass(t, w, NodeTypeStringConcat, "concat")
	objectSet := addByClass(t, w, NodeTypeObjectSet, "objectSet")
	objectGet := addByClass(t, w, NodeTypeObjectGet, "objectGet")
	stringer := addByClass(t, w, NodeTypeObjectToString, "stringer")

	setVariableSet(t, w, intSet, "A", 7)
	setVariableGet(t, w, intGet, "A")
	setVariableSet(t, w, stringSet, "A", "seven")
	setVariableGet(t, w, stringGet, "A")
	setVariableSet(t, w, objectSet, "A", `{"name":"object A","count":7}`)
	setVariableGet(t, w, objectGet, "A")
	linkByPortName(t, w, intGet, "Value", sum, "input 1")
	linkByPortName(t, w, stringGet, "Value", concat, "input 1")
	linkByPortName(t, w, objectGet, "Value", stringer, "input")

	triggerStdNode(t, w, intSet)
	triggerStdNode(t, w, intGet)
	triggerStdNode(t, w, stringSet)
	triggerStdNode(t, w, stringGet)
	triggerStdNode(t, w, objectSet)
	triggerStdNode(t, w, objectGet)
	assertNodeLabel(t, w, sum, "7")
	assertNodeLabel(t, w, concat, "seven")
	assertObjectStringContains(t, w, stringer, `"name":"object A"`, `"count":7`)
}

func TestVariableGetRequestReplayAndAttachDetachReattach(t *testing.T) {
	w := newStdWorkspace(t)
	source := addByClass(t, w, NodeTypeIntConstant, "source")
	set := addByClass(t, w, NodeTypeIntSet, "set")
	get := addByClass(t, w, NodeTypeIntGet, "get")
	sum := addByClass(t, w, NodeTypeSum, "sum")
	setConstant(t, w, source, 4)
	setVariableSet(t, w, set, "cached", 0)
	setVariableGet(t, w, get, "cached")
	linkByPortName(t, w, source, "output", set, "Value")
	valueLink := linkByPortName(t, w, get, "Value", sum, "input 1")

	w.EmitEvent(sum, valueLink, RequestValue{})
	assertNodeLabel(t, w, sum, "0")

	triggerStdNode(t, w, set)
	triggerStdNode(t, w, get)
	assertNodeLabel(t, w, sum, "4")

	setConstant(t, w, source, 9)
	triggerStdNode(t, w, set)
	w.EmitEvent(sum, valueLink, RequestValue{})
	assertNodeLabel(t, w, sum, "4")

	triggerStdNode(t, w, get)
	assertNodeLabel(t, w, sum, "9")

	w.RemoveLink(valueLink)
	assertNodeLabel(t, w, sum, "0")
	valueLink = linkByPortName(t, w, get, "Value", sum, "input 1")
	assertNodeLabel(t, w, sum, "0")
	w.EmitEvent(sum, valueLink, RequestValue{})
	assertNodeLabel(t, w, sum, "0")

	triggerStdNode(t, w, get)
	assertNodeLabel(t, w, sum, "9")
}

func TestVariableNodesThroughSelectSelectOutAndGateway(t *testing.T) {
	w := newStdWorkspace(t)
	a := addByClass(t, w, NodeTypeIntConstant, "a")
	b := addByClass(t, w, NodeTypeIntConstant, "b")
	selector := addByClass(t, w, NodeTypeTrueConstant, "selector")
	selectNode := addByClass(t, w, NodeTypeSelect, "select")
	setGateway := addByClass(t, w, NodeTypeGateway, "setGateway")
	set := addByClass(t, w, NodeTypeIntSet, "set")
	get := addByClass(t, w, NodeTypeIntGet, "get")
	selectOut := addByClass(t, w, NodeTypeSelectOut, "selectOut")
	getGateway := addByClass(t, w, NodeTypeGateway, "getGateway")
	sum := addByClass(t, w, NodeTypeSum, "sum")
	setConstant(t, w, a, 4)
	setConstant(t, w, b, 8)
	setVariableSet(t, w, set, "routed", 0)
	setVariableGet(t, w, get, "routed")

	linkByPortName(t, w, a, "output", selectNode, "In 0")
	linkByPortName(t, w, b, "output", selectNode, "In 1")
	linkByPortName(t, w, selector, "output", selectNode, "Selector")
	linkByPortName(t, w, selectNode, "Out", setGateway, "In")
	linkByPortName(t, w, setGateway, "Out", set, "Value")
	linkByPortName(t, w, set, "Trigger", get, "Trigger")
	linkByPortName(t, w, get, "Value", selectOut, "In")
	linkByPortName(t, w, selectOut, "Out 0", getGateway, "In")
	linkByPortName(t, w, getGateway, "Out", sum, "input 1")

	assertNodeLabel(t, w, sum, "0")
	triggerStdNode(t, w, setGateway)
	triggerStdNode(t, w, set)
	triggerStdNode(t, w, getGateway)
	assertNodeLabel(t, w, sum, "8")
}

func TestVariableNodesSaveRestoreCopyPasteAndRefCounting(t *testing.T) {
	w := newStdWorkspace(t)
	trigger := addByClass(t, w, NodeTypeTrigger, "trigger")
	set := addByClass(t, w, NodeTypeIntSet, "set")
	get := addByClass(t, w, NodeTypeIntGet, "get")
	sum := addByClass(t, w, NodeTypeSum, "sum")
	setVariableSet(t, w, set, "saved", 6)
	setVariableGet(t, w, get, "saved")
	linkByPortName(t, w, trigger, "Trigger", set, "Trigger")
	linkByPortName(t, w, set, "Trigger", get, "Trigger")
	linkByPortName(t, w, get, "Value", sum, "input 1")
	triggerStdNode(t, w, trigger)
	assertNodeLabel(t, w, sum, "6")

	cfg := configer.NewMemory(nil)
	if err := w.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig variables: %v", err)
	}
	assertConfigValue(t, cfg, configer.Path{"set", "name"}, "saved")
	assertConfigIntValue(t, cfg, configer.Path{"set", "value"}, 6)
	assertConfigValue(t, cfg, configer.Path{"get", "name"}, "saved")

	restored, err := pasta.WorkspaceFromConfig(allStdClasses(), cfg, testLogFactory{})
	if err != nil {
		t.Fatalf("WorkspaceFromConfig variables: %v", err)
	}
	restoredTrigger, _ := restored.NodeIDByName("trigger")
	restoredSum, _ := restored.NodeIDByName("sum")
	triggerStdNode(t, restored, restoredTrigger)
	assertNodeLabel(t, restored, restoredSum, "6")

	restoredSet, _ := restored.NodeIDByName("set")
	restoredGet, _ := restored.NodeIDByName("get")
	clip := restored.Copy([]uint64{restoredTrigger, restoredSet, restoredGet, restoredSum})
	pasted := restored.Paste(clip)
	if len(pasted) != 4 {
		t.Fatalf("Paste variable graph returned %v, want four nodes", pasted)
	}
	pastedTrigger := pastedNodeByClass(t, restored, pasted, NodeTypeTrigger)
	pastedSum := pastedNodeByClass(t, restored, pasted, NodeTypeSum)
	triggerStdNode(t, restored, pastedTrigger)
	assertNodeLabel(t, restored, pastedSum, "6")

	restored.RemoveNode(restoredSet)
	restored.RemoveNode(restoredGet)
	restored.RemoveNode(pastedNodeByClass(t, restored, pasted, NodeTypeIntSet))
	restored.RemoveNode(pastedNodeByClass(t, restored, pasted, NodeTypeIntGet))
	freshGet := addByClass(t, restored, NodeTypeIntGet, "freshGet")
	setVariableGet(t, restored, freshGet, "saved")
	freshSum := addByClass(t, restored, NodeTypeSum, "freshSum")
	linkByPortName(t, restored, freshGet, "Value", freshSum, "input 1")
	triggerStdNode(t, restored, freshGet)
	assertNodeLabel(t, restored, freshSum, "0")
}

func setVariableSet(t *testing.T, w *pasta.Workspace, node uint64, name string, value any) {
	t.Helper()
	w.SendNodeFormularMsg(node, formular.FormApplyMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageFormApply, MenuID: pasta.NodeMenuID(node), MenuGeneration: 1},
		BlockID:     variableMenuBlock,
		Values:      map[string]any{"name": name, "value": value},
	})
}

func assertConfigIntValue(t *testing.T, cfg configer.Config, path configer.Path, want int) {
	t.Helper()
	raw, err := cfg.Get(path)
	if err != nil {
		t.Fatalf("Get(%v): %v", path, err)
	}
	got, ok := parseIntAny(raw)
	if !ok || got != want {
		t.Fatalf("Get(%v) = %#v, want int %d", path, raw, want)
	}
}

func setVariableGet(t *testing.T, w *pasta.Workspace, node uint64, name string) {
	t.Helper()
	w.SendNodeFormularMsg(node, formular.FormApplyMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageFormApply, MenuID: pasta.NodeMenuID(node), MenuGeneration: 1},
		BlockID:     variableMenuBlock,
		Values:      map[string]any{"name": name},
	})
}

func triggerStdNode(t *testing.T, w *pasta.Workspace, node uint64) {
	t.Helper()
	if err := w.Trigger(node); err != nil {
		t.Fatalf("Trigger node %d: %v", node, err)
	}
}

func linkVariablePair(t *testing.T, w *pasta.Workspace, trigger, source uint64, setClass, getClass, name string) uint64 {
	t.Helper()
	set := addByClass(t, w, setClass, name+" set")
	get := addByClass(t, w, getClass, name+" get")
	setVariableSet(t, w, set, name, variableDefaultValueForClass(setClass))
	setVariableGet(t, w, get, name)
	linkByPortName(t, w, source, "output", set, "Value")
	linkByPortName(t, w, trigger, "Trigger", set, "Trigger")
	linkByPortName(t, w, set, "Trigger", get, "Trigger")
	return get
}

func variableDefaultValueForClass(class string) any {
	switch class {
	case NodeTypeBoolSet:
		return false
	case NodeTypeFloatSet:
		return 0.0
	case NodeTypeStringSet:
		return ""
	case NodeTypeObjectSet:
		return "null"
	default:
		return 0
	}
}
