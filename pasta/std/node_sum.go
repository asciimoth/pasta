package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeSum is the class name for SumClass.
const NodeTypeSum = "pasta/Sum"

// SumClass creates variadic summation nodes.
//
// Inputs are named "input 1", "input 2", and so on. The node adds another
// input when all existing inputs are linked and removes trailing free inputs
// until only one free input remains. Inputs are kept sorted by index.
type SumClass struct{}

func (SumClass) ClassName() string        { return NodeTypeSum }
func (SumClass) ShortDescription() string { return "Sum values" }
func (SumClass) LongDescription() string {
	return "Sums dynamically managed inputs. The first attached input selects pasta/int or pasta/float output; missing input values are 0."
}
func (SumClass) DefaultNodeParams() pasta.NodeClassParams { return variadicMathParams() }
func (SumClass) NewNode(_ configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
	return newMathNode("sum", true, previous...), nil
}
