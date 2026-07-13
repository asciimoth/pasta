package std

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
	"github.com/asciimoth/pasta/pasta"
)

func TestStdObjectConstantMenuSaveRestoreAndInvalidJSON(t *testing.T) {
	w := newStdWorkspace(t)
	node := addByClass(t, w, NodeTypeObjectConstant, "object")

	state := formular.NewMenuSnapshotState()
	var forced bool
	sub := w.SubscribeNotifications(func(notification pasta.WorkspaceNotification) {
		if notification.Kind != pasta.NotificationNodeMenu {
			return
		}
		if msg, ok := notification.Formular.(formular.MenuSnapshotMessage); ok && msg.Force {
			forced = true
		}
		state.Apply(notification.Formular)
	})
	if !w.SubscribeNodeMenu(node, sub) {
		t.Fatalf("SubscribeNodeMenu(%d) returned false", node)
	}

	setObjectConstant(t, w, node, `{"name":"Ada","scores":[1,2,null]}`)
	want := objectFromJSONForTest(t, `{"name":"Ada","scores":[1,2,null]}`)
	got := objectMenuValue(t, state, node)
	if !got.Equal(want) {
		t.Fatalf("object menu value = %s, want %s", got.JSONString(), want.JSONString())
	}
	if got := w.Snapshot().Nodes[node].Label; got != "" {
		t.Fatalf("object constant label = %q, want empty", got)
	}

	before := w.Snapshot().Nodes[node].Label
	setObjectConstant(t, w, node, `{"name":`)
	if got := w.Snapshot().Nodes[node].Label; got != before {
		t.Fatalf("label after invalid JSON = %q, want %q", got, before)
	}
	if !forced {
		t.Fatal("invalid JSON did not force a menu snapshot")
	}
	got = objectMenuValue(t, state, node)
	if !got.Equal(want) {
		t.Fatalf("object menu value after invalid JSON = %s, want %s", got.JSONString(), want.JSONString())
	}

	cfg := configer.NewMemory(nil)
	if err := w.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig object constant: %v", err)
	}
	raw, err := cfg.Get(configer.Path{"object", "value"})
	if err != nil {
		t.Fatalf("saved object value missing: %v", err)
	}
	saved, ok := objectFromConfigValue(raw)
	if !ok || !saved.Equal(want) {
		t.Fatalf("saved object = %#v, want %s", raw, want.JSONString())
	}

	restored, err := pasta.WorkspaceFromConfig(allStdClasses(), cfg, testLogFactory{})
	if err != nil {
		t.Fatalf("WorkspaceFromConfig object constant: %v", err)
	}
	restoredNode, ok := restored.NodeIDByName("object")
	if !ok {
		t.Fatal("restored object constant missing")
	}
	restoredState := subscribeStdMenus(t, restored, map[string]uint64{"object": restoredNode})
	got = objectMenuValue(t, restoredState, restoredNode)
	if !got.Equal(want) {
		t.Fatalf("restored object menu value = %s, want %s", got.JSONString(), want.JSONString())
	}
}

func TestStdObjectPackerUnpackerDetachCopyPasteAndRestore(t *testing.T) {
	w := newStdWorkspace(t)
	nodes := map[string]uint64{
		"name":     addByClass(t, w, NodeTypeStringConstant, "name"),
		"count":    addByClass(t, w, NodeTypeIntConstant, "count"),
		"one":      addByClass(t, w, NodeTypeIntConstant, "one"),
		"flag":     addByClass(t, w, NodeTypeTrueConstant, "flag"),
		"base":     addByClass(t, w, NodeTypeObjectConstant, "base"),
		"raw":      addByClass(t, w, NodeTypeObjectConstant, "raw"),
		"packer":   addByClass(t, w, NodeTypeObjectPacker, "packer"),
		"unpacker": addByClass(t, w, NodeTypeObjectUnpacker, "unpacker"),
		"stringer": addByClass(t, w, NodeTypeObjectToString, "stringer"),
		"upper":    addByClass(t, w, NodeTypeStringUpper, "upper"),
		"sum":      addByClass(t, w, NodeTypeSum, "sum"),
		"and":      addByClass(t, w, NodeTypeBoolAnd, "and"),
	}
	setConstant(t, w, nodes["name"], "ada")
	setConstant(t, w, nodes["count"], 7)
	setConstant(t, w, nodes["one"], 1)
	setObjectConstant(t, w, nodes["base"], `{"person":{"title":"base","count":1},"flags":[false,true],"keep":true,"raw":{"base":true}}`)
	setObjectConstant(t, w, nodes["raw"], `{"meta":{"source":"constant"}}`)
	setObjectPackerConfig(t, w, nodes["packer"], objectKindMap, []objectPackerFieldSpec{
		{id: "name", name: "Name", typ: TypeString, path: `["person","name"]`},
		{id: "count", name: "Count", typ: TypeInt, path: `["person","count"]`},
		{id: "flag", name: "Flag", typ: TypeBool, path: `["flags",0]`},
		{id: "raw", name: "Raw", typ: TypeObject, path: `["raw"]`},
	}, []objectContainerSpec{
		{id: "person", path: `["person"]`, kind: objectKindMap},
		{id: "flags", path: `["flags"]`, kind: objectKindVector},
	}, objectDeletePathSpec{
		id: "title", path: `["person","title"]`,
	}, objectDeletePathSpec{
		id: "keep", path: `["keep"]`,
	}, objectDeletePathSpec{
		id: "raw-base", path: `["raw","base"]`,
	})
	setObjectUnpackerConfig(t, w, nodes["unpacker"], []objectUnpackerOutputSpec{
		{id: "name", name: "Name", typ: TypeString, path: `["person","name"]`, def: "missing"},
		{id: "count", name: "Count", typ: TypeInt, path: `["person","count"]`, def: 100},
		{id: "title", name: "Title", typ: TypeString, path: `["person","title"]`, def: "missing"},
		{id: "flag", name: "Flag", typ: TypeBool, path: `["flags",0]`, def: false},
		{id: "raw", name: "Raw", typ: TypeObject, path: `["raw"]`},
	})

	linkByPortName(t, w, nodes["base"], "output", nodes["packer"], "Base")
	linkByPortName(t, w, nodes["name"], "output", nodes["packer"], "In Name")
	countLink := linkByPortName(t, w, nodes["count"], "output", nodes["packer"], "In Count")
	linkByPortName(t, w, nodes["flag"], "output", nodes["packer"], "In Flag")
	linkByPortName(t, w, nodes["raw"], "output", nodes["packer"], "In Raw")
	objectLink := linkByPortName(t, w, nodes["packer"], "output", nodes["unpacker"], "input")
	linkByPortName(t, w, nodes["packer"], "output", nodes["stringer"], "input")
	linkByPortName(t, w, nodes["unpacker"], "Out Name", nodes["upper"], "input 1")
	linkByPortName(t, w, nodes["unpacker"], "Out Count", nodes["sum"], "input 1")
	linkByPortName(t, w, nodes["one"], "output", nodes["sum"], "input 2")
	linkByPortName(t, w, nodes["unpacker"], "Out Flag", nodes["and"], "input 1")
	linkByPortName(t, w, nodes["flag"], "output", nodes["and"], "input 2")
	assertObjectDerivedLabels(t, w, nodes, "ADA", "8", "true")
	assertObjectStringContains(t, w, nodes["stringer"], `"source":"constant"`)
	assertObjectStringOmits(t, w, nodes["stringer"], `"title":"base"`, `"keep":true`, `"base":true`)

	_, _, err := w.AddLink(
		portByName(t, w.Snapshot(), nodes["raw"], "right", "output"),
		portByName(t, w.Snapshot(), nodes["unpacker"], "left", "input"),
	)
	if !errors.Is(err, pasta.ErrLinkDup) {
		t.Fatalf("second object input link error = %v, want %v", err, pasta.ErrLinkDup)
	}

	w.RemoveLink(objectLink)
	assertObjectDerivedLabels(t, w, nodes, "MISSING", "101", "false")
	objectLink = linkByPortName(t, w, nodes["packer"], "output", nodes["unpacker"], "input")
	assertObjectDerivedLabels(t, w, nodes, "ADA", "8", "true")

	w.RemoveLink(countLink)
	assertObjectDerivedLabels(t, w, nodes, "ADA", "101", "true")
	linkByPortName(t, w, nodes["count"], "output", nodes["packer"], "In Count")
	assertObjectDerivedLabels(t, w, nodes, "ADA", "8", "true")
	_ = objectLink

	cfg := configer.NewMemory(nil)
	if err := w.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig object graph: %v", err)
	}
	rawFields, err := cfg.Get(configer.Path{"packer", "fields"})
	if err != nil {
		t.Fatalf("saved packer fields missing: %v", err)
	}
	if fields, ok := rawFields.([]any); !ok || len(fields) != 4 {
		t.Fatalf("saved packer fields = %#v, want four fields", rawFields)
	}
	rawDeletes, err := cfg.Get(configer.Path{"packer", "delete_paths"})
	if err != nil {
		t.Fatalf("saved packer delete paths missing: %v", err)
	}
	if deletes, ok := rawDeletes.([]any); !ok || len(deletes) != 3 {
		t.Fatalf("saved packer delete paths = %#v, want three paths", rawDeletes)
	}
	restored, err := pasta.WorkspaceFromConfig(allStdClasses(), cfg, testLogFactory{})
	if err != nil {
		t.Fatalf("WorkspaceFromConfig object graph: %v", err)
	}
	restoredNodes := map[string]uint64{}
	for name := range nodes {
		id, ok := restored.NodeIDByName(name)
		if !ok {
			t.Fatalf("restored node %q missing", name)
		}
		restoredNodes[name] = id
	}
	assertLeftPortNames(t, restored, restoredNodes["packer"], []string{"Base", "In Name", "In Count", "In Flag", "In Raw"})
	assertRightPortNames(t, restored, restoredNodes["unpacker"], []string{"Out Name", "Out Count", "Out Title", "Out Flag", "Out Raw"})
	assertObjectDerivedLabels(t, restored, restoredNodes, "ADA", "8", "true")
	assertObjectStringContains(t, restored, restoredNodes["stringer"], `"source":"constant"`)
	assertObjectStringOmits(t, restored, restoredNodes["stringer"], `"title":"base"`, `"keep":true`, `"base":true`)

	clip := w.Copy([]uint64{
		nodes["name"], nodes["count"], nodes["one"], nodes["flag"], nodes["base"], nodes["raw"],
		nodes["packer"], nodes["unpacker"], nodes["stringer"], nodes["upper"], nodes["sum"], nodes["and"],
	})
	pasted := w.Paste(clip)
	if len(pasted) != 12 {
		t.Fatalf("Paste object graph returned %v, want twelve nodes", pasted)
	}
	pastedByClass := map[string]uint64{
		NodeTypeStringUpper:    pastedNodeByClass(t, w, pasted, NodeTypeStringUpper),
		NodeTypeSum:            pastedNodeByClass(t, w, pasted, NodeTypeSum),
		NodeTypeBoolAnd:        pastedNodeByClass(t, w, pasted, NodeTypeBoolAnd),
		NodeTypeObjectPacker:   pastedNodeByClass(t, w, pasted, NodeTypeObjectPacker),
		NodeTypeObjectUnpacker: pastedNodeByClass(t, w, pasted, NodeTypeObjectUnpacker),
		NodeTypeObjectToString: pastedNodeByClass(t, w, pasted, NodeTypeObjectToString),
	}
	pastedNodes := map[string]uint64{
		"upper": pastedByClass[NodeTypeStringUpper],
		"sum":   pastedByClass[NodeTypeSum],
		"and":   pastedByClass[NodeTypeBoolAnd],
	}
	assertObjectDerivedLabels(t, w, pastedNodes, "ADA", "8", "true")
	assertLeftPortNames(t, w, pastedByClass[NodeTypeObjectPacker], []string{"Base", "In Name", "In Count", "In Flag", "In Raw"})
	assertRightPortNames(t, w, pastedByClass[NodeTypeObjectUnpacker], []string{"Out Name", "Out Count", "Out Title", "Out Flag", "Out Raw"})
	assertObjectStringContains(t, w, pastedByClass[NodeTypeObjectToString], `"source":"constant"`)
	assertObjectStringOmits(t, w, pastedByClass[NodeTypeObjectToString], `"title":"base"`, `"keep":true`, `"base":true`)
}

func TestStdObjectPackerDeletesBasePaths(t *testing.T) {
	w := newStdWorkspace(t)
	nodes := map[string]uint64{
		"base":     addByClass(t, w, NodeTypeObjectConstant, "base"),
		"packer":   addByClass(t, w, NodeTypeObjectPacker, "packer"),
		"stringer": addByClass(t, w, NodeTypeObjectToString, "stringer"),
	}
	setObjectConstant(t, w, nodes["base"], `{"keep":"yes","drop":{"nested":true},"items":["a","b","c"],"meta":{"secret":"x","public":"y"}}`)
	setObjectPackerConfig(t, w, nodes["packer"], objectKindMap, nil, []objectContainerSpec{
		{id: "items", path: `["items"]`, kind: objectKindVector},
	},
		objectDeletePathSpec{id: "drop", path: `["drop"]`},
		objectDeletePathSpec{id: "secret", path: `["meta","secret"]`},
		objectDeletePathSpec{id: "middle", path: `["items",1]`},
	)
	linkByPortName(t, w, nodes["base"], "output", nodes["packer"], "Base")
	linkByPortName(t, w, nodes["packer"], "output", nodes["stringer"], "input")

	assertObjectStringContains(t, w, nodes["stringer"], `"keep":"yes"`, `"items":["a","c"]`, `"public":"y"`)
	assertObjectStringOmits(t, w, nodes["stringer"], `"drop"`, `"secret"`, `"b"`)

	setObjectPackerConfig(t, w, nodes["packer"], objectKindMap, []objectPackerFieldSpec{
		{id: "drop", name: "Drop", typ: TypeString, path: `["drop"]`},
	}, nil,
		objectDeletePathSpec{id: "drop", path: `["drop"]`},
	)
	source := addByClass(t, w, NodeTypeStringConstant, "source")
	setConstant(t, w, source, "replacement")
	linkByPortName(t, w, source, "output", nodes["packer"], "In Drop")
	assertObjectStringContains(t, w, nodes["stringer"], `"drop":"replacement"`)
	assertObjectStringOmits(t, w, nodes["stringer"], `"nested":true`)
}

func TestStdObjectPackerAppendsToBaseVectors(t *testing.T) {
	w := newStdWorkspace(t)
	nodes := map[string]uint64{
		"base":     addByClass(t, w, NodeTypeObjectConstant, "base"),
		"name":     addByClass(t, w, NodeTypeStringConstant, "name"),
		"count":    addByClass(t, w, NodeTypeIntConstant, "count"),
		"tag":      addByClass(t, w, NodeTypeStringConstant, "tag"),
		"packer":   addByClass(t, w, NodeTypeObjectPacker, "packer"),
		"stringer": addByClass(t, w, NodeTypeObjectToString, "stringer"),
	}
	setObjectConstant(t, w, nodes["base"], `{"items":["base"],"meta":{"keep":true}}`)
	setConstant(t, w, nodes["name"], "ada")
	setConstant(t, w, nodes["count"], 7)
	setConstant(t, w, nodes["tag"], "done")
	setObjectPackerConfig(t, w, nodes["packer"], objectKindMap, []objectPackerFieldSpec{
		{id: "name", name: "Name", typ: TypeString, path: `["items"]`, operation: objectPackerOperationAppend},
		{id: "count", name: "Count", typ: TypeInt, path: `["items"]`, operation: objectPackerOperationAppend},
		{id: "tag", name: "Tag", typ: TypeString, path: `["meta","tag"]`},
	}, nil)

	linkByPortName(t, w, nodes["base"], "output", nodes["packer"], "Base")
	linkByPortName(t, w, nodes["name"], "output", nodes["packer"], "In Name")
	linkByPortName(t, w, nodes["count"], "output", nodes["packer"], "In Count")
	linkByPortName(t, w, nodes["tag"], "output", nodes["packer"], "In Tag")
	linkByPortName(t, w, nodes["packer"], "output", nodes["stringer"], "input")
	assertObjectStringContains(t, w, nodes["stringer"], `"items":["base","ada",7]`, `"keep":true`, `"tag":"done"`)

	cfg := configer.NewMemory(nil)
	if err := w.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig object append graph: %v", err)
	}
	rawFields, err := cfg.Get(configer.Path{"packer", "fields"})
	if err != nil {
		t.Fatalf("saved append packer fields missing: %v", err)
	}
	fields, ok := rawFields.([]any)
	if !ok || len(fields) != 3 {
		t.Fatalf("saved append packer fields = %#v, want three fields", rawFields)
	}
	for i, want := range []string{objectPackerOperationAppend, objectPackerOperationAppend, objectPackerOperationSet} {
		field, ok := fields[i].(map[string]any)
		if !ok {
			t.Fatalf("saved append field %d = %#v, want map", i, fields[i])
		}
		if got := field["operation"]; got != want {
			t.Fatalf("saved append field %d operation = %#v, want %q", i, got, want)
		}
	}

	restored, err := pasta.WorkspaceFromConfig(allStdClasses(), cfg, testLogFactory{})
	if err != nil {
		t.Fatalf("WorkspaceFromConfig object append graph: %v", err)
	}
	restoredStringer, ok := restored.NodeIDByName("stringer")
	if !ok {
		t.Fatal("restored stringer missing")
	}
	assertObjectStringContains(t, restored, restoredStringer, `"items":["base","ada",7]`, `"keep":true`, `"tag":"done"`)
}

func TestStdObjectPackerAppendsToRootVector(t *testing.T) {
	w := newStdWorkspace(t)
	nodes := map[string]uint64{
		"base":     addByClass(t, w, NodeTypeObjectConstant, "base"),
		"tail":     addByClass(t, w, NodeTypeStringConstant, "tail"),
		"packer":   addByClass(t, w, NodeTypeObjectPacker, "packer"),
		"stringer": addByClass(t, w, NodeTypeObjectToString, "stringer"),
	}
	setObjectConstant(t, w, nodes["base"], `[1,2]`)
	setConstant(t, w, nodes["tail"], "tail")
	setObjectPackerConfig(t, w, nodes["packer"], objectKindVector, []objectPackerFieldSpec{
		{id: "tail", name: "Tail", typ: TypeString, path: `[]`, operation: objectPackerOperationAppend},
	}, nil)

	linkByPortName(t, w, nodes["base"], "output", nodes["packer"], "Base")
	linkByPortName(t, w, nodes["tail"], "output", nodes["packer"], "In Tail")
	linkByPortName(t, w, nodes["packer"], "output", nodes["stringer"], "input")
	assertObjectStringContains(t, w, nodes["stringer"], `[1,2,"tail"]`)
}

func TestStdObjectLinksThroughSelectSelectOutAndGateway(t *testing.T) {
	w := newStdWorkspace(t)
	nodes := map[string]uint64{
		"zero":       addByClass(t, w, NodeTypeObjectConstant, "zero"),
		"one":        addByClass(t, w, NodeTypeObjectConstant, "one"),
		"selector":   addByClass(t, w, NodeTypeTrueConstant, "selector"),
		"select":     addByClass(t, w, NodeTypeSelect, "select"),
		"unpack":     addByClass(t, w, NodeTypeObjectUnpacker, "unpack"),
		"upper":      addByClass(t, w, NodeTypeStringUpper, "upper"),
		"selectOut":  addByClass(t, w, NodeTypeSelectOut, "selectOut"),
		"unpackOut":  addByClass(t, w, NodeTypeObjectUnpacker, "unpackOut"),
		"upperOut":   addByClass(t, w, NodeTypeStringUpper, "upperOut"),
		"gateway":    addByClass(t, w, NodeTypeGateway, "gateway"),
		"trigger":    addByClass(t, w, NodeTypeTrigger, "trigger"),
		"unpackGate": addByClass(t, w, NodeTypeObjectUnpacker, "unpackGate"),
		"upperGate":  addByClass(t, w, NodeTypeStringUpper, "upperGate"),
	}
	setObjectConstant(t, w, nodes["zero"], `{"name":"zero"}`)
	setObjectConstant(t, w, nodes["one"], `{"name":"one"}`)
	for _, name := range []string{"unpack", "unpackOut", "unpackGate"} {
		setObjectUnpackerConfig(t, w, nodes[name], []objectUnpackerOutputSpec{
			{id: "name", name: "Name", typ: TypeString, path: `["name"]`, def: "missing"},
		})
	}

	linkByPortName(t, w, nodes["zero"], "output", nodes["select"], "In 0")
	linkByPortName(t, w, nodes["one"], "output", nodes["select"], "In 1")
	linkByPortName(t, w, nodes["select"], "Out", nodes["unpack"], "input")
	linkByPortName(t, w, nodes["unpack"], "Out Name", nodes["upper"], "input 1")
	if got := w.Snapshot().Nodes[nodes["upper"]].Label; got != "ZERO" {
		t.Fatalf("select initial label = %q, want ZERO", got)
	}
	linkByPortName(t, w, nodes["selector"], "output", nodes["select"], "Selector")
	if got := w.Snapshot().Nodes[nodes["upper"]].Label; got != "ONE" {
		t.Fatalf("select switched label = %q, want ONE", got)
	}
	assertSelectDataPortTypes(t, w, nodes["select"], TypeObject)

	linkByPortName(t, w, nodes["one"], "output", nodes["selectOut"], "In")
	linkByPortName(t, w, nodes["selector"], "output", nodes["selectOut"], "Selector")
	linkByPortName(t, w, nodes["selectOut"], "Out 1", nodes["unpackOut"], "input")
	linkByPortName(t, w, nodes["unpackOut"], "Out Name", nodes["upperOut"], "input 1")
	if got := w.Snapshot().Nodes[nodes["upperOut"]].Label; got != "ONE" {
		t.Fatalf("selectOut label = %q, want ONE", got)
	}

	linkByPortName(t, w, nodes["zero"], "output", nodes["gateway"], "In")
	linkByPortName(t, w, nodes["gateway"], "Out", nodes["unpackGate"], "input")
	linkByPortName(t, w, nodes["unpackGate"], "Out Name", nodes["upperGate"], "input 1")
	if got := w.Snapshot().Nodes[nodes["upperGate"]].Label; got != "MISSING" {
		t.Fatalf("gateway label before trigger = %q, want MISSING", got)
	}
	_, _, err := w.AddLink(
		portByName(t, w.Snapshot(), nodes["one"], "right", "output"),
		portByName(t, w.Snapshot(), nodes["gateway"], "left", "In"),
	)
	if !errors.Is(err, pasta.ErrLinkDup) {
		t.Fatalf("second gateway object input link error = %v, want %v", err, pasta.ErrLinkDup)
	}
	linkByPortName(t, w, nodes["trigger"], "Trigger", nodes["gateway"], "Trigger")
	if err := w.Trigger(nodes["trigger"]); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if got := w.Snapshot().Nodes[nodes["upperGate"]].Label; got != "ZERO" {
		t.Fatalf("gateway label after trigger = %q, want ZERO", got)
	}
}

func TestStdObjectToStringFormatsAndOmitsEmptyValues(t *testing.T) {
	w := newStdWorkspace(t)
	object := addByClass(t, w, NodeTypeObjectConstant, "object")
	stringer := addByClass(t, w, NodeTypeObjectToString, "stringer")
	upper := addByClass(t, w, NodeTypeStringUpper, "upper")

	setObjectConstant(t, w, object, `{"keep":true,"drop":null,"emptyMap":{},"items":[1,null,{},{"name":"Ada"}]}`)
	linkByPortName(t, w, object, "output", stringer, "input")
	linkByPortName(t, w, stringer, "output", upper, "input 1")
	assertObjectStringContains(t, w, stringer, `"drop":null`, `"emptyMap":{}`, `"items":[1,null,{},{"name":"Ada"}]`)
	if got := w.Snapshot().Nodes[stringer].Label; got != "" {
		t.Fatalf("ObjectToString label = %q, want empty", got)
	}

	setObjectToStringFlag(t, w, stringer, "pretty", true)
	pretty := objectStringResult(t, w, stringer)
	if !strings.Contains(pretty, "\n  ") {
		t.Fatalf("pretty object string = %q, want indentation", pretty)
	}

	setObjectToStringFlag(t, w, stringer, "omit_empty", true)
	omitted := objectStringResult(t, w, stringer)
	for _, forbidden := range []string{"drop", "emptyMap", "null", "{}"} {
		if strings.Contains(omitted, forbidden) {
			t.Fatalf("omitted object string = %q, unexpectedly contains %q", omitted, forbidden)
		}
	}
	assertObjectStringContains(t, w, stringer, `"keep": true`, `"items": [`)
	if got := w.Snapshot().Nodes[upper].Label; !strings.Contains(got, `"KEEP": TRUE`) {
		t.Fatalf("ObjectToString output label = %q, want propagated string", got)
	}
}

type objectPackerFieldSpec struct {
	id        string
	name      string
	typ       string
	path      string
	operation string
}

type objectContainerSpec struct {
	id   string
	path string
	kind string
}

type objectDeletePathSpec struct {
	id   string
	path string
}

type objectUnpackerOutputSpec struct {
	id   string
	name string
	typ  string
	path string
	def  any
}

func setObjectConstant(t *testing.T, w *pasta.Workspace, node uint64, value string) {
	t.Helper()
	w.SendNodeFormularMsg(node, formular.FormApplyMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageFormApply, MenuID: pasta.NodeMenuID(node), MenuGeneration: 1},
		BlockID:     "state",
		Values:      map[string]any{"value": value},
	})
}

func setObjectPackerConfig(t *testing.T, w *pasta.Workspace, node uint64, root string, fields []objectPackerFieldSpec, containers []objectContainerSpec, deletes ...objectDeletePathSpec) {
	t.Helper()
	fieldValues := make([]formular.ArrayElementValue, 0, len(fields))
	for _, field := range fields {
		operation := field.operation
		if operation == "" {
			operation = objectPackerOperationSet
		}
		fieldValues = append(fieldValues, formular.ArrayElementValue{
			ID:       field.id,
			Template: "field",
			Values: map[string]any{
				"name":      field.name,
				"type":      objectTypeMenuName(field.typ),
				"path":      field.path,
				"operation": operation,
			},
		})
	}
	containerValues := make([]formular.ArrayElementValue, 0, len(containers))
	for _, container := range containers {
		containerValues = append(containerValues, formular.ArrayElementValue{
			ID:       container.id,
			Template: "container",
			Values: map[string]any{
				"path": container.path,
				"kind": container.kind,
			},
		})
	}
	deleteValues := make([]formular.ArrayElementValue, 0, len(deletes))
	for _, deletePath := range deletes {
		deleteValues = append(deleteValues, formular.ArrayElementValue{
			ID:       deletePath.id,
			Template: "delete",
			Values: map[string]any{
				"path": deletePath.path,
			},
		})
	}
	w.SendNodeFormularMsg(node, formular.FormApplyMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageFormApply, MenuID: pasta.NodeMenuID(node), MenuGeneration: 1},
		BlockID:     "object",
		Values: map[string]any{
			"root":         root,
			"fields":       fieldValues,
			"containers":   containerValues,
			"delete_paths": deleteValues,
		},
	})
}

func setObjectUnpackerConfig(t *testing.T, w *pasta.Workspace, node uint64, outputs []objectUnpackerOutputSpec) {
	t.Helper()
	outputValues := make([]formular.ArrayElementValue, 0, len(outputs))
	for _, output := range outputs {
		outputValues = append(outputValues, formular.ArrayElementValue{
			ID:       output.id,
			Template: "output",
			Values: map[string]any{
				"name":    output.name,
				"type":    objectTypeMenuName(output.typ),
				"path":    output.path,
				"default": output.def,
			},
		})
	}
	w.SendNodeFormularMsg(node, formular.FormApplyMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageFormApply, MenuID: pasta.NodeMenuID(node), MenuGeneration: 1},
		BlockID:     "object",
		Values:      map[string]any{"outputs": outputValues},
	})
}

func objectFromJSONForTest(t *testing.T, text string) Object {
	t.Helper()
	object, err := ObjectFromJSON([]byte(text))
	if err != nil {
		t.Fatalf("ObjectFromJSON(%s): %v", text, err)
	}
	return object
}

func objectMenuValue(t *testing.T, state *formular.MenuSnapshotState, node uint64) Object {
	t.Helper()
	snapshot, ok := state.Snapshot(pasta.NodeMenuID(node))
	if !ok {
		t.Fatalf("missing object menu snapshot for node %d", node)
	}
	for _, block := range snapshot.Blocks {
		if block.ID != "state" {
			continue
		}
		for _, item := range block.Items {
			if item.ID == "value" && item.Field != nil {
				text, ok := parseStringAny(item.Value)
				if !ok {
					t.Fatalf("object menu value has type %T", item.Value)
				}
				return objectFromJSONForTest(t, text)
			}
		}
	}
	t.Fatalf("missing state.value field in %#v", snapshot)
	return NilObject()
}

func assertObjectDerivedLabels(t *testing.T, w *pasta.Workspace, nodes map[string]uint64, upper, sum, and string) {
	t.Helper()
	snapshot := w.Snapshot()
	if got := snapshot.Nodes[nodes["upper"]].Label; got != upper {
		t.Fatalf("upper label = %q, want %q", got, upper)
	}
	if got := snapshot.Nodes[nodes["sum"]].Label; got != sum {
		t.Fatalf("sum label = %q, want %q", got, sum)
	}
	if got := snapshot.Nodes[nodes["and"]].Label; got != and {
		t.Fatalf("and label = %q, want %q", got, and)
	}
}

func setObjectToStringFlag(t *testing.T, w *pasta.Workspace, node uint64, field string, value bool) {
	t.Helper()
	w.SendNodeFormularMsg(node, formular.FieldUpdateMessage{
		MessageBase: formular.MessageBase{Type: formular.MessageFieldUpdate, MenuID: pasta.NodeMenuID(node), MenuGeneration: 1},
		Field:       formular.FieldRef{BlockID: "state", FieldID: field},
		Value:       value,
	})
}

func objectStringResult(t *testing.T, w *pasta.Workspace, node uint64) string {
	t.Helper()
	state := subscribeStdMenus(t, w, map[string]uint64{"stringer": node})
	snapshot, ok := state.Snapshot(pasta.NodeMenuID(node))
	if !ok {
		t.Fatalf("missing ObjectToString menu for node %d", node)
	}
	for _, block := range snapshot.Blocks {
		if block.ID != "state" {
			continue
		}
		for _, item := range block.Items {
			if item.ID == "value" && item.Field != nil {
				value, ok := parseStringAny(item.Value)
				if !ok {
					t.Fatalf("ObjectToString result has type %T", item.Value)
				}
				if !item.Readonly || !item.Multiline {
					t.Fatalf("ObjectToString result readonly/multiline = %v/%v, want true/true", item.Readonly, item.Multiline)
				}
				return value
			}
		}
	}
	t.Fatalf("missing ObjectToString result field in %#v", snapshot)
	return ""
}

func assertObjectStringContains(t *testing.T, w *pasta.Workspace, node uint64, wants ...string) {
	t.Helper()
	got := objectStringResult(t, w, node)
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("ObjectToString result = %q, want substring %q", got, want)
		}
	}
}

func assertObjectStringOmits(t *testing.T, w *pasta.Workspace, node uint64, forbidden ...string) {
	t.Helper()
	got := objectStringResult(t, w, node)
	for _, text := range forbidden {
		if strings.Contains(got, text) {
			t.Fatalf("ObjectToString result = %q, unexpectedly contains %q", got, text)
		}
	}
}

func assertRightPortNames(t *testing.T, w *pasta.Workspace, node uint64, want []string) {
	t.Helper()
	snapshot := w.Snapshot()
	got := make([]string, 0, len(snapshot.Nodes[node].RightPorts))
	for _, port := range snapshot.Nodes[node].RightPorts {
		got = append(got, snapshot.Ports[port].Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("right port names = %#v, want %#v", got, want)
	}
}

func pastedNodeByClass(t *testing.T, w *pasta.Workspace, ids []uint64, class string) uint64 {
	t.Helper()
	snapshot := w.Snapshot()
	var out uint64
	for _, id := range ids {
		node := snapshot.Nodes[id]
		if node.Class != class {
			continue
		}
		if out != 0 {
			t.Fatalf("duplicate pasted class %s: %d and %d", class, out, id)
		}
		out = id
	}
	if out == 0 {
		t.Fatalf("missing pasted class %s in %v", class, ids)
	}
	return out
}
