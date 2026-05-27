package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeStringUpper is the class name for StringUpperClass.
const NodeTypeStringUpper = "pasta/StringUpper"

// StringUpperClass creates one-input nodes that uppercase strings.
type StringUpperClass struct{}

func (StringUpperClass) ClassName() string        { return NodeTypeStringUpper }
func (StringUpperClass) ShortDescription() string { return "Uppercase string" }
func (StringUpperClass) LongDescription() string {
	return "Outputs strings.ToUpper(input 1) as pasta/string. Missing input values are empty strings."
}
func (StringUpperClass) DefaultNodeParams() pasta.NodeClassParams {
	return fixedStringParams(TypeString, 1)
}
func (StringUpperClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	return newStringNode("upper", false), nil
}
