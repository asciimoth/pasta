package std

// TypeTrigger is the Pasta link type for ephemeral on-demand trigger events.
//
// Trigger events flow through ordinary Pasta links from right-directed ports to
// left-directed ports, like pasta/int, pasta/float, pasta/string, and
// pasta/bool values. Unlike value links, a trigger link has no retained current
// value: nodes must not emit a trigger just because an input changed, a link was
// added, a workspace became ready, or another node requested a value.
//
// RequestValue is intentionally unsupported for trigger links. Nodes receiving
// RequestValue on a pasta/trigger link should ignore it, and middleware nodes
// such as Select, SelectOut, and Gateway must not store a trigger event for
// later replay. A trigger receiver should perform its action only for trigger
// events that are deliverable now.
//
// Trigger payloads are intentionally minimal. Standard nodes send Trigger{},
// but receivers should treat any non-RequestValue payload on a pasta/trigger
// link as a trigger so third-party nodes can attach domain-specific context.
//
// Trigger ports commonly appear before value ports in a node's port list. Both
// left-directed and right-directed trigger ports may have multiple links when a
// node wants fan-in, fan-out, or both. Nodes that also implement OnTrigger
// should handle OnTrigger and received trigger events consistently. Nodes with
// trigger outputs and no trigger inputs commonly emit a trigger event from
// OnTrigger as the menu/API equivalent of pressing a run button.
const TypeTrigger = "pasta/trigger"

// Trigger is the conventional payload sent on pasta/trigger links by standard
// nodes. Trigger receivers should rely on the link type, not this concrete
// payload type, when deciding whether to run.
type Trigger struct{}
