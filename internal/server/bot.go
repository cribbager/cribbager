package server

import (
	crand "crypto/rand"
	"encoding/binary"
	mrand "math/rand"

	"github.com/cribbager/cribbager/internal/bot"
)

// newBot builds a seated production bot by name. name must already be validated
// (bot.Valid) — an unknown name would leave the seat empty, so callers guard it
// at the request boundary and this falls back to the default as a belt-and-braces
// safety net. The RNG is seeded from crypto/rand so the random bot's choices are
// unpredictable across sessions; the deterministic bots ignore it.
func newBot(name string) bot.Bot {
	b, err := bot.New(name, mrand.New(mrand.NewSource(cryptoSeed())))
	if err != nil {
		b, _ = bot.New(bot.DefaultName, mrand.New(mrand.NewSource(cryptoSeed())))
	}
	return b
}

// cryptoSeed draws a 64-bit seed from crypto/rand for a bot's math/rand source.
func cryptoSeed() int64 {
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		return 0 // crypto/rand failing is fatal elsewhere; a fixed seed is a safe fallback here
	}
	return int64(binary.LittleEndian.Uint64(b[:]))
}
