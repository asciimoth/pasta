package std

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/formular"
)

type numberValue struct {
	typ string
	i   int
	f   float64
}

func intValue(v int) numberValue {
	return numberValue{typ: TypeInt, i: v, f: float64(v)}
}

func floatValue(v float64) numberValue {
	return numberValue{typ: TypeFloat, i: int(v), f: v}
}

func zeroValue(typ string) numberValue {
	if typ == TypeFloat {
		return floatValue(0)
	}
	return intValue(0)
}

func oneValue(typ string) numberValue {
	if typ == TypeFloat {
		return floatValue(1)
	}
	return intValue(1)
}

func valueFromPayload(typ string, payload any) (numberValue, bool) {
	switch typ {
	case TypeInt:
		switch v := payload.(type) {
		case Int:
			return intValue(int(v)), true
		case int:
			return intValue(v), true
		default:
			return intValue(0), false
		}
	case TypeFloat:
		switch v := payload.(type) {
		case Float:
			return floatValue(float64(v)), true
		case float64:
			return floatValue(v), true
		default:
			return floatValue(0), false
		}
	default:
		return numberValue{}, false
	}
}

func (v numberValue) payload() any {
	if v.typ == TypeFloat {
		return Float(v.f)
	}
	return Int(v.i)
}

func (v numberValue) menuValue() any {
	if v.typ == TypeFloat {
		return v.f
	}
	return v.i
}

func (v numberValue) as(typ string) numberValue {
	if typ == TypeFloat {
		return floatValue(v.f)
	}
	return intValue(v.i)
}

func (v numberValue) label() string {
	if v.typ == TypeFloat {
		return strconv.FormatFloat(v.f, 'g', -1, 64)
	}
	return strconv.Itoa(v.i)
}

func parseIntAny(value any) (int, bool) {
	switch v := value.(type) {
	case Int:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		i, err := v.Int64()
		if err == nil {
			return int(i), true
		}
		f, err := v.Float64()
		return int(f), err == nil
	case string:
		i, err := strconv.Atoi(v)
		return i, err == nil
	default:
		return 0, false
	}
}

func parseFloatAny(value any) (float64, bool) {
	switch v := value.(type) {
	case Float:
		return float64(v), true
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func readInt(cfg configer.Config, fallback int) int {
	if cfg == nil {
		return fallback
	}
	raw, err := cfg.Get(configer.Path{"value"})
	if err != nil {
		return fallback
	}
	if v, ok := parseIntAny(raw); ok {
		return v
	}
	return fallback
}

func readFloat(cfg configer.Config, fallback float64) float64 {
	if cfg == nil {
		return fallback
	}
	raw, err := cfg.Get(configer.Path{"value"})
	if err != nil {
		return fallback
	}
	if v, ok := parseFloatAny(raw); ok {
		return v
	}
	return fallback
}

func parseBoolAny(value any) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		b, err := strconv.ParseBool(v)
		return b, err == nil
	default:
		return false, false
	}
}

func readBool(cfg configer.Config, fallback bool) bool {
	if cfg == nil {
		return fallback
	}
	raw, err := cfg.Get(configer.Path{"value"})
	if err != nil {
		return fallback
	}
	if v, ok := parseBoolAny(raw); ok {
		return v
	}
	return fallback
}

func parseStringAny(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case String:
		return string(v), true
	default:
		return "", false
	}
}

func readString(cfg configer.Config, fallback string) string {
	if cfg == nil {
		return fallback
	}
	raw, err := cfg.Get(configer.Path{"value"})
	if err != nil {
		return fallback
	}
	if v, ok := parseStringAny(raw); ok {
		return v
	}
	return fallback
}

func menuFieldKind(typ string) string {
	if typ == TypeString {
		return formular.FieldText
	}
	if typ == TypeFloat {
		return formular.FieldFloat
	}
	return formular.FieldInt
}

func formatSaveValue(v numberValue) string {
	if v.typ == TypeFloat {
		return strconv.FormatFloat(v.f, 'g', -1, 64)
	}
	return strconv.Itoa(v.i)
}

func inputName(index int) string {
	return fmt.Sprintf("input %d", index)
}
