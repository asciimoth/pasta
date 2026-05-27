package std

// RequestValue asks the right-directed endpoint of a link to send its current
// value back over the same link.
//
// Standard link types use RequestValue for input refreshes. Third-party nodes
// and link types should accept RequestValue on right-directed ports when they
// want to interoperate with standard nodes that can re-request active inputs,
// including SelectClass.
type RequestValue struct{}
