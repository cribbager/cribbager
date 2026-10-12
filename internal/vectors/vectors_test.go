package vectors

import (
	"os"
	"path/filepath"
	"testing"
)

// TestVectorsUpToDate regenerates every vector file in memory and fails if the
// committed copy under testdata/vectors differs. Fix with: go run ./cmd/vectors
func TestVectorsUpToDate(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "vectors")
	for name, want := range Generate() {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v (run `go run ./cmd/vectors`)", name, err)
		}
		if string(got) != string(want) {
			t.Errorf("%s is stale; run `go run ./cmd/vectors` and commit the result", name)
		}
	}
}

// TestGenerateDeterministic guards against nondeterminism creeping into the generator.
func TestGenerateDeterministic(t *testing.T) {
	a, b := Generate(), Generate()
	for name := range a {
		if string(a[name]) != string(b[name]) {
			t.Errorf("%s differs between two runs", name)
		}
	}
}
