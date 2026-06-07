package text_test

import (
	"fmt"

	"example.com/text"
)

func ExampleCount() {
	// JavaScript reports 4 for this: two surrogate pairs. A reader sees one flag.
	c, _ := text.Count("🇰🇪")
	fmt.Println(c.UTF16, c.Bytes, c.Runes, c.Graphemes)
	// Output: 4 8 2 1
}

func ExampleCount_family() {
	c, _ := text.Count("👩‍👩‍👧")
	fmt.Println(c.UTF16, c.Graphemes)
	// Output: 8 1
}

func ExampleNormalize() {
	// The same letter, composed and decomposed. They are not equal until
	// normalised, which is why user input has to be.
	a, _ := text.Normalize("é", text.NFC)
	b, _ := text.Normalize("é", text.NFC)
	fmt.Println(a == b, len(a), len("é"))
	// Output: true 2 3
}

func ExampleNormalize_unknownForm() {
	_, err := text.Normalize("x", "NFX")
	fmt.Println(err)
	// Output: unknown form "NFX", want NFC, NFD, NFKC or NFKD
}

func ExampleLatin() {
	out, _ := text.Latin("Ćao, Žan-Filip")
	fmt.Println(out)
	// Output: Cao, Zan-Filip
}

func ExampleSlug() {
	out, _ := text.Slug("Héllo, Wörld! (2026)")
	fmt.Println(out)
	// Output: hello-world-2026
}

func ExampleSlug_keepsNonLatinScripts() {
	// A naive [^a-z0-9] filter deletes this entirely.
	out, _ := text.Slug("東京 タワー")
	fmt.Println(out)
	// Output: 東京-タワー
}

func ExampleTitle() {
	// Turkish, where the capital of "i" is "İ" rather than "I". A
	// locale-blind toUpperCase gets this wrong in both directions.
	tr, _ := text.Title("istanbul is beautiful", "tr")
	en, _ := text.Title("istanbul is beautiful", "en")
	fmt.Println(tr)
	fmt.Println(en)
	// Output:
	// İstanbul İs Beautiful
	// Istanbul Is Beautiful
}

func ExampleTitle_malformedLanguage() {
	_, err := text.Title("x", "!!")
	fmt.Println(err)
	// Output: unknown language "!!": language: tag is not well-formed
}

func ExampleSort() {
	out, _ := text.Sort([]string{"Zebra", "Äpfel", "apple", "Ćao"}, "en")
	fmt.Println(out)
	// Output: [Äpfel apple Ćao Zebra]
}

func ExampleTruncate() {
	// Cutting at eight code units would split the flag in half.
	out, _ := text.Truncate("hi 🇰🇪 there", 5)
	fmt.Println(out)
	// Output: hi 🇰🇪
}
