package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeStringLower is the class name for StringLowerClass.
const NodeTypeStringLower = "pasta/StringLower"

// StringLowerClass creates one-input nodes that lowercase strings.
type StringLowerClass struct{}

func (StringLowerClass) ClassName() string        { return NodeTypeStringLower }
func (StringLowerClass) ShortDescription() string { return "Lowercase string" }
func (StringLowerClass) LongDescription() string {
	return "Outputs strings.ToLower(input 1) as pasta/string. Missing input values are empty strings."
}
func (StringLowerClass) DefaultNodeParams() pasta.NodeClassParams {
	return fixedStringParams(TypeString, 1)
}
func (StringLowerClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	return newStringNode("lower", false), nil
}
