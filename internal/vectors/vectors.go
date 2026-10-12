// Package vectors generates the language-neutral rules test vectors that pin the
// Go engine's behaviour for other implementations (the Swift engine in the iOS
// app). The Go engine is the reference: the vectors are produced by running its
// scorers and game state machine, deterministically from fixed seeds, and are
// committed under testdata/vectors. A test regenerates them in memory and fails
// if the committed files are stale; `go run ./cmd/vectors` rewrites them.
package vectors

import (
	"bytes"
	"encoding/json"
)

// Version is bumped whenever a file's schema changes incompatibly.
const Version = 1

const (
	HandFile = "hand_scoring.json"
	PegFile  = "pegging.json"
	GameFile = "game_flow.json"
	handSeed = 20250101
	pegSeed  = 20250102
	gameSeed = 20250103
)

// Generate returns every vector file, keyed by file name, as bytes.
func Generate() map[string][]byte {
	hv := handVectors(handSeed)
	pv := pegVectors(pegSeed)
	gv := gameVectors(gameSeed)
	return map[string][]byte{
		HandFile: file("hand_scoring", handSeed, hv),
		PegFile:  file("pegging", pegSeed, pv),
		GameFile: file("game_flow", gameSeed, gv),
	}
}

// file renders {version, kind, seed, count, vectors:[...]} with one vector per
// line so diffs stay readable and the files stay small.
func file[T any](kind string, seed int64, vs []T) []byte {
	var b bytes.Buffer
	head, _ := json.Marshal(struct {
		Version int    `json:"version"`
		Kind    string `json:"kind"`
		Seed    int64  `json:"seed"`
		Count   int    `json:"count"`
	}{Version, kind, seed, len(vs)})
	b.Write(head[:len(head)-1])
	b.WriteString(`,"vectors":[` + "\n")
	for i, v := range vs {
		enc, err := json.Marshal(v)
		if err != nil {
			panic(err)
		}
		b.WriteString("  ")
		b.Write(enc)
		if i < len(vs)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("]}\n")
	return b.Bytes()
}
