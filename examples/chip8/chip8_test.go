package chip8_test

import (
	"fmt"
	"strings"

	"example.com/chip8"
)

// drawZero is a small program: point I at the built-in glyph for 0, set the
// draw position to the origin, draw it, then halt by jumping to itself.
var drawZero = []byte{
	0x60, 0x00, // LD V0, 0
	0x61, 0x00, // LD V1, 0
	0xF0, 0x29, // LD I, sprite for V0
	0xD0, 0x15, // DRW V0, V1, 5
	0x12, 0x08, // JP 208  (itself: halt)
}

func ExampleLoad() {
	s, _ := chip8.Load(drawZero, 1)
	fmt.Println(s.PC, s.Cycles, s.Halted)
	// Output: 512 0 false
}

func ExampleLoad_empty() {
	_, err := chip8.Load(nil, 1)
	fmt.Println(err)
	// Output: program is empty
}

func ExampleRun() {
	chip8.Load(drawZero, 1)
	s, _ := chip8.Run(100)
	fmt.Println(s.Halted, s.Cycles)
	// Output: true 5
}

func ExampleRender() {
	chip8.Load(drawZero, 1)
	chip8.Run(100)
	out, _ := chip8.Render()
	// The glyph for zero is a five row box in the top left corner.
	for _, line := range strings.Split(out, "\n")[:5] {
		fmt.Println(strings.TrimRight(line[:8], "."))
	}
	// Output:
	// ####
	// #..#
	// #..#
	// #..#
	// ####
}

func ExampleDisplay() {
	chip8.Load(drawZero, 1)
	chip8.Run(100)
	f, _ := chip8.Display()
	lit := 0
	for _, p := range f.Pixels {
		if p != 0 {
			lit++
		}
	}
	fmt.Println(f.Width, f.Height, len(f.Pixels), lit)
	// Output: 64 32 2048 14
}

func ExampleDisassemble() {
	d, _ := chip8.Disassemble(drawZero)
	fmt.Println(len(d), d[0].Opcode, d[0].Mnemonic)
	fmt.Println(d[3].Mnemonic, "-", d[3].Description)
	// Output:
	// 5 6000 LD V0, 00
	// DRW V0, V1, 5 - draw a sprite
}

func ExampleDisassemble_oddLength() {
	_, err := chip8.Disassemble([]byte{0x60})
	fmt.Println(err)
	// Output: program has an odd length, so the last instruction is truncated
}

func ExampleRun_tooManyCycles() {
	chip8.Load(drawZero, 1)
	_, err := chip8.Run(0)
	fmt.Println(err)
	// Output: cycle count must be between 1 and 100000, got 0
}

func ExamplePress() {
	chip8.Load(drawZero, 1)
	s, _ := chip8.Press(3, true)
	fmt.Println(s.WaitingForKey)
	// Output: false
}
