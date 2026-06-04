package sample

func mapSlice[T any](in []T, fn func(T) T) []T {
	out := make([]T, 0, len(in))
	for _, T := range in {
		T := fn(T)
		out = append(out, T)
	}
	return out
}
