package sample

type Handler struct {
	Map func(int) int
}

func useHandler(h Handler, v int) int {
	return h.Map(v)
}
