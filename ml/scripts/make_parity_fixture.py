"""Generate the PyTorch<->Go parity fixture for internal/nn.

Builds a small seeded MLP with random (untrained) weights, exports it with the
real exporter, runs a batch of random inputs through PyTorch, and writes both
the weights and the input/output pairs to internal/nn/testdata/. The Go parity
test then loads the same weights, runs the same inputs, and asserts the outputs
agree — proving the hand-written Go forward pass computes the same function as
PyTorch before any real model depends on it.

Run from ml/:  uv run scripts/make_parity_fixture.py
"""

import json
from pathlib import Path

import torch
import torch.nn as nn

from cribml.export import export_mlp

# Deliberately awkward sizes (not powers of two, not square) so a transposed
# weight matrix or an off-by-one in the Go loops cannot accidentally still work.
IN, H1, H2, OUT = 21, 17, 9, 3
BATCH = 16

torch.manual_seed(20260708)
model = nn.Sequential(
    nn.Linear(IN, H1), nn.ReLU(),
    nn.Linear(H1, H2), nn.ReLU(),
    nn.Linear(H2, OUT),
)

testdata = Path(__file__).resolve().parents[2] / "internal" / "nn" / "testdata"
testdata.mkdir(parents=True, exist_ok=True)

export_mlp(model, testdata / "parity_mlp.json")

x = torch.randn(BATCH, IN)
with torch.no_grad():
    y = model(x)

(testdata / "parity_io.json").write_text(json.dumps({
    "inputs": x.double().tolist(),
    "outputs": y.double().tolist(),
}))

print(f"wrote parity_mlp.json and parity_io.json ({BATCH} cases) to {testdata}")
