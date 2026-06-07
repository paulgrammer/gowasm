package money_test

import (
	"fmt"

	"example.com/money"
)

func ExampleAdd() {
	// The canonical floating point failure: in JavaScript this is
	// 0.30000000000000004.
	a, _ := money.Parse("0.10", money.USD)
	b, _ := money.Parse("0.20", money.USD)
	sum, _ := money.Add(a, b)
	fmt.Println(sum.Display, sum.Minor)
	// Output: $0.30 30
}

func ExampleParse() {
	a, _ := money.Parse("1234567.89", money.USD)
	fmt.Println(a.Display)
	// Output: $1,234,567.89
}

func ExampleParse_yenHasNoDecimals() {
	a, _ := money.Parse("1500", money.JPY)
	fmt.Println(a.Display, a.Minor)
	// Output: ¥1,500 1500
}

func ExampleParse_tooManyDecimals() {
	_, err := money.Parse("1.999", money.USD)
	fmt.Println(err)
	// Output: 1.999 has 3 decimal places, but USD has 2
}

func ExampleAdd_currencyMismatch() {
	a, _ := money.Parse("1.00", money.USD)
	b, _ := money.Parse("1.00", money.EUR)
	_, err := money.Add(a, b)
	fmt.Println(err)
	// Output: cannot combine USD and EUR
}

func ExampleSplit() {
	// Ten dollars three ways. The odd cent goes somewhere rather than nowhere.
	a, _ := money.Parse("10.00", money.USD)
	d, _ := money.Split(a, 3)
	for _, s := range d.Shares {
		fmt.Print(s.Display, " ")
	}
	fmt.Println()
	// Output: $3.34 $3.33 $3.33
}

func ExampleApplyRate() {
	a, _ := money.Parse("19.99", money.USD)
	tax, _ := money.ApplyRate(a, "0.0825")
	fmt.Println(tax.Display)
	// Output: $1.65
}

func ExampleSum() {
	a, _ := money.Parse("1.01", money.USD)
	b, _ := money.Parse("2.02", money.USD)
	c, _ := money.Parse("3.03", money.USD)
	total, _ := money.Sum(a, b, c)
	fmt.Println(total.Display)
	// Output: $6.06
}

func ExampleCompare() {
	a, _ := money.Parse("5.00", money.GBP)
	b, _ := money.Parse("5.01", money.GBP)
	n, _ := money.Compare(a, b)
	fmt.Println(n)
	// Output: -1
}
