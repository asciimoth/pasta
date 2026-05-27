package std

import "github.com/asciimoth/pasta/pasta"

// Register adds all standard Pasta node classes to w.
func Register(w *pasta.Workspace) error {
	for _, class := range []pasta.NodeClass{
		IntConstantClass{},
		FloatConstantClass{},
		SubClass{},
		DivClass{},
		MulClass{},
		SumClass{},
	} {
		if err := w.AddNodeClass(class); err != nil {
			return err
		}
	}
	return nil
}
