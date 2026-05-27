package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeBoolAnd is the class name for BoolAndClass.
const NodeTypeBoolAnd = "pasta/BoolAnd"

// BoolAndClass creates two-input boolean AND nodes.
type BoolAndClass struct{}

func (BoolAndClass) ClassName() string        { return NodeTypeBoolAnd }
func (BoolAndClass) ShortDescription() string { return "Boolean AND" }
func (BoolAndClass) LongDescription() string {
	return "Outputs input 1 && input 2 on a pasta/bool right-directed port. Missing inputs are false."
}
func (BoolAndClass) DefaultNodeParams() pasta.NodeClassParams {
	return fixedBoolParams(2)
}
func (BoolAndClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	return newBoolNode("and"), nil
}
