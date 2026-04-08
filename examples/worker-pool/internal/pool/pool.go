// Package pool is the worker pool from Brandur Leach's "Go Worker Pools"
// (https://brandur.org/go-worker-pool), kept as written.
//
// The design point worth preserving is where errors live. An earlier version in
// that article sends errors to a channel, which deadlocks as soon as more work
// items fail than the channel can buffer: workers block trying to report an
// error nobody is draining yet. Storing the error on the Task removes the
// channel, and with it the failure mode -- the caller reads Task.Err after Run
// returns, however many tasks failed.
package pool

import "sync"

// Task encapsulates a work item that should go in a work pool.
type Task struct {
	// Err holds an error that occurred during a task. Its result is only
	// meaningful after Run has been called for the pool that holds it.
	Err error

	f func() error
}

// NewTask initializes a new task based on a given work function.
func NewTask(f func() error) *Task {
	return &Task{f: f}
}

// Run runs a Task and does appropriate accounting via a given sync.WaitGroup.
func (t *Task) Run(wg *sync.WaitGroup) {
	t.Err = t.f()
	wg.Done()
}

// Pool is a worker group that runs a number of tasks at a configured
// concurrency.
type Pool struct {
	Tasks []*Task

	concurrency int
	tasksChan   chan *Task
	wg          sync.WaitGroup
}

// NewPool initializes a new pool with the given tasks and at the given
// concurrency.
func NewPool(tasks []*Task, concurrency int) *Pool {
	return &Pool{
		Tasks:       tasks,
		concurrency: concurrency,
		tasksChan:   make(chan *Task),
	}
}

// Run runs all work within the pool and blocks until it's finished.
func (p *Pool) Run() {
	for i := 0; i < p.concurrency; i++ {
		go p.work()
	}

	p.wg.Add(len(p.Tasks))
	for _, task := range p.Tasks {
		p.tasksChan <- task
	}

	// all workers return
	close(p.tasksChan)

	p.wg.Wait()
}

// The work loop for any single goroutine.
func (p *Pool) work() {
	for task := range p.tasksChan {
		task.Run(&p.wg)
	}
}
