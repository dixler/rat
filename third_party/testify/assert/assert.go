package assert

import (
	"fmt"
	"reflect"
	"strings"
)

type TestingT interface{ Errorf(string, ...any) }

func Equal(t TestingT, expected, actual any, msgAndArgs ...any) bool {
	if reflect.DeepEqual(expected, actual) {
		return true
	}
	t.Errorf("not equal: expected=%v actual=%v%s", expected, actual, msg(msgAndArgs...))
	return false
}

func True(t TestingT, value bool, msgAndArgs ...any) bool {
	if value {
		return true
	}
	t.Errorf("expected true%s", msg(msgAndArgs...))
	return false
}

func False(t TestingT, value bool, msgAndArgs ...any) bool {
	if !value {
		return true
	}
	t.Errorf("expected false%s", msg(msgAndArgs...))
	return false
}

func Len(t TestingT, object any, length int, msgAndArgs ...any) bool {
	v := reflect.ValueOf(object)
	if !v.IsValid() {
		t.Errorf("invalid value%s", msg(msgAndArgs...))
		return false
	}
	if v.Len() == length {
		return true
	}
	t.Errorf("length mismatch: got=%d want=%d%s", v.Len(), length, msg(msgAndArgs...))
	return false
}

func Contains(t TestingT, s, contains any, msgAndArgs ...any) bool {
	if ss, ok := s.(string); ok {
		cs, ok := contains.(string)
		if ok && strings.Contains(ss, cs) {
			return true
		}
		t.Errorf("%q does not contain %q%s", ss, contains, msg(msgAndArgs...))
		return false
	}
	v := reflect.ValueOf(s)
	for i := 0; i < v.Len(); i++ {
		if reflect.DeepEqual(v.Index(i).Interface(), contains) {
			return true
		}
	}
	t.Errorf("container does not contain value%s", msg(msgAndArgs...))
	return false
}

func NotNil(t TestingT, object any, msgAndArgs ...any) bool {
	if object == nil {
		t.Errorf("expected non-nil%s", msg(msgAndArgs...))
		return false
	}
	v := reflect.ValueOf(object)
	if v.Kind() >= reflect.Chan && v.Kind() <= reflect.Slice && v.IsNil() {
		t.Errorf("expected non-nil%s", msg(msgAndArgs...))
		return false
	}
	return true
}

func NoError(t TestingT, err error, msgAndArgs ...any) bool {
	if err == nil {
		return true
	}
	t.Errorf("unexpected error: %v%s", err, msg(msgAndArgs...))
	return false
}

func Error(t TestingT, err error, msgAndArgs ...any) bool {
	if err != nil {
		return true
	}
	t.Errorf("expected error%s", msg(msgAndArgs...))
	return false
}

func msg(args ...any) string {
	if len(args) == 0 {
		return ""
	}
	return ": " + fmt.Sprint(args...)
}
