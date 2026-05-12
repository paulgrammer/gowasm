// Package chess exposes a full chess engine to JavaScript.
//
// Move legality is not a small problem. Castling rights, en passant, pinned
// pieces, promotion, the fifty move rule, threefold repetition and stalemate
// are all rules that a naive board representation gets wrong, and getting them
// wrong is invisible until a game reaches an unusual position.
//
// This is the sort of thing worth borrowing rather than reimplementing, and it
// is a good demonstration of the type generation: the state is a real domain
// model, so the generated TypeScript has enums, nested structs and errors to
// describe rather than strings.
package chess

import (
	"fmt"
	"strings"

	"github.com/notnil/chess"
)

// Outcome is how a game ended, or that it has not.
type Outcome string

const (
	InProgress Outcome = "*"
	WhiteWon   Outcome = "1-0"
	BlackWon   Outcome = "0-1"
	Draw       Outcome = "1/2-1/2"
)

// Color is whose turn it is.
type Color string

const (
	White Color = "white"
	Black Color = "black"
)

// Move is one legal move in a position.
type Move struct {
	// San is the move in standard algebraic notation, such as "Nf3".
	San string `json:"san"`
	// From and To are squares, such as "g1" and "f3".
	From string `json:"from"`
	To   string `json:"to"`
	// Promotion is the piece a pawn becomes, when the move promotes.
	Promotion string `json:"promotion,omitempty"`
	Capture   bool   `json:"capture,omitempty"`
	Check     bool   `json:"check,omitempty"`
	Castle    bool   `json:"castle,omitempty"`
	EnPassant bool   `json:"enPassant,omitempty"`
}

// Position is the full state of a game.
type Position struct {
	// FEN is the standard one-line encoding of the position.
	FEN string `json:"fen"`
	// Board is eight rows of eight, from rank 8 down to rank 1, using the
	// usual letters and a space for an empty square.
	Board    []string `json:"board"`
	Turn     Color    `json:"turn"`
	Outcome  Outcome  `json:"outcome"`
	Method   string   `json:"method,omitempty"`
	Check    bool     `json:"check"`
	Moves    int      `json:"moves"`
	HalfMove int      `json:"halfMoveClock"`
	// Legal lists every move available in this position.
	Legal []Move `json:"legal"`
}

func gameFrom(fen string) (*chess.Game, error) {
	if strings.TrimSpace(fen) == "" {
		return chess.NewGame(), nil
	}
	setup, err := chess.FEN(fen)
	if err != nil {
		return nil, fmt.Errorf("invalid FEN %q: %w", fen, err)
	}
	return chess.NewGame(setup), nil
}

func describe(g *chess.Game) Position {
	pos := g.Position()

	rows := []string{}
	for rank := 7; rank >= 0; rank-- {
		var b strings.Builder
		for file := range 8 {
			sq := chess.Square(rank*8 + file)
			if p := pos.Board().Piece(sq); p != chess.NoPiece {
				b.WriteString(p.String())
			} else {
				b.WriteString(" ")
			}
		}
		rows = append(rows, b.String())
	}

	turn := White
	if pos.Turn() == chess.Black {
		turn = Black
	}

	legal := []Move{}
	for _, m := range g.ValidMoves() {
		legal = append(legal, toMove(g, m))
	}

	p := Position{
		FEN:      g.FEN(),
		Board:    rows,
		Turn:     turn,
		Outcome:  Outcome(g.Outcome().String()),
		Check:    inCheck(g),
		Moves:    len(g.Moves()),
		HalfMove: pos.HalfMoveClock(),
		Legal:    legal,
	}
	if g.Outcome() != chess.NoOutcome {
		p.Method = g.Method().String()
	}
	return p
}

// inCheck reports whether the side to move is in check. The library does not
// export that directly, but it tags the move that produced the position.
func inCheck(g *chess.Game) bool {
	moves := g.Moves()
	if len(moves) == 0 {
		return false
	}
	return moves[len(moves)-1].HasTag(chess.Check)
}

func toMove(g *chess.Game, m *chess.Move) Move {
	out := Move{
		San:       chess.AlgebraicNotation{}.Encode(g.Position(), m),
		From:      m.S1().String(),
		To:        m.S2().String(),
		Capture:   m.HasTag(chess.Capture),
		Check:     m.HasTag(chess.Check),
		EnPassant: m.HasTag(chess.EnPassant),
		Castle:    m.HasTag(chess.KingSideCastle) || m.HasTag(chess.QueenSideCastle),
	}
	if m.Promo() != chess.NoPieceType {
		out.Promotion = m.Promo().String()
	}
	return out
}

// New returns the starting position.
func New() (Position, error) {
	return describe(chess.NewGame()), nil
}

// Load reads a position from FEN. An empty string gives the starting position.
func Load(fen string) (Position, error) {
	g, err := gameFrom(fen)
	if err != nil {
		return Position{}, err
	}
	return describe(g), nil
}

// Play applies a sequence of moves in algebraic notation to a position and
// returns the result. An illegal move is an error naming the move.
func Play(fen string, moves []string) (Position, error) {
	g, err := gameFrom(fen)
	if err != nil {
		return Position{}, err
	}
	for i, san := range moves {
		if err := g.MoveStr(san); err != nil {
			return Position{}, fmt.Errorf("move %d (%s) is not legal here: %w", i+1, san, err)
		}
	}
	return describe(g), nil
}

// LegalMoves lists the moves available in a position.
func LegalMoves(fen string) ([]Move, error) {
	g, err := gameFrom(fen)
	if err != nil {
		return nil, err
	}
	out := []Move{}
	for _, m := range g.ValidMoves() {
		out = append(out, toMove(g, m))
	}
	return out, nil
}

// ParsePGN reads a game in PGN and returns its final position along with the
// moves played.
func ParsePGN(text string) (Position, []string, error) {
	fn, err := chess.PGN(strings.NewReader(text))
	if err != nil {
		return Position{}, nil, fmt.Errorf("cannot read PGN: %w", err)
	}
	g := chess.NewGame(fn)

	moves := []string{}
	replay := chess.NewGame()
	for _, m := range g.Moves() {
		moves = append(moves, chess.AlgebraicNotation{}.Encode(replay.Position(), m))
		if err := replay.Move(m); err != nil {
			break
		}
	}
	return describe(g), moves, nil
}
