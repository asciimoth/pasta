package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeIntConstant is the class name for IntConstantClass.
const NodeTypeIntConstant = "pasta/IntConstant"

// IntConstantClass creates nodes with one editable int value and one
// right-directed pasta/int output port.
type IntConstantClass struct{}

func (IntConstantClass) ClassName() string        { return NodeTypeIntConstant }
func (IntConstantClass) ShortDescription() string { return "Integer constant" }
func (IntConstantClass) LongDescription() string {
	return "Outputs an editable int value on one pasta/int right-directed port. The value changes when the menu form is applied."
}
func (IntConstantClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: TypeInt, InitialPorts: []pasta.Port{rightPort(TypeInt)}}
}
func (IntConstantClass) NewNode(cfg configer.Config, _ ...*pasta.NodeClassState) (pasta.Node, error) {
	return newConstantNode(TypeInt, readInt(cfg, 0)), nil
}
