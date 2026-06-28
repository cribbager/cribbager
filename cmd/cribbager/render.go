package main

// ASCII card art and layout helpers for the interactive `play` command.
// Everything here works on card strings ("5H", "TD", "AS") produced by
// cribbage.Card.String(), so the engine types stay at the boundary. (The peg
// board itself is drawn in board.go.)

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/cribbager/cribbager/internal/cribbage"
)

var (
	colorEnabled = os.Getenv("NO_COLOR") == ""
	clearEnabled = true
)

func init() {
	fi, err := os.Stdout.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		colorEnabled = false
		clearEnabled = false
	}
}

func clearScreen() {
	if clearEnabled {
		fmt.Print("\x1b[2J\x1b[H")
	}
}

const (
	ansiReset = "\x1b[0m"
	ansiRed   = "\x1b[31m"
	ansiBold  = "\x1b[1m"
	ansiDim   = "\x1b[2m"
)

func color(s, code string) string {
	if !colorEnabled {
		return s
	}
	return code + s + ansiReset
}

func suitSymbol(s byte) string {
	switch s {
	case 'S':
		return "♠"
	case 'H':
		return "♥"
	case 'D':
		return "♦"
	case 'C':
		return "♣"
	}
	return string(s)
}

func isRed(s byte) bool { return s == 'H' || s == 'D' }

// displayRank renders "T" as "10"; everything else is unchanged.
func displayRank(r string) string {
	if r == "T" {
		return "10"
	}
	return r
}

func splitCard(card string) (string, byte) {
	if len(card) < 2 {
		return card, '?'
	}
	return displayRank(card[:len(card)-1]), card[len(card)-1]
}

// inlineCard renders a compact colored card such as "5♥".
func inlineCard(card string) string {
	rank, suit := splitCard(card)
	s := rank + suitSymbol(suit)
	if isRed(suit) {
		return color(s, ansiRed)
	}
	return s
}

// cardLines returns the five lines of ASCII art for a single card.
func cardLines(card string) []string {
	rank, suit := splitCard(card)
	sym := suitSymbol(suit)
	if isRed(suit) {
		sym = color(sym, ansiRed)
	}
	return []string{
		"┌─────┐",
		"│" + fmt.Sprintf("%-5s", rank) + "│",
		"│  " + sym + "  │",
		"│" + fmt.Sprintf("%5s", rank) + "│",
		"└─────┘",
	}
}

// cardBlock lays a row of cards side by side (5 lines, plus a 1-based index row
// when indices is true).
func cardBlock(cs []string, indices bool) []string {
	if len(cs) == 0 {
		return nil
	}
	lines := make([][]string, len(cs))
	for i, c := range cs {
		lines[i] = cardLines(c)
	}
	rows := make([]string, 0, 6)
	for row := 0; row < 5; row++ {
		var b strings.Builder
		for i := range cs {
			b.WriteString(lines[i][row])
		}
		rows = append(rows, b.String())
	}
	if indices {
		var b strings.Builder
		for i := range cs {
			b.WriteString(fmt.Sprintf("   %d   ", i+1))
		}
		rows = append(rows, b.String())
	}
	return rows
}

func faceDownLines() []string {
	return []string{
		"┌─────┐",
		"│     │",
		"│  " + color("•", ansiDim) + "  │",
		"│     │",
		"└─────┘",
	}
}

// faceDownBlock renders n face-down cards side by side (the opponent's hidden
// hand).
func faceDownBlock(n int) []string {
	if n <= 0 {
		return []string{"", "", "", "", ""}
	}
	fd := faceDownLines()
	rows := make([]string, 5)
	for r := 0; r < 5; r++ {
		var b strings.Builder
		for i := 0; i < n; i++ {
			b.WriteString(fd[r])
		}
		rows[r] = b.String()
	}
	return rows
}

// visibleWidth counts display columns, ignoring ANSI escapes.
func visibleWidth(s string) int {
	n, i := 0, 0
	for i < len(s) {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		n++
	}
	return n
}

// joinBlocksHoriz places two line-blocks side by side with a gap, padding the
// left to a uniform width.
func joinBlocksHoriz(left, right []string, gap string) []string {
	leftW := 0
	for _, l := range left {
		if w := visibleWidth(l); w > leftW {
			leftW = w
		}
	}
	h := len(left)
	if len(right) > h {
		h = len(right)
	}
	out := make([]string, h)
	for i := 0; i < h; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out[i] = l + strings.Repeat(" ", leftW-visibleWidth(l)) + gap + r
	}
	return out
}

// cardStrings converts engine cards to display strings, sorted by rank then
// suit (for a hand). Pass sort=false to preserve order (the play pile).
func cardStrings(cards []cribbage.Card, sortHand bool) []string {
	cs := append([]cribbage.Card(nil), cards...)
	if sortHand {
		sortByRankSuit(cs)
	}
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.String()
	}
	return out
}

func sortByRankSuit(cs []cribbage.Card) {
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0; j-- {
			a, b := cs[j-1], cs[j]
			if a.Rank < b.Rank || (a.Rank == b.Rank && a.Suit <= b.Suit) {
				break
			}
			cs[j-1], cs[j] = cs[j], cs[j-1]
		}
	}
}
