package std

import (
	"encoding/json"
	"strconv"
)

// TypeInt is the Pasta link type carrying Int values backed by Go int.
//
// Values flow from right-directed ports to left-directed ports, which
// corresponds to left-to-right graph data flow. A node owning a right-directed
// pasta/int port sends its current Int value, whose underlying type is Go int,
// on every connected link when a new link connects, when OnReady runs, when the
// value changes, and when the peer sends RequestValue in the opposite
// direction. Int implements Comparable so any/any-compatible receivers can
// compare numeric payloads without knowing the concrete type in advance. A node
// owning a left-directed pasta/int port may send RequestValue when it needs the
// current value again. Left-directed receivers treat disconnected ports and
// connected links that have not yet delivered a value as 0. Standard
// left-directed pasta/int ports accept at most one link; right-directed
// pasta/int ports may have multiple outgoing links and broadcast the same value
// to each connected peer.
const TypeInt = "pasta/int"

func IntFromPayload(value any) (int, bool) {
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
