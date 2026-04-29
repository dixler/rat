package sample

import "fmt"

type Worker interface {
	Work(task string) string
}

type printer struct{}

func (printer) Work(task string) string {
	return fmt.Sprint("work:", task)
}

func runWorker(w Worker, task string) string {
	return w.Work(task)
}
