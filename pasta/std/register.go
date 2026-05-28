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
	return []pasta.NodeClass{
		IntConstantClass{},
		FloatConstantClass{},
		StringConstantClass{},
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
		SelectClass{},
		BoolConstantClass{},
	}
}
