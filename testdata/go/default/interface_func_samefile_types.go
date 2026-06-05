package sample

type Item struct {
	Value int
}

type ItemProcessor interface {
	Apply(func(Item) Item) Item
}

type localProcessor struct{}

func (p *localProcessor) Apply(fn func(Item) Item) Item {
	item := Item{Value: 1}
	return fn(item)
}

func runItemProcessor(p ItemProcessor, fn func(Item) Item) Item {
	return p.Apply(fn)
}
