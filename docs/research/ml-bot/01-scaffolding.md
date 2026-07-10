# Chapter 1 — Scaffolding: the pipeline and the hand-written forward pass

*2026-07-08. Code: `internal/nn`, `cmd/mldata`, `ml/`.*

Before any learning happens, we need three pieces of plumbing: a way to make
training data, a place to train, and a way for a trained network to actually
play inside the Go server. This chapter builds all three — and the third one
is where the first real ML lesson lives.

## What a neural network actually is

Strip away the mystique and a multi-layer perceptron (MLP) — the simplest and
still most useful network shape — is a pipeline of two alternating steps:

1. **A linear layer**: multiply the input vector by a weight matrix, add a
   bias vector. `y = Wx + b`. That's it — a weighted sum, the same math as a
   spreadsheet of hand-tuned feature weights.
2. **A nonlinearity**: apply some non-linear squashing to each element. We use
   ReLU, the fashionable and almost embarrassingly simple choice:
   `relu(x) = max(0, x)`. Negative values become zero, positives pass through.

Stack those a few times — linear, clamp, linear, clamp, linear — and you have
an MLP. The nonlinearity is not decoration; it is the entire reason networks
are interesting. A stack of purely linear layers collapses algebraically into
one linear layer (`W₃(W₂(W₁x)) = Wx`), which can only learn straight-line
relationships. The clamps between layers break that collapse and let the
network represent bent, bumpy, arbitrarily shaped functions — like "the value
of keeping 5-5-J-Q spikes when the starter might be a ten-card".

Where do `W` and `b` come from? Training (next chapter) — an optimizer nudges
them millions of times until the outputs are useful. But *running* a trained
network involves none of that. Inference is just the multiply-adds above.

## Decision: train in PyTorch, infer in hand-written Go

Training needs autograd, optimizers, GPU support — an ecosystem. That's
PyTorch (`ml/`, uv-managed, Python 3.13). But shipping a Python runtime inside
the Go server, or a 200MB ONNX dependency, to run a few thousand multiply-adds
would be absurd. So the trained weights are exported to a plain JSON file and
`internal/nn` — about eighty lines of real code — runs inference natively.

`nn.go`'s whole hot path:

```go
for j, row := range l.W {
    s := l.B[j]
    for k, w := range row {
        s += w * x[k]
    }
    out[j] = s
}
// ...then, between layers only: if out[j] < 0 { out[j] = 0 }
```

If you've ever suspected there's no magic inside a neural net: there is the
entire net, playing cards.

## The parity test — never trust two implementations to agree

The subtle risk in a two-language split is *silent* disagreement: transpose the
weight matrix, apply ReLU after the last layer, mix up row/column order, and
you don't get an error — you get a bot that plays confidently and badly, and
you'll waste a week blaming the training run.

So the contract is enforced by a fixture test:

- `ml/scripts/make_parity_fixture.py` builds a small *random* (untrained) MLP
  with deliberately awkward sizes (21→17→9→3 — nothing square, nothing a
  transposed matrix could survive), exports it with the real exporter, runs 16
  random inputs through PyTorch, and writes weights + inputs + outputs to
  `internal/nn/testdata/`.
- `TestParityWithPyTorch` loads those, runs the same inputs through the Go
  forward pass, and demands agreement within 1e-4 (PyTorch computes in
  float32, Go in float64; every float32 weight is exactly representable in
  float64, so the only drift is arithmetic rounding — orders of magnitude
  below the tolerance).

**Result: parity holds.** Every future model rides on this guarantee.

## The data generator — a perfect teacher

`cmd/mldata` deals random six-card hands and, for each, labels *all 15*
keep/discard splits using `eval.RankDiscards` — the champion's exact
evaluator. One JSON line per hand:

```json
{"hand":["KS","6S","9H","8C","3D","2S"],
 "splits":[{"keep":["KS","6S","9H","8C"],"discard":["3D","2S"],
            "ehand":3.913,"crib_ev":6.833}, ...]}
```

Two things worth noticing:

- **The labels are exact, not sampled.** `ehand` is the true expectation over
  all 46 starters; `crib_ev` is a precomputed exact table. Real ML problems
  almost never get noise-free labels; we start here precisely because when
  training misbehaves, the data won't be the suspect.
- **One line serves both seats.** A dealer's target is `ehand + crib_ev`, the
  pone's is `ehand − crib_ev`. Emitting the parts, not the sums, halves the
  data and hands the trainer the dealer/pone symmetry explicitly.

`-seed` makes datasets reproducible: `go run ./cmd/mldata -n 500000 -seed 1`
regenerates byte-identical data, so experiments can be rerun exactly.
Generated data lives in `ml/data/` (git-ignored — we commit the generator,
not the artifact).

## What's deliberately not here yet

No card *encoding* — the JSONL stores cards as strings, and turning them into
input vectors is a training-side decision (and the next chapter's opening
lesson, because it matters far more than people expect). No training loop, no
model architecture choices. Scaffolding's job was to make those the *only*
open questions.

## State at chapter end

- `internal/nn`: MLP loader + forward pass, unit tests, PyTorch parity test — all green.
- `ml/`: uv project (Python 3.13, torch, numpy), `cribml.export`, parity fixture script.
- `cmd/mldata`: reproducible exact-labeled discard datasets, one JSONL row per hand.
