package main

// The interactive `play` command runs a full game in-process against a bot
// opponent (selected with --bot). It drives the engine directly and renders the
// table from your seat each turn, reusing the card art and peg board.

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/cribbager/cribbager/internal/bot"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
	"github.com/cribbager/cribbager/internal/scoring/pegging"
)

type ui struct {
	g         *game.Game
	you       game.Seat
	opp       game.Seat
	oppBot    bot.Bot
	sc        *bufio.Scanner
	feed      [2]string // latest status line per seat
	feedStale bool      // a series just reset; clear feed before the next series' plays
	prev      [2]int    // previous score per seat (trailing peg)
	starter   string    // current hand's starter, "" if not cut
}

func cmdPlay(args []string) error {
	seed := time.Now().UnixNano()
	botName := bot.DefaultName
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--seed":
			if i+1 < len(args) {
				if _, err := fmt.Sscan(args[i+1], &seed); err != nil {
					return fmt.Errorf("--seed: %q is not a number", args[i+1])
				}
				i++
			}
		case "--bot":
			if i+1 < len(args) {
				botName = args[i+1]
				i++
			}
		}
	}

	// XOR with the golden-ratio constant so the bot's RNG is decorrelated from
	// the deck RNG even though both derive from the same --seed.
	opp, err := bot.New(botName, rand.New(rand.NewSource(seed^0x9e3779b9)))
	if err != nil {
		return err
	}

	g := game.New(game.Options{Deck: game.NewSeededDeck(seed)})
	u := &ui{
		g:      g,
		you:    game.Seat0,
		opp:    game.Seat1,
		oppBot: opp,
		sc:     bufio.NewScanner(os.Stdin),
	}
	return u.run()
}

func (u *ui) run() error {
	for {
		v := u.g.View(u.you)
		clearScreen()
		fmt.Print(u.renderTable(v))

		if v.Winner != nil {
			u.printResult(v)
			return nil
		}

		switch v.Phase {
		case game.PhaseDiscard:
			if len(v.YourHand) == 6 {
				cmd, ok := u.promptDiscard(v)
				if !ok {
					return nil
				}
				if err := u.apply(u.you, cmd); err != nil {
					return err
				}
			} else {
				u.opponentDiscard()
				time.Sleep(500 * time.Millisecond)
			}

		case game.PhasePlay:
			if v.ToPlay != nil && *v.ToPlay == u.you {
				cmd, ok := u.promptPlay(v)
				if !ok {
					return nil
				}
				if err := u.apply(u.you, cmd); err != nil {
					return err
				}
			} else {
				u.opponentPlay()
				time.Sleep(700 * time.Millisecond)
			}
		}
	}
}

// --- applying and event processing -------------------------------------------

func (u *ui) apply(seat game.Seat, cmd game.Command) error {
	starterForShow := u.starter
	dealerForShow := u.g.View(u.you).Dealer
	old := u.g.Scores()

	evs, err := u.g.Apply(seat, cmd)
	if err != nil {
		fmt.Fprintln(os.Stderr, color("rejected: "+err.Error(), ansiRed))
		time.Sleep(time.Second)
		return nil
	}

	// A prior series reset leaves its closing line (e.g. "thirty-one — 2") on
	// screen for one render; clear it now, before this batch's new plays, so it
	// doesn't linger into the next count series.
	if u.feedStale {
		u.feed = [2]string{}
		u.feedStale = false
	}

	for _, e := range evs {
		switch e := e.(type) {
		case game.CardPlayed:
			u.feed[e.Seat] = pegPhrase(e.Score)
		case game.GoAwarded:
			u.feed[e.Seat] = "go — peg 1"
		case game.Pass:
			u.feed[e.Seat] = "go"
		case game.StarterCut:
			u.starter = e.Card.String()
			if e.Heels > 0 {
				u.feed[dealerForShow] = "his heels — 2"
			}
		case game.SeriesReset:
			u.feedStale = true // clear on the next batch, after this reset is shown once
		case game.HandDealt:
			u.feed = [2]string{}
			u.feedStale = false
			u.starter = ""
		}
	}

	now := u.g.Scores()
	for s := 0; s < 2; s++ {
		if now[s] != old[s] {
			u.prev[s] = old[s]
		}
	}

	var show []game.Event
	for _, e := range evs {
		switch e.(type) {
		case game.HandShown, game.CribShown:
			show = append(show, e)
		}
	}
	if len(show) > 0 {
		clearScreen()
		fmt.Print(u.renderCount(show, starterForShow, dealerForShow))
		fmt.Print(color("\npress enter to continue ", ansiDim))
		if !u.readLine() {
			return errQuit
		}
	}
	return nil
}

var errQuit = fmt.Errorf("quit")

// --- the opponent (a bot, selected via --bot) --------------------------------

func (u *ui) opponentDiscard() {
	_ = u.apply(u.opp, game.Discard{Cards: u.oppBot.Discard(u.g.View(u.opp))})
}

func (u *ui) opponentPlay() {
	_ = u.apply(u.opp, game.Play{Card: u.oppBot.Play(u.g.View(u.opp))})
}

// --- table rendering ----------------------------------------------------------

func (u *ui) renderTable(v game.PlayerView) string {
	var b strings.Builder
	b.WriteString(color("♠ ♥ Cribbage ♦ ♣\n\n", ansiBold))

	youDeal := v.Dealer == u.you

	// opponent (top): face-down hand, with the deck+starter on its right if the
	// opponent is the dealer.
	opp := faceDownBlock(v.OpponentCards)
	board := pegBoard(v.Scores[u.opp], u.prev[u.opp], v.Scores[u.you], u.prev[u.you])
	if !youDeal {
		opp = padTo(opp, boardWidth)
		opp = attachStarter(opp, u.starterPtr())
	}
	emitLines(&b, withStatus(opp, u.feed[u.opp]))

	board = attachStarter(board, nil) // keep board aligned with the dealer rows
	emitLines(&b, board)

	if v.Phase == game.PhasePlay || len(currentPlays(u.g, u.you)) > 0 {
		emitLines(&b, peggingArea(currentPlays(u.g, u.you), v.Count))
	}

	// you (bottom): your hand with selection numbers, deck+starter if you deal.
	hand := cardBlock(cardStrings(v.YourHand, true), true)
	if youDeal {
		hand = padTo(hand, boardWidth)
		hand = attachStarter(hand, u.starterPtr())
	}
	emitLines(&b, withStatus(hand, u.feed[u.you]))

	b.WriteString(fmt.Sprintf("\n  You %d   ·   Opponent %d\n", v.Scores[u.you], v.Scores[u.opp]))
	return b.String()
}

func (u *ui) starterPtr() *string {
	if u.starter == "" {
		return nil
	}
	s := u.starter
	return &s
}

// withStatus appends a bold status line under a block (blank if empty).
func withStatus(block []string, status string) []string {
	if status == "" {
		return block
	}
	return append(block, color(status, ansiBold))
}

// padTo right-pads each line of a block to width so an attached block aligns.
func padTo(block []string, width int) []string {
	out := make([]string, len(block))
	for i, l := range block {
		if pad := width - visibleWidth(l); pad > 0 {
			l += strings.Repeat(" ", pad)
		}
		out[i] = l
	}
	return out
}

type uiPlay struct {
	card  string
	mine  bool
	group int
}

// currentPlays reconstructs the current hand's play sequence (with group
// boundaries at each series reset) from the event log.
func currentPlays(g *game.Game, you game.Seat) []uiPlay {
	evs := g.Events()
	start := 0
	for i, e := range evs {
		if _, ok := e.(game.HandDealt); ok {
			start = i
		}
	}
	var plays []uiPlay
	group := 0
	for _, e := range evs[start:] {
		switch e := e.(type) {
		case game.SeriesReset:
			group++
		case game.CardPlayed:
			plays = append(plays, uiPlay{card: e.Card.String(), mine: e.Seat == you, group: group})
		}
	}
	return plays
}

// peggingArea lays the round's played cards left to right (yours dropped one
// row), grouped by sub-sequence, with the running count to the right.
func peggingArea(plays []uiPlay, count int) []string {
	const (
		rows     = 6
		cardW    = 7
		groupGap = 3
	)
	type placed struct {
		x, y  int
		lines []string
	}
	var ps []placed
	x := 0
	for i, p := range plays {
		if i > 0 && p.group != plays[i-1].group {
			x += groupGap
		}
		y := 0
		if p.mine {
			y = 1
		}
		ps = append(ps, placed{x: x, y: y, lines: cardLines(p.card)})
		x += cardW
	}

	out := make([]string, rows)
	for r := 0; r < rows; r++ {
		var sb strings.Builder
		cursor := 0
		for _, pc := range ps {
			if r >= pc.y && r < pc.y+5 {
				if pc.x > cursor {
					sb.WriteString(strings.Repeat(" ", pc.x-cursor))
				}
				sb.WriteString(pc.lines[r-pc.y])
				cursor = pc.x + cardW
			}
		}
		out[r] = sb.String()
	}
	if count > 0 {
		width := 0
		for _, l := range out {
			if w := visibleWidth(l); w > width {
				width = w
			}
		}
		mid := (rows - 1) / 2
		out[mid] += strings.Repeat(" ", width-visibleWidth(out[mid])) + "   " + color(fmt.Sprintf("(%d)", count), ansiBold)
	}
	return out
}

// --- the count (show) screen --------------------------------------------------

func (u *ui) renderCount(show []game.Event, starter string, dealer game.Seat) string {
	var youHand, oppHand, cribCards []string
	var youTot, oppTot, cribTot int
	cribShown := false
	for _, e := range show {
		switch e := e.(type) {
		case game.HandShown:
			if e.Seat == u.you {
				youHand, youTot = cardStrings(e.Cards, true), e.Score.Total
			} else {
				oppHand, oppTot = cardStrings(e.Cards, true), e.Score.Total
			}
		case game.CribShown:
			cribCards, cribTot, cribShown = cardStrings(e.Cards, true), e.Score.Total, true
		}
	}

	var b strings.Builder
	b.WriteString(color("♠ ♥ Cribbage ♦ ♣\n\n", ansiBold))
	scores := u.g.Scores()

	oppRow := handStarterBar(oppHand, starter, fmt.Sprintf(" %d pts ", oppTot))
	if cribShown && dealer != u.you {
		oppRow = rightAligned(oppRow, handStarterBar(cribCards, starter, fmt.Sprintf(" Crib: %d ", cribTot)), boardWidth)
	}
	emitLines(&b, oppRow)
	emitLines(&b, pegBoard(scores[u.opp], u.prev[u.opp], scores[u.you], u.prev[u.you]))
	youRow := handStarterBar(youHand, starter, fmt.Sprintf(" %d pts ", youTot))
	if cribShown && dealer == u.you {
		youRow = rightAligned(youRow, handStarterBar(cribCards, starter, fmt.Sprintf(" Crib: %d ", cribTot)), boardWidth)
	}
	emitLines(&b, youRow)
	return b.String()
}

func handStarterBar(hand []string, starter, label string) []string {
	if len(hand) == 0 {
		return nil
	}
	block := cardBlock(hand, false)
	width := 7 * len(hand)
	if starter != "" {
		block = joinBlocksHoriz(block, cardLines(starter), "  ")
		width += 2 + 7
	}
	return append(block, scoreBar(label, width))
}

func scoreBar(label string, width int) string {
	lw := visibleWidth(label)
	if lw >= width {
		return color(label, ansiBold)
	}
	left := (width - lw) / 2
	return color(strings.Repeat("─", left)+label+strings.Repeat("─", width-lw-left), ansiBold)
}

// --- prompts ------------------------------------------------------------------

func (u *ui) promptDiscard(v game.PlayerView) (game.Command, bool) {
	hand := cardStrings(v.YourHand, true)
	fmt.Println()
	for {
		fmt.Print("Discard two to the crib (e.g. '1 4' or '5H JD'): ")
		line, ok := u.readLineText()
		if !ok {
			return nil, false
		}
		toks := strings.Fields(line)
		if len(toks) != 2 {
			fmt.Println("  pick exactly two")
			continue
		}
		c0, ok0 := resolveCard(toks[0], hand)
		c1, ok1 := resolveCard(toks[1], hand)
		if !ok0 || !ok1 || c0 == c1 {
			fmt.Println("  pick two distinct cards from your hand")
			continue
		}
		return game.Discard{Cards: [2]cribbage.Card{mustParse(c0), mustParse(c1)}}, true
	}
}

func (u *ui) promptPlay(v game.PlayerView) (game.Command, bool) {
	hand := cardStrings(v.YourHand, true)
	room := 31 - v.Count
	fmt.Println()
	for {
		fmt.Printf("Play (count %d): ", v.Count)
		line, ok := u.readLineText()
		if !ok {
			return nil, false
		}
		c, ok := resolveCard(strings.TrimSpace(line), hand)
		if !ok {
			fmt.Println("  pick a card from your hand")
			continue
		}
		if mustParse(c).Rank.PipValue() > room {
			fmt.Printf("  %s is too big (only %d room)\n", inlineCard(c), room)
			continue
		}
		return game.Play{Card: mustParse(c)}, true
	}
}

// resolveCard accepts a 1-based hand index or a card name.
func resolveCard(arg string, hand []string) (string, bool) {
	for i, c := range hand {
		if fmt.Sprintf("%d", i+1) == arg {
			return c, true
		}
	}
	up := normalizeCardText(arg)
	for _, c := range hand {
		if c == up {
			return c, true
		}
	}
	return "", false
}

func mustParse(s string) cribbage.Card {
	c, _ := cribbage.ParseCard(s)
	return c
}

func (u *ui) readLine() bool {
	_, ok := u.readLineText()
	return ok
}

func (u *ui) readLineText() (string, bool) {
	if !u.sc.Scan() {
		return "", false
	}
	return u.sc.Text(), true
}

// --- result -------------------------------------------------------------------

// Skunk thresholds (ACC): a loser short of skunkLine is skunked; short of
// doubleSkunkLine, double-skunked.
const (
	skunkLine       = 91
	doubleSkunkLine = 61
)

func (u *ui) printResult(v game.PlayerView) {
	you, opp := v.Scores[u.you], v.Scores[u.opp]
	winner := "You"
	win, lose := you, opp
	if opp > you {
		winner, win, lose = "The opponent", opp, you
	}
	msg := fmt.Sprintf("GAME OVER — %s win %d–%d", winner, win, lose)
	switch {
	case lose < doubleSkunkLine:
		msg += "  ·  DOUBLE SKUNK!"
	case lose < skunkLine:
		msg += "  ·  SKUNK!"
	}
	fmt.Println("\n" + color(msg, ansiBold))
}

// --- block helpers ------------------------------------------------------------

func emitLines(b *strings.Builder, lines []string) {
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
}

// rightAligned places the right block so its right edge sits at totalWidth.
func rightAligned(left, right []string, totalWidth int) []string {
	rw := 0
	for _, r := range right {
		if w := visibleWidth(r); w > rw {
			rw = w
		}
	}
	leftCol := totalWidth - rw
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
		pad := leftCol - visibleWidth(l)
		if pad < 0 {
			pad = 0
		}
		out[i] = l + strings.Repeat(" ", pad) + r
	}
	return out
}

// pegPhrase renders a play's scoring as a short status line.
func pegPhrase(r pegging.Result) string {
	var parts []string
	for _, c := range r.Combos() {
		switch c.Kind {
		case pegging.Fifteen:
			parts = append(parts, "fifteen 2")
		case pegging.ThirtyOne:
			parts = append(parts, "thirty-one 2")
		case pegging.Pair:
			parts = append(parts, fmt.Sprintf("%s %d", pegPairName(c.Points), c.Points))
		case pegging.Run:
			parts = append(parts, fmt.Sprintf("run of %d", c.RunLength))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " + ") + fmt.Sprintf("  (%d)", r.Total)
}
