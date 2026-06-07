package workerpool_test

import (
	"fmt"

	"example.com/workerpool"
)

func ExampleRun() {
	summary, _ := workerpool.Run([]workerpool.Job{
		{ID: 1, Input: "alpha"},
		{ID: 2, Input: "beta"},
		{ID: 3, Input: "gamma"},
	}, 2)
	fmt.Println(summary.Succeeded, summary.Failed, summary.Concurrency)
	// Output: 3 0 2
}

func ExampleRun_recordsEveryError() {
	// The article's point: however many tasks fail, none of them block, because
	// the error lives on the task rather than in a channel someone must drain.
	summary, _ := workerpool.Run([]workerpool.Job{
		{ID: 1, Input: ""},
		{ID: 2, Input: ""},
		{ID: 3, Input: ""},
	}, 2)
	fmt.Println(summary.Succeeded, summary.Failed)
	// Output: 0 3
}

func ExampleRun_ordersResultsByInput() {
	summary, _ := workerpool.Run([]workerpool.Job{
		{ID: 10, Input: "a"},
		{ID: 20, Input: "b"},
		{ID: 30, Input: "c"},
	}, 3)
	for _, r := range summary.Results {
		fmt.Print(r.ID, " ")
	}
	fmt.Println()
	// Output: 10 20 30
}

func ExampleRun_invalidConcurrency() {
	_, err := workerpool.Run([]workerpool.Job{{ID: 1, Input: "x"}}, 0)
	fmt.Println(err)
	// Output: concurrency must be at least 1, got 0
}

func ExampleRunWithErrorLimit() {
	summary, _ := workerpool.RunWithErrorLimit([]workerpool.Job{
		{ID: 1, Input: ""},
		{ID: 2, Input: ""},
		{ID: 3, Input: ""},
	}, 2, 2)
	fmt.Println(summary.Failed, summary.Aborted)
	// Output: 2 true
}

func ExampleRunWithErrorLimit_invalidLimit() {
	_, err := workerpool.RunWithErrorLimit(nil, 1, 0)
	fmt.Println(err)
	// Output: limit must be at least 1, got 0
}
