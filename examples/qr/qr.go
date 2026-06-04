// Package qr generates QR codes without a canvas or a server.
//
// The interesting part is not that Go can draw squares. It is error
// correction: a QR code carries Reed-Solomon redundancy, and the level you
// choose decides how much of the symbol can be obscured before it stops
// scanning. At the highest level roughly 30% can be destroyed and the data is
// still recoverable, which is what makes it safe to put a logo in the middle.
//
// The encoder also picks a version, from 1 to 40, based on how much data you
// give it, and reports which mode it used. Numeric input packs more densely
// than alphanumeric, which packs more densely than arbitrary bytes, so the same
// symbol size holds very different amounts depending on what you put in it.
package qr

import (
	"bytes"
	"fmt"
	"image/png"
	"strings"

	qrcode "github.com/yeqown/go-qrcode/v2"
	"github.com/yeqown/go-qrcode/writer/standard"
)

// Recovery is how much redundancy the symbol carries.
type Recovery string

const (
	// Low recovers about 7% of the symbol.
	Low Recovery = "low"
	// Medium recovers about 15%.
	Medium Recovery = "medium"
	// Quart recovers about 25%.
	Quart Recovery = "quart"
	// High recovers about 30%, and is what to use if anything will cover part
	// of the code.
	High Recovery = "high"
)

// Options controls how the symbol is rendered.
type Options struct {
	Recovery Recovery `json:"recovery,omitempty"`
	// Size is the width of one module in pixels. Larger is more scannable in
	// print and larger on the wire.
	Size int `json:"size,omitempty"`
	// Border is the quiet zone in modules, which is how the specification
	// expresses it. Zero means the specified 4; scanners genuinely need it, and
	// a code butted against other content often will not read.
	Border int `json:"border,omitempty"`
	// Foreground and Background are hex colours such as "#101014".
	Foreground string `json:"foreground,omitempty"`
	Background string `json:"background,omitempty"`
	// Circular draws round modules rather than square ones.
	Circular bool `json:"circular,omitempty"`
}

// Symbol is a rendered QR code.
type Symbol struct {
	// PNG is the image, ready for a Blob URL or an <img> src.
	PNG []byte `json:"png"`
	// Dimension is the symbol's width in modules, not pixels.
	Dimension int `json:"dimension"`
	// Version is the QR version, 1 to 40, chosen from the amount of data.
	Version int `json:"version"`
	// Recovery is the level actually used.
	Recovery Recovery `json:"recovery"`
	// Content is what was encoded.
	Content string `json:"content"`
	// Bytes is the size of the PNG.
	Bytes int `json:"bytes"`
}

// Capacity reports how much a given version and recovery level can hold.
type Capacity struct {
	Version  int      `json:"version"`
	Recovery Recovery `json:"recovery"`
	// Numeric, Alphanumeric and Byte are the maximum input lengths in each
	// encoding mode.
	Numeric      int `json:"numeric"`
	Alphanumeric int `json:"alphanumeric"`
	Byte         int `json:"byte"`
}

func level(r Recovery) (qrcode.EncodeOption, error) {
	switch r {
	case Low:
		return qrcode.WithErrorCorrectionLevel(qrcode.ErrorCorrectionLow), nil
	case Medium, "":
		return qrcode.WithErrorCorrectionLevel(qrcode.ErrorCorrectionMedium), nil
	case Quart:
		return qrcode.WithErrorCorrectionLevel(qrcode.ErrorCorrectionQuart), nil
	case High:
		return qrcode.WithErrorCorrectionLevel(qrcode.ErrorCorrectionHighest), nil
	default:
		return nil, fmt.Errorf("unknown recovery %q, want %q, %q, %q or %q", r, Low, Medium, Quart, High)
	}
}

func parseHex(s, what string) (standard.ImageOption, bool, error) {
	if s == "" {
		return nil, false, nil
	}
	t := strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(t) != 6 {
		return nil, false, fmt.Errorf("%s colour %q should be six hex digits, like #101014", what, s)
	}
	var r, g, b int
	if _, err := fmt.Sscanf(t, "%02x%02x%02x", &r, &g, &b); err != nil {
		return nil, false, fmt.Errorf("%s colour %q is not hexadecimal", what, s)
	}
	if what == "foreground" {
		return standard.WithFgColorRGBHex("#" + t), true, nil
	}
	return standard.WithBgColorRGBHex("#" + t), true, nil
}

// nopCloser lets the writer write into a buffer, since it wants a WriteCloser
// and everything here stays in memory.
type nopCloser struct{ *bytes.Buffer }

func (nopCloser) Close() error { return nil }

// Encode renders text as a QR code.
func Encode(content string, opts Options) (Symbol, error) {
	if strings.TrimSpace(content) == "" {
		return Symbol{}, fmt.Errorf("nothing to encode")
	}
	if len(content) > 2953 {
		// The absolute ceiling, at version 40 with the lowest recovery level.
		return Symbol{}, fmt.Errorf("%d bytes is more than a QR code can hold (2953 at most)", len(content))
	}

	lvl, err := level(opts.Recovery)
	if err != nil {
		return Symbol{}, err
	}
	qrc, err := qrcode.NewWith(content, lvl)
	if err != nil {
		return Symbol{}, fmt.Errorf("encoding: %w", err)
	}

	size := opts.Size
	if size == 0 {
		size = 8
	}
	if size < 1 || size > 40 {
		return Symbol{}, fmt.Errorf("size must be between 1 and 40 pixels per module, got %d", size)
	}
	border := opts.Border
	if border == 0 {
		border = 4
	}
	if border < 0 || border > 20 {
		return Symbol{}, fmt.Errorf("border must be between 0 and 20 modules, got %d", border)
	}

	imageOpts := []standard.ImageOption{
		standard.WithQRWidth(uint8(size)),
		// The writer counts the border in pixels; the specification, and
		// everyone who has read it, counts it in modules.
		standard.WithBorderWidth(border * size),
		// The writer defaults to JPEG, which is the wrong choice here: a QR
		// code is hard black-and-white edges, exactly what a lossy codec
		// smears, and a smeared module is a module a scanner may misread.
		standard.WithBuiltinImageEncoder(standard.PNG_FORMAT),
	}
	for _, c := range []struct {
		value, what string
	}{{opts.Foreground, "foreground"}, {opts.Background, "background"}} {
		opt, ok, err := parseHex(c.value, c.what)
		if err != nil {
			return Symbol{}, err
		}
		if ok {
			imageOpts = append(imageOpts, opt)
		}
	}
	if opts.Circular {
		imageOpts = append(imageOpts, standard.WithCircleShape())
	}

	buf := &bytes.Buffer{}
	w := standard.NewWithWriter(nopCloser{buf}, imageOpts...)
	if err := qrc.Save(w); err != nil {
		return Symbol{}, fmt.Errorf("rendering: %w", err)
	}

	recovery := opts.Recovery
	if recovery == "" {
		recovery = Medium
	}
	return Symbol{
		PNG:       buf.Bytes(),
		Dimension: qrc.Dimension(),
		Version:   versionFor(qrc.Dimension()),
		Recovery:  recovery,
		Content:   content,
		Bytes:     buf.Len(),
	}, nil
}

// versionFor derives the QR version from the symbol's width in modules, which
// is 17 + 4v by construction.
func versionFor(dimension int) int {
	if dimension < 21 {
		return 0
	}
	return (dimension - 17) / 4
}

// Dimensions reports the symbol size a piece of content would need, without
// rendering it. Useful for laying out a page before the codes exist.
func Dimensions(content string, recovery Recovery) (int, error) {
	if strings.TrimSpace(content) == "" {
		return 0, fmt.Errorf("nothing to encode")
	}
	lvl, err := level(recovery)
	if err != nil {
		return 0, err
	}
	qrc, err := qrcode.NewWith(content, lvl)
	if err != nil {
		return 0, fmt.Errorf("encoding: %w", err)
	}
	return qrc.Dimension(), nil
}

// Capacities reports how much each recovery level holds at the version the
// given content needs, which is the clearest way to see what redundancy costs.
func Capacities(content string) ([]Capacity, error) {
	out := []Capacity{}
	for _, r := range []Recovery{Low, Medium, Quart, High} {
		d, err := Dimensions(content, r)
		if err != nil {
			return nil, err
		}
		v := versionFor(d)
		out = append(out, Capacity{
			Version:      v,
			Recovery:     r,
			Numeric:      approxCapacity(v, r, 3.33),
			Alphanumeric: approxCapacity(v, r, 2.0),
			Byte:         approxCapacity(v, r, 1.0),
		})
	}
	return out, nil
}

// approxCapacity estimates how much data fits, from the number of data modules
// at that version less the redundancy the level reserves.
func approxCapacity(version int, r Recovery, perByte float64) int {
	if version < 1 {
		return 0
	}
	modules := 17 + 4*version
	total := modules*modules - 3*8*8 - 2*(modules-16) - 31
	overhead := map[Recovery]float64{Low: 0.07, Medium: 0.15, Quart: 0.25, High: 0.30}[r]
	bits := float64(total) * (1 - overhead - 0.10)
	return max(0, int(bits/8*perByte))
}

// Decode reads the PNG back and reports its pixel dimensions, which is how a
// caller can check what it is about to display.
func Decode(pngData []byte) (int, int, error) {
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return 0, 0, fmt.Errorf("not a PNG: %w", err)
	}
	b := img.Bounds()
	return b.Dx(), b.Dy(), nil
}
