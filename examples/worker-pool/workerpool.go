// Package workerpool drives Brandur Leach's Go worker pool
// (https://brandur.org/go-worker-pool) from JavaScript.
//
// The pool itself is in internal/pool, kept exactly as the article writes it:
// a Pool holding []*Task, a fixed number of goroutines ranging over an
// unbuffered channel, sync.WaitGroup for completion, and each Task carrying its
// own Err so no error channel can saturate and deadlock.
//
// # Why the pool is not exported directly
//
// The article's constructor is NewTask(f func() error). A function is code, and
// code cannot cross the WebAssembly boundary -- only data can. gowasm refuses to
// generate a binding for a func parameter rather than pretending otherwise, so
// this package exposes the pool in terms of jobs: JavaScript describes the work,
// and the closures are built here.
//
// # Concurrency, not parallelism
//
// Under GOOS=js GOARCH=wasm the Go runtime is single-threaded: GOMAXPROCS is 1.
// Raising the concurrency does not make CPU-bound work finish sooner. What the
// pool still gives you is what it is really for -- a bounded number of items in
// flight, orderly completion, and per-task error accounting. Call Describe to
// confirm the thread count rather than taking it on trust.
package workerpool

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"runtime"
	"strings"

	"example.com/workerpool/internal/pool"
)

// Job is one unit of work for the pool.
type Job struct {
	ID    int    `json:"id"`
	Input string `json:"input"`
}

// Result is the outcome of one job. Exactly one of Output and Error is set,
// mirroring how the article's Task carries either its work or its Err.
type Result struct {
	ID     int    `json:"id"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Summary is what a completed run reports.
type Summary struct {
	Results     []Result `json:"results"`
	Concurrency int      `json:"concurrency"`
	Succeeded   int      `json:"succeeded"`
	Failed      int      `json:"failed"`
	// Aborted is set when an error limit stopped the caller reading further,
	// which is the "Too many errors." case from the article's usage example.
	Aborted bool `json:"aborted,omitempty"`
}

// Environment reports what the Go runtime sees inside WebAssembly.
type Environment struct {
	GoVersion  string `json:"goVersion"`
	NumCPU     int    `json:"numCPU"`
	GOMAXPROCS int    `json:"gomaxprocs"`
	Goroutines int    `json:"goroutines"`
}

// Describe reports the runtime environment, so the single-threaded claim above
// can be checked rather than believed.
func Describe() Environment {
	return Environment{
		GoVersion:  runtime.Version(),
		NumCPU:     runtime.NumCPU(),
		GOMAXPROCS: runtime.GOMAXPROCS(0),
		Goroutines: runtime.NumGoroutine(),
	}
}

// work is the actual job. It fails on blank input so error accounting is
// visible in the results.
func work(job Job) (string, error) {
	if strings.TrimSpace(job.Input) == "" {
		return "", fmt.Errorf("job %d: input is empty", job.ID)
	}
	sum := sha256.Sum256([]byte(job.Input))
	return hex.EncodeToString(sum[:8]), nil
}

// build turns jobs into the article's []*Task, capturing each result alongside
// the Task so it can be read back after Run.
//
// The recover is deliberate. Task.Run calls wg.Done() only after f returns, so a
// panicking task would leave the WaitGroup permanently short and hang Run. That
// is not a flaw worth editing into the article's code: the guard belongs in the
// work function, which is the part we own.
func build(jobs []Job) ([]*pool.Task, []string) {
	outputs := make([]string, len(jobs))
	tasks := make([]*pool.Task, len(jobs))

	for i, job := range jobs {
		tasks[i] = pool.NewTask(func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("job %d: panic: %v", job.ID, r)
				}
			}()
			out, err := work(job)
			if err != nil {
				return err
			}
			outputs[i] = out
			return nil
		})
	}
	return tasks, outputs
}

// Run processes every job at the given concurrency and returns results in input
// order, whatever order they actually completed in.
//
// A failing job does not stop the run: its error is recorded against its own
// result, exactly as Task.Err does in the article.
func Run(jobs []Job, concurrency int) (Summary, error) {
	return runLimit(jobs, concurrency, 0)
}

// RunWithErrorLimit stops reporting once limit errors have been seen, which is
// the "Too many errors." loop from the end of the article. The pool still runs
// every task -- the limit applies to reading the results, not to the work.
func RunWithErrorLimit(jobs []Job, concurrency, limit int) (Summary, error) {
	if limit < 1 {
		return Summary{}, fmt.Errorf("limit must be at least 1, got %d", limit)
	}
	return runLimit(jobs, concurrency, limit)
}

func runLimit(jobs []Job, concurrency, limit int) (Summary, error) {
	if concurrency < 1 {
		return Summary{}, fmt.Errorf("concurrency must be at least 1, got %d", concurrency)
	}
	if len(jobs) == 0 {
		return Summary{Results: []Result{}, Concurrency: concurrency}, nil
	}

	tasks, outputs := build(jobs)
	p := pool.NewPool(tasks, concurrency)
	p.Run()

	summary := Summary{
		Results:     make([]Result, 0, len(jobs)),
		Concurrency: concurrency,
	}

	// Task.Err is only meaningful after Run has returned, which is why the
	// results are gathered here rather than as the work completes.
	for i, task := range p.Tasks {
		if task.Err != nil {
			summary.Failed++
			summary.Results = append(summary.Results, Result{ID: jobs[i].ID, Error: task.Err.Error()})
			if limit > 0 && summary.Failed >= limit {
				summary.Aborted = true
				break
			}
			continue
		}
		summary.Succeeded++
		summary.Results = append(summary.Results, Result{ID: jobs[i].ID, Output: outputs[i]})
	}
	return summary, nil
}
