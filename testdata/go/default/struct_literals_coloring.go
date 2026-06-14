package sample

type Point struct {
	X int
	Y int
}

type Line struct {
	From Point
	To   Point
}

func buildLine(dx int) Line {
	base := Point{X: 1, Y: dx}
	partial := Point{X: 1}
	positional := Point{1, dx}
	missing := Point{1}
	ptr := &Point{X: base.X, Y: base.Y}
	_ = partial
	_ = positional
	_ = missing
	return Line{
		From: Point{X: ptr.X, Y: ptr.Y},
		To:   Point{X: base.X + dx, Y: base.Y + dx},
	}
}
