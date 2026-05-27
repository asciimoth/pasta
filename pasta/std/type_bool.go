package std

// TypeBool is the Pasta link type carrying Go bool values.
//
// Values flow from right-directed ports to left-directed ports, which
// corresponds to left-to-right graph data flow. A node owning a right-directed
// pasta/bool port sends its current bool value on every connected link when a
// new link connects, when OnReady runs, when the value changes, and when the
// peer sends RequestBoolValue in the opposite direction. A node owning a
// left-directed pasta/bool port may send RequestBoolValue when it needs the
// current value again. Left-directed receivers treat disconnected ports and
// connected links that have not yet delivered a value as false. Standard
// left-directed pasta/bool ports accept at most one link; right-directed
// pasta/bool ports may have multiple outgoing links and broadcast the same
// value to each connected peer.
const TypeBool = "pasta/bool"

// RequestBoolValue asks the right-directed endpoint of a pasta/bool link to
// send its current bool value back over the same link.
type RequestBoolValue struct{}
