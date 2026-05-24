package pasta_test

import (
	"errors"
	"testing"

	"github.com/asciimoth/pasta/pasta"
)

func TestValidateClassName(t *testing.T) {
	tests := []struct {
		name string
		want error
	}{
		{name: "example.com/ClassName"},
		{name: "example1.com2/Class123"},
		{name: "example/Class"},
		{name: "", want: pasta.ErrClassName},
		{name: "example.com", want: pasta.ErrClassName},
		{name: "/ClassName", want: pasta.ErrClassName},
		{name: "example.com/", want: pasta.ErrClassName},
		{name: "example.com/className", want: pasta.ErrClassName},
		{name: "example.com/1Class", want: pasta.ErrClassName},
		{name: "example.com/ClassName/Other", want: pasta.ErrClassName},
		{name: "example-com/ClassName", want: pasta.ErrClassName},
		{name: "example.com/Class.Name", want: pasta.ErrClassName},
		{name: "example.com/\u041a\u043b\u0430\u0441\u0441", want: pasta.ErrClassName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pasta.ValidateClassName(tt.name)
			if !errors.Is(err, tt.want) {
				t.Fatalf("ValidateClassName(%q) = %v, want %v", tt.name, err, tt.want)
			}
		})
	}
}

func TestValidateTypeName(t *testing.T) {
	tests := []struct {
		name string
		want error
	}{
		{name: "example.com/typeName"},
		{name: "example1.com2/type123"},
		{name: "example/type"},
		{name: "", want: pasta.ErrTypeName},
		{name: "example.com", want: pasta.ErrTypeName},
		{name: "/typeName", want: pasta.ErrTypeName},
		{name: "example.com/", want: pasta.ErrTypeName},
		{name: "example.com/TypeName", want: pasta.ErrTypeName},
		{name: "example.com/1type", want: pasta.ErrTypeName},
		{name: "example.com/typeName/other", want: pasta.ErrTypeName},
		{name: "example-com/typeName", want: pasta.ErrTypeName},
		{name: "example.com/type.name", want: pasta.ErrTypeName},
		{name: "example.com/\u0442\u0438\u043f", want: pasta.ErrTypeName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pasta.ValidateTypeName(tt.name)
			if !errors.Is(err, tt.want) {
				t.Fatalf("ValidateTypeName(%q) = %v, want %v", tt.name, err, tt.want)
			}
		})
	}
}
