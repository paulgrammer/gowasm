package image_test

import (
	"fmt"

	"example.com/image"
)

// sample is a small generated image, so the examples need no input file.
func sample() []byte {
	data, _ := image.Generate(image.Gradient, 64, 64, image.PNG)
	return data
}

func ExampleGenerate() {
	data, _ := image.Generate(image.Gradient, 64, 48, image.PNG)
	info, _ := image.Inspect(data)
	fmt.Println(info.Width, info.Height, info.Format)
	// Output: 64 48 png
}

func ExampleGenerate_unknownPattern() {
	_, err := image.Generate("spiral", 10, 10, image.PNG)
	fmt.Println(err)
	// Output: unknown pattern "spiral", want "gradient", "checker" or "wheel"
}

func ExampleGenerate_badSize() {
	_, err := image.Generate(image.Gradient, 0, 10, image.PNG)
	fmt.Println(err)
	// Output: size must be between 1 and 4096, got 0x10
}

func ExampleInspect_notAnImage() {
	_, err := image.Inspect([]byte("this is not an image"))
	fmt.Println(err != nil)
	// Output: true
}

func ExampleResize() {
	small, _ := image.Resize(sample(), 32, 0, image.PNG)
	info, _ := image.Inspect(small)
	// The height follows from the width, keeping the aspect ratio.
	fmt.Println(info.Width, info.Height)
	// Output: 32 32
}

func ExampleResize_needsADimension() {
	_, err := image.Resize(sample(), 0, 0, image.PNG)
	fmt.Println(err)
	// Output: give a width, a height, or both
}

func ExampleCrop() {
	part, _ := image.Crop(sample(), 8, 8, 16, 16, image.PNG)
	info, _ := image.Inspect(part)
	fmt.Println(info.Width, info.Height)
	// Output: 16 16
}

func ExampleCrop_outsideTheImage() {
	_, err := image.Crop(sample(), 60, 60, 20, 20, image.PNG)
	fmt.Println(err)
	// Output: crop 60,60 20x20 falls outside the 64x64 image
}

func ExampleRotate() {
	// Rotating by 45 degrees grows the canvas so nothing is cut off.
	turned, _ := image.Rotate(sample(), 45, image.PNG)
	info, _ := image.Inspect(turned)
	fmt.Println(info.Width > 64, info.Height > 64)
	// Output: true true
}

func ExampleBlur_badRadius() {
	_, err := image.Blur(sample(), 0, image.PNG)
	fmt.Println(err)
	// Output: radius must be between 0 and 100, got 0
}

func ExampleGrayscale() {
	gray, _ := image.Grayscale(sample(), image.PNG)
	info, _ := image.Inspect(gray)
	fmt.Println(info.Width, info.Height, info.Format)
	// Output: 64 64 png
}

func ExampleHistogram() {
	h, _ := image.Histogram(sample())
	fmt.Println(len(h.Red), len(h.Green), len(h.Blue), h.Peak > 0)
	// Output: 256 256 256 true
}

func ExampleAdjust() {
	// A JPEG this time, to show the format is a parameter rather than fixed.
	out, _ := image.Adjust(sample(), image.Adjustments{Brightness: 0.2, Saturation: -0.5}, image.JPEG)
	info, _ := image.Inspect(out)
	fmt.Println(info.Format)
	// Output: jpeg
}

func ExampleAdjust_unknownFormat() {
	_, err := image.Adjust(sample(), image.Adjustments{}, "webp")
	fmt.Println(err)
	// Output: unknown format "webp", want "png" or "jpeg"
}
