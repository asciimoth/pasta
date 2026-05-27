package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeTrueConstant is the class name for TrueConstantClass.
const NodeTypeTrueConstant = "pasta/TrueConstant"

// TrueConstantClass creates nodes with one right-directed pasta/bool output
// port that always carries true.
type TrueConstantClass struct{}

func (TrueConstantClass) ClassName() string        { return NodeTypeTrueConstant }
func (TrueConstantClass) ShortDescription() string { return "True constant" }
func (TrueConstantClass) LongDescription() string {
	return "Outputs true on one pasta/bool right-directed port. The label and readonly menu field show the current value."
}
func (TrueConstantClass) DefaultNodeParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: TypeBool, InitialPorts: []pasta.Port{rightPort(TypeBool)}}
}
func (TrueConstantClass) NewNode(cfg configer.Config, _ ...*pasta.NodeClassState) (pasta.Node, error) {
	return newBoolConstantNode(readBool(cfg, true)), nil
}
