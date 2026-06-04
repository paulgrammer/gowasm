// Package image processes images entirely in memory.
//
// Every operation here has a JavaScript equivalent, so unlike the regex or CUE
// examples this is not about capability. It is about where the work happens.
// The canvas approach means pixels go through the GPU compositor and back, and
// anything non-trivial means either a large JavaScript library or a round trip
// to a server that then holds your photograph.
//
// bild is a pure Go image library with the usual operations, and the result
// crosses the boundary as encoded bytes: a []byte in Go, a Uint8Array in
// TypeScript, which goes straight into a Blob URL or an ImageBitmap.
package image

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"

	"github.com/anthonynsimon/bild/adjust"
	"github.com/anthonynsimon/bild/blur"
	"github.com/anthonynsimon/bild/effect"
	"github.com/anthonynsimon/bild/histogram"
	"github.com/anthonynsimon/bild/transform"
)

// Format is an encoding for the result.
type Format string

const (
	PNG  Format = "png"
	JPEG Format = "jpeg"
)

// Info describes a decoded image.
type Info struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	// Format is the encoding it was decoded from.
	Format string `json:"format"`
	Bytes  int    `json:"bytes"`
}

// Adjustments are the tone controls, all optional.
//
// Zero means "leave alone" for every field, so a caller can set one without
// having to restate the others.
type Adjustments struct {
	// Brightness and Contrast run from -1 to 1.
	Brightness float64 `json:"brightness,omitempty"`
	Contrast   float64 `json:"contrast,omitempty"`
	// Saturation runs from -1 to 1.
	Saturation float64 `json:"saturation,omitempty"`
	// Gamma is a multiplier; 1 leaves the image unchanged.
	Gamma float64 `json:"gamma,omitempty"`
	// Hue rotates by degrees.
	Hue int `json:"hue,omitempty"`
}

// Channels holds one histogram bin set per colour channel, 256 buckets each.
type Channels struct {
	Red   []int `json:"red"`
	Green []int `json:"green"`
	Blue  []int `json:"blue"`
	// Peak is the largest count in any bucket, for scaling a chart.
	Peak int `json:"peak"`
}

// Pattern is a generated test image, so a caller has something to work on
// without supplying a file.
type Pattern string

const (
	Gradient   Pattern = "gradient"
	Checker    Pattern = "checker"
	ColorWheel Pattern = "wheel"
)

func decode(data []byte) (image.Image, string, error) {
	if len(data) == 0 {
		return nil, "", fmt.Errorf("no image data")
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("cannot decode: %w", err)
	}
	return img, format, nil
}

func encode(img image.Image, format Format) ([]byte, error) {
	var buf bytes.Buffer
	switch format {
	case PNG, "":
		if err := png.Encode(&buf, img); err != nil {
			return nil, fmt.Errorf("encoding png: %w", err)
		}
	case JPEG:
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 88}); err != nil {
			return nil, fmt.Errorf("encoding jpeg: %w", err)
		}
	default:
		return nil, fmt.Errorf("unknown format %q, want %q or %q", format, PNG, JPEG)
	}
	return buf.Bytes(), nil
}

// apply decodes, transforms and re-encodes, which every operation below does.
func apply(data []byte, format Format, fn func(image.Image) image.Image) ([]byte, error) {
	img, _, err := decode(data)
	if err != nil {
		return nil, err
	}
	return encode(fn(img), format)
}

// Inspect decodes an image and reports its dimensions without altering it.
func Inspect(data []byte) (Info, error) {
	img, format, err := decode(data)
	if err != nil {
		return Info{}, err
	}
	b := img.Bounds()
	return Info{Width: b.Dx(), Height: b.Dy(), Format: format, Bytes: len(data)}, nil
}

// Generate draws a test image, so the demo needs no input file.
func Generate(pattern Pattern, width, height int, format Format) ([]byte, error) {
	if width < 1 || height < 1 || width > 4096 || height > 4096 {
		return nil, fmt.Errorf("size must be between 1 and 4096, got %dx%d", width, height)
	}

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	switch pattern {
	case Gradient:
		for y := range height {
			for x := range width {
				img.Set(x, y, color.RGBA{
					R: uint8(255 * x / width),
					G: uint8(255 * y / height),
					B: uint8(255 - 255*x/width),
					A: 255,
				})
			}
		}
	case Checker:
		const cell = 32
		for y := range height {
			for x := range width {
				if (x/cell+y/cell)%2 == 0 {
					img.Set(x, y, color.RGBA{240, 240, 235, 255})
				} else {
					img.Set(x, y, color.RGBA{40, 44, 52, 255})
				}
			}
		}
	case ColorWheel:
		cx, cy := float64(width)/2, float64(height)/2
		radius := math.Min(cx, cy)
		for y := range height {
			for x := range width {
				dx, dy := float64(x)-cx, float64(y)-cy
				dist := math.Hypot(dx, dy)
				if dist > radius {
					img.Set(x, y, color.RGBA{20, 20, 24, 255})
					continue
				}
				hue := (math.Atan2(dy, dx)/math.Pi + 1) / 2
				img.Set(x, y, hsv(hue, dist/radius, 1))
			}
		}
	default:
		return nil, fmt.Errorf("unknown pattern %q, want %q, %q or %q", pattern, Gradient, Checker, ColorWheel)
	}
	return encode(img, format)
}

func hsv(h, s, v float64) color.RGBA {
	i := math.Floor(h * 6)
	f := h*6 - i
	p, q, t := v*(1-s), v*(1-f*s), v*(1-(1-f)*s)
	var r, g, b float64
	switch int(i) % 6 {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	default:
		r, g, b = v, p, q
	}
	return color.RGBA{uint8(r * 255), uint8(g * 255), uint8(b * 255), 255}
}

// Blur applies a Gaussian blur of the given radius.
func Blur(data []byte, radius float64, format Format) ([]byte, error) {
	if radius <= 0 || radius > 100 {
		return nil, fmt.Errorf("radius must be between 0 and 100, got %v", radius)
	}
	return apply(data, format, func(img image.Image) image.Image {
		return blur.Gaussian(img, radius)
	})
}

// Sharpen increases local contrast at edges.
func Sharpen(data []byte, format Format) ([]byte, error) {
	return apply(data, format, func(img image.Image) image.Image { return effect.Sharpen(img) })
}

// Grayscale removes colour.
func Grayscale(data []byte, format Format) ([]byte, error) {
	return apply(data, format, func(img image.Image) image.Image { return effect.Grayscale(img) })
}

// Invert produces a negative.
func Invert(data []byte, format Format) ([]byte, error) {
	return apply(data, format, func(img image.Image) image.Image { return effect.Invert(img) })
}

// Sepia tones the image warm.
func Sepia(data []byte, format Format) ([]byte, error) {
	return apply(data, format, func(img image.Image) image.Image { return effect.Sepia(img) })
}

// EdgeDetect finds edges with a Sobel operator.
func EdgeDetect(data []byte, format Format) ([]byte, error) {
	return apply(data, format, func(img image.Image) image.Image { return effect.Sobel(img) })
}

// Emboss gives a raised relief effect.
func Emboss(data []byte, format Format) ([]byte, error) {
	return apply(data, format, func(img image.Image) image.Image { return effect.Emboss(img) })
}

// Dilate grows bright regions, which is the usual first step in a morphological
// clean-up.
func Dilate(data []byte, radius float64, format Format) ([]byte, error) {
	if radius <= 0 || radius > 50 {
		return nil, fmt.Errorf("radius must be between 0 and 50, got %v", radius)
	}
	return apply(data, format, func(img image.Image) image.Image { return effect.Dilate(img, radius) })
}

// Adjust applies the tone controls.
func Adjust(data []byte, a Adjustments, format Format) ([]byte, error) {
	return apply(data, format, func(img image.Image) image.Image {
		out := img
		if a.Brightness != 0 {
			out = adjust.Brightness(out, a.Brightness)
		}
		if a.Contrast != 0 {
			out = adjust.Contrast(out, a.Contrast)
		}
		if a.Saturation != 0 {
			out = adjust.Saturation(out, a.Saturation)
		}
		if a.Gamma != 0 && a.Gamma != 1 {
			out = adjust.Gamma(out, a.Gamma)
		}
		if a.Hue != 0 {
			out = adjust.Hue(out, a.Hue)
		}
		return out
	})
}

// Resize scales the image. A zero width or height is derived from the other so
// the aspect ratio is kept.
func Resize(data []byte, width, height int, format Format) ([]byte, error) {
	img, _, err := decode(data)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()

	switch {
	case width <= 0 && height <= 0:
		return nil, fmt.Errorf("give a width, a height, or both")
	case width <= 0:
		width = b.Dx() * height / b.Dy()
	case height <= 0:
		height = b.Dy() * width / b.Dx()
	}
	if width > 8192 || height > 8192 {
		return nil, fmt.Errorf("result would be %dx%d, which is too large", width, height)
	}
	return encode(transform.Resize(img, width, height, transform.Linear), format)
}

// Rotate turns the image by an angle in degrees, growing the canvas to fit.
func Rotate(data []byte, degrees float64, format Format) ([]byte, error) {
	return apply(data, format, func(img image.Image) image.Image {
		return transform.Rotate(img, degrees, &transform.RotationOptions{ResizeBounds: true})
	})
}

// FlipHorizontal mirrors left to right.
func FlipHorizontal(data []byte, format Format) ([]byte, error) {
	return apply(data, format, func(img image.Image) image.Image { return transform.FlipH(img) })
}

// FlipVertical mirrors top to bottom.
func FlipVertical(data []byte, format Format) ([]byte, error) {
	return apply(data, format, func(img image.Image) image.Image { return transform.FlipV(img) })
}

// Crop cuts a rectangle out of the image.
func Crop(data []byte, x, y, width, height int, format Format) ([]byte, error) {
	img, _, err := decode(data)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	if width < 1 || height < 1 {
		return nil, fmt.Errorf("the crop must be at least 1x1, got %dx%d", width, height)
	}
	if x < 0 || y < 0 || x+width > b.Dx() || y+height > b.Dy() {
		return nil, fmt.Errorf("crop %d,%d %dx%d falls outside the %dx%d image",
			x, y, width, height, b.Dx(), b.Dy())
	}
	return encode(transform.Crop(img, image.Rect(x, y, x+width, y+height)), format)
}

// Histogram counts how many pixels fall in each intensity bucket, per channel.
//
// It returns data rather than a picture, so the caller can draw it however it
// likes: a chart, a table, or an exposure warning.
func Histogram(data []byte) (Channels, error) {
	img, _, err := decode(data)
	if err != nil {
		return Channels{}, err
	}
	h := histogram.NewRGBAHistogram(img)

	out := Channels{
		Red:   append([]int{}, h.R.Bins...),
		Green: append([]int{}, h.G.Bins...),
		Blue:  append([]int{}, h.B.Bins...),
	}
	for _, bins := range [][]int{out.Red, out.Green, out.Blue} {
		for _, n := range bins {
			out.Peak = max(out.Peak, n)
		}
	}
	return out, nil
}
