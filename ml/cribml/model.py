"""Model builder for the discard value net.

Kept to the exact shape cribml.export / internal/nn support: alternating
Linear/ReLU ending in a Linear. One scalar output — the predicted split value
in points for the seat described by the input's dealer flag.
"""

import torch.nn as nn

from .data import INPUT_DIM


def build_mlp(hidden: list[int], in_dim: int = INPUT_DIM, out_dim: int = 1) -> nn.Sequential:
    dims = [in_dim, *hidden]
    layers: list[nn.Module] = []
    for a, b in zip(dims, dims[1:]):
        layers += [nn.Linear(a, b), nn.ReLU()]
    layers.append(nn.Linear(dims[-1], out_dim))
    return nn.Sequential(*layers)


def param_count(model: nn.Module) -> int:
    return sum(p.numel() for p in model.parameters())
