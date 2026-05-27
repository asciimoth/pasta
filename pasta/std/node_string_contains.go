package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeStringContains is the class name for StringContainsClass.
const NodeTypeStringContains = "pasta/StringContains"

// StringContainsClass creates two-input nodes that output strings.Contains.
type StringContainsClass struct{}

func (StringContainsClass) ClassName() string        { return NodeTypeStringContains }
func (StringContainsClass) ShortDescription() string { return "String contains" }
func (StringContainsClass) LongDescription() string {
	return "Outputs whether input 1 contains input 2 as pasta/bool. Missing input values are empty strings."
}
func (StringContainsClass) DefaultNodeParams() pasta.NodeClassParams {
	return fixedStringParams(TypeBool, 2)
}
func (StringContainsClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	return newStringNode("contains", false), nil
}
