package qr_test

import (
	"fmt"

	"example.com/qr"
)

func ExampleEncode() {
	s, _ := qr.Encode("https://github.com/paulgrammer/gowasm", qr.Options{})
	fmt.Println(s.Dimension, s.Version, s.Recovery, string(s.PNG[1:4]))
	// Output: 29 3 medium PNG
}

func ExampleEncode_recoveryCostsSpace() {
	// The same content needs a larger symbol as redundancy goes up.
	for _, r := range []qr.Recovery{qr.Low, qr.Medium, qr.Quart, qr.High} {
		d, _ := qr.Dimensions("https://github.com/paulgrammer/gowasm", r)
		fmt.Printf("%s %d\n", r, d)
	}
	// Output:
	// low 29
	// medium 29
	// quart 33
	// high 37
}

func ExampleEncode_empty() {
	_, err := qr.Encode("   ", qr.Options{})
	fmt.Println(err)
	// Output: nothing to encode
}

func ExampleEncode_unknownRecovery() {
	_, err := qr.Encode("hello", qr.Options{Recovery: "paranoid"})
	fmt.Println(err)
	// Output: unknown recovery "paranoid", want "low", "medium", "quart" or "high"
}

func ExampleEncode_badSize() {
	_, err := qr.Encode("hello", qr.Options{Size: 99})
	fmt.Println(err)
	// Output: size must be between 1 and 40 pixels per module, got 99
}

func ExampleEncode_badColour() {
	_, err := qr.Encode("hello", qr.Options{Foreground: "reddish"})
	fmt.Println(err)
	// Output: foreground colour "reddish" should be six hex digits, like #101014
}

func ExampleEncode_numericPacksTighter() {
	// Digits encode more densely than arbitrary bytes, so the same length of
	// input needs a smaller symbol.
	digits, _ := qr.Dimensions("12345678901234567890123456789012345678901234567890", qr.Medium)
	mixed, _ := qr.Dimensions("abcdefghij!@#$%^&*()abcdefghij!@#$%^&*()abcdefghij", qr.Medium)
	fmt.Println(digits, mixed, digits < mixed)
	// Output: 25 33 true
}

func ExampleDimensions() {
	d, _ := qr.Dimensions("hello", qr.Low)
	fmt.Println(d)
	// Output: 21
}

func ExampleCapacities() {
	caps, _ := qr.Capacities("hello")
	fmt.Println(len(caps), caps[0].Recovery, caps[3].Recovery)
	fmt.Println(caps[0].Numeric > caps[0].Byte)
	// Output:
	// 4 low high
	// true
}

func ExampleDecode() {
	s, _ := qr.Encode("hello", qr.Options{Size: 4, Border: 2})
	w, h, _ := qr.Decode(s.PNG)
	// 21 modules of data, plus a 2 module quiet zone on each side, at 4 pixels
	// per module.
	fmt.Println(w, h, w == (s.Dimension+2*2)*4)
	// Output: 100 100 true
}

func ExampleEncode_sizeScalesTheImage() {
	small, _ := qr.Encode("hello", qr.Options{Size: 4})
	large, _ := qr.Encode("hello", qr.Options{Size: 8})
	sw, _, _ := qr.Decode(small.PNG)
	lw, _, _ := qr.Decode(large.PNG)
	fmt.Println(sw, lw, lw == sw*2)
	// Output: 116 232 true
}

func ExampleDecode_notAPNG() {
	_, _, err := qr.Decode([]byte("nope"))
	fmt.Println(err != nil)
	// Output: true
}
