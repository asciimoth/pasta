package pasta

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"strconv"
)

// MenuFieldKind describes the value type and basic renderer contract for a menu field.
type MenuFieldKind string

const (
	// MenuFieldReadOnly is display-only JSON-compatible data.
	MenuFieldReadOnly MenuFieldKind = "read-only"
	// MenuFieldString is an editable string value.
	MenuFieldString MenuFieldKind = "string"
	// MenuFieldInt64 is an editable signed 64-bit integer value.
	MenuFieldInt64 MenuFieldKind = "int64"
	// MenuFieldFloat64 is an editable float64 value.
	MenuFieldFloat64 MenuFieldKind = "float64"
	// MenuFieldBool is an editable bool value.
	MenuFieldBool MenuFieldKind = "bool"
)

// MenuRenderHint describes an optional preferred UI affordance.
type MenuRenderHint string

const (
	// MenuRenderCheckbox asks renderers to show a bool field as a checkbox.
	MenuRenderCheckbox MenuRenderHint = "checkbox"
)

// NodeMenu is an ephemeral, JSON-serializable node control surface.
//
// Version is assigned by the workspace and increments on replacement or
// accepted state updates. Blocks defaults to one "default" block when omitted.
type NodeMenu struct {
	Version  int64             `json:"version"`
	Blocks   []MenuBlock       `json:"blocks"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// MenuBlock groups fields, buttons, and repeatable groups under one optional title.
type MenuBlock struct {
	ID       string            `json:"id"`
	Title    string            `json:"title,omitempty"`
	Fields   []MenuField       `json:"fields,omitempty"`
	Buttons  []MenuButton      `json:"buttons,omitempty"`
	Repeats  []MenuRepeat      `json:"repeats,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// MenuField is one display or editable scalar menu value.
type MenuField struct {
	ID       string            `json:"id"`
	Label    string            `json:"label,omitempty"`
	Kind     MenuFieldKind     `json:"kind"`
	Value    any               `json:"value,omitempty"`
	ReadOnly bool              `json:"readOnly,omitempty"`
	Options  []MenuOption      `json:"options,omitempty"`
	Render   MenuRenderHint    `json:"render,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// MenuOption is one accepted value for a scalar field.
type MenuOption struct {
	Value    any    `json:"value"`
	Label    string `json:"label,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

// MenuButton is an ephemeral action exposed by a node menu.
type MenuButton struct {
	ID       string            `json:"id"`
	Label    string            `json:"label,omitempty"`
	Disabled bool              `json:"disabled,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// MenuRepeat describes a variable-length list whose items share one field template.
type MenuRepeat struct {
	ID       string            `json:"id"`
	Title    string            `json:"title,omitempty"`
	Template []MenuField       `json:"template"`
	Items    []MenuRepeatItem  `json:"items,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// MenuRepeatItem is one state-bearing item inside a repeatable menu group.
type MenuRepeatItem struct {
	ID       string            `json:"id"`
	Title    string            `json:"title,omitempty"`
	Fields   []MenuField       `json:"fields"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// MenuStateUpdate is an externally proposed partial state update.
//
// Version must match the current menu version when non-zero. Field updates
// target block fields. Repeat updates replace the state items for a repeat.
type MenuStateUpdate struct {
	Version int64              `json:"version,omitempty"`
	Fields  []MenuFieldUpdate  `json:"fields,omitempty"`
	Repeats []MenuRepeatUpdate `json:"repeats,omitempty"`
}

// MenuFieldUpdate updates one non-repeat field value.
type MenuFieldUpdate struct {
	Block string `json:"block"`
	Field string `json:"field"`
	Value any    `json:"value"`
}

// MenuRepeatUpdate replaces the item state for one repeat group.
type MenuRepeatUpdate struct {
	Block  string                `json:"block"`
	Repeat string                `json:"repeat"`
	Items  []MenuRepeatItemState `json:"items"`
}

// MenuRepeatItemState is the proposed state for one repeat item.
type MenuRepeatItemState struct {
	ID       string            `json:"id"`
	Title    string            `json:"title,omitempty"`
	Fields   map[string]any    `json:"fields"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// MenuButtonRef identifies one button in one menu block.
type MenuButtonRef struct {
	Block  string `json:"block"`
	Button string `json:"button"`
}

// MarshalNodeMenu returns deterministic JSON text for a full menu document.
func MarshalNodeMenu(menu NodeMenu) (string, error) {
	normalized, err := normalizeMenu(menu, menu.Version)
	if err != nil {
		return "", opErr("marshal node menu", "validate", err)
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", opErr("marshal node menu", "marshal", err)
	}
	return string(data), nil
}

// UnmarshalNodeMenu parses and validates a full menu document from JSON text.
func UnmarshalNodeMenu(text string) (NodeMenu, error) {
	var menu NodeMenu
	dec := json.NewDecoder(bytes.NewBufferString(text))
	dec.UseNumber()
	if err := dec.Decode(&menu); err != nil {
		return NodeMenu{}, opErr("unmarshal node menu", "unmarshal", err)
	}
	normalized, err := normalizeMenu(menu, menu.Version)
	if err != nil {
		return NodeMenu{}, opErr("unmarshal node menu", "validate", err)
	}
	return normalized, nil
}

// MarshalMenuStateUpdate returns deterministic JSON text for a proposed menu update.
func MarshalMenuStateUpdate(update MenuStateUpdate) (string, error) {
	normalized, err := normalizeMenuStateUpdate(update)
	if err != nil {
		return "", opErr("marshal menu state update", "validate", err)
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", opErr("marshal menu state update", "marshal", err)
	}
	return string(data), nil
}

// UnmarshalMenuStateUpdate parses and validates a proposed menu update.
func UnmarshalMenuStateUpdate(text string) (MenuStateUpdate, error) {
	var update MenuStateUpdate
	dec := json.NewDecoder(bytes.NewBufferString(text))
	dec.UseNumber()
	if err := dec.Decode(&update); err != nil {
		return MenuStateUpdate{}, opErr("unmarshal menu state update", "unmarshal", err)
	}
	normalized, err := normalizeMenuStateUpdate(update)
	if err != nil {
		return MenuStateUpdate{}, opErr("unmarshal menu state update", "validate", err)
	}
	return normalized, nil
}

func normalizeMenu(menu NodeMenu, version int64) (NodeMenu, error) {
	menu.Version = version
	menu.Metadata = cloneStringMap(menu.Metadata)
	if len(menu.Blocks) == 0 {
		menu.Blocks = []MenuBlock{{ID: "default"}}
	}
	seenBlocks := map[string]bool{}
	for i := range menu.Blocks {
		block, err := normalizeMenuBlock(menu.Blocks[i])
		if err != nil {
			return NodeMenu{}, err
		}
		if seenBlocks[block.ID] {
			return NodeMenu{}, ErrDuplicate
		}
		seenBlocks[block.ID] = true
		menu.Blocks[i] = block
	}
	return menu, nil
}

func normalizeMenuBlock(block MenuBlock) (MenuBlock, error) {
	if !validMenuID(block.ID) {
		return MenuBlock{}, ErrInvalidName
	}
	block.Metadata = cloneStringMap(block.Metadata)
	seenFields := map[string]bool{}
	for i := range block.Fields {
		field, err := normalizeMenuField(block.Fields[i])
		if err != nil {
			return MenuBlock{}, err
		}
		if seenFields[field.ID] {
			return MenuBlock{}, ErrDuplicate
		}
		seenFields[field.ID] = true
		block.Fields[i] = field
	}
	seenButtons := map[string]bool{}
	for i := range block.Buttons {
		button := block.Buttons[i]
		if !validMenuID(button.ID) {
			return MenuBlock{}, ErrInvalidName
		}
		if seenButtons[button.ID] {
			return MenuBlock{}, ErrDuplicate
		}
		seenButtons[button.ID] = true
		button.Metadata = cloneStringMap(button.Metadata)
		block.Buttons[i] = button
	}
	seenRepeats := map[string]bool{}
	for i := range block.Repeats {
		repeat, err := normalizeMenuRepeat(block.Repeats[i])
		if err != nil {
			return MenuBlock{}, err
		}
		if seenRepeats[repeat.ID] {
			return MenuBlock{}, ErrDuplicate
		}
		seenRepeats[repeat.ID] = true
		block.Repeats[i] = repeat
	}
	return block, nil
}

func normalizeMenuRepeat(repeat MenuRepeat) (MenuRepeat, error) {
	if !validMenuID(repeat.ID) {
		return MenuRepeat{}, ErrInvalidName
	}
	repeat.Metadata = cloneStringMap(repeat.Metadata)
	if len(repeat.Template) == 0 {
		return MenuRepeat{}, ErrInvalidMenu
	}
	template := make(map[string]MenuField, len(repeat.Template))
	for i := range repeat.Template {
		field, err := normalizeMenuField(repeat.Template[i])
		if err != nil {
			return MenuRepeat{}, err
		}
		if template[field.ID].ID != "" {
			return MenuRepeat{}, ErrDuplicate
		}
		template[field.ID] = field
		repeat.Template[i] = field
	}
	seenItems := map[string]bool{}
	for i := range repeat.Items {
		item, err := normalizeMenuRepeatItem(repeat.Items[i], repeat.Template, template)
		if err != nil {
			return MenuRepeat{}, err
		}
		if seenItems[item.ID] {
			return MenuRepeat{}, ErrDuplicate
		}
		seenItems[item.ID] = true
		repeat.Items[i] = item
	}
	return repeat, nil
}

func normalizeMenuRepeatItem(item MenuRepeatItem, orderedTemplate []MenuField, template map[string]MenuField) (MenuRepeatItem, error) {
	if !validMenuID(item.ID) {
		return MenuRepeatItem{}, ErrInvalidName
	}
	item.Metadata = cloneStringMap(item.Metadata)
	supplied := make(map[string]MenuField, len(item.Fields))
	for _, field := range item.Fields {
		if !validMenuID(field.ID) {
			return MenuRepeatItem{}, ErrInvalidName
		}
		if supplied[field.ID].ID != "" {
			return MenuRepeatItem{}, ErrDuplicate
		}
		if template[field.ID].ID == "" {
			return MenuRepeatItem{}, ErrNotFound
		}
		supplied[field.ID] = field
	}
	fields := make([]MenuField, len(orderedTemplate))
	for i, base := range orderedTemplate {
		field := base
		if got, ok := supplied[base.ID]; ok {
			field.Value = got.Value
			field.Label = firstNonEmpty(got.Label, field.Label)
			field.ReadOnly = field.ReadOnly || got.ReadOnly
			field.Metadata = mergeStringMap(field.Metadata, got.Metadata)
		}
		normalized, err := normalizeMenuField(field)
		if err != nil {
			return MenuRepeatItem{}, err
		}
		fields[i] = normalized
	}
	item.Fields = fields
	return item, nil
}

func normalizeMenuField(field MenuField) (MenuField, error) {
	if !validMenuID(field.ID) {
		return MenuField{}, ErrInvalidName
	}
	if !validMenuFieldKind(field.Kind) {
		return MenuField{}, ErrInvalidMenu
	}
	if field.Kind == MenuFieldReadOnly {
		field.ReadOnly = true
	}
	if field.Render != "" && (field.Render != MenuRenderCheckbox || field.Kind != MenuFieldBool) {
		return MenuField{}, ErrInvalidMenu
	}
	value, err := canonicalMenuValue(field.Kind, field.Value)
	if err != nil {
		return MenuField{}, err
	}
	field.Value = value
	field.Metadata = cloneStringMap(field.Metadata)
	for i := range field.Options {
		option := field.Options[i]
		value, err := canonicalMenuValue(field.Kind, option.Value)
		if err != nil {
			return MenuField{}, err
		}
		option.Value = value
		field.Options[i] = option
	}
	if len(field.Options) > 0 && !menuValueInOptions(field.Value, field.Options) {
		return MenuField{}, ErrTypeMismatch
	}
	return field, nil
}

func normalizeMenuStateUpdate(update MenuStateUpdate) (MenuStateUpdate, error) {
	if update.Version < 0 {
		return MenuStateUpdate{}, ErrInvalidMenu
	}
	seenFields := map[[2]string]bool{}
	for i := range update.Fields {
		field := update.Fields[i]
		if !validMenuID(field.Block) || !validMenuID(field.Field) {
			return MenuStateUpdate{}, ErrInvalidName
		}
		if seenFields[[2]string{field.Block, field.Field}] {
			return MenuStateUpdate{}, ErrDuplicate
		}
		if !jsonCompatible(field.Value) {
			return MenuStateUpdate{}, ErrInvalidMenu
		}
		field.Value = cloneJSONValue(field.Value)
		update.Fields[i] = field
		seenFields[[2]string{field.Block, field.Field}] = true
	}
	seenRepeats := map[[2]string]bool{}
	for i := range update.Repeats {
		repeat := update.Repeats[i]
		if !validMenuID(repeat.Block) || !validMenuID(repeat.Repeat) {
			return MenuStateUpdate{}, ErrInvalidName
		}
		if seenRepeats[[2]string{repeat.Block, repeat.Repeat}] {
			return MenuStateUpdate{}, ErrDuplicate
		}
		seenRepeats[[2]string{repeat.Block, repeat.Repeat}] = true
		seenItems := map[string]bool{}
		for j := range repeat.Items {
			item := repeat.Items[j]
			if !validMenuID(item.ID) {
				return MenuStateUpdate{}, ErrInvalidName
			}
			if seenItems[item.ID] {
				return MenuStateUpdate{}, ErrDuplicate
			}
			seenItems[item.ID] = true
			item.Metadata = cloneStringMap(item.Metadata)
			fields := make(map[string]any, len(item.Fields))
			for id, value := range item.Fields {
				if !validMenuID(id) {
					return MenuStateUpdate{}, ErrInvalidName
				}
				if !jsonCompatible(value) {
					return MenuStateUpdate{}, ErrInvalidMenu
				}
				fields[id] = cloneJSONValue(value)
			}
			item.Fields = fields
			repeat.Items[j] = item
		}
		update.Repeats[i] = repeat
	}
	return update, nil
}

func applyMenuStateUpdate(menu NodeMenu, update MenuStateUpdate) (NodeMenu, error) {
	update, err := normalizeMenuStateUpdate(update)
	if err != nil {
		return NodeMenu{}, err
	}
	if update.Version != 0 && update.Version != menu.Version {
		return NodeMenu{}, ErrStaleMenu
	}
	out := cloneNodeMenu(menu)
	for _, fieldUpdate := range update.Fields {
		field := findMenuField(&out, fieldUpdate.Block, fieldUpdate.Field)
		if field == nil {
			return NodeMenu{}, ErrNotFound
		}
		if field.ReadOnly || field.Kind == MenuFieldReadOnly {
			return NodeMenu{}, ErrInvalidMenu
		}
		value, err := canonicalMenuValue(field.Kind, fieldUpdate.Value)
		if err != nil {
			return NodeMenu{}, err
		}
		if len(field.Options) > 0 && !menuValueInOptions(value, field.Options) {
			return NodeMenu{}, ErrTypeMismatch
		}
		field.Value = value
	}
	for _, repeatUpdate := range update.Repeats {
		repeat := findMenuRepeat(&out, repeatUpdate.Block, repeatUpdate.Repeat)
		if repeat == nil {
			return NodeMenu{}, ErrNotFound
		}
		items := make([]MenuRepeatItem, len(repeatUpdate.Items))
		template := make(map[string]MenuField, len(repeat.Template))
		for _, field := range repeat.Template {
			template[field.ID] = field
		}
		for i, itemUpdate := range repeatUpdate.Items {
			item := MenuRepeatItem{
				ID:       itemUpdate.ID,
				Title:    itemUpdate.Title,
				Metadata: cloneStringMap(itemUpdate.Metadata),
				Fields:   make([]MenuField, len(repeat.Template)),
			}
			for j, base := range repeat.Template {
				field := base
				if value, ok := itemUpdate.Fields[base.ID]; ok {
					if field.ReadOnly || field.Kind == MenuFieldReadOnly {
						return NodeMenu{}, ErrInvalidMenu
					}
					canonical, err := canonicalMenuValue(field.Kind, value)
					if err != nil {
						return NodeMenu{}, err
					}
					if len(field.Options) > 0 && !menuValueInOptions(canonical, field.Options) {
						return NodeMenu{}, ErrTypeMismatch
					}
					field.Value = canonical
				}
				item.Fields[j] = field
			}
			for fieldID := range itemUpdate.Fields {
				if template[fieldID].ID == "" {
					return NodeMenu{}, ErrNotFound
				}
			}
			items[i] = item
		}
		repeat.Items = items
	}
	return normalizeMenu(out, out.Version)
}

func findMenuField(menu *NodeMenu, blockID, fieldID string) *MenuField {
	for bi := range menu.Blocks {
		if menu.Blocks[bi].ID != blockID {
			continue
		}
		for fi := range menu.Blocks[bi].Fields {
			if menu.Blocks[bi].Fields[fi].ID == fieldID {
				return &menu.Blocks[bi].Fields[fi]
			}
		}
	}
	return nil
}

func findMenuRepeat(menu *NodeMenu, blockID, repeatID string) *MenuRepeat {
	for bi := range menu.Blocks {
		if menu.Blocks[bi].ID != blockID {
			continue
		}
		for ri := range menu.Blocks[bi].Repeats {
			if menu.Blocks[bi].Repeats[ri].ID == repeatID {
				return &menu.Blocks[bi].Repeats[ri]
			}
		}
	}
	return nil
}

func findMenuButton(menu NodeMenu, ref MenuButtonRef) (MenuButton, bool) {
	for _, block := range menu.Blocks {
		if block.ID != ref.Block {
			continue
		}
		for _, button := range block.Buttons {
			if button.ID == ref.Button {
				return button, true
			}
		}
	}
	return MenuButton{}, false
}

func cloneNodeMenu(menu NodeMenu) NodeMenu {
	out := NodeMenu{
		Version:  menu.Version,
		Blocks:   make([]MenuBlock, len(menu.Blocks)),
		Metadata: cloneStringMap(menu.Metadata),
	}
	for i, block := range menu.Blocks {
		out.Blocks[i] = cloneMenuBlock(block)
	}
	return out
}

func cloneMenuBlock(block MenuBlock) MenuBlock {
	out := MenuBlock{
		ID:       block.ID,
		Title:    block.Title,
		Fields:   cloneMenuFields(block.Fields),
		Buttons:  make([]MenuButton, len(block.Buttons)),
		Repeats:  make([]MenuRepeat, len(block.Repeats)),
		Metadata: cloneStringMap(block.Metadata),
	}
	for i, button := range block.Buttons {
		out.Buttons[i] = button
		out.Buttons[i].Metadata = cloneStringMap(button.Metadata)
	}
	for i, repeat := range block.Repeats {
		out.Repeats[i] = MenuRepeat{
			ID:       repeat.ID,
			Title:    repeat.Title,
			Template: cloneMenuFields(repeat.Template),
			Items:    make([]MenuRepeatItem, len(repeat.Items)),
			Metadata: cloneStringMap(repeat.Metadata),
		}
		for j, item := range repeat.Items {
			out.Repeats[i].Items[j] = MenuRepeatItem{
				ID:       item.ID,
				Title:    item.Title,
				Fields:   cloneMenuFields(item.Fields),
				Metadata: cloneStringMap(item.Metadata),
			}
		}
	}
	return out
}

func cloneMenuFields(fields []MenuField) []MenuField {
	out := make([]MenuField, len(fields))
	for i, field := range fields {
		out[i] = field
		out[i].Value = cloneJSONValue(field.Value)
		out[i].Options = make([]MenuOption, len(field.Options))
		for j, option := range field.Options {
			out[i].Options[j] = option
			out[i].Options[j].Value = cloneJSONValue(option.Value)
		}
		out[i].Metadata = cloneStringMap(field.Metadata)
	}
	return out
}

func validMenuID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' || r == '/' {
			continue
		}
		return false
	}
	return true
}

func validMenuFieldKind(kind MenuFieldKind) bool {
	switch kind {
	case MenuFieldReadOnly, MenuFieldString, MenuFieldInt64, MenuFieldFloat64, MenuFieldBool:
		return true
	default:
		return false
	}
}

func canonicalMenuValue(kind MenuFieldKind, value any) (any, error) {
	switch kind {
	case MenuFieldReadOnly:
		if !jsonCompatible(value) {
			return nil, ErrInvalidMenu
		}
		return cloneJSONValue(value), nil
	case MenuFieldString:
		v, ok := value.(string)
		if value == nil {
			return "", nil
		}
		if !ok {
			return nil, ErrTypeMismatch
		}
		return v, nil
	case MenuFieldBool:
		v, ok := value.(bool)
		if value == nil {
			return false, nil
		}
		if !ok {
			return nil, ErrTypeMismatch
		}
		return v, nil
	case MenuFieldInt64:
		return canonicalInt64(value)
	case MenuFieldFloat64:
		return canonicalFloat64(value)
	default:
		return nil, ErrInvalidMenu
	}
}

func canonicalInt64(value any) (int64, error) {
	if value == nil {
		return 0, nil
	}
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		if uint64(v) > math.MaxInt64 {
			return 0, ErrTypeMismatch
		}
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		if v > math.MaxInt64 {
			return 0, ErrTypeMismatch
		}
		return int64(v), nil
	case float64:
		if math.Trunc(v) != v || v < math.MinInt64 || v > math.MaxInt64 {
			return 0, ErrTypeMismatch
		}
		return int64(v), nil
	case json.Number:
		i, err := strconv.ParseInt(v.String(), 10, 64)
		if err != nil {
			return 0, ErrTypeMismatch
		}
		return i, nil
	default:
		return 0, ErrTypeMismatch
	}
}

func canonicalFloat64(value any) (float64, error) {
	if value == nil {
		return 0, nil
	}
	switch v := value.(type) {
	case float32:
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, ErrInvalidMenu
		}
		return f, nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, ErrInvalidMenu
		}
		return v, nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		i, err := canonicalInt64(v)
		return float64(i), err
	case json.Number:
		f, err := strconv.ParseFloat(v.String(), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, ErrTypeMismatch
		}
		return f, nil
	default:
		return 0, ErrTypeMismatch
	}
}

func menuValueInOptions(value any, options []MenuOption) bool {
	return slices.ContainsFunc(options, func(option MenuOption) bool {
		return reflect.DeepEqual(value, option.Value)
	})
}

func jsonCompatible(value any) bool {
	switch v := value.(type) {
	case nil, bool, string, json.Number:
		return true
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	case float32:
		f := float64(v)
		return !math.IsNaN(f) && !math.IsInf(f, 0)
	case float64:
		return !math.IsNaN(v) && !math.IsInf(v, 0)
	case map[string]any:
		for _, child := range v {
			if !jsonCompatible(child) {
				return false
			}
		}
		return true
	case []any:
		for _, child := range v {
			if !jsonCompatible(child) {
				return false
			}
		}
		return true
	case map[string]string, []string:
		return true
	default:
		return false
	}
}

func cloneJSONValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, child := range v {
			out[k] = cloneJSONValue(child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = cloneJSONValue(child)
		}
		return out
	case map[string]string:
		return cloneStringMap(v)
	case []string:
		return append([]string(nil), v...)
	case json.Number:
		return v
	default:
		return v
	}
}

func mergeStringMap(a, b map[string]string) map[string]string {
	out := cloneStringMap(a)
	if len(b) == 0 {
		return out
	}
	if out == nil {
		out = make(map[string]string, len(b))
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
