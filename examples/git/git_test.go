package git_test

import (
	"fmt"

	"example.com/git"
)

// history builds a small repository with two commits at fixed times, so the
// hashes are stable and can be asserted on.
func history() {
	git.Init()
	git.Record(
		[]git.File{{Path: "README.md", Content: "# demo\n"}},
		"first commit", "Ada", "ada@example.com", "2026-01-01T00:00:00Z",
	)
	git.Record(
		[]git.File{{Path: "README.md", Content: "# demo\n\nnow with prose.\n"}, {Path: "LICENSE", Content: "MIT\n"}},
		"expand the readme", "Ada", "ada@example.com", "2026-01-02T00:00:00Z",
	)
}

func ExampleInit() {
	s, _ := git.Init()
	fmt.Println(s.Clean, s.Branch)
	// Output: true master
}

func ExampleRecord() {
	git.Init()
	c, _ := git.Record(
		[]git.File{{Path: "a.txt", Content: "hello\n"}},
		"add a", "Ada", "ada@example.com", "2026-01-01T00:00:00Z",
	)
	fmt.Println(c.Message, c.Author, c.When, len(c.Parents))
	// Output: add a Ada 2026-01-01T00:00:00Z 0
}

func ExampleRecord_noMessage() {
	git.Init()
	_, err := git.Record([]git.File{{Path: "a", Content: "x"}}, "  ", "A", "a@b", "2026-01-01T00:00:00Z")
	fmt.Println(err)
	// Output: a commit needs a message
}

func ExampleRecord_badTime() {
	git.Init()
	_, err := git.Record([]git.File{{Path: "a", Content: "x"}}, "m", "A", "a@b", "yesterday")
	fmt.Println(err != nil)
	// Output: true
}

func ExampleLog() {
	history()
	log, _ := git.Log(10)
	for _, c := range log {
		fmt.Println(c.Message)
	}
	// Output:
	// expand the readme
	// first commit
}

func ExampleBranch() {
	history()
	s, _ := git.Branch("feature")
	fmt.Println(s.Branch)
	// Output: feature
}

func ExampleBranches() {
	history()
	git.Branch("feature")
	names, _ := git.Branches()
	fmt.Println(names)
	// Output: [feature master]
}

func ExampleCheckout_unknownBranch() {
	history()
	_, err := git.Checkout("nope")
	fmt.Println(err != nil)
	// Output: true
}

func ExampleCompare() {
	history()
	d, _ := git.Compare("HEAD~1", "HEAD")
	for _, c := range d.Changes {
		fmt.Println(c.Kind, c.Path, "+"+fmt.Sprint(c.Insertions), "-"+fmt.Sprint(c.Deletions))
	}
	// Output:
	// added LICENSE +1 -0
	// modified README.md +2 -0
}

func ExampleReadFile() {
	history()
	out, _ := git.ReadFile("HEAD~1", "README.md")
	fmt.Printf("%q\n", out)
	// Output: "# demo\n"
}

func ExampleReadFile_missing() {
	history()
	_, err := git.ReadFile("HEAD~1", "LICENSE")
	fmt.Println(err != nil)
	// Output: true
}

func ExampleShow() {
	history()
	c, _ := git.Show("HEAD")
	fmt.Println(c.Message, len(c.Parents))
	// Output: expand the readme 1
}
