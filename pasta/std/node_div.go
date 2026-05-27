package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeDiv is the class name for DivClass.
const NodeTypeDiv = "pasta/Div"

// DivClass creates two-input division nodes.
//
// The first attached input selects pasta/int or pasta/float as the node primary
// and output type. The other input is converted to that type. Division by zero
// produces 0 instead of panic or error.
type DivClass struct{}

func (DivClass) ClassName() string        { return NodeTypeDiv }
func (DivClass) ShortDescription() string { return "Divide two values" }
func (DivClass) LongDescription() string {
	return "Divides input 1 by input 2. The first attached input selects pasta/int or pasta/float output; division by zero produces 0."
}
func (DivClass) DefaultNodeParams() pasta.NodeClassParams { return fixedMathParams() }
func (DivClass) NewNode(_ configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
	return newMathNode("div", false, previous...), nil
}
