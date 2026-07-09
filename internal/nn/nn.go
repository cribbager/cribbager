// Package nn runs inference for small multi-layer perceptrons trained
// elsewhere (PyTorch — see ml/). It is deliberately tiny: a weights-file
// loader and a forward pass, no training, no dependencies. The networks it
// serves are a few thousand parameters, and the point of hand-writing this
// is the lesson itself — a trained neural net is nothing but multiply-adds
// and a clamp; all the magic happened at training time.
//
// The weights file is JSON: an ordered list of dense layers, each a weight
// matrix in PyTorch layout ([out][in], so y = Wx + b) and a bias vector.
// ReLU is applied after every layer except the last, matching the exporter
// in ml/cribml/export.py. The two sides are held equal by the parity test
// (parity_test.go), whose fixtures PyTorch generates.
package nn

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// layer is one dense layer: W is [out][in], B has length out.
type layer struct {
	W [][]float64 `json:"w"`
	B []float64   `json:"b"`
}

// MLP is a loaded multi-layer perceptron, ready for Forward. It is immutable
// after Load and safe for concurrent use.
type MLP struct {
	layers []layer
}

// mlpFile is the on-disk weights format. Arch and Activation are recorded so
// a future format change fails loudly at load time instead of silently
// computing garbage.
type mlpFile struct {
	Arch       string  `json:"arch"`
	Activation string  `json:"activation"`
	Layers     []layer `json:"layers"`
}

// Load reads a weights file and validates its shape: every weight row the
// same width, bias length matching the row count, and each layer's input
// width matching the previous layer's output. Validating once here means
// Forward can be a bare loop with no checks in the hot path.
func Load(r io.Reader) (*MLP, error) {
	var f mlpFile
	if err := json.NewDecoder(r).Decode(&f); err != nil {
		return nil, fmt.Errorf("nn: decoding weights: %w", err)
	}
	if f.Arch != "mlp" {
		return nil, fmt.Errorf("nn: unsupported arch %q (want \"mlp\")", f.Arch)
	}
	if f.Activation != "relu" {
		return nil, fmt.Errorf("nn: unsupported activation %q (want \"relu\")", f.Activation)
	}
	if len(f.Layers) == 0 {
		return nil, fmt.Errorf("nn: weights file has no layers")
	}
	for i, l := range f.Layers {
		if len(l.W) == 0 || len(l.W[0]) == 0 {
			return nil, fmt.Errorf("nn: layer %d has an empty weight matrix", i)
		}
		in := len(l.W[0])
		for j, row := range l.W {
			if len(row) != in {
				return nil, fmt.Errorf("nn: layer %d row %d has %d weights, want %d", i, j, len(row), in)
			}
		}
		if len(l.B) != len(l.W) {
			return nil, fmt.Errorf("nn: layer %d has %d biases for %d outputs", i, len(l.B), len(l.W))
		}
		if i > 0 && in != len(f.Layers[i-1].B) {
			return nil, fmt.Errorf("nn: layer %d expects %d inputs but layer %d produces %d", i, in, i-1, len(f.Layers[i-1].B))
		}
	}
	return &MLP{layers: f.Layers}, nil
}

// LoadFile is Load on a file path. Weights files are small (hundreds of KB),
// so it reads whole-file — no open handle to manage.
func LoadFile(path string) (*MLP, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("nn: %w", err)
	}
	return Load(bytes.NewReader(raw))
}

// InputSize is the length Forward's input must have.
func (m *MLP) InputSize() int { return len(m.layers[0].W[0]) }

// OutputSize is the length of Forward's result.
func (m *MLP) OutputSize() int { return len(m.layers[len(m.layers)-1].B) }

// Forward runs the network: each layer computes y = Wx + b, and every layer
// but the last clamps negatives to zero (ReLU). The input length must equal
// InputSize; a mismatch is a caller bug and panics, like an out-of-range
// slice index. The input slice is not modified.
func (m *MLP) Forward(in []float64) []float64 {
	if len(in) != m.InputSize() {
		panic(fmt.Sprintf("nn: Forward got %d inputs, model wants %d", len(in), m.InputSize()))
	}
	x := in
	for i, l := range m.layers {
		out := make([]float64, len(l.W))
		for j, row := range l.W {
			s := l.B[j]
			for k, w := range row {
				s += w * x[k]
			}
			out[j] = s
		}
		if i < len(m.layers)-1 {
			for j := range out {
				if out[j] < 0 {
					out[j] = 0
				}
			}
		}
		x = out
	}
	return x
}
