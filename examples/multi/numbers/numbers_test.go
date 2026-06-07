package numbers_test

import (
	"fmt"

	"example.com/multi/numbers"
)

func ExampleNew() {
	fmt.Println(numbers.New(2).Separator)
	// Output: ,
}

func ExampleFormat() {
	fmt.Println(numbers.Format(1234567.891, numbers.Options{Decimals: 2, Separator: ","}))
	// Output: 1,234,567.89
}

func ExampleFormat_suffix() {
	fmt.Println(numbers.Format(-9876.5, numbers.Options{Decimals: 1, Separator: " ", Suffix: " kg"}))
	// Output: -9 876.5 kg
}

func ExampleStats() {
	s, _ := numbers.Stats([]float64{2, 4, 9})
	fmt.Println(s.Count, s.Min, s.Max, s.Mean)
	// Output: 3 2 9 5
}

func ExampleStats_empty() {
	_, err := numbers.Stats([]float64{})
	fmt.Println(err)
	// Output: no values to summarise
}
