package sample

type Reader interface {
	Read([]byte) (int, error)
}

func applyFn(fn func(int) int, v int) int {
	return fn(v)
}

func consumeReader(r Reader, buf []byte) int {
	n, _ := r.Read(buf)
	return n
}
