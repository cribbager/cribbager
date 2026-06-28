package game

import "encoding/gob"

// A GameState holds its event log as []Event — an interface. gob needs every
// concrete type that travels through an interface registered before it can
// encode or decode it, so register all event types here (once, at init). This is
// what lets a persistence layer gob-encode a GameState / Record.
func init() {
	gob.Register(CutForDeal{})
	gob.Register(HandDealt{})
	gob.Register(Discarded{})
	gob.Register(StarterCut{})
	gob.Register(CardPlayed{})
	gob.Register(Pass{})
	gob.Register(GoAwarded{})
	gob.Register(SeriesReset{})
	gob.Register(HandShown{})
	gob.Register(CribShown{})
	gob.Register(GameWon{})
}
