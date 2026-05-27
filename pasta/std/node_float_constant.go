package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeFloatConstant is the class name for FloatConstantClass.
const NodeTypeFloatConstant = "pasta/FloatConstant"

// FloatConstantClass creates nodes with one editable float64 value and one
// right-directed pasta/float output port.
type FloatConstantClass struct{}

func (FloatConstantClass) ClassName() string        { return NodeTypeFloatConstant }
func (FloatConstantClass) ShortDescription() string { return "Floating-point constant" }
func (FloatConstantClass) LongDescription() string {
	return "Outputs an editable float64 value on one pasta/float right-directed port. The label and menu field always show the current value."
}
func (FloatConstantClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: TypeFloat, InitialPorts: []pasta.Port{rightPort(TypeFloat)}}
}
func (FloatConstantClass) NewNode(cfg configer.Config, _ ...*pasta.NodeClassState) (pasta.Node, error) {
	return newConstantNode(TypeFloat, readFloat(cfg, 0)), nil
}
