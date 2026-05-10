// Package chip8 is a CHIP-8 interpreter driven from JavaScript.
//
// CHIP-8 is a virtual machine from 1977: 35 instructions, 4KB of memory, a
// 64x32 monochrome display. It is the traditional first emulator, and it makes
// a good example here because the interesting state is genuinely structured --
// registers, a stack, a framebuffer, a keypad -- so the generated TypeScript
// has something to describe beyond strings and numbers.
//
// The frame buffer crosses the boundary as a []byte, which the generated client
// converts to a Uint8Array. One byte per pixel is wasteful on the wire and
// exactly what you want on the other end, where it goes straight into an
// ImageData or a WebGL texture without unpacking bits.
package chip8

import (
	"fmt"
	"math/rand"
)

const (
	// Width and Height are the display dimensions, in pixels.
	Width  = 64
	Height = 32

	memorySize   = 4096
	programStart = 0x200
	stackDepth   = 16
)

// font is the built-in hexadecimal glyph set, five bytes per character. Every
// CHIP-8 program expects it at the bottom of memory.
var font = [80]byte{
	0xF0, 0x90, 0x90, 0x90, 0xF0, 0x20, 0x60, 0x20, 0x20, 0x70,
	0xF0, 0x10, 0xF0, 0x80, 0xF0, 0xF0, 0x10, 0xF0, 0x10, 0xF0,
	0x90, 0x90, 0xF0, 0x10, 0x10, 0xF0, 0x80, 0xF0, 0x10, 0xF0,
	0xF0, 0x80, 0xF0, 0x90, 0xF0, 0xF0, 0x10, 0x20, 0x40, 0x40,
	0xF0, 0x90, 0xF0, 0x90, 0xF0, 0xF0, 0x90, 0xF0, 0x10, 0xF0,
	0xF0, 0x90, 0xF0, 0x90, 0x90, 0xE0, 0x90, 0xE0, 0x90, 0xE0,
	0xF0, 0x80, 0x80, 0x80, 0xF0, 0xE0, 0x90, 0x90, 0x90, 0xE0,
	0xF0, 0x80, 0xF0, 0x80, 0xF0, 0xF0, 0x80, 0xF0, 0x80, 0x80,
}

// State is everything a debugger would want to show.
type State struct {
	// PC is the program counter.
	PC uint16 `json:"pc"`
	// I is the address register.
	I uint16 `json:"i"`
	// V holds the sixteen general purpose registers.
	V []int `json:"v"`
	// Stack holds pending return addresses.
	Stack []int `json:"stack"`
	// DelayTimer and SoundTimer count down at 60Hz.
	DelayTimer int `json:"delayTimer"`
	SoundTimer int `json:"soundTimer"`
	// Cycles is how many instructions have been executed.
	Cycles int `json:"cycles"`
	// Halted is set when the program cannot continue.
	Halted bool `json:"halted"`
	// WaitingForKey is set while a program is blocked on input.
	WaitingForKey bool `json:"waitingForKey"`
}

// Frame is one rendered display.
type Frame struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	// Pixels is one byte per pixel, 0 or 255, row major. Ready to be dropped
	// into an ImageData alpha channel or a WebGL texture.
	Pixels []byte `json:"pixels"`
	// Beeping is true while the sound timer is running.
	Beeping bool `json:"beeping"`
}

// Disassembly is one decoded instruction.
type Disassembly struct {
	Address     int    `json:"address"`
	Opcode      string `json:"opcode"`
	Mnemonic    string `json:"mnemonic"`
	Description string `json:"description"`
}

type machine struct {
	mem     [memorySize]byte
	v       [16]byte
	i       uint16
	pc      uint16
	stack   [stackDepth]uint16
	sp      byte
	display [Width * Height]byte
	keys    [16]bool

	delay, sound  byte
	cycles        int
	halted        bool
	waitKey       bool
	waitRegister  byte
	loaded        bool
	rng           *rand.Rand
}

var vm = &machine{rng: rand.New(rand.NewSource(1))}

// Load resets the machine and loads a program.
//
// The seed makes the random instruction reproducible, so a test can assert on
// a program's output rather than merely that it did not crash.
func Load(program []byte, seed int) (State, error) {
	if len(program) == 0 {
		return State{}, fmt.Errorf("program is empty")
	}
	if len(program) > memorySize-programStart {
		return State{}, fmt.Errorf("program is %d bytes, which does not fit in %d", len(program), memorySize-programStart)
	}

	vm = &machine{pc: programStart, rng: rand.New(rand.NewSource(int64(seed))), loaded: true}
	copy(vm.mem[:], font[:])
	copy(vm.mem[programStart:], program)
	return snapshot(), nil
}

// Reset clears the display and registers, keeping the loaded program.
func Reset() (State, error) {
	if !vm.loaded {
		return State{}, fmt.Errorf("no program loaded")
	}
	program := make([]byte, memorySize-programStart)
	copy(program, vm.mem[programStart:])
	return Load(program, 1)
}

// Step executes a single instruction.
func Step() (State, error) {
	if !vm.loaded {
		return State{}, fmt.Errorf("no program loaded")
	}
	if err := vm.step(); err != nil {
		return snapshot(), err
	}
	return snapshot(), nil
}

// Run executes up to n instructions, stopping early if the machine halts or
// blocks on input. Timers tick once per 8 instructions, roughly 60Hz at the
// usual 500Hz clock.
func Run(n int) (State, error) {
	if !vm.loaded {
		return State{}, fmt.Errorf("no program loaded")
	}
	if n < 1 || n > 100000 {
		return State{}, fmt.Errorf("cycle count must be between 1 and 100000, got %d", n)
	}
	for c := range n {
		if vm.halted || vm.waitKey {
			break
		}
		if err := vm.step(); err != nil {
			return snapshot(), err
		}
		if c%8 == 7 {
			vm.tick()
		}
	}
	return snapshot(), nil
}

// Display returns the current frame.
func Display() (Frame, error) {
	if !vm.loaded {
		return Frame{}, fmt.Errorf("no program loaded")
	}
	pixels := make([]byte, len(vm.display))
	for i, on := range vm.display {
		if on != 0 {
			pixels[i] = 255
		}
	}
	return Frame{Width: Width, Height: Height, Pixels: pixels, Beeping: vm.sound > 0}, nil
}

// Render draws the display as text, which is how the examples below assert on
// it without a canvas.
func Render() (string, error) {
	if !vm.loaded {
		return "", fmt.Errorf("no program loaded")
	}
	out := make([]byte, 0, (Width+1)*Height)
	for y := range Height {
		for x := range Width {
			if vm.display[y*Width+x] != 0 {
				out = append(out, '#')
			} else {
				out = append(out, '.')
			}
		}
		out = append(out, '\n')
	}
	return string(out), nil
}

// Press sets or clears one of the sixteen keys.
func Press(key int, down bool) (State, error) {
	if key < 0 || key > 15 {
		return State{}, fmt.Errorf("key must be 0 to 15, got %d", key)
	}
	vm.keys[key] = down
	if down && vm.waitKey {
		vm.v[vm.waitRegister] = byte(key)
		vm.waitKey = false
	}
	return snapshot(), nil
}

// Disassemble decodes a program without running it.
func Disassemble(program []byte) ([]Disassembly, error) {
	if len(program)%2 != 0 {
		return nil, fmt.Errorf("program has an odd length, so the last instruction is truncated")
	}
	out := make([]Disassembly, 0, len(program)/2)
	for i := 0; i+1 < len(program); i += 2 {
		op := uint16(program[i])<<8 | uint16(program[i+1])
		m, d := decode(op)
		out = append(out, Disassembly{
			Address:     programStart + i,
			Opcode:      fmt.Sprintf("%04X", op),
			Mnemonic:    m,
			Description: d,
		})
	}
	return out, nil
}

func snapshot() State {
	v := make([]int, 16)
	for i, r := range vm.v {
		v[i] = int(r)
	}
	stack := make([]int, vm.sp)
	for i := range vm.sp {
		stack[i] = int(vm.stack[i])
	}
	return State{
		PC: vm.pc, I: vm.i, V: v, Stack: stack,
		DelayTimer: int(vm.delay), SoundTimer: int(vm.sound),
		Cycles: vm.cycles, Halted: vm.halted, WaitingForKey: vm.waitKey,
	}
}

func (m *machine) tick() {
	if m.delay > 0 {
		m.delay--
	}
	if m.sound > 0 {
		m.sound--
	}
}

func (m *machine) step() error {
	if int(m.pc)+1 >= memorySize {
		m.halted = true
		return fmt.Errorf("program counter ran past the end of memory")
	}
	op := uint16(m.mem[m.pc])<<8 | uint16(m.mem[m.pc+1])
	m.pc += 2
	m.cycles++

	x := byte(op>>8) & 0x0F
	y := byte(op>>4) & 0x0F
	n := byte(op) & 0x0F
	kk := byte(op)
	nnn := op & 0x0FFF

	switch op & 0xF000 {
	case 0x0000:
		switch op {
		case 0x00E0:
			m.display = [Width * Height]byte{}
		case 0x00EE:
			if m.sp == 0 {
				m.halted = true
				return fmt.Errorf("return with an empty stack")
			}
			m.sp--
			m.pc = m.stack[m.sp]
		default:
			m.halted = true
			return fmt.Errorf("unknown instruction %04X", op)
		}
	case 0x1000:
		if nnn == m.pc-2 {
			// A jump to itself is the idiomatic way a CHIP-8 program ends.
			m.halted = true
		}
		m.pc = nnn
	case 0x2000:
		if int(m.sp) >= stackDepth {
			m.halted = true
			return fmt.Errorf("call stack overflow")
		}
		m.stack[m.sp] = m.pc
		m.sp++
		m.pc = nnn
	case 0x3000:
		if m.v[x] == kk {
			m.pc += 2
		}
	case 0x4000:
		if m.v[x] != kk {
			m.pc += 2
		}
	case 0x5000:
		if m.v[x] == m.v[y] {
			m.pc += 2
		}
	case 0x6000:
		m.v[x] = kk
	case 0x7000:
		m.v[x] += kk
	case 0x8000:
		m.arithmetic(x, y, n)
	case 0x9000:
		if m.v[x] != m.v[y] {
			m.pc += 2
		}
	case 0xA000:
		m.i = nnn
	case 0xB000:
		m.pc = nnn + uint16(m.v[0])
	case 0xC000:
		m.v[x] = byte(m.rng.Intn(256)) & kk
	case 0xD000:
		m.draw(x, y, n)
	case 0xE000:
		switch kk {
		case 0x9E:
			if m.keys[m.v[x]&0x0F] {
				m.pc += 2
			}
		case 0xA1:
			if !m.keys[m.v[x]&0x0F] {
				m.pc += 2
			}
		}
	case 0xF000:
		return m.misc(x, kk)
	}
	return nil
}

func (m *machine) arithmetic(x, y, n byte) {
	switch n {
	case 0x0:
		m.v[x] = m.v[y]
	case 0x1:
		m.v[x] |= m.v[y]
	case 0x2:
		m.v[x] &= m.v[y]
	case 0x3:
		m.v[x] ^= m.v[y]
	case 0x4:
		sum := uint16(m.v[x]) + uint16(m.v[y])
		m.v[0xF] = 0
		if sum > 255 {
			m.v[0xF] = 1
		}
		m.v[x] = byte(sum)
	case 0x5:
		borrow := byte(0)
		if m.v[x] >= m.v[y] {
			borrow = 1
		}
		m.v[x] -= m.v[y]
		m.v[0xF] = borrow
	case 0x6:
		lsb := m.v[x] & 1
		m.v[x] >>= 1
		m.v[0xF] = lsb
	case 0x7:
		borrow := byte(0)
		if m.v[y] >= m.v[x] {
			borrow = 1
		}
		m.v[x] = m.v[y] - m.v[x]
		m.v[0xF] = borrow
	case 0xE:
		msb := m.v[x] >> 7
		m.v[x] <<= 1
		m.v[0xF] = msb
	}
}

// draw XORs a sprite into the display and sets VF when a pixel is erased,
// which is how CHIP-8 programs do collision detection.
func (m *machine) draw(x, y, n byte) {
	ox, oy := int(m.v[x])%Width, int(m.v[y])%Height
	m.v[0xF] = 0
	for row := range int(n) {
		if int(m.i)+row >= memorySize {
			break
		}
		sprite := m.mem[int(m.i)+row]
		py := oy + row
		if py >= Height {
			break
		}
		for col := range 8 {
			if sprite&(0x80>>col) == 0 {
				continue
			}
			px := ox + col
			if px >= Width {
				break
			}
			idx := py*Width + px
			if m.display[idx] != 0 {
				m.v[0xF] = 1
			}
			m.display[idx] ^= 1
		}
	}
}

func (m *machine) misc(x, kk byte) error {
	switch kk {
	case 0x07:
		m.v[x] = m.delay
	case 0x0A:
		m.waitKey = true
		m.waitRegister = x
	case 0x15:
		m.delay = m.v[x]
	case 0x18:
		m.sound = m.v[x]
	case 0x1E:
		m.i += uint16(m.v[x])
	case 0x29:
		m.i = uint16(m.v[x]&0x0F) * 5
	case 0x33:
		if int(m.i)+2 >= memorySize {
			return fmt.Errorf("BCD write past the end of memory")
		}
		v := m.v[x]
		m.mem[m.i] = v / 100
		m.mem[m.i+1] = (v / 10) % 10
		m.mem[m.i+2] = v % 10
	case 0x55:
		for r := byte(0); r <= x; r++ {
			if int(m.i)+int(r) >= memorySize {
				return fmt.Errorf("register store past the end of memory")
			}
			m.mem[m.i+uint16(r)] = m.v[r]
		}
	case 0x65:
		for r := byte(0); r <= x; r++ {
			if int(m.i)+int(r) >= memorySize {
				return fmt.Errorf("register load past the end of memory")
			}
			m.v[r] = m.mem[m.i+uint16(r)]
		}
	}
	return nil
}

func decode(op uint16) (mnemonic, description string) {
	x := byte(op>>8) & 0x0F
	y := byte(op>>4) & 0x0F
	n := byte(op) & 0x0F
	kk := byte(op)
	nnn := op & 0x0FFF

	switch op & 0xF000 {
	case 0x0000:
		switch op {
		case 0x00E0:
			return "CLS", "clear the display"
		case 0x00EE:
			return "RET", "return from a subroutine"
		}
	case 0x1000:
		return fmt.Sprintf("JP %03X", nnn), "jump"
	case 0x2000:
		return fmt.Sprintf("CALL %03X", nnn), "call a subroutine"
	case 0x3000:
		return fmt.Sprintf("SE V%X, %02X", x, kk), "skip if equal"
	case 0x4000:
		return fmt.Sprintf("SNE V%X, %02X", x, kk), "skip if not equal"
	case 0x6000:
		return fmt.Sprintf("LD V%X, %02X", x, kk), "load a constant"
	case 0x7000:
		return fmt.Sprintf("ADD V%X, %02X", x, kk), "add a constant"
	case 0x8000:
		return fmt.Sprintf("ALU V%X, V%X", x, y), "register arithmetic"
	case 0xA000:
		return fmt.Sprintf("LD I, %03X", nnn), "set the address register"
	case 0xC000:
		return fmt.Sprintf("RND V%X, %02X", x, kk), "random byte"
	case 0xD000:
		return fmt.Sprintf("DRW V%X, V%X, %X", x, y, n), "draw a sprite"
	case 0xF000:
		return fmt.Sprintf("LD V%X, [F%02X]", x, kk), "misc load or store"
	}
	return fmt.Sprintf("DW %04X", op), "data, or an unknown instruction"
}
