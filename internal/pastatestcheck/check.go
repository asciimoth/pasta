package pastatestcheck

import (
	"errors"
	"reflect"
	"testing"
)

func Require(t *testing.T, condition bool, format string, args ...any) {
	t.Helper()
	if !condition {
		t.Fatalf(format, args...)
	}
}

func NoError(t *testing.T, err error, format string, args ...any) {
	t.Helper()
	if err != nil {
		args = append(args, err)
		t.Fatalf(format, args...)
	}
}

func ErrorIs(t *testing.T, err, target error, format string, args ...any) {
	t.Helper()
	if !errors.Is(err, target) {
		args = append(args, err)
		t.Fatalf(format, args...)
	}
}

func DeepEqual(t *testing.T, got, want any, format string, args ...any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf(format, args...)
	}
}
