package std

// TypeFloat is the Pasta link type carrying Float values backed by Go float64.
//
// Values flow from right-directed ports to left-directed ports, which
// corresponds to left-to-right graph data flow. A node owning a right-directed
// pasta/float port sends its current Float value, whose underlying type is Go
// float64, on every connected link when a new link connects, when OnReady runs,
// when the value changes, and when the peer sends RequestFloatValue in the
// opposite direction. Float implements Comparable so any/any-compatible
// receivers can compare numeric payloads without knowing the concrete type in
// advance. A node owning a left-directed pasta/float port may send
// RequestFloatValue when it needs the current value again. Left-directed
// receivers treat disconnected ports and connected links that have not yet
// delivered a value as 0. Standard left-directed pasta/float ports accept at
// most one link; right-directed pasta/float ports may have multiple outgoing
// links and broadcast the same value to each connected peer.
const TypeFloat = "pasta/float"

// RequestFloatValue asks the right-directed endpoint of a pasta/float link to
// send its current Float value back over the same link.
type RequestFloatValue struct{}
