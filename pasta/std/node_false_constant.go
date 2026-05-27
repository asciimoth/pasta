package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeFalseConstant is the class name for FalseConstantClass.
const NodeTypeFalseConstant = "pasta/FalseConstant"

// FalseConstantClass creates nodes with one right-directed pasta/bool output
// port that always carries false.
type FalseConstantClass struct{}

func (FalseConstantClass) ClassName() string        { return NodeTypeFalseConstant }
func (FalseConstantClass) ShortDescription() string { return "False constant" }
func (FalseConstantClass) LongDescription() string {
	return "Outputs false on one pasta/bool right-directed port. The label and readonly menu field show the current value."
}
func (FalseConstantClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: TypeBool, InitialPorts: []pasta.Port{rightPort(TypeBool)}}
}
func (FalseConstantClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	return newBoolConstantNode(false), nil
}
