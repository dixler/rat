package sample

type Box[T any] interface {
	Put(v T)
}

type holder[T any] struct {
	value T
}

func (h *holder[T]) Put(v T) {
	h.value = v
}

func useHolder[T any](h *holder[T], v T) {
	h.Put(v)
}
