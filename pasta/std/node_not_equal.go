package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeNotEqual is the class name for NotEqualClass.
const NodeTypeNotEqual = "pasta/NotEqual"

// NotEqualClass creates two-input numeric comparison nodes that output input 1
// != input 2 as pasta/bool. Inputs accept any/any-compatible numeric payloads.
type NotEqualClass struct{}

func (NotEqualClass) ClassName() string        { return NodeTypeNotEqual }
func (NotEqualClass) ShortDescription() string { return "Inequality comparison" }
func (NotEqualClass) LongDescription() string {
	return "Outputs whether input 1 is not equal to input 2. Inputs accept any/any links and cast event payloads to Comparable."
}
func (NotEqualClass) DefaultNodeParams() pasta.NodeClassParams { return comparisonParams() }
func (NotEqualClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	return newCompareNode("notEqual"), nil
}
