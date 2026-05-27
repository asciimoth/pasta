package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeStringTrimSpace is the class name for StringTrimSpaceClass.
const NodeTypeStringTrimSpace = "pasta/StringTrimSpace"

// StringTrimSpaceClass creates one-input nodes that trim leading/trailing space.
type StringTrimSpaceClass struct{}

func (StringTrimSpaceClass) ClassName() string        { return NodeTypeStringTrimSpace }
func (StringTrimSpaceClass) ShortDescription() string { return "Trim string space" }
func (StringTrimSpaceClass) LongDescription() string {
	return "Outputs strings.TrimSpace(input 1) as pasta/string. Missing input values are empty strings."
}
func (StringTrimSpaceClass) DefaultNodeParams() pasta.NodeClassParams {
	return fixedStringParams(TypeString, 1)
}
func (StringTrimSpaceClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	return newStringNode("trimSpace", false), nil
}
