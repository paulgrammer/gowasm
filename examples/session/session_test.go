package session_test

import (
	"fmt"

	"example.com/session"
)

// Only free functions with literal arguments become recorded fixtures. A call
// on a handle has no literal form -- the object lives in Go -- so the methods
// below are covered by verify.mjs instead.

func ExamplePing() {
	fmt.Println(session.Ping())
	// Output: session
}

func ExampleOpen_badLimit() {
	_, err := session.Open("cart", 0)
	fmt.Println(err)
	// Output: limit must be positive, got 0
}

func ExampleStore_Set() {
	s := session.New("cart")
	_ = s.Set("sku", "1234")
	v, _ := s.Get("sku")
	fmt.Println(v, s.Writes)
	// Output: 1234 1
}

func ExampleStore_Txn() {
	s := session.New("cart")
	t := s.Txn()
	_ = t.Set("a", "1")
	_ = t.Set("b", "2")
	n, _ := t.Commit()
	fmt.Println(n, s.Keys())
	// Output: 2 [a b]
}
