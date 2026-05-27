package std

// TypeInt is the Pasta link type carrying Int values backed by Go int.
//
// Values flow from right-directed ports to left-directed ports, which
// corresponds to left-to-right graph data flow. A node owning a right-directed
// pasta/int port sends its current Int value, whose underlying type is Go int,
// on every connected link when a new link connects, when OnReady runs, when the
// value changes, and when the peer sends RequestIntValue in the opposite
// direction. Int implements Comparable so any/any-compatible receivers can
// compare numeric payloads without knowing the concrete type in advance. A node
// owning a left-directed pasta/int port may send RequestIntValue when it needs
// the current value again. Left-directed receivers treat disconnected ports and
// connected links that have not yet delivered a value as 0. Standard
// left-directed pasta/int ports accept at most one link; right-directed
// pasta/int ports may have multiple outgoing links and broadcast the same value
// to each connected peer.
const TypeInt = "pasta/int"

// RequestIntValue asks the right-directed endpoint of a pasta/int link to send
// its current Int value back over the same link.
type RequestIntValue struct{}
