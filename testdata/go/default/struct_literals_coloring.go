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
	ptr := &Point{X: base.X, Y: base.Y}
	return Line{
		From: Point{X: ptr.X, Y: ptr.Y},
		To:   Point{X: base.X + dx, Y: base.Y + dx},
	}
}
