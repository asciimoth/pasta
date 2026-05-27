package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeBoolNot is the class name for BoolNotClass.
const NodeTypeBoolNot = "pasta/BoolNot"

// BoolNotClass creates one-input boolean NOT nodes.
type BoolNotClass struct{}

func (BoolNotClass) ClassName() string        { return NodeTypeBoolNot }
func (BoolNotClass) ShortDescription() string { return "Boolean NOT" }
func (BoolNotClass) LongDescription() string {
	return "Outputs !input 1 on a pasta/bool right-directed port. A missing input is false."
}
func (BoolNotClass) DefaultNodeParams() pasta.NodeClassParams {
	return fixedBoolParams(1)
}
func (BoolNotClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	return newBoolNode("not"), nil
}
