package std

import (
	"github.com/asciimoth/configer/configer"
	"github.com/asciimoth/pasta/pasta"
)

// NodeTypeMore is the class name for MoreClass.
const NodeTypeMore = "pasta/More"

// MoreClass creates two-input numeric comparison nodes that output input 1 >
// input 2 as pasta/bool. Inputs accept any/any-compatible numeric payloads.
type MoreClass struct{}

func (MoreClass) ClassName() string        { return NodeTypeMore }
func (MoreClass) ShortDescription() string { return "Greater-than comparison" }
func (MoreClass) LongDescription() string {
	return "Outputs whether input 1 is more than input 2. Inputs accept any/any links and cast event payloads to Comparable."
}
func (MoreClass) DefaultNodeParams() pasta.NodeClassParams { return comparisonParams() }
func (MoreClass) NewNode(configer.Config, ...*pasta.NodeClassState) (pasta.Node, error) {
	return newCompareNode("more"), nil
}
