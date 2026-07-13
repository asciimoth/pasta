package std

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/asciimoth/persist"
)

// TypeObject is the Pasta link type carrying immutable JSON-like object values.
//
// Object values flow from right-directed ports to left-directed ports like the
// other standard value types. A pasta/object payload is an Object backed by a
// persist.Value whose top-level kind is nil, map, or vector. Map and vector
// contents may contain the full persist JSON value domain: nil, int, float,
// string, bool, map, and vector. Standard left-directed pasta/object ports
// accept at most one incoming link; right-directed pasta/object ports may fan
// out to any number of receivers.
const TypeObject = "pasta/object"

// Object is the event payload used by standard pasta/object producers.
//
// The wrapped persist.Value is immutable. Its top level is always nil, a
// persist.Map, or a persist.Vector; nested collection values may contain scalar
// JSON leaves.
type Object struct {
	value persist.Value
}

// NilObject returns the null object value.
func NilObject() Object {
	return Object{value: persist.Nil()}
}

// MapObject wraps m as an object value.
func MapObject(m persist.Map) Object {
	return Object{value: persist.MapValue(m)}
}

// VectorObject wraps v as an object value.
func VectorObject(v persist.Vector) Object {
	return Object{value: persist.VectorValue(v)}
}

// ObjectFromValue wraps v when v is nil, a map, or a vector.
func ObjectFromValue(v persist.Value) (Object, bool) {
	switch v.Kind() {
	case persist.KindNil, persist.KindMap, persist.KindVector:
		return Object{value: v}, true
	default:
		return Object{}, false
	}
}

// ObjectFromPayload extracts an Object from a pasta/object event payload.
func ObjectFromPayload(payload any) (Object, bool) {
	switch v := payload.(type) {
	case Object:
		return v, true
	case persist.Value:
		return ObjectFromValue(v)
	case persist.Map:
		return MapObject(v), true
	case persist.Vector:
		return VectorObject(v), true
	case nil:
		return NilObject(), true
	default:
		return Object{}, false
	}
}

// ObjectFromJSON parses a JSON object, array, or null document into Object.
func ObjectFromJSON(data []byte) (Object, error) {
	value, err := persist.FromJSON(data)
	if err != nil {
		return Object{}, err
	}
	object, ok := ObjectFromValue(value)
	if !ok {
		return Object{}, fmt.Errorf("pasta/object: top-level JSON value must be object, array, or null, got %s", value.Kind())
	}
	return object, nil
}

// Value returns the immutable persist value backing o.
func (o Object) Value() persist.Value {
	return o.value
}

// Map extracts the top-level map value.
func (o Object) Map() (persist.Map, bool) {
	return o.value.Map()
}

// Vector extracts the top-level vector value.
func (o Object) Vector() (persist.Vector, bool) {
	return o.value.Vector()
}

// IsNil reports whether o is the null object value.
func (o Object) IsNil() bool {
	return o.value.Kind() == persist.KindNil
}

// Equal reports deep JSON value equality.
func (o Object) Equal(other Object) bool {
	return o.value.Equal(other.value)
}

// JSON returns o encoded as compact JSON.
func (o Object) JSON() ([]byte, error) {
	return o.value.ToJSON()
}

// JSONString returns o encoded as compact JSON. It returns "null" if encoding
// fails, which can happen only for non-string map keys or non-finite floats.
func (o Object) JSONString() string {
	data, err := o.JSON()
	if err != nil {
		return "null"
	}
	return string(data)
}

// PrettyJSONString returns o encoded as indented JSON for node menus.
func (o Object) PrettyJSONString() string {
	data, err := o.JSON()
	if err != nil {
		return "null"
	}
	var out bytes.Buffer
	if err := json.Indent(&out, data, "", "  "); err != nil {
		return string(data)
	}
	return out.String()
}

func objectFromConfigValue(value any) (Object, bool) {
	data, err := json.Marshal(value)
	if err != nil {
		return Object{}, false
	}
	object, err := ObjectFromJSON(data)
	return object, err == nil
}

func objectToConfigValue(object Object) (any, error) {
	return persistValueToConfigValue(object.value)
}

func persistValueToConfigValue(value persist.Value) (any, error) {
	switch value.Kind() {
	case persist.KindNil:
		return nil, nil
	case persist.KindInt:
		v, _ := value.Int64()
		return v, nil
	case persist.KindFloat:
		v, _ := value.Float64()
		return v, nil
	case persist.KindString:
		v, _ := value.StringValue()
		return v, nil
	case persist.KindBool:
		v, _ := value.BoolValue()
		return v, nil
	case persist.KindMap:
		m, _ := value.Map()
		out := make(map[string]any, m.Len())
		var err error
		m.Range(func(k persist.Key, v persist.Value) bool {
			if k.Kind() != persist.KindString {
				err = errors.New("pasta/object: object map keys must be strings to save as JSON config")
				return false
			}
			key, _ := k.StringValue()
			out[key], err = persistValueToConfigValue(v)
			return err == nil
		})
		if err != nil {
			return nil, err
		}
		return out, nil
	case persist.KindVector:
		v, _ := value.Vector()
		out := make([]any, v.Len())
		var err error
		v.Range(func(i int, x persist.Value) bool {
			out[i], err = persistValueToConfigValue(x)
			return err == nil
		})
		if err != nil {
			return nil, err
		}
		return out, nil
	default:
		return nil, errors.New("pasta/object: invalid persist value")
	}
}

func readObject(cfgValue any, fallback Object) Object {
	if object, ok := objectFromConfigValue(cfgValue); ok {
		return object
	}
	return fallback
}
