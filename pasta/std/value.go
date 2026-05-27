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

func valueFromPayload(typ string, payload any) (numberValue, bool) {
	switch typ {
	case TypeInt:
		v, ok := payload.(int)
		if !ok {
			return intValue(0), false
		}
		return intValue(v), true
	case TypeFloat:
		v, ok := payload.(float64)
		if !ok {
			return floatValue(0), false
		}
		return floatValue(v), true
	default:
		return numberValue{}, false
	}
}

func (v numberValue) payload() any {
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

func menuFieldKind(typ string) string {
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
