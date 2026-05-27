package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeStringConcat is the class name for StringConcatClass.
const NodeTypeStringConcat = "pasta/StringConcat"

// StringConcatClass creates variadic string concatenation nodes.
type StringConcatClass struct{}

func (StringConcatClass) ClassName() string        { return NodeTypeStringConcat }
func (StringConcatClass) ShortDescription() string { return "Concatenate strings" }
func (StringConcatClass) LongDescription() string {
	return "Concatenates dynamically managed pasta/string inputs. Missing input values are empty strings."
}
func (StringConcatClass) DefaultNodeParams() pasta.NodeClassParams { return variadicStringParams() }
func (StringConcatClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	return newStringNode("concat", true), nil
}
