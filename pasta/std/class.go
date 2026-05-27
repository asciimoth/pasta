package std

import (
	"errors"

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

func firstState(previous []*pasta.NodeClassState) *pasta.NodeClassState {
	if len(previous) == 0 {
		return nil
	}
	return previous[0]
}

func errUnsupportedType(typ string) error {
	return errors.New("unsupported pasta std type " + typ)
}
