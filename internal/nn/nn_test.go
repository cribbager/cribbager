package nn

import (
	"math"
	"strings"
	"testing"
)

// tiny is a 2→2→1 net small enough to run by hand. With ReLU between the
// layers, input (1, 2) goes:
//
//	hidden pre-act = (1·1 + 2·2 + 0.5, 1·(-1) + 2·1 + 0)   = (5.5, 1)
//	hidden post-ReLU = (5.5, 1)
//	output = 5.5·2 + 1·(-3) + 1 = 9
//
// and input (0, -1) goes:
//
//	hidden pre-act = (-2 + 0.5, -1) = (-1.5, -1)
//	hidden post-ReLU = (0, 0)        ← both clamped
//	output = 0 + 0 + 1 = 1
const tiny = `{
	"arch": "mlp",
	"activation": "relu",
	"layers": [
		{"w": [[1, 2], [-1, 1]], "b": [0.5, 0]},
		{"w": [[2, -3]], "b": [1]}
	]
}`

func loadTiny(t *testing.T) *MLP {
	t.Helper()
	m, err := Load(strings.NewReader(tiny))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return m
}

func TestForwardByHand(t *testing.T) {
	m := loadTiny(t)
	for _, tc := range []struct {
		in   []float64
		want float64
	}{
		{[]float64{1, 2}, 9},  // both hidden units active
		{[]float64{0, -1}, 1}, // both hidden units clamped by ReLU
	} {
		got := m.Forward(tc.in)
		if len(got) != 1 || math.Abs(got[0]-tc.want) > 1e-12 {
			t.Errorf("Forward(%v) = %v, want [%v]", tc.in, got, tc.want)
		}
	}
}

func TestSizes(t *testing.T) {
	m := loadTiny(t)
	if m.InputSize() != 2 || m.OutputSize() != 1 {
		t.Errorf("sizes = (%d, %d), want (2, 1)", m.InputSize(), m.OutputSize())
	}
}

func TestForwardDoesNotModifyInput(t *testing.T) {
	m := loadTiny(t)
	in := []float64{1, 2}
	m.Forward(in)
	if in[0] != 1 || in[1] != 2 {
		t.Errorf("Forward modified its input: %v", in)
	}
}

func TestForwardPanicsOnBadInputLength(t *testing.T) {
	m := loadTiny(t)
	defer func() {
		if recover() == nil {
			t.Error("Forward with wrong input length did not panic")
		}
	}()
	m.Forward([]float64{1, 2, 3})
}

func TestLoadRejectsMalformedFiles(t *testing.T) {
	for name, src := range map[string]string{
		"not json":         `{`,
		"wrong arch":       `{"arch": "cnn", "activation": "relu", "layers": [{"w": [[1]], "b": [0]}]}`,
		"wrong activation": `{"arch": "mlp", "activation": "tanh", "layers": [{"w": [[1]], "b": [0]}]}`,
		"no layers":        `{"arch": "mlp", "activation": "relu", "layers": []}`,
		"empty matrix":     `{"arch": "mlp", "activation": "relu", "layers": [{"w": [], "b": []}]}`,
		"ragged rows":      `{"arch": "mlp", "activation": "relu", "layers": [{"w": [[1, 2], [3]], "b": [0, 0]}]}`,
		"bias mismatch":    `{"arch": "mlp", "activation": "relu", "layers": [{"w": [[1, 2]], "b": [0, 0]}]}`,
		"chain mismatch": `{"arch": "mlp", "activation": "relu", "layers": [
			{"w": [[1, 2]], "b": [0]},
			{"w": [[1, 2]], "b": [0]}
		]}`,
	} {
		if _, err := Load(strings.NewReader(src)); err == nil {
			t.Errorf("Load(%s) succeeded, want error", name)
		}
	}
}
