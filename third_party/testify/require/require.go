package require

import "github.com/stretchr/testify/assert"

type TestingT interface {
	assert.TestingT
	FailNow()
}

func Equal(t TestingT, expected, actual any, msgAndArgs ...any) {
	if !assert.Equal(t, expected, actual, msgAndArgs...) {
		t.FailNow()
	}
}
func True(t TestingT, value bool, msgAndArgs ...any) {
	if !assert.True(t, value, msgAndArgs...) {
		t.FailNow()
	}
}
func False(t TestingT, value bool, msgAndArgs ...any) {
	if !assert.False(t, value, msgAndArgs...) {
		t.FailNow()
	}
}
func Len(t TestingT, object any, length int, msgAndArgs ...any) {
	if !assert.Len(t, object, length, msgAndArgs...) {
		t.FailNow()
	}
}
func Contains(t TestingT, s, contains any, msgAndArgs ...any) {
	if !assert.Contains(t, s, contains, msgAndArgs...) {
		t.FailNow()
	}
}
func NotNil(t TestingT, object any, msgAndArgs ...any) {
	if !assert.NotNil(t, object, msgAndArgs...) {
		t.FailNow()
	}
}
func NoError(t TestingT, err error, msgAndArgs ...any) {
	if !assert.NoError(t, err, msgAndArgs...) {
		t.FailNow()
	}
}
func Error(t TestingT, err error, msgAndArgs ...any) {
	if !assert.Error(t, err, msgAndArgs...) {
		t.FailNow()
	}
}
