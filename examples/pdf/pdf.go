// Package pdf creates and manipulates PDFs entirely in memory.
//
// The pitch here is privacy rather than performance. Merging two PDFs, pulling
// out a page, stamping a watermark or setting a password normally means
// uploading the document to someone's server. Compiled to WebAssembly, all of
// it happens in the tab: no upload, no temporary file, no data processing
// agreement, no retention policy to trust.
//
// # A note on which library
//
// This uses pdfcpu, which is Apache 2.0.
//
// UniPDF is the better known Go PDF library and is more capable, but it is
// commercial: it refuses to write anything without a paid licence key, failing
// with "unipdf license code required" at runtime. That makes it unusable in a
// public example, in CI, or by anyone who has not bought a licence. If you have
// one, the same structure works: swap the calls and add a
// license.SetMeteredKey call at start-up.
package pdf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Anchor is where on the page a piece of text sits.
type Anchor string

const (
	TopLeft      Anchor = "topleft"
	TopCenter    Anchor = "topcenter"
	TopRight     Anchor = "topright"
	Left         Anchor = "left"
	Center       Anchor = "center"
	Right        Anchor = "right"
	BottomLeft   Anchor = "bottomleft"
	BottomCenter Anchor = "bottomcenter"
	BottomRight  Anchor = "bottomright"
)

// Text is one run of text on a page.
type Text struct {
	Value  string `json:"value"`
	Anchor Anchor `json:"anchor"`
	// Size is the font size in points. Zero means 12.
	Size float64 `json:"size,omitempty"`
	// Font is a standard PDF font name such as Helvetica or Courier.
	Font string `json:"font,omitempty"`
}

// Page is one page of a document being created.
type Page struct {
	Text []Text `json:"text"`
}

// Document describes a PDF that was read.
type Document struct {
	Pages int `json:"pages"`
	// Sizes gives each page's dimensions in points.
	Sizes []Size `json:"sizes"`
	// Bytes is the size of the file.
	Bytes int `json:"bytes"`
	// Encrypted reports whether opening it needs a password.
	Encrypted bool `json:"encrypted"`
	Version   string `json:"version"`
}

// Size is a page's dimensions in points, 72 to the inch.
type Size struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

func init() {
	// pdfcpu reads a configuration file from the user config directory at
	// start-up. Under js/wasm the filesystem is a stub, so that read returns
	// nonsense and the parse fails with "permissions is numeric, got 0xF0C3".
	// There is nothing to configure here anyway.
	model.ConfigPath = "disable"
}

func conf() *model.Configuration {
	c := model.NewDefaultConfiguration()
	c.ValidationMode = model.ValidationRelaxed
	return c
}

func reader(data []byte) io.ReadSeeker { return bytes.NewReader(data) }

// Create builds a PDF from a description of its pages.
func Create(pages []Page) ([]byte, error) {
	if len(pages) == 0 {
		return nil, fmt.Errorf("a document needs at least one page")
	}

	// pdfcpu takes its page description as JSON, so the typed input above is
	// translated rather than exposing that schema to callers.
	type jsonFont struct {
		Name string  `json:"name"`
		Size float64 `json:"size"`
	}
	type jsonText struct {
		Value  string   `json:"value"`
		Anchor string   `json:"anchor"`
		Font   jsonFont `json:"font"`
	}
	type jsonContent struct {
		Text []jsonText `json:"text"`
	}
	type jsonPage struct {
		Content jsonContent `json:"content"`
	}

	doc := struct {
		Pages map[string]jsonPage `json:"pages"`
	}{Pages: map[string]jsonPage{}}

	for i, p := range pages {
		if len(p.Text) == 0 {
			return nil, fmt.Errorf("page %d has no content", i+1)
		}
		texts := make([]jsonText, 0, len(p.Text))
		for j, t := range p.Text {
			if strings.TrimSpace(t.Value) == "" {
				return nil, fmt.Errorf("page %d, text %d is empty", i+1, j+1)
			}
			if _, err := types.ParseAnchor(string(t.Anchor)); err != nil {
				return nil, fmt.Errorf("page %d, text %d: %w", i+1, j+1, err)
			}
			size := t.Size
			if size == 0 {
				size = 12
			}
			font := t.Font
			if font == "" {
				font = "Helvetica"
			}
			texts = append(texts, jsonText{
				Value:  t.Value,
				Anchor: string(t.Anchor),
				Font:   jsonFont{Name: font, Size: size},
			})
		}
		doc.Pages[fmt.Sprint(i+1)] = jsonPage{Content: jsonContent{Text: texts}}
	}

	spec, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	if err := api.Create(nil, bytes.NewReader(spec), &out, conf()); err != nil {
		return nil, fmt.Errorf("creating the document: %w", err)
	}
	return out.Bytes(), nil
}

// Info reads a document's structure without modifying it.
//
// An encrypted document is reported as such rather than treated as unreadable:
// that it is locked is the most useful thing to know about it. Pass the
// password to see inside.
func Info(data []byte, password string) (Document, error) {
	version := ""
	if len(data) > 8 && bytes.HasPrefix(data, []byte("%PDF-")) {
		version = string(data[5:8])
	}

	c := conf()
	c.UserPW = password
	c.OwnerPW = password

	n, err := api.PageCount(reader(data), c)
	if err != nil {
		if strings.Contains(err.Error(), "password") {
			return Document{Bytes: len(data), Version: version, Encrypted: true, Sizes: []Size{}}, nil
		}
		return Document{}, fmt.Errorf("not a readable PDF: %w", err)
	}
	dims, err := api.PageDims(reader(data), c)
	if err != nil {
		return Document{}, fmt.Errorf("reading page sizes: %w", err)
	}

	sizes := make([]Size, 0, len(dims))
	for _, d := range dims {
		sizes = append(sizes, Size{Width: d.Width, Height: d.Height})
	}

	// Asked of the parsed document rather than by searching the bytes for
	// "/Encrypt", which appears in plenty of unencrypted files and can be
	// hidden inside an object stream in encrypted ones.
	encrypted := false
	if ctx, err := api.ReadContext(reader(data), c); err == nil {
		encrypted = ctx.Encrypt != nil
	}

	return Document{
		Pages:     n,
		Sizes:     sizes,
		Bytes:     len(data),
		Encrypted: encrypted,
		Version:   version,
	}, nil
}

// Merge joins documents into one, in the order given.
func Merge(docs [][]byte) ([]byte, error) {
	if len(docs) < 2 {
		return nil, fmt.Errorf("merging needs at least two documents, got %d", len(docs))
	}
	readers := make([]io.ReadSeeker, 0, len(docs))
	for i, d := range docs {
		if len(d) == 0 {
			return nil, fmt.Errorf("document %d is empty", i+1)
		}
		readers = append(readers, reader(d))
	}

	var out bytes.Buffer
	if err := api.MergeRaw(readers, &out, false, conf()); err != nil {
		return nil, fmt.Errorf("merging: %w", err)
	}
	return out.Bytes(), nil
}

// ExtractPages keeps only the selected pages.
//
// Selectors are pdfcpu's own: "1", "2-4", "-3" for up to three, "5-" for five
// onwards, and "even" or "odd".
func ExtractPages(data []byte, pages []string) ([]byte, error) {
	if len(pages) == 0 {
		return nil, fmt.Errorf("no pages selected")
	}
	var out bytes.Buffer
	if err := api.Trim(reader(data), &out, pages, conf()); err != nil {
		return nil, fmt.Errorf("extracting %v: %w", pages, err)
	}
	return out.Bytes(), nil
}

// Rotate turns pages by a multiple of 90 degrees. Empty pages means all.
func Rotate(data []byte, degrees int, pages []string) ([]byte, error) {
	if degrees%90 != 0 {
		return nil, fmt.Errorf("rotation must be a multiple of 90, got %d", degrees)
	}
	var out bytes.Buffer
	if err := api.Rotate(reader(data), &out, degrees, pages, conf()); err != nil {
		return nil, fmt.Errorf("rotating: %w", err)
	}
	return out.Bytes(), nil
}

// Watermark stamps text across the selected pages. Empty pages means all.
func Watermark(data []byte, text string, pages []string) ([]byte, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("the watermark needs some text")
	}
	wm, err := api.TextWatermark(text, "font:Helvetica, points:48, col:0.7 0.7 0.7, rot:45, op:0.4", true, false, types.POINTS)
	if err != nil {
		return nil, fmt.Errorf("preparing the watermark: %w", err)
	}

	var out bytes.Buffer
	if err := api.AddWatermarks(reader(data), &out, pages, wm, conf()); err != nil {
		return nil, fmt.Errorf("stamping: %w", err)
	}
	return out.Bytes(), nil
}

// Encrypt sets a user password to open and an owner password to change
// permissions.
func Encrypt(data []byte, userPassword, ownerPassword string) ([]byte, error) {
	if userPassword == "" && ownerPassword == "" {
		return nil, fmt.Errorf("at least one password is required")
	}
	c := conf()
	c.UserPW = userPassword
	c.OwnerPW = ownerPassword

	var out bytes.Buffer
	if err := api.Encrypt(reader(data), &out, c); err != nil {
		return nil, fmt.Errorf("encrypting: %w", err)
	}
	return out.Bytes(), nil
}

// Decrypt removes protection, given the password.
func Decrypt(data []byte, password string) ([]byte, error) {
	c := conf()
	c.UserPW = password
	c.OwnerPW = password

	var out bytes.Buffer
	if err := api.Decrypt(reader(data), &out, c); err != nil {
		return nil, fmt.Errorf("decrypting: %w", err)
	}
	return out.Bytes(), nil
}

// Optimize rewrites a document, removing redundant objects.
func Optimize(data []byte) ([]byte, error) {
	var out bytes.Buffer
	if err := api.Optimize(reader(data), &out, conf()); err != nil {
		return nil, fmt.Errorf("optimising: %w", err)
	}
	return out.Bytes(), nil
}
