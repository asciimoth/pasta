package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeEqual is the class name for EqualClass.
const NodeTypeEqual = "pasta/Equal"

// EqualClass creates two-input numeric comparison nodes that output input 1 ==
// input 2 as pasta/bool. Inputs accept any/any-compatible numeric payloads.
type EqualClass struct{}

func (EqualClass) ClassName() string        { return NodeTypeEqual }
func (EqualClass) ShortDescription() string { return "Equality comparison" }
func (EqualClass) LongDescription() string {
	return "Outputs whether input 1 equals input 2. Inputs accept any/any links and cast event payloads to Comparable."
}
func (EqualClass) DefaultNodeParams() pasta.NodeClassParams { return comparisonParams() }
func (EqualClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	return newCompareNode("equal"), nil
}
