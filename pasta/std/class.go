package std

import (
	"github.com/asciimoth/pasta/pasta"
)

func fixedMathParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{InitialPorts: []pasta.Port{
		rightPort(pasta.AnyType),
		inputPort(1),
		inputPort(2),
	}}
}

func variadicMathParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{InitialPorts: []pasta.Port{
		rightPort(pasta.AnyType),
		inputPort(1),
	}}
}

func rightPort(typ string) pasta.Port {
	return pasta.Port{Direction: "right", Name: "output", Types: []string{typ}}
}

func inputPort(index int) pasta.Port {
	return pasta.Port{Direction: "left", Name: inputName(index), Types: []string{TypeInt, TypeFloat}}
}

func stringInputPort(index int) pasta.Port {
	return pasta.Port{Direction: "left", Name: inputName(index), Types: []string{TypeString}}
}

func variadicStringParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: TypeString, InitialPorts: []pasta.Port{
		rightPort(TypeString),
		stringInputPort(1),
	}}
}

func fixedStringParams(output string, inputs int) pasta.NodeClassParams {
	ports := []pasta.Port{rightPort(output)}
	for i := 1; i <= inputs; i++ {
		ports = append(ports, stringInputPort(i))
	}
	return pasta.NodeClassParams{PrimaryType: output, InitialPorts: ports}
}

func fixedBoolParams(inputs int) pasta.NodeClassParams {
	ports := []pasta.Port{rightPort(TypeBool)}
	for i := 1; i <= inputs; i++ {
		ports = append(ports, pasta.Port{Direction: "left", Name: inputName(i), Types: []string{TypeBool}})
	}
	return pasta.NodeClassParams{PrimaryType: TypeBool, InitialPorts: ports}
}

func comparisonParams() pasta.NodeClassParams {
	return pasta.NodeClassParams{PrimaryType: TypeBool, InitialPorts: []pasta.Port{
		rightPort(TypeBool),
		{Direction: "left", Name: inputName(1), Types: []string{pasta.AnyType}},
		{Direction: "left", Name: inputName(2), Types: []string{pasta.AnyType}},
	}}
}

func firstState(previous []*pasta.NodeClassState) *pasta.NodeClassState {
	if len(previous) == 0 {
		return nil
	}
	return previous[0]
}
