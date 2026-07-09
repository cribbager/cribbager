package nn

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

// TestParityWithPyTorch loads a network exported by the real Python exporter
// (ml/cribml/export.py) and asserts our forward pass produces the same outputs
// PyTorch did on the same inputs. This is the contract test that lets every
// later model trust the train-in-Python/infer-in-Go split: if the layouts,
// activation placement, or arithmetic ever drift, this fails, not a bot.
//
// Regenerate the fixtures with:  cd ml && uv run scripts/make_parity_fixture.py
//
// Tolerance: PyTorch computes in float32, we compute in float64 over the same
// (exactly-representable) weights, so small rounding differences are expected;
// 1e-4 absolute is orders of magnitude above them and far below anything that
// could change a bot's argmax.
func TestParityWithPyTorch(t *testing.T) {
	m, err := LoadFile("testdata/parity_mlp.json")
	if err != nil {
		t.Fatalf("loading parity weights: %v", err)
	}

	raw, err := os.ReadFile("testdata/parity_io.json")
	if err != nil {
		t.Fatalf("loading parity cases: %v", err)
	}
	var cases struct {
		Inputs  [][]float64 `json:"inputs"`
		Outputs [][]float64 `json:"outputs"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("decoding parity cases: %v", err)
	}
	if len(cases.Inputs) == 0 || len(cases.Inputs) != len(cases.Outputs) {
		t.Fatalf("malformed parity cases: %d inputs, %d outputs", len(cases.Inputs), len(cases.Outputs))
	}

	for i, in := range cases.Inputs {
		got := m.Forward(in)
		want := cases.Outputs[i]
		if len(got) != len(want) {
			t.Fatalf("case %d: got %d outputs, want %d", i, len(got), len(want))
		}
		for j := range got {
			if math.Abs(got[j]-want[j]) > 1e-4 {
				t.Errorf("case %d output %d: Go %v, PyTorch %v", i, j, got[j], want[j])
			}
		}
	}
}
