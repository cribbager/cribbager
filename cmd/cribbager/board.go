package main

import "strings"

// Terminal rendering of the cribbage peg board for the `cribbager play` command.
//
// It draws a standard long (out-and-back) board: each player has two rows of
// holes. The out row carries the start hole then holes 1–60 left to right; the
// back row carries the game hole (121) at the far left then 120 down to 61. So a
// peg travels out along the top row and back along the bottom, and the start (0)
// and game (121) holes line up at the far left. The front peg (bold ●) marks the
// current score and the back peg (dim ●) the previous score; before the first
// point the front peg sits in the game hole and the back peg in the start hole.

const (
	pegBoardWidth = 61 + 12           // 61 holes plus the spaces grouping them in fives
	boardWidth    = pegBoardWidth + 4 // "│ " + row + " │"
)

func pegLine(score, prev int, holeAt func(i int) int) string {
	front := score
	if score == 0 {
		front = 121 // parked in the game hole until the first score moves it out
	}
	var b strings.Builder
	for i := 0; i < 61; i++ {
		if i >= 1 && (i-1)%5 == 0 {
			b.WriteByte(' ')
		}
		switch pos := holeAt(i); {
		case pos == front:
			b.WriteString(color("●", ansiBold))
		case pos == prev:
			b.WriteString(color("●", ansiDim))
		default:
			b.WriteString(color("○", ansiDim))
		}
	}
	return b.String()
}

func outRowHole(i int) int  { return i }       // 0..60 (0 is the start hole)
func backRowHole(i int) int { return 121 - i } // 121..61 (121 is the game hole)

// pegBoard renders the bordered board: the opponent's two rows on top, a spacer,
// then your two rows.
func pegBoard(opp, oppPrev, you, youPrev int) []string {
	edge := strings.Repeat("─", pegBoardWidth+2)
	blank := "│ " + strings.Repeat(" ", pegBoardWidth) + " │"
	row := func(s string) string { return "│ " + s + " │" }
	return []string{
		"╭" + edge + "╮",
		row(pegLine(opp, oppPrev, outRowHole)),
		row(pegLine(opp, oppPrev, backRowHole)),
		blank,
		row(pegLine(you, youPrev, outRowHole)),
		row(pegLine(you, youPrev, backRowHole)),
		"╰" + edge + "╯",
	}
}

// attachStarter places the deck (face down) and, once cut, the starter to the
// right of the board.
func attachStarter(board []string, starter *string) []string {
	right := faceDownLines()
	if starter != nil {
		right = joinBlocksHoriz(right, cardLines(*starter), "  ")
	}
	return joinBlocksHoriz(board, right, "   ")
}
