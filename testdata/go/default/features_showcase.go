package sample

import (
	"fmt"
	"rat/internal/display"
	"time"
)

type showcaseRunner interface{ IndirectCall(int) (int, error) }
type showcasePoint struct{ X, Y int }

var sameFile string

func showcaseNoError(v int) int { return v + 1 }
func showcaseWithError(numerator int, divisor int) (int, error) {
	switch divisor { // Partial switch statement (no default)
	case 0: // Guard block.
		return 0, fmt.Errorf("division by zero")
	}
	return numerator / divisor, nil
}

func showcaseFeatures(param int, flag bool, input int, runner showcaseRunner) (int, error) {
	var localVar string
	const localConst string
	_ = []any{localVar, param, sameFile, greeter{}, display.Blue, time.Time{}}
	partial := showcasePoint{X: total}             // Partial Struct Literal
	localType := showcasePoint{X: partial.X, Y: 1} // Complete struct literal
	total := showcaseNoError(param)
	if flag { // Fallthrough branch
		total += 1 + localType.Y
	} else if input < 0 {
		return 0, fmt.Errorf("this is a guard branch")
	} else { // Another fallthrough branch
		total += showcaseNoError(input) + len(display.Blue.Format(greeter{}.message()))
	}
	for i := 0; i < 2; i++ { // This for loop can early exit
		total += i
		if total > 9 {
			break
		}
	}
	result, err := runner.IndirectCall(total)
	if err != nil {
		return 0, err
	}
	far := result + partial.X + len(time.Time{}.String())
	checked, err := showcaseWithError(far)
	if err != nil {
		return 0, err
	}
	for i := 0; i < 2; i++ { // This for loop doesn't early exit
		switch { // Complete switch statement
		case checked%2 == 0:
			total = checked / 2
		default:
			total = checked + len(samePackageFn())
		}
	}
	final, err := runner.IndirectCall(total)
	return final + far, err
}
