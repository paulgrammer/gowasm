package pdf_test

import (
	"fmt"

	"example.com/pdf"
)

// invoice is a two page document, built from nothing.
func invoice() []byte {
	data, _ := pdf.Create([]pdf.Page{
		{Text: []pdf.Text{
			{Value: "Invoice 2026-001", Anchor: pdf.TopCenter, Size: 24},
			{Value: "Generated inside WebAssembly", Anchor: pdf.Center, Size: 12},
		}},
		{Text: []pdf.Text{
			{Value: "Terms and conditions", Anchor: pdf.TopCenter, Size: 18},
		}},
	})
	return data
}

func ExampleCreate() {
	data := invoice()
	fmt.Println(len(data) > 0, string(data[:8]))
	// Output: true %PDF-1.7
}

func ExampleCreate_noPages() {
	_, err := pdf.Create(nil)
	fmt.Println(err)
	// Output: a document needs at least one page
}

func ExampleCreate_badAnchor() {
	_, err := pdf.Create([]pdf.Page{{Text: []pdf.Text{{Value: "x", Anchor: "middle"}}}})
	fmt.Println(err)
	// Output: page 1, text 1: unknown anchor: middle
}

func ExampleCreate_emptyText() {
	_, err := pdf.Create([]pdf.Page{{Text: []pdf.Text{{Value: "  ", Anchor: pdf.Center}}}})
	fmt.Println(err)
	// Output: page 1, text 1 is empty
}

func ExampleInfo() {
	d, _ := pdf.Info(invoice(), "")
	fmt.Println(d.Pages, d.Version, d.Encrypted)
	fmt.Printf("%.0f x %.0f\n", d.Sizes[0].Width, d.Sizes[0].Height)
	// Output:
	// 2 1.7 false
	// 595 x 842
}

func ExampleInfo_notAPDF() {
	_, err := pdf.Info([]byte("this is not a PDF"), "")
	fmt.Println(err != nil)
	// Output: true
}

func ExampleExtractPages() {
	one, _ := pdf.ExtractPages(invoice(), []string{"1"})
	d, _ := pdf.Info(one, "")
	fmt.Println(d.Pages)
	// Output: 1
}

func ExampleExtractPages_noneSelected() {
	_, err := pdf.ExtractPages(invoice(), nil)
	fmt.Println(err)
	// Output: no pages selected
}

func ExampleMerge() {
	both, _ := pdf.Merge([][]byte{invoice(), invoice()})
	d, _ := pdf.Info(both, "")
	fmt.Println(d.Pages)
	// Output: 4
}

func ExampleMerge_needsTwo() {
	_, err := pdf.Merge([][]byte{invoice()})
	fmt.Println(err)
	// Output: merging needs at least two documents, got 1
}

func ExampleRotate_badAngle() {
	_, err := pdf.Rotate(invoice(), 45, nil)
	fmt.Println(err)
	// Output: rotation must be a multiple of 90, got 45
}

func ExampleWatermark_needsText() {
	_, err := pdf.Watermark(invoice(), "   ", nil)
	fmt.Println(err)
	// Output: the watermark needs some text
}

func ExampleEncrypt() {
	locked, _ := pdf.Encrypt(invoice(), "open-me", "own-me")

	// Without the password it is genuinely locked, and Info says so rather
	// than failing.
	shut, _ := pdf.Info(locked, "")
	// With it, the document reads normally again.
	open, _ := pdf.Info(locked, "open-me")
	fmt.Println(shut.Encrypted, shut.Pages, "|", open.Encrypted, open.Pages)
	// Output: true 0 | true 2
}

func ExampleDecrypt() {
	locked, _ := pdf.Encrypt(invoice(), "open-me", "own-me")
	plain, _ := pdf.Decrypt(locked, "own-me")
	d, _ := pdf.Info(plain, "")
	fmt.Println(d.Encrypted, d.Pages)
	// Output: false 2
}

func ExampleEncrypt_needsAPassword() {
	_, err := pdf.Encrypt(invoice(), "", "")
	fmt.Println(err)
	// Output: at least one password is required
}
