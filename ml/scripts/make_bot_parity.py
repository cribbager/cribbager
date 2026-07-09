"""Install trained discard weights into the Go lab and emit its parity fixture.

Copies the exported weights into internal/bot/lab/testdata/ and writes
discard_parity.json: sample splits with (a) the exact indices that must be hot
in the 105-dim encoding and (b) the value a NUMPY forward pass over the
exported weights predicts. The Go test re-encodes each split with the bot's
own encoder and runs internal/nn inference; agreement proves the whole
Go-side chain (encoding + weights file + forward) computes what training
computed. Numpy here is a third, independent forward implementation — it
checks the exported file itself, not torch state.

Run from ml/:  uv run scripts/make_bot_parity.py [--run runs/discard-v1]
"""

import argparse
import json
import shutil
from pathlib import Path

import numpy as np

from cribml.data import card_index, DEALER_FLAG

HANDS = 12  # × 2 seats × 15 splits = 360 parity cases


def forward(layers, v):
    for i, l in enumerate(layers):
        v = np.array(l["w"]) @ v + np.array(l["b"])
        if i < len(layers) - 1:
            v = np.maximum(v, 0.0)
    return v


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--run", default="runs/discard-v1")
    ap.add_argument("--data", default="data/discard-120k.jsonl")
    args = ap.parse_args()

    ml = Path(__file__).resolve().parents[1]
    testdata = ml.parent / "internal" / "bot" / "lab" / "testdata"
    testdata.mkdir(parents=True, exist_ok=True)

    weights = json.loads((ml / args.run / "weights.json").read_text())
    shutil.copy(ml / args.run / "weights.json", testdata / "discard-v1.json")

    cases = []
    with open(ml / args.data) as f:
        for _ in range(HANDS):
            row = json.loads(f.readline())
            for s in row["splits"]:
                for dealer in (True, False):
                    ones = sorted(
                        [card_index(c) for c in s["keep"]]
                        + [52 + card_index(c) for c in s["discard"]]
                        + ([DEALER_FLAG] if dealer else [])
                    )
                    v = np.zeros(105)
                    v[ones] = 1.0
                    cases.append({
                        "keep": s["keep"],
                        "discard": s["discard"],
                        "dealer": dealer,
                        "ones": ones,
                        "value": float(forward(weights["layers"], v)[0]),
                    })

    (testdata / "discard_parity.json").write_text(json.dumps({"cases": cases}))
    print(f"installed weights and wrote {len(cases)} parity cases to {testdata}")


if __name__ == "__main__":
    main()
