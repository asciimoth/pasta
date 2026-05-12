package pastatest_test

import (
	"testing"

	"github.com/asciimoth/pasta/pasta"
	"github.com/asciimoth/pasta/pasta/pastatest"
)

const valueType = "example.com/value"

func TestRunSuiteStaticLibrary(t *testing.T) {
	source := pasta.ClassSpec{
		Name: "example.com/Source",
		Default: pasta.NodeState{
			DisplayName: "Source",
			Metadata:    map[string]string{"default": "source"},
		},
		Outputs: []pasta.PortSpec{{
			ID:        pastatest.Output(1),
			Name:      "out",
			Direction: pasta.OutputPort,
			FixedType: valueType,
			Metadata:  map[string]string{"side": "out"},
		}},
		Metadata: map[string]string{"kind": "source"},
	}
	sink := pasta.ClassSpec{
		Name: "example.com/Sink",
		Inputs: []pasta.PortSpec{{
			ID:        pastatest.Input(1),
			Name:      "in",
			Direction: pasta.InputPort,
			FixedType: valueType,
		}},
		Metadata: map[string]string{"kind": "sink"},
	}
	pastatest.RunSuite(t, pastatest.StaticSuite("example.com", []pasta.ClassSpec{source, sink}, []pastatest.LinkCase{{
		Name:   "source to sink",
		Input:  pastatest.Endpoint{Class: sink.Name, Port: pastatest.Input(1)},
		Output: pastatest.Endpoint{Class: source.Name, Port: pastatest.Output(1)},
		Type:   valueType,
	}}))
}
