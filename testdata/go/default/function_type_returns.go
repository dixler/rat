package sample

type parseWithError func(string) (int, error)

type parseWithoutError func(string) int

type pointerFactory func(int) *int

type sliceFactory func(int) []int

type mapFactory func(string) map[string]int

type functionTypeHolder struct {
	WithError    func(string) (string, error)
	WithoutError func(string) string
	Mutable      func(int) (*int, []int, map[string]int)
	NonMutable   func(int) (int, string, bool)
}

func useFunctionTypes(text string, err error) (int, error) {
	withError := func(value string) (int, error) {
		if value == "" {
			return 0, err
		}
		return len(value), nil
	}
	withoutError := func(value string) int {
		return len(value)
	}
	return combineFunctionTypes(withError, withoutError, text)
}

func combineFunctionTypes(withError parseWithError, withoutError parseWithoutError, text string) (int, error) {
	parsed, err := withError(text)
	if err != nil {
		return 0, err
	}
	return parsed + withoutError(text), nil
}

func returnMutableFunctionTypes(seed int) (pointerFactory, sliceFactory, mapFactory) {
	return func(extra int) *int {
			value := seed + extra
			return &value
		}, func(extra int) []int {
			return []int{seed, extra}
		}, func(name string) map[string]int {
			return map[string]int{name: seed}
		}
}

func returnNonMutableFunctionType(seed int) func(int) (int, string, bool) {
	return func(extra int) (int, string, bool) {
		return seed + extra, "ready", seed == extra
	}
}
