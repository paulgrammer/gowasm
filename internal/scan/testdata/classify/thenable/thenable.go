// Package thenable has a method that would make the class a thenable, so
// `await` on an instance would call it instead of resolving.
package thenable

type Task struct{}

func New() *Task            { return &Task{} }
func (t *Task) Then() error { return nil }
func (t *Task) Run() error  { return nil }
