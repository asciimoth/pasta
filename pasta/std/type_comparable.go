package std

// Comparable is implemented by event payloads that can compare
// themselves with another value carried over an any/any-compatible link.
//
// Implementations accept any because any/any links deliberately defer payload
// interpretation to cooperating nodes. Unsupported peer values compare false
// for More, Less, and Equal, and true for NotEqual.
type Comparable interface {
	More(any) bool
	Less(any) bool
	Equal(any) bool
	NotEqual(any) bool
}

// Int is the event payload used by standard pasta/int producers.
type Int int

// Float is the event payload used by standard pasta/float producers.
type Float float64

// String is the event payload used by standard pasta/string producers.
type String string

func (v Int) More(other any) bool {
	o, ok := comparableFloat(other)
	return ok && float64(v) > o
}

func (v Int) Less(other any) bool {
	o, ok := comparableFloat(other)
	return ok && float64(v) < o
}

func (v Int) Equal(other any) bool {
	o, ok := comparableFloat(other)
	return ok && float64(v) == o
}

func (v Int) NotEqual(other any) bool {
	return !v.Equal(other)
}

func (v Float) More(other any) bool {
	o, ok := comparableFloat(other)
	return ok && float64(v) > o
}

func (v Float) Less(other any) bool {
	o, ok := comparableFloat(other)
	return ok && float64(v) < o
}

func (v Float) Equal(other any) bool {
	o, ok := comparableFloat(other)
	return ok && float64(v) == o
}

func (v Float) NotEqual(other any) bool {
	return !v.Equal(other)
}

func (v String) More(other any) bool {
	o, ok := comparableString(other)
	return ok && string(v) > o
}

func (v String) Less(other any) bool {
	o, ok := comparableString(other)
	return ok && string(v) < o
}

func (v String) Equal(other any) bool {
	o, ok := comparableString(other)
	return ok && string(v) == o
}

func (v String) NotEqual(other any) bool {
	return !v.Equal(other)
}

func comparableFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case Int:
		return float64(v), true
	case Float:
		return float64(v), true
	case int:
		return float64(v), true
	case float64:
		return v, true
	default:
		return 0, false
	}
}

func comparableString(value any) (string, bool) {
	switch v := value.(type) {
	case String:
		return string(v), true
	case string:
		return v, true
	default:
		return "", false
	}
}
