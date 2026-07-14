package std

import "github.com/asciimoth/pasta/pasta"

// TypeLoop is the Pasta link type used to associate one loop node with one
// iteration node.
//
// Loop links carry LoopStartIteration messages from a loop node to an Iter
// node and LoopEndIteration messages back from Iter to the loop node. Unlike
// value links and trigger links, pasta/loop links are control-flow associations
// and are excluded from ordinary graph dependency cycle checks.
const TypeLoop = pasta.LoopLinkType

// LoopStartIteration tells an Iter node that a new loop iteration has started.
//
// Iteration is intentionally small and wraps naturally; receivers only use it
// to distinguish the current iteration from stale messages.
type LoopStartIteration struct {
	Iteration uint32
}

// LoopEndIteration tells a loop node that the current iteration has ended.
//
// Break is true for break and false for continue. Iteration must match the
// LoopStartIteration that opened the iteration.
type LoopEndIteration struct {
	Iteration uint32
	Break     bool
}
