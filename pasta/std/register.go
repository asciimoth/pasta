package std

import "github.com/asciimoth/pasta/pasta"

// Register adds all standard Pasta node classes to w.
func Register(w *pasta.Workspace) error {
	for _, class := range StdClasses() {
		if err := w.AddNodeClass(class); err != nil {
			return err
		}
	}
	return nil
}

func StdClasses() []pasta.NodeClass {
	variables := newVariableClassStores()
	return []pasta.NodeClass{
		IntConstantClass{},
		FloatConstantClass{},
		StringConstantClass{},
		ObjectConstantClass{},
		ObjectPackerClass{},
		ObjectUnpackerClass{},
		ObjectToStringClass{},
		SubClass{},
		DivClass{},
		MulClass{},
		SumClass{},
		StringConcatClass{},
		StringFormatClass{},
		StringLengthClass{},
		StringContainsClass{},
		StringSplitClass{},
		StringUpperClass{},
		StringLowerClass{},
		StringTrimSpaceClass{},
		TrueConstantClass{},
		FalseConstantClass{},
		BoolAndClass{},
		BoolNotClass{},
		BoolOrClass{},
		MoreClass{},
		LessClass{},
		EqualClass{},
		NotEqualClass{},
		TriggerClass{},
		PopUpClass{},
		GatewayClass{},
		SelectClass{},
		SelectOutClass{},
		BoolConstantClass{},
		BoolGetClass{store: variables.get(TypeBool)},
		BoolSetClass{store: variables.get(TypeBool)},
		IntGetClass{store: variables.get(TypeInt)},
		IntSetClass{store: variables.get(TypeInt)},
		FloatGetClass{store: variables.get(TypeFloat)},
		FloatSetClass{store: variables.get(TypeFloat)},
		StringGetClass{store: variables.get(TypeString)},
		StringSetClass{store: variables.get(TypeString)},
		ObjectGetClass{store: variables.get(TypeObject)},
		ObjectSetClass{store: variables.get(TypeObject)},
	}
}
