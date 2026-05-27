package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeStringLength is the class name for StringLengthClass.
const NodeTypeStringLength = "pasta/StringLength"

// StringLengthClass creates one-input nodes that output len(input) as pasta/int.
type StringLengthClass struct{}

func (StringLengthClass) ClassName() string        { return NodeTypeStringLength }
func (StringLengthClass) ShortDescription() string { return "String length" }
func (StringLengthClass) LongDescription() string {
	return "Outputs the byte length of one pasta/string input as pasta/int. Missing input values are empty strings."
}
func (StringLengthClass) DefaultNodeParams() pasta.NodeClassParams {
	return fixedStringParams(TypeInt, 1)
}
func (StringLengthClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	return newStringNode("length", false), nil
}
