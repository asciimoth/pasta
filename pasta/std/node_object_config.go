package std

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/asciimoth/formular"
	"github.com/asciimoth/persist"
)

const (
	objectKindMap    = "map"
	objectKindVector = "vector"
)

var objectBasicTypes = []any{"string", "int", "float", "bool", "object"}

func objectTypeMenuName(typ string) string {
	switch typ {
	case TypeInt:
		return "int"
	case TypeFloat:
		return "float"
	case TypeString:
		return "string"
	case TypeBool:
		return "bool"
	case TypeObject:
		return "object"
	default:
		return typ
	}
}

func objectTypeFromMenu(value any) (string, bool) {
	typ, ok := parseStringAny(value)
	if !ok {
		return "", false
	}
	switch typ {
	case "int", TypeInt:
		return TypeInt, true
	case "float", TypeFloat:
		return TypeFloat, true
	case "string", TypeString:
		return TypeString, true
	case "bool", TypeBool:
		return TypeBool, true
	case "object", TypeObject:
		return TypeObject, true
	default:
		return "", false
	}
}

type objectPathStep struct {
	Key     string
	Index   int
	IsIndex bool
}

func (s objectPathStep) configValue() any {
	if s.IsIndex {
		return s.Index
	}
	return s.Key
}

func (s objectPathStep) parentKind() string {
	if s.IsIndex {
		return objectKindVector
	}
	return objectKindMap
}

func objectPathConfigValue(path []objectPathStep) []any {
	out := make([]any, 0, len(path))
	for _, step := range path {
		out = append(out, step.configValue())
	}
	return out
}

func objectPathText(path []objectPathStep) string {
	data, err := json.Marshal(objectPathConfigValue(path))
	if err != nil {
		return "[]"
	}
	return string(data)
}

func parseObjectPath(value any, allowEmpty bool) ([]objectPathStep, bool) {
	var raw []any
	switch v := value.(type) {
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			if allowEmpty {
				return nil, true
			}
			return nil, false
		}
		dec := json.NewDecoder(strings.NewReader(text))
		dec.UseNumber()
		if err := dec.Decode(&raw); err != nil {
			return nil, false
		}
		var extra any
		if err := dec.Decode(&extra); err == nil {
			return nil, false
		}
	case []any:
		raw = v
	case []string:
		raw = make([]any, len(v))
		for i := range v {
			raw[i] = v[i]
		}
	case []int:
		raw = make([]any, len(v))
		for i := range v {
			raw[i] = v[i]
		}
	default:
		return nil, false
	}
	if len(raw) == 0 && !allowEmpty {
		return nil, false
	}
	path := make([]objectPathStep, 0, len(raw))
	for _, item := range raw {
		step, ok := parseObjectPathStep(item)
		if !ok {
			return nil, false
		}
		path = append(path, step)
	}
	return path, true
}

func parseObjectPathStep(value any) (objectPathStep, bool) {
	switch v := value.(type) {
	case string:
		if v == "" {
			return objectPathStep{}, false
		}
		return objectPathStep{Key: v}, true
	case int:
		if v < 0 {
			return objectPathStep{}, false
		}
		return objectPathStep{Index: v, IsIndex: true}, true
	case int64:
		if v < 0 || v > math.MaxInt {
			return objectPathStep{}, false
		}
		return objectPathStep{Index: int(v), IsIndex: true}, true
	case float64:
		if v < 0 || math.Trunc(v) != v || v > math.MaxInt {
			return objectPathStep{}, false
		}
		return objectPathStep{Index: int(v), IsIndex: true}, true
	case json.Number:
		i, err := v.Int64()
		if err != nil || i < 0 || i > math.MaxInt {
			return objectPathStep{}, false
		}
		return objectPathStep{Index: int(i), IsIndex: true}, true
	default:
		return objectPathStep{}, false
	}
}

func objectPathKey(path []objectPathStep) string {
	if len(path) == 0 {
		return ""
	}
	var b strings.Builder
	for _, step := range path {
		if step.IsIndex {
			b.WriteString("i:")
			b.WriteString(strconv.Itoa(step.Index))
		} else {
			b.WriteString("s:")
			b.WriteString(strconv.Quote(step.Key))
		}
		b.WriteByte('/')
	}
	return b.String()
}

func objectTypeSupported(typ string) bool {
	return typ == TypeInt || typ == TypeFloat || typ == TypeString || typ == TypeBool || typ == TypeObject
}

func objectContainerKind(value any) (string, bool) {
	kind, ok := parseStringAny(value)
	if !ok {
		return "", false
	}
	switch kind {
	case objectKindMap, objectKindVector:
		return kind, true
	default:
		return "", false
	}
}

type objectContainerConfig struct {
	ID   string
	Path []objectPathStep
	Kind string
}

type objectDeletePathConfig struct {
	ID   string
	Path []objectPathStep
}

func parseObjectContainers(value any) ([]objectContainerConfig, bool) {
	items, ok := parseObjectArray(value)
	if !ok {
		return nil, false
	}
	containers := make([]objectContainerConfig, 0, len(items))
	for i, item := range items {
		container, ok := objectContainerFromValues(item.ID, item.Values)
		if !ok {
			return nil, false
		}
		if container.ID == "" {
			container.ID = "container-" + strconv.Itoa(i+1)
		}
		containers = append(containers, container)
	}
	return normalizeObjectContainers(containers), true
}

func objectContainerFromValues(id string, values map[string]any) (objectContainerConfig, bool) {
	kind, ok := objectContainerKind(values["kind"])
	if !ok {
		return objectContainerConfig{}, false
	}
	path, ok := parseObjectPath(values["path"], false)
	if !ok {
		return objectContainerConfig{}, false
	}
	return objectContainerConfig{ID: id, Path: path, Kind: kind}, true
}

func normalizeObjectContainers(containers []objectContainerConfig) []objectContainerConfig {
	out := make([]objectContainerConfig, 0, len(containers))
	seenIDs := map[string]struct{}{}
	seenPaths := map[string]struct{}{}
	for i, container := range containers {
		if container.Kind != objectKindVector {
			container.Kind = objectKindMap
		}
		if len(container.Path) == 0 {
			continue
		}
		pathKey := objectPathKey(container.Path)
		if _, seen := seenPaths[pathKey]; seen {
			continue
		}
		seenPaths[pathKey] = struct{}{}
		if container.ID == "" {
			container.ID = "container-" + strconv.Itoa(i+1)
		}
		if _, seen := seenIDs[container.ID]; seen {
			container.ID = "container-" + strconv.Itoa(i+1)
		}
		seenIDs[container.ID] = struct{}{}
		out = append(out, container)
	}
	return out
}

func objectContainersArrayValue(containers []objectContainerConfig) []formular.ArrayElementValue {
	values := make([]formular.ArrayElementValue, 0, len(containers))
	for _, container := range containers {
		values = append(values, formular.ArrayElementValue{
			ID:       container.ID,
			Template: "container",
			Values: map[string]any{
				"path": objectPathText(container.Path),
				"kind": container.Kind,
			},
		})
	}
	return values
}

func objectContainerElements(containers []objectContainerConfig) []formular.ArrayElement {
	elements := make([]formular.ArrayElement, 0, len(containers))
	for _, container := range containers {
		elements = append(elements, formular.ArrayElement{
			ID:       container.ID,
			Template: "container",
			Items: []formular.Item{
				{Type: formular.ItemField, ID: "path", Label: "Path", Field: &formular.Field{Kind: formular.FieldText, Value: objectPathText(container.Path), Placeholder: `["items"]`}},
				{Type: formular.ItemField, ID: "kind", Label: "Type", Field: &formular.Field{Kind: formular.FieldRadio, Value: container.Kind, AllowedValues: []any{objectKindMap, objectKindVector}}},
			},
		})
	}
	return elements
}

func objectContainerTemplates() []formular.ArrayTemplate {
	return []formular.ArrayTemplate{{
		Name:  "container",
		Label: "Container",
		Items: []formular.Item{
			{Type: formular.ItemField, ID: "path", Label: "Path", Field: &formular.Field{Kind: formular.FieldText, Placeholder: `["items"]`}},
			{Type: formular.ItemField, ID: "kind", Label: "Type", Field: &formular.Field{Kind: formular.FieldRadio, Value: objectKindMap, AllowedValues: []any{objectKindMap, objectKindVector}}},
		},
	}}
}

func objectContainersConfig(containers []objectContainerConfig) []any {
	out := make([]any, 0, len(containers))
	for _, container := range normalizeObjectContainers(containers) {
		out = append(out, map[string]any{
			"id":   container.ID,
			"path": objectPathConfigValue(container.Path),
			"kind": container.Kind,
		})
	}
	return out
}

func parseObjectDeletePaths(value any) ([]objectDeletePathConfig, bool) {
	switch v := value.(type) {
	case []formular.ArrayElementValue:
		out := make([]objectDeletePathConfig, 0, len(v))
		for i, element := range v {
			path, ok := parseObjectPath(element.Values["path"], false)
			if !ok {
				return nil, false
			}
			id := element.ID
			if id == "" {
				id = "delete-" + strconv.Itoa(i+1)
			}
			out = append(out, objectDeletePathConfig{ID: id, Path: path})
		}
		return normalizeObjectDeletePaths(out), true
	case []any:
		out := make([]objectDeletePathConfig, 0, len(v))
		for i, item := range v {
			if direct, ok := parseObjectDirectMap(item); ok {
				path, ok := parseObjectPath(direct.Values["path"], false)
				if !ok {
					return nil, false
				}
				id := direct.ID
				if id == "" {
					id = "delete-" + strconv.Itoa(i+1)
				}
				out = append(out, objectDeletePathConfig{ID: id, Path: path})
				continue
			}
			path, ok := parseObjectPath(item, false)
			if !ok {
				return nil, false
			}
			out = append(out, objectDeletePathConfig{ID: "delete-" + strconv.Itoa(i+1), Path: path})
		}
		return normalizeObjectDeletePaths(out), true
	default:
		return nil, false
	}
}

func normalizeObjectDeletePaths(paths []objectDeletePathConfig) []objectDeletePathConfig {
	out := make([]objectDeletePathConfig, 0, len(paths))
	seenIDs := map[string]struct{}{}
	seenPaths := map[string]struct{}{}
	for i, path := range paths {
		if len(path.Path) == 0 {
			continue
		}
		pathKey := objectPathKey(path.Path)
		if _, seen := seenPaths[pathKey]; seen {
			continue
		}
		seenPaths[pathKey] = struct{}{}
		if path.ID == "" {
			path.ID = "delete-" + strconv.Itoa(i+1)
		}
		if _, seen := seenIDs[path.ID]; seen {
			path.ID = "delete-" + strconv.Itoa(i+1)
		}
		seenIDs[path.ID] = struct{}{}
		out = append(out, path)
	}
	return out
}

func objectDeletePathsArrayValue(paths []objectDeletePathConfig) []formular.ArrayElementValue {
	values := make([]formular.ArrayElementValue, 0, len(paths))
	for _, path := range paths {
		values = append(values, formular.ArrayElementValue{
			ID:       path.ID,
			Template: "delete",
			Values: map[string]any{
				"path": objectPathText(path.Path),
			},
		})
	}
	return values
}

func objectDeletePathElements(paths []objectDeletePathConfig) []formular.ArrayElement {
	elements := make([]formular.ArrayElement, 0, len(paths))
	for _, path := range paths {
		elements = append(elements, formular.ArrayElement{
			ID:       path.ID,
			Template: "delete",
			Items: []formular.Item{
				{Type: formular.ItemField, ID: "path", Label: "Path", Field: &formular.Field{Kind: formular.FieldText, Value: objectPathText(path.Path), Placeholder: `["legacy"]`}},
			},
		})
	}
	return elements
}

func objectDeletePathTemplates() []formular.ArrayTemplate {
	return []formular.ArrayTemplate{{
		Name:  "delete",
		Label: "Delete path",
		Items: []formular.Item{
			{Type: formular.ItemField, ID: "path", Label: "Path", Field: &formular.Field{Kind: formular.FieldText, Placeholder: `["legacy"]`}},
		},
	}}
}

func objectDeletePathsConfig(paths []objectDeletePathConfig) []any {
	out := make([]any, 0, len(paths))
	for _, path := range normalizeObjectDeletePaths(paths) {
		out = append(out, map[string]any{
			"id":   path.ID,
			"path": objectPathConfigValue(path.Path),
		})
	}
	return out
}

type objectArrayItem struct {
	ID     string
	Values map[string]any
}

func parseObjectArray(value any) ([]objectArrayItem, bool) {
	switch v := value.(type) {
	case []formular.ArrayElementValue:
		out := make([]objectArrayItem, 0, len(v))
		for _, element := range v {
			out = append(out, objectArrayItem{ID: element.ID, Values: element.Values})
		}
		return out, true
	case []any:
		out := make([]objectArrayItem, 0, len(v))
		for _, item := range v {
			if direct, ok := parseObjectDirectMap(item); ok {
				out = append(out, direct)
				continue
			}
			element, ok := parseArrayElementMap(item)
			if !ok {
				return nil, false
			}
			out = append(out, objectArrayItem{ID: element.ID, Values: element.Values})
		}
		return out, true
	default:
		return nil, false
	}
}

func parseObjectDirectMap(value any) (objectArrayItem, bool) {
	m, ok := value.(map[string]any)
	if !ok {
		return objectArrayItem{}, false
	}
	if _, hasValues := m["values"]; hasValues {
		return objectArrayItem{}, false
	}
	id, _ := parseStringAny(m["id"])
	values := make(map[string]any, len(m))
	for key, value := range m {
		if key != "id" {
			values[key] = value
		}
	}
	return objectArrayItem{ID: id, Values: values}, true
}

type objectLayoutPath struct {
	Path   []objectPathStep
	Append bool
}

func validateObjectLayout(rootKind string, fieldPaths []objectLayoutPath, containers []objectContainerConfig) error {
	if rootKind != objectKindMap && rootKind != objectKindVector {
		return errors.New("invalid object root type")
	}
	kinds := map[string]string{"": rootKind}
	for _, container := range containers {
		if container.Kind != objectKindMap && container.Kind != objectKindVector {
			return errors.New("invalid object container type")
		}
		if len(container.Path) == 0 {
			return errors.New("object container path must not be empty")
		}
		if err := validateObjectPathParents(kinds, container.Path); err != nil {
			return err
		}
		key := objectPathKey(container.Path)
		if existing, ok := kinds[key]; ok && existing != container.Kind {
			return fmt.Errorf("object container %s conflicts with inferred %s", objectPathText(container.Path), existing)
		}
		kinds[key] = container.Kind
	}
	for _, fieldPath := range fieldPaths {
		if len(fieldPath.Path) == 0 {
			if fieldPath.Append && rootKind == objectKindVector {
				continue
			}
			return errors.New("object field path must not be empty")
		}
		if err := validateObjectPathParents(kinds, fieldPath.Path); err != nil {
			return err
		}
		if !fieldPath.Append {
			continue
		}
		key := objectPathKey(fieldPath.Path)
		if existing, ok := kinds[key]; ok && existing != objectKindVector {
			return fmt.Errorf("object append path %s conflicts with inferred %s", objectPathText(fieldPath.Path), existing)
		}
		kinds[key] = objectKindVector
	}
	return nil
}

func validateObjectPathParents(kinds map[string]string, path []objectPathStep) error {
	for i, step := range path {
		parentPath := path[:i]
		parentKey := objectPathKey(parentPath)
		parentKind, ok := kinds[parentKey]
		if !ok {
			parentKind = step.parentKind()
			kinds[parentKey] = parentKind
		}
		if parentKind != step.parentKind() {
			return fmt.Errorf("object path %s expects %s at %s, got %s", objectPathText(path), step.parentKind(), objectPathText(parentPath), parentKind)
		}
		if i < len(path)-1 {
			childPath := path[:i+1]
			childKey := objectPathKey(childPath)
			childKind := path[i+1].parentKind()
			if existing, ok := kinds[childKey]; ok && existing != childKind {
				return fmt.Errorf("object path %s expects child %s at %s, got %s", objectPathText(path), childKind, objectPathText(childPath), existing)
			}
			kinds[childKey] = childKind
		}
	}
	return nil
}

type objectBuildNode struct {
	kind     string
	mapItems map[string]objectBuildEntry
	vecItems map[int]objectBuildEntry
	leaf     persist.Value
	isLeaf   bool
}

type objectBuildEntry struct {
	value     persist.Value
	child     *objectBuildNode
	childKind string
}

func newObjectBuildNode(kind string) *objectBuildNode {
	return &objectBuildNode{kind: kind, mapItems: map[string]objectBuildEntry{}, vecItems: map[int]objectBuildEntry{}}
}

// buildObjectValueWithBase deletes configured subgraphs from base, overlays the
// configured object construction, then applies ordered appends. Set fields and
// configured containers win conflicts while unrelated base values are
// preserved. Append fields treat their path as the destination vector and add
// their input after any existing base or configured vector entries.
func buildObjectValueWithBase(base persist.Value, rootKind string, containers []objectContainerConfig, deletePaths []objectDeletePathConfig, fields []objectBuildField) Object {
	root := newObjectBuildNode(rootKind)
	kinds := map[string]string{"": rootKind}
	for _, container := range containers {
		kinds[objectPathKey(container.Path)] = container.Kind
		root.ensureContainer(container.Path, container.Kind, kinds)
	}
	appends := make([]objectBuildField, 0)
	for _, field := range fields {
		if field.Operation == objectPackerOperationAppend {
			root.ensureContainer(field.Path, objectKindVector, kinds)
			appends = append(appends, field)
			continue
		}
		root.set(field.Path, field.Value, kinds)
	}
	base = deleteObjectBasePaths(base, deletePaths)
	value := root.mergeOver(base)
	for _, field := range appends {
		value = appendObjectValue(value, field.Path, field.Value)
	}
	object, ok := ObjectFromValue(value)
	if !ok {
		return NilObject()
	}
	return object
}

type objectBuildField struct {
	Path      []objectPathStep
	Value     persist.Value
	Operation string
}

func (n *objectBuildNode) ensureContainer(path []objectPathStep, kind string, kinds map[string]string) *objectBuildNode {
	if len(path) == 0 {
		return n
	}
	step := path[0]
	childKind := kindForObjectChild(path, kind, kinds)
	child := n.child(step, childKind)
	if len(path) == 1 {
		return child
	}
	return child.ensureContainer(path[1:], kind, kinds)
}

func (n *objectBuildNode) set(path []objectPathStep, value persist.Value, kinds map[string]string) {
	if len(path) == 0 {
		n.leaf = value
		n.isLeaf = true
		return
	}
	step := path[0]
	if len(path) == 1 {
		n.setEntry(step, objectBuildEntry{value: value})
		return
	}
	childKind := kindForObjectChild(path, "", kinds)
	child := n.child(step, childKind)
	child.set(path[1:], value, kinds)
}

func appendObjectValue(value persist.Value, path []objectPathStep, item persist.Value) persist.Value {
	if len(path) == 0 {
		vector, _ := value.Vector()
		return persist.VectorValue(vector.Append(item))
	}
	step := path[0]
	if step.IsIndex {
		vector, _ := value.Vector()
		child := persist.Nil()
		if step.Index < vector.Len() {
			child, _ = vector.Get(step.Index)
		}
		nextChild := appendObjectValue(child, path[1:], item)
		for vector.Len() <= step.Index {
			vector = vector.Append(persist.Nil())
		}
		nextVector, ok := vector.Set(step.Index, nextChild)
		if !ok {
			return value
		}
		return persist.VectorValue(nextVector)
	}
	m, _ := value.Map()
	key := persist.KString(step.Key)
	child, _ := m.Get(key)
	nextChild := appendObjectValue(child, path[1:], item)
	return persist.MapValue(m.Assoc(key, nextChild))
}

func kindForObjectChild(path []objectPathStep, explicit string, kinds map[string]string) string {
	if explicit != "" && len(path) == 1 {
		return explicit
	}
	childPathKey := objectPathKey(path[:1])
	if kind, ok := kinds[childPathKey]; ok {
		return kind
	}
	if len(path) > 1 {
		return path[1].parentKind()
	}
	if explicit != "" {
		return explicit
	}
	return objectKindMap
}

func (n *objectBuildNode) child(step objectPathStep, kind string) *objectBuildNode {
	if kind != objectKindVector {
		kind = objectKindMap
	}
	switch n.kind {
	case objectKindVector:
		entry := n.vecItems[step.Index]
		if entry.child == nil || entry.childKind != kind {
			entry = objectBuildEntry{child: newObjectBuildNode(kind), childKind: kind}
			n.vecItems[step.Index] = entry
		}
		return entry.child
	default:
		entry := n.mapItems[step.Key]
		if entry.child == nil || entry.childKind != kind {
			entry = objectBuildEntry{child: newObjectBuildNode(kind), childKind: kind}
			n.mapItems[step.Key] = entry
		}
		return entry.child
	}
}

func (n *objectBuildNode) setEntry(step objectPathStep, entry objectBuildEntry) {
	if n.kind == objectKindVector {
		n.vecItems[step.Index] = entry
		return
	}
	n.mapItems[step.Key] = entry
}

func (n *objectBuildNode) mergeOver(base persist.Value) persist.Value {
	if n.isLeaf {
		return n.leaf
	}
	switch n.kind {
	case objectKindVector:
		return n.mergeVectorOver(base)
	default:
		return n.mergeMapOver(base)
	}
}

func (n *objectBuildNode) mergeMapOver(base persist.Value) persist.Value {
	m := persist.NewMap()
	if baseMap, ok := base.Map(); ok {
		baseMap.Range(func(k persist.Key, v persist.Value) bool {
			m = m.Assoc(k, v)
			return true
		})
	}
	keys := make([]string, 0, len(n.mapItems))
	for key := range n.mapItems {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		entry := n.mapItems[key]
		baseValue := persist.Nil()
		if baseMap, ok := base.Map(); ok {
			if found, exists := baseMap.Get(persist.KString(key)); exists {
				baseValue = found
			}
		}
		value := entry.value
		if entry.child != nil {
			value = entry.child.mergeOver(baseValue)
		} else {
			value = mergePersistValueOver(value, baseValue)
		}
		m = m.Assoc(persist.KString(key), value)
	}
	return persist.MapValue(m)
}

func (n *objectBuildNode) mergeVectorOver(base persist.Value) persist.Value {
	maxIndex := -1
	if baseVector, ok := base.Vector(); ok {
		maxIndex = baseVector.Len() - 1
	}
	for index := range n.vecItems {
		if index > maxIndex {
			maxIndex = index
		}
	}
	values := make([]persist.Value, 0, maxIndex+1)
	baseVector, hasBaseVector := base.Vector()
	for i := 0; i <= maxIndex; i++ {
		value := persist.Nil()
		if hasBaseVector && i < baseVector.Len() {
			if found, ok := baseVector.Get(i); ok {
				value = found
			}
		}
		if entry, ok := n.vecItems[i]; ok {
			if entry.child != nil {
				value = entry.child.mergeOver(value)
			} else {
				value = mergePersistValueOver(entry.value, value)
			}
		}
		values = append(values, value)
	}
	return persist.VectorValue(persist.NewVector(values...))
}

func mergePersistValueOver(value, base persist.Value) persist.Value {
	switch value.Kind() {
	case persist.KindMap:
		valueMap, _ := value.Map()
		baseMap, ok := base.Map()
		if !ok {
			return value
		}
		out := persist.NewMap()
		baseMap.Range(func(k persist.Key, v persist.Value) bool {
			out = out.Assoc(k, v)
			return true
		})
		valueMap.Range(func(k persist.Key, v persist.Value) bool {
			baseValue := persist.Nil()
			if found, exists := baseMap.Get(k); exists {
				baseValue = found
			}
			out = out.Assoc(k, mergePersistValueOver(v, baseValue))
			return true
		})
		return persist.MapValue(out)
	case persist.KindVector:
		valueVector, _ := value.Vector()
		baseVector, ok := base.Vector()
		if !ok {
			return value
		}
		maxLen := valueVector.Len()
		if baseVector.Len() > maxLen {
			maxLen = baseVector.Len()
		}
		items := make([]persist.Value, 0, maxLen)
		for i := 0; i < maxLen; i++ {
			baseValue := persist.Nil()
			if found, exists := baseVector.Get(i); exists {
				baseValue = found
			}
			if item, exists := valueVector.Get(i); exists {
				items = append(items, mergePersistValueOver(item, baseValue))
			} else {
				items = append(items, baseValue)
			}
		}
		return persist.VectorValue(persist.NewVector(items...))
	default:
		return value
	}
}

func deleteObjectBasePaths(base persist.Value, paths []objectDeletePathConfig) persist.Value {
	for _, path := range normalizeObjectDeletePaths(paths) {
		if next, ok := deleteObjectPath(base, path.Path); ok {
			base = next
		}
	}
	return base
}

func deleteObjectPath(value persist.Value, path []objectPathStep) (persist.Value, bool) {
	if len(path) == 0 {
		return value, false
	}
	step := path[0]
	if step.IsIndex {
		vector, ok := value.Vector()
		if !ok || step.Index >= vector.Len() {
			return value, false
		}
		if len(path) == 1 {
			items := make([]persist.Value, 0, vector.Len()-1)
			for i := 0; i < vector.Len(); i++ {
				if i == step.Index {
					continue
				}
				item, _ := vector.Get(i)
				items = append(items, item)
			}
			return persist.VectorValue(persist.NewVector(items...)), true
		}
		child, _ := vector.Get(step.Index)
		nextChild, ok := deleteObjectPath(child, path[1:])
		if !ok {
			return value, false
		}
		nextVector, ok := vector.Set(step.Index, nextChild)
		if !ok {
			return value, false
		}
		return persist.VectorValue(nextVector), true
	}
	m, ok := value.Map()
	if !ok {
		return value, false
	}
	key := persist.KString(step.Key)
	child, exists := m.Get(key)
	if !exists {
		return value, false
	}
	if len(path) == 1 {
		return persist.MapValue(m.Dissoc(key)), true
	}
	nextChild, ok := deleteObjectPath(child, path[1:])
	if !ok {
		return value, false
	}
	return persist.MapValue(m.Assoc(key, nextChild)), true
}

func payloadToPersistValue(typ string, payload any) (persist.Value, bool) {
	switch typ {
	case TypeInt:
		value, ok := IntFromPayload(payload)
		if !ok {
			return persist.Nil(), false
		}
		return persist.Int(int64(value)), true
	case TypeFloat:
		value, ok := parseFloatAny(payload)
		if !ok {
			return persist.Nil(), false
		}
		return persist.Float(value), true
	case TypeString:
		value, ok := StringFromPayload(payload)
		if !ok {
			return persist.Nil(), false
		}
		return persist.StringClone(value), true
	case TypeBool:
		value, ok := parseBoolAny(payload)
		if !ok {
			return persist.Nil(), false
		}
		return persist.Bool(value), true
	case TypeObject:
		value, ok := ObjectFromPayload(payload)
		if !ok {
			return persist.Nil(), false
		}
		return value.Value(), true
	default:
		return persist.Nil(), false
	}
}
