package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeSub is the class name for SubClass.
const NodeTypeSub = "pasta/Sub"

// SubClass creates two-input subtraction nodes.
//
// The first attached input selects pasta/int or pasta/float as the node primary
// and output type. The other input is converted to that type. Missing values are
// 0, and the readonly label/menu field show input 1 - input 2.
type SubClass struct{}

func (SubClass) ClassName() string        { return NodeTypeSub }
func (SubClass) ShortDescription() string { return "Subtract two values" }
func (SubClass) LongDescription() string {
	return "Subtracts input 2 from input 1. The first attached input selects pasta/int or pasta/float output; missing inputs and invalid operations produce 0-compatible values."
}
func (SubClass) DefaultNodeParams() pasta.NodeClassParams { return fixedMathParams() }
func (SubClass) NewNode(_ configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
	return newMathNode("sub", false, previous...), nil
}
