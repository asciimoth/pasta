package std

// TypeString is the Pasta link type carrying String values backed by Go string.
//
// Values flow from right-directed ports to left-directed ports, which
// corresponds to left-to-right graph data flow. A node owning a right-directed
// pasta/string port sends its current String value on every connected link when
// a new link connects, when OnReady runs, when the value changes, and when the
// peer sends RequestValue in the opposite direction. String implements
// Comparable using Go's lexicographic string ordering so any/any-compatible
// receivers can compare string payloads without knowing the concrete type in
// advance. A node owning a left-directed pasta/string port may send RequestValue
// when it needs the current value again. Left-directed receivers treat
// disconnected ports and connected links that have not yet delivered a value as
// the empty string. Standard left-directed pasta/string ports accept at most one
// link; right-directed pasta/string ports may have multiple outgoing links and
// broadcast the same value to each connected peer.
const TypeString = "pasta/string"

func StringFromPayload(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case String:
		return string(v), true
	default:
		return "", false
	}
}
