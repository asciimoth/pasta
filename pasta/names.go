package pasta

import "strings"

// ValidLibraryName reports whether name is a domain-like library name.
func ValidLibraryName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if isASCIILetter(c) || isASCIIDigit(c) || c == '.' || c == '-' {
			continue
		}
		return false
	}
	return isASCIILetter(name[0])
}

// ValidQualifiedName reports whether name is a URL-like class or type name.
func ValidQualifiedName(name string) bool {
	if name == "" || !isASCIILetter(name[0]) {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if isASCIILetter(c) || isASCIIDigit(c) || c == '.' || c == '-' || c == '/' {
			continue
		}
		return false
	}
	return true
}

// ValidClassName reports whether className is a valid class under libraryName.
func ValidClassName(libraryName, className string) bool {
	if !ValidLibraryName(libraryName) || !ValidQualifiedName(className) {
		return false
	}
	local, ok := strings.CutPrefix(className, libraryName+"/")
	return ok && local != "" && isASCIIUpper(local[0])
}

// ValidTypeName reports whether typeName is a valid namespaced type name.
func ValidTypeName(typeName string) bool {
	if !ValidQualifiedName(typeName) {
		return false
	}
	slash := strings.LastIndexByte(typeName, '/')
	if slash < 0 || slash == len(typeName)-1 {
		return false
	}
	return isASCIILower(typeName[slash+1])
}

func isASCIILetter(c byte) bool { return isASCIILower(c) || isASCIIUpper(c) }
func isASCIILower(c byte) bool  { return c >= 'a' && c <= 'z' }
func isASCIIUpper(c byte) bool  { return c >= 'A' && c <= 'Z' }
func isASCIIDigit(c byte) bool  { return c >= '0' && c <= '9' }
