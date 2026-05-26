package pasta

import (
	"errors"
	"strings"
)

var (
	// ErrClassName reports that a class name is malformed.
	ErrClassName = errors.New("invalid class name")
	// ErrTypeName reports that a type name is malformed.
	ErrTypeName = errors.New("invalid type name")
	// ErrNodeName reports that a node name is malformed.
	ErrNodeName = errors.New("invalid node name")
)

const (
	// AnyType is a wildcard port and link type.
	//
	// Ports with this type can link to any other port regardless of its type
	// list. Node implementations should generally allow attached AnyType links
	// and ignore them when they have no specific handling, because wildcard
	// ports and links are commonly used as placeholders that may later be
	// replaced with a more specific type.
	AnyType = "any/any"
)

// ValidateClassName reports whether name is a valid class name.
//
// A class name has the form "example.com/ClassName". The prefix may contain
// only alphanumeric characters and dots. The suffix must start with an
// uppercase ASCII letter and contain only alphanumeric characters.
func ValidateClassName(name string) error {
	return validateName(name, isUpper, ErrClassName)
}

// ValidateTypeName reports whether name is a valid type name.
//
// A type name has the form "example.com/typeName". The prefix may contain only
// alphanumeric characters and dots. The suffix must start with a lowercase
// ASCII letter and contain only alphanumeric characters.
func ValidateTypeName(name string) error {
	if name == AnyType {
		return nil
	}
	return validateName(name, isLower, ErrTypeName)
}

// ValidateNodeName reports whether name is a valid node name.
//
// Node names must be non-empty and may contain any characters except '[' and ']'.
func ValidateNodeName(name string) error {
	if name == "" || strings.ContainsAny(name, "[]") {
		return ErrNodeName
	}
	return nil
}

func validateName(name string, firstNameChar func(byte) bool, err error) error {
	prefix, suffix, ok := strings.Cut(name, "/")
	if !ok || prefix == "" || suffix == "" || strings.Contains(suffix, "/") {
		return err
	}

	for i := 0; i < len(prefix); i++ {
		if !isAlphaNumeric(prefix[i]) && prefix[i] != '.' {
			return err
		}
	}

	if !firstNameChar(suffix[0]) {
		return err
	}
	for i := 1; i < len(suffix); i++ {
		if !isAlphaNumeric(suffix[i]) {
			return err
		}
	}

	return nil
}

func isAlphaNumeric(c byte) bool {
	return isUpper(c) || isLower(c) || ('0' <= c && c <= '9')
}

func isUpper(c byte) bool {
	return 'A' <= c && c <= 'Z'
}

func isLower(c byte) bool {
	return 'a' <= c && c <= 'z'
}
