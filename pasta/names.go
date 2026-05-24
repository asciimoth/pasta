package pasta

import (
	"errors"
	"strings"
)

var (
	ErrClassName = errors.New("invalid class name")
	ErrTypeName  = errors.New("invalid type name")
)

// Class name looks like `example.com/ClassName`.
// It can contain only alpha-numeric chars, `.` and single `/`
// Name part (righter from `/`) must starts with capital letter and contain
// only alpha-numeric
func ValidateClassName(name string) error {
	return validateName(name, isUpper, ErrClassName)
}

// Class name looks like `example.com/typeName`.
// It can contain only alpha-numeric chars, `.` and single `/`
// Name part (righter from `/`) must starts with small letter and contain
// only alpha-numeric
func ValidateTypeName(name string) error {
	return validateName(name, isLower, ErrTypeName)
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
