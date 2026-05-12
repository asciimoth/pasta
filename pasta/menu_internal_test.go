package pasta

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"testing"
)

func TestMenuValidationBranches(t *testing.T) {
	invalidMenus := []struct {
		name string
		menu NodeMenu
		err  error
	}{
		{
			name: "invalid block id",
			menu: NodeMenu{Blocks: []MenuBlock{{ID: ""}}},
			err:  ErrInvalidName,
		},
		{
			name: "invalid button id",
			menu: NodeMenu{Blocks: []MenuBlock{{ID: "main", Buttons: []MenuButton{{ID: "bad id"}}}}},
			err:  ErrInvalidName,
		},
		{
			name: "duplicate field",
			menu: NodeMenu{Blocks: []MenuBlock{{ID: "main", Fields: []MenuField{{ID: "name", Kind: MenuFieldString}, {ID: "name", Kind: MenuFieldString}}}}},
			err:  ErrDuplicate,
		},
		{
			name: "duplicate button",
			menu: NodeMenu{Blocks: []MenuBlock{{ID: "main", Buttons: []MenuButton{{ID: "run"}, {ID: "run"}}}}},
			err:  ErrDuplicate,
		},
		{
			name: "invalid repeat id",
			menu: NodeMenu{Blocks: []MenuBlock{{ID: "main", Repeats: []MenuRepeat{{ID: ""}}}}},
			err:  ErrInvalidName,
		},
		{
			name: "duplicate repeat",
			menu: NodeMenu{Blocks: []MenuBlock{{ID: "main", Repeats: []MenuRepeat{
				{ID: "rows", Template: []MenuField{{ID: "name", Kind: MenuFieldString}}},
				{ID: "rows", Template: []MenuField{{ID: "name", Kind: MenuFieldString}}},
			}}}},
			err: ErrDuplicate,
		},
		{
			name: "repeat without template",
			menu: NodeMenu{Blocks: []MenuBlock{{ID: "main", Repeats: []MenuRepeat{{ID: "rows"}}}}},
			err:  ErrInvalidMenu,
		},
		{
			name: "duplicate repeat template field",
			menu: NodeMenu{Blocks: []MenuBlock{{ID: "main", Repeats: []MenuRepeat{{
				ID:       "rows",
				Template: []MenuField{{ID: "name", Kind: MenuFieldString}, {ID: "name", Kind: MenuFieldString}},
			}}}}},
			err: ErrDuplicate,
		},
		{
			name: "invalid repeat template field",
			menu: NodeMenu{Blocks: []MenuBlock{{ID: "main", Repeats: []MenuRepeat{{
				ID:       "rows",
				Template: []MenuField{{ID: "name", Kind: "bad"}},
			}}}}},
			err: ErrInvalidMenu,
		},
		{
			name: "invalid repeat item id",
			menu: NodeMenu{Blocks: []MenuBlock{{ID: "main", Repeats: []MenuRepeat{{
				ID:       "rows",
				Template: []MenuField{{ID: "name", Kind: MenuFieldString}},
				Items:    []MenuRepeatItem{{ID: ""}},
			}}}}},
			err: ErrInvalidName,
		},
		{
			name: "duplicate repeat item",
			menu: NodeMenu{Blocks: []MenuBlock{{ID: "main", Repeats: []MenuRepeat{{
				ID:       "rows",
				Template: []MenuField{{ID: "name", Kind: MenuFieldString}},
				Items:    []MenuRepeatItem{{ID: "one"}, {ID: "one"}},
			}}}}},
			err: ErrDuplicate,
		},
		{
			name: "repeat item unknown field",
			menu: NodeMenu{Blocks: []MenuBlock{{ID: "main", Repeats: []MenuRepeat{{
				ID:       "rows",
				Template: []MenuField{{ID: "name", Kind: MenuFieldString}},
				Items:    []MenuRepeatItem{{ID: "one", Fields: []MenuField{{ID: "missing"}}}},
			}}}}},
			err: ErrNotFound,
		},
		{
			name: "repeat item invalid field id",
			menu: NodeMenu{Blocks: []MenuBlock{{ID: "main", Repeats: []MenuRepeat{{
				ID:       "rows",
				Template: []MenuField{{ID: "name", Kind: MenuFieldString}},
				Items:    []MenuRepeatItem{{ID: "one", Fields: []MenuField{{ID: "bad id"}}}},
			}}}}},
			err: ErrInvalidName,
		},
		{
			name: "repeat item duplicate field",
			menu: NodeMenu{Blocks: []MenuBlock{{ID: "main", Repeats: []MenuRepeat{{
				ID:       "rows",
				Template: []MenuField{{ID: "name", Kind: MenuFieldString}},
				Items:    []MenuRepeatItem{{ID: "one", Fields: []MenuField{{ID: "name"}, {ID: "name"}}}},
			}}}}},
			err: ErrDuplicate,
		},
		{
			name: "repeat item invalid value",
			menu: NodeMenu{Blocks: []MenuBlock{{ID: "main", Repeats: []MenuRepeat{{
				ID:       "rows",
				Template: []MenuField{{ID: "name", Kind: MenuFieldString}},
				Items:    []MenuRepeatItem{{ID: "one", Fields: []MenuField{{ID: "name", Value: 1}}}},
			}}}}},
			err: ErrTypeMismatch,
		},
		{
			name: "invalid field kind",
			menu: NodeMenu{Blocks: []MenuBlock{{ID: "main", Fields: []MenuField{{ID: "field", Kind: "other"}}}}},
			err:  ErrInvalidMenu,
		},
		{
			name: "invalid field id",
			menu: NodeMenu{Blocks: []MenuBlock{{ID: "main", Fields: []MenuField{{ID: "", Kind: MenuFieldString}}}}},
			err:  ErrInvalidName,
		},
		{
			name: "invalid render",
			menu: NodeMenu{Blocks: []MenuBlock{{ID: "main", Fields: []MenuField{{ID: "field", Kind: MenuFieldString, Render: MenuRenderCheckbox}}}}},
			err:  ErrInvalidMenu,
		},
		{
			name: "bad option value",
			menu: NodeMenu{Blocks: []MenuBlock{{ID: "main", Fields: []MenuField{{ID: "field", Kind: MenuFieldInt64, Value: int64(1), Options: []MenuOption{{Value: "bad"}}}}}}},
			err:  ErrTypeMismatch,
		},
	}
	for _, tt := range invalidMenus {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := normalizeMenu(tt.menu, 1); !errors.Is(err, tt.err) {
				t.Fatalf("normalizeMenu error = %v, want %v", err, tt.err)
			}
		})
	}

	menu, err := normalizeMenu(NodeMenu{Metadata: map[string]string{"owner": "test"}, Blocks: []MenuBlock{{
		ID: "main",
		Repeats: []MenuRepeat{{
			ID:       "rows",
			Template: []MenuField{{ID: "name", Label: "Name", Kind: MenuFieldString, Metadata: map[string]string{"base": "yes"}}},
			Items: []MenuRepeatItem{{
				ID:       "one",
				Title:    "One",
				Metadata: map[string]string{"item": "yes"},
				Fields:   []MenuField{{ID: "name", Value: "alpha", Label: "Custom", ReadOnly: true, Metadata: map[string]string{"override": "yes"}}},
			}},
		}},
	}}}, 3)
	if err != nil {
		t.Fatal(err)
	}
	field := menu.Blocks[0].Repeats[0].Items[0].Fields[0]
	if field.Value != "alpha" || field.Label != "Custom" || !field.ReadOnly || field.Metadata["base"] != "yes" || field.Metadata["override"] != "yes" {
		t.Fatalf("merged repeat field = %#v", field)
	}
}

func TestMenuStateUpdateValidationBranches(t *testing.T) {
	invalidUpdates := []struct {
		name   string
		update MenuStateUpdate
		err    error
	}{
		{name: "negative version", update: MenuStateUpdate{Version: -1}, err: ErrInvalidMenu},
		{name: "invalid field id", update: MenuStateUpdate{Fields: []MenuFieldUpdate{{Block: "main", Field: "bad id"}}}, err: ErrInvalidName},
		{name: "duplicate field", update: MenuStateUpdate{Fields: []MenuFieldUpdate{{Block: "main", Field: "name"}, {Block: "main", Field: "name"}}}, err: ErrDuplicate},
		{name: "non json field", update: MenuStateUpdate{Fields: []MenuFieldUpdate{{Block: "main", Field: "name", Value: func() {}}}}, err: ErrInvalidMenu},
		{name: "invalid repeat id", update: MenuStateUpdate{Repeats: []MenuRepeatUpdate{{Block: "main", Repeat: "bad id"}}}, err: ErrInvalidName},
		{name: "duplicate repeat", update: MenuStateUpdate{Repeats: []MenuRepeatUpdate{{Block: "main", Repeat: "rows"}, {Block: "main", Repeat: "rows"}}}, err: ErrDuplicate},
		{name: "invalid repeat item", update: MenuStateUpdate{Repeats: []MenuRepeatUpdate{{Block: "main", Repeat: "rows", Items: []MenuRepeatItemState{{ID: ""}}}}}, err: ErrInvalidName},
		{name: "duplicate repeat item", update: MenuStateUpdate{Repeats: []MenuRepeatUpdate{{Block: "main", Repeat: "rows", Items: []MenuRepeatItemState{{ID: "one"}, {ID: "one"}}}}}, err: ErrDuplicate},
		{name: "invalid repeat field id", update: MenuStateUpdate{Repeats: []MenuRepeatUpdate{{Block: "main", Repeat: "rows", Items: []MenuRepeatItemState{{ID: "one", Fields: map[string]any{"bad id": 1}}}}}}, err: ErrInvalidName},
		{name: "non json repeat field", update: MenuStateUpdate{Repeats: []MenuRepeatUpdate{{Block: "main", Repeat: "rows", Items: []MenuRepeatItemState{{ID: "one", Fields: map[string]any{"value": math.NaN()}}}}}}, err: ErrInvalidMenu},
	}
	for _, tt := range invalidUpdates {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := normalizeMenuStateUpdate(tt.update); !errors.Is(err, tt.err) {
				t.Fatalf("normalizeMenuStateUpdate error = %v, want %v", err, tt.err)
			}
		})
	}

	update, err := normalizeMenuStateUpdate(MenuStateUpdate{
		Fields: []MenuFieldUpdate{{Block: "main", Field: "json", Value: map[string]any{"list": []any{json.Number("1"), map[string]string{"k": "v"}, []string{"a"}}}}},
		Repeats: []MenuRepeatUpdate{{Block: "main", Repeat: "rows", Items: []MenuRepeatItemState{{
			ID:       "one",
			Metadata: map[string]string{"m": "v"},
			Fields:   map[string]any{"json": []any{map[string]any{"n": json.Number("2")}}},
		}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	update.Fields[0].Value.(map[string]any)["list"].([]any)[1].(map[string]string)["k"] = "mutated"
	if got := update.Repeats[0].Items[0].Fields["json"].([]any)[0].(map[string]any)["n"]; got != json.Number("2") {
		t.Fatalf("normalized update clone changed unexpectedly: %#v", got)
	}
}

func TestApplyMenuStateUpdateBranches(t *testing.T) {
	menu := NodeMenu{Version: 2, Blocks: []MenuBlock{{
		ID: "main",
		Fields: []MenuField{
			{ID: "choice", Kind: MenuFieldString, Value: "a", Options: []MenuOption{{Value: "a"}, {Value: "b"}}},
			{ID: "locked", Kind: MenuFieldString, Value: "x", ReadOnly: true},
		},
		Repeats: []MenuRepeat{{
			ID:       "rows",
			Template: []MenuField{{ID: "name", Kind: MenuFieldString}, {ID: "count", Kind: MenuFieldInt64, Value: int64(1), Options: []MenuOption{{Value: int64(1)}}}, {ID: "locked", Kind: MenuFieldReadOnly, Value: "x"}},
		}},
	}}}
	menu, err := normalizeMenu(menu, menu.Version)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		update MenuStateUpdate
		err    error
	}{
		{name: "invalid update", update: MenuStateUpdate{Version: -1}, err: ErrInvalidMenu},
		{name: "stale", update: MenuStateUpdate{Version: 1}, err: ErrStaleMenu},
		{name: "missing field", update: MenuStateUpdate{Version: 2, Fields: []MenuFieldUpdate{{Block: "main", Field: "missing", Value: "x"}}}, err: ErrNotFound},
		{name: "readonly field", update: MenuStateUpdate{Version: 2, Fields: []MenuFieldUpdate{{Block: "main", Field: "locked", Value: "x"}}}, err: ErrInvalidMenu},
		{name: "option mismatch", update: MenuStateUpdate{Version: 2, Fields: []MenuFieldUpdate{{Block: "main", Field: "choice", Value: "c"}}}, err: ErrTypeMismatch},
		{name: "missing repeat", update: MenuStateUpdate{Version: 2, Repeats: []MenuRepeatUpdate{{Block: "main", Repeat: "missing"}}}, err: ErrNotFound},
		{name: "readonly repeat field", update: MenuStateUpdate{Version: 2, Repeats: []MenuRepeatUpdate{{Block: "main", Repeat: "rows", Items: []MenuRepeatItemState{{ID: "one", Fields: map[string]any{"locked": "y"}}}}}}, err: ErrInvalidMenu},
		{name: "repeat field type mismatch", update: MenuStateUpdate{Version: 2, Repeats: []MenuRepeatUpdate{{Block: "main", Repeat: "rows", Items: []MenuRepeatItemState{{ID: "one", Fields: map[string]any{"count": "bad"}}}}}}, err: ErrTypeMismatch},
		{name: "repeat field option mismatch", update: MenuStateUpdate{Version: 2, Repeats: []MenuRepeatUpdate{{Block: "main", Repeat: "rows", Items: []MenuRepeatItemState{{ID: "one", Fields: map[string]any{"count": int64(2)}}}}}}, err: ErrTypeMismatch},
		{name: "unknown repeat field", update: MenuStateUpdate{Version: 2, Repeats: []MenuRepeatUpdate{{Block: "main", Repeat: "rows", Items: []MenuRepeatItemState{{ID: "one", Fields: map[string]any{"missing": "y"}}}}}}, err: ErrNotFound},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := applyMenuStateUpdate(menu, tt.update); !errors.Is(err, tt.err) {
				t.Fatalf("applyMenuStateUpdate error = %v, want %v", err, tt.err)
			}
		})
	}

	next, err := applyMenuStateUpdate(menu, MenuStateUpdate{
		Version: 2,
		Fields:  []MenuFieldUpdate{{Block: "main", Field: "choice", Value: "b"}},
		Repeats: []MenuRepeatUpdate{{Block: "main", Repeat: "rows", Items: []MenuRepeatItemState{{ID: "one", Title: "One", Fields: map[string]any{"name": "alpha", "count": int64(1)}, Metadata: map[string]string{"m": "v"}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if next.Blocks[0].Fields[0].Value != "b" || next.Blocks[0].Repeats[0].Items[0].Fields[0].Value != "alpha" {
		t.Fatalf("updated menu = %#v", next)
	}
}

func TestMenuCanonicalValueBranches(t *testing.T) {
	intCases := []any{nil, int(1), int8(2), int16(3), int32(4), int64(5), uint(6), uint8(7), uint16(8), uint32(9), uint64(10), float64(11), json.Number("12")}
	for _, input := range intCases {
		if _, err := canonicalInt64(input); err != nil {
			t.Fatalf("canonicalInt64(%T) error = %v", input, err)
		}
	}
	for _, input := range []any{uint64(math.MaxInt64) + 1, float64(1.5), json.Number("nope"), "bad"} {
		if _, err := canonicalInt64(input); !errors.Is(err, ErrTypeMismatch) {
			t.Fatalf("canonicalInt64(%T) error = %v, want type mismatch", input, err)
		}
	}
	if strconv.IntSize == 64 {
		if _, err := canonicalInt64(uint(math.MaxInt64) + 1); !errors.Is(err, ErrTypeMismatch) {
			t.Fatalf("canonicalInt64(uint overflow) error = %v, want type mismatch", err)
		}
	}

	floatCases := []any{nil, float32(1.25), float64(2.5), int(3), int8(4), int16(5), int32(6), int64(7), uint(8), uint8(9), uint16(10), uint32(11), uint64(12), json.Number("13.5")}
	for _, input := range floatCases {
		if _, err := canonicalFloat64(input); err != nil {
			t.Fatalf("canonicalFloat64(%T) error = %v", input, err)
		}
	}
	for _, input := range []any{float32(float32(math.Inf(1))), math.NaN(), json.Number("NaN"), "bad"} {
		if _, err := canonicalFloat64(input); err == nil {
			t.Fatalf("canonicalFloat64(%T) succeeded, want error", input)
		}
	}

	if got, err := canonicalMenuValue(MenuFieldString, nil); err != nil || got != "" {
		t.Fatalf("nil string canonical = %#v, %v", got, err)
	}
	if got, err := canonicalMenuValue(MenuFieldBool, nil); err != nil || got != false {
		t.Fatalf("nil bool canonical = %#v, %v", got, err)
	}
	if _, err := canonicalMenuValue(MenuFieldBool, "bad"); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("bool type mismatch error = %v, want type mismatch", err)
	}
	if _, err := canonicalMenuValue(MenuFieldReadOnly, map[string]any{"bad": func() {}}); !errors.Is(err, ErrInvalidMenu) {
		t.Fatalf("read-only invalid json error = %v, want invalid menu", err)
	}
	if _, err := canonicalMenuValue("unknown", nil); !errors.Is(err, ErrInvalidMenu) {
		t.Fatalf("unknown kind error = %v, want invalid menu", err)
	}
}

func TestMenuMarshalUnmarshalErrorsAndFindMisses(t *testing.T) {
	if _, err := MarshalNodeMenu(NodeMenu{Blocks: []MenuBlock{{ID: ""}}}); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("MarshalNodeMenu validation error = %v, want invalid name", err)
	}
	if _, err := MarshalNodeMenu(NodeMenu{Blocks: []MenuBlock{{ID: "main", Fields: []MenuField{{ID: "value", Kind: MenuFieldReadOnly, Value: json.Number("not-a-number")}}}}}); err == nil {
		t.Fatal("MarshalNodeMenu with invalid json.Number succeeded, want marshal error")
	}
	if _, err := UnmarshalNodeMenu("{"); err == nil {
		t.Fatal("UnmarshalNodeMenu invalid JSON succeeded")
	}
	if _, err := UnmarshalNodeMenu(`{"blocks":[{"id":""}]}`); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("UnmarshalNodeMenu validation error = %v, want invalid name", err)
	}

	if _, err := MarshalMenuStateUpdate(MenuStateUpdate{Version: -1}); !errors.Is(err, ErrInvalidMenu) {
		t.Fatalf("MarshalMenuStateUpdate validation error = %v, want invalid menu", err)
	}
	if _, err := MarshalMenuStateUpdate(MenuStateUpdate{Fields: []MenuFieldUpdate{{Block: "main", Field: "value", Value: json.Number("not-a-number")}}}); err == nil {
		t.Fatal("MarshalMenuStateUpdate with invalid json.Number succeeded, want marshal error")
	}
	if _, err := UnmarshalMenuStateUpdate("{"); err == nil {
		t.Fatal("UnmarshalMenuStateUpdate invalid JSON succeeded")
	}
	if _, err := UnmarshalMenuStateUpdate(`{"version":-1}`); !errors.Is(err, ErrInvalidMenu) {
		t.Fatalf("UnmarshalMenuStateUpdate validation error = %v, want invalid menu", err)
	}

	menu := NodeMenu{Blocks: []MenuBlock{{ID: "other"}, {ID: "main"}}}
	if findMenuField(&menu, "other", "field") != nil || findMenuRepeat(&menu, "other", "repeat") != nil {
		t.Fatal("find helpers returned values for missing block")
	}
	if _, ok := findMenuButton(menu, MenuButtonRef{Block: "main", Button: "missing"}); ok {
		t.Fatal("findMenuButton found missing button")
	}
}

func TestMenuCloneAndMergeBranches(t *testing.T) {
	value := map[string]any{"list": []any{map[string]any{"n": json.Number("1")}, []string{"a", "b"}, map[string]string{"k": "v"}}}
	cloned := cloneJSONValue(value).(map[string]any)
	if !reflect.DeepEqual(value, cloned) {
		t.Fatalf("cloneJSONValue = %#v, want %#v", cloned, value)
	}
	cloned["list"].([]any)[0].(map[string]any)["n"] = json.Number("2")
	if value["list"].([]any)[0].(map[string]any)["n"] != json.Number("1") {
		t.Fatal("cloneJSONValue leaked nested mutation")
	}

	if got := mergeStringMap(nil, map[string]string{"b": "2"}); got["b"] != "2" {
		t.Fatalf("mergeStringMap nil base = %#v", got)
	}
	if got := mergeStringMap(map[string]string{"a": "1"}, nil); got["a"] != "1" || len(got) != 1 {
		t.Fatalf("mergeStringMap nil override = %#v", got)
	}
	if got := firstNonEmpty("", "", "value"); got != "value" {
		t.Fatalf("firstNonEmpty = %q, want value", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Fatalf("firstNonEmpty all empty = %q, want empty", got)
	}
	if jsonCompatible(float32(math.NaN())) {
		t.Fatal("jsonCompatible accepted NaN float32")
	}
	if jsonCompatible([]any{func() {}}) {
		t.Fatal("jsonCompatible accepted invalid array child")
	}
}
