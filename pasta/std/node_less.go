package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeLess is the class name for LessClass.
const NodeTypeLess = "pasta/Less"

// LessClass creates two-input numeric comparison nodes that output input 1 <
// input 2 as pasta/bool. Inputs accept any/any-compatible numeric payloads.
type LessClass struct{}

func (LessClass) ClassName() string        { return NodeTypeLess }
func (LessClass) ShortDescription() string { return "Less-than comparison" }
func (LessClass) LongDescription() string {
	return "Outputs whether input 1 is less than input 2. Inputs accept any/any links and cast event payloads to Comparable."
}
func (LessClass) DefaultNodeParams() pasta.NodeClassParams { return comparisonParams() }
func (LessClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	return newCompareNode("less"), nil
}
