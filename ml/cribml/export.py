"""Export a trained torch MLP to the JSON weights format internal/nn loads.

The contract with the Go side (see internal/nn/nn.go):

- The model is a plain feed-forward stack: Linear, ReLU, Linear, ReLU, ...,
  Linear. ReLU after every layer except the last, nothing else (no dropout at
  inference, no batchnorm, no other activations).
- Weight matrices are written in PyTorch's native [out][in] layout (y = Wx + b),
  so the Go forward pass indexes them the same way nn.Linear does.
- Values are written as JSON numbers from float64 tensors. Every float32 weight
  is exactly representable in float64, so export loses no precision; the only
  divergence left between the two sides is float32-vs-float64 arithmetic order,
  which the parity test bounds.
"""

import json
from pathlib import Path

import torch.nn as nn


def export_mlp(model: nn.Sequential, path: str | Path) -> None:
    """Write model's weights to path in the internal/nn JSON format.

    model must be an alternating Linear/ReLU stack ending in a Linear;
    anything else raises ValueError rather than exporting weights that Go
    would silently run with the wrong activation structure.
    """
    modules = list(model)
    if not modules:
        raise ValueError("export_mlp: empty model")
    layers = []
    for i, m in enumerate(modules):
        want_linear = i % 2 == 0
        if want_linear:
            if not isinstance(m, nn.Linear):
                raise ValueError(f"export_mlp: module {i} is {type(m).__name__}, want Linear")
            layers.append({
                "w": m.weight.detach().cpu().double().tolist(),
                "b": m.bias.detach().cpu().double().tolist(),
            })
        elif not isinstance(m, nn.ReLU):
            raise ValueError(f"export_mlp: module {i} is {type(m).__name__}, want ReLU")
    if not isinstance(modules[-1], nn.Linear):
        raise ValueError("export_mlp: model must end with a Linear layer")

    out = {"arch": "mlp", "activation": "relu", "layers": layers}
    Path(path).write_text(json.dumps(out))
