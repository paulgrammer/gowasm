package chess_test

import (
	"fmt"

	"example.com/chess"
)

func ExampleNew() {
	p, _ := chess.New()
	fmt.Println(p.Turn, len(p.Legal), p.Outcome)
	// Output: white 20 *
}

func ExamplePlay() {
	// Scholar's mate.
	p, _ := chess.Play("", []string{"e4", "e5", "Bc4", "Nc6", "Qh5", "Nf6", "Qxf7#"})
	fmt.Println(p.Outcome, p.Method, p.Check)
	// Output: 1-0 Checkmate true
}

func ExamplePlay_illegalMove() {
	_, err := chess.Play("", []string{"e4", "e5", "Ke2", "Ke7", "Qh8"})
	fmt.Println(err != nil)
	// Output: true
}

func ExamplePlay_fool() {
	// The fastest possible checkmate, and black wins it.
	p, _ := chess.Play("", []string{"f3", "e5", "g4", "Qh4#"})
	fmt.Println(p.Outcome, p.Method)
	// Output: 0-1 Checkmate
}

func ExampleLoad() {
	// A position one move from stalemate, which a naive engine calls a loss.
	p, _ := chess.Load("7k/5Q2/6K1/8/8/8/8/8 b - - 0 1")
	fmt.Println(p.Turn, len(p.Legal), p.Outcome)
	// Output: black 0 1/2-1/2
}

func ExampleLoad_invalid() {
	_, err := chess.Load("not a position")
	fmt.Println(err != nil)
	// Output: true
}

func ExampleLegalMoves() {
	moves, _ := chess.LegalMoves("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	fmt.Println(len(moves), moves[0].From, moves[0].To)
	// Output: 20 b1 a3
}
