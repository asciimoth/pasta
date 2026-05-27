package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeMul is the class name for MulClass.
const NodeTypeMul = "pasta/Mul"

// MulClass creates variadic multiplication nodes.
//
// Inputs are named "input 1", "input 2", and so on. The node adds another
// input when all existing inputs are linked and removes trailing free inputs
// until only one free input remains. Inputs are kept sorted by index.
type MulClass struct{}

func (MulClass) ClassName() string        { return NodeTypeMul }
func (MulClass) ShortDescription() string { return "Multiply values" }
func (MulClass) LongDescription() string {
	return "Multiplies dynamically managed inputs. The first attached input selects pasta/int or pasta/float output; missing input values are 0."
}
func (MulClass) DefaultNodeParams() pasta.NodeClassParams { return variadicMathParams() }
func (MulClass) NewNode(_ configer.Config, previous ...*pasta.NodeClassState) (pasta.Node, error) {
	return newMathNode("mul", true, previous...), nil
}
