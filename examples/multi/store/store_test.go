package store_test

import (
	"fmt"

	"example.com/multi/store"
)

func ExampleNew() {
	fmt.Println(store.New("session").Limit)
	// Output: 16
}

func ExampleOpen() {
	s := store.Open(store.Options{Namespace: "session", Limit: 2})
	fmt.Println(s.Namespace, s.Limit, len(s.Items))
	// Output: session 2 0
}

func ExamplePut() {
	s, _ := store.Put(store.Store{Namespace: "session", Limit: 2, Items: map[string]string{}}, "user", "ada")
	fmt.Println(store.Keys(s))
	// Output: [user]
}

func ExamplePut_full() {
	_, err := store.Put(store.Store{Namespace: "session", Limit: 1, Items: map[string]string{"a": "1"}}, "b", "2")
	fmt.Println(err)
	// Output: session is full at 1 keys
}

func ExampleGet() {
	v, _ := store.Get(store.Store{Namespace: "session", Items: map[string]string{"user": "ada"}}, "user")
	fmt.Println(v)
	// Output: ada
}

func ExampleGet_missing() {
	_, err := store.Get(store.Store{Namespace: "session", Items: map[string]string{}}, "user")
	fmt.Println(err)
	// Output: no key "user" in session
}
