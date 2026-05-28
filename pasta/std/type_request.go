package std

import "io"

// RequestValue asks the right-directed endpoint of a link to send its current
// value back over the same link.
//
// Standard link types use RequestValue for input refreshes. Third-party nodes
// and link types should accept RequestValue on right-directed ports when they
// want to interoperate with standard nodes that can re-request active inputs,
// including SelectClass.
type RequestValue struct{}

// ClosablePayload marks event payloads that middleware nodes should close when
// a routed path changes and the payload should no longer remain usable through
// the old path.
//
// Middleware nodes such as Select may track ClosablePayload values that
// pass through them and call Close when switching to another path. Link types
// that carry capability-like resources can implement this interface to revoke
// access previously handed through middleware.
type ClosablePayload interface {
	io.Closer
}
