package cue_test

import (
	"fmt"

	"example.com/cue"
)

// schema constrains a server config: a port in a range, a required name, a
// log level from a fixed set, and a replica count that defaults to 1.
const schema = `
name!:     string & =~"^[a-z][a-z0-9-]*$"
port:      int & >=1024 & <=65535
logLevel:  "debug" | "info" | "warn" | "error"
replicas:  int & >=1 | *1
`

func ExampleValidate() {
	r, _ := cue.Validate(schema, `{"name":"api","port":8080,"logLevel":"info"}`)
	fmt.Println(r.Valid, len(r.Violations))
	fmt.Println(r.Concrete)
	// Output:
	// true 0
	// {"name":"api","port":8080,"logLevel":"info","replicas":1}
}

func ExampleValidate_portOutOfRange() {
	r, _ := cue.Validate(schema, `{"name":"api","port":80,"logLevel":"info"}`)
	fmt.Println(r.Valid, r.Violations[0].Path)
	// Output: false port
}

func ExampleValidate_badLogLevel() {
	r, _ := cue.Validate(schema, `{"name":"api","port":8080,"logLevel":"verbose"}`)
	fmt.Println(r.Valid, r.Violations[0].Path)
	// Output: false logLevel
}

func ExampleValidate_missingRequiredField() {
	r, _ := cue.Validate(schema, `{"port":8080,"logLevel":"info"}`)
	fmt.Println(r.Valid)
	// Output: false
}

func ExampleValidate_badJSON() {
	_, err := cue.Validate(schema, `{not json}`)
	fmt.Println(err != nil)
	// Output: true
}

func ExampleValidate_brokenSchema() {
	_, err := cue.Validate(`x: >>>`, `{}`)
	fmt.Println(err != nil)
	// Output: true
}

func ExampleUnify() {
	// A base and an environment override, composed by intersection. Order does
	// not matter and nothing is overwritten; the result holds both.
	out, _ := cue.Unify([]string{
		`{replicas: int | *1, region: "eu"}`,
		`{replicas: 5}`,
	})
	fmt.Println(out)
	// Output: {
	// 	replicas: 5
	// 	region:   "eu"
	// }
}

func ExampleUnify_conflict() {
	// Two values that genuinely disagree are an error, not a last-writer-wins
	// surprise discovered in production.
	_, err := cue.Unify([]string{`{region: "eu"}`, `{region: "us"}`})
	fmt.Println(err != nil)
	// Output: true
}

func ExampleExport() {
	out, _ := cue.Export(`{a: 1, b: a + 1}`)
	fmt.Println(out)
	// Output: {"a":1,"b":2}
}

func ExampleExport_notConcrete() {
	_, err := cue.Export(`{a: int}`)
	fmt.Println(err != nil)
	// Output: true
}

func ExampleCheck() {
	v, _ := cue.Check(`port: int & >=1024`)
	fmt.Println(len(v))
	// Output: 0
}
