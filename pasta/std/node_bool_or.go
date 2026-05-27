package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeBoolOr is the class name for BoolOrClass.
const NodeTypeBoolOr = "pasta/BoolOr"

// BoolOrClass creates two-input boolean OR nodes.
type BoolOrClass struct{}

func (BoolOrClass) ClassName() string        { return NodeTypeBoolOr }
func (BoolOrClass) ShortDescription() string { return "Boolean OR" }
func (BoolOrClass) LongDescription() string {
	return "Outputs input 1 || input 2 on a pasta/bool right-directed port. Missing inputs are false."
}
func (BoolOrClass) DefaultNodeParams() pasta.NodeClassParams {
	return fixedBoolParams(2)
}
func (BoolOrClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	return newBoolNode("or"), nil
}
