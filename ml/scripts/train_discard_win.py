"""Train the position-aware win-delta discard network (chapter 8).

Rows come pre-encoded from cmd/mldata -mode win (encoder lives in Go): x is
128 dims (split + scores + the split's exact expected points), the target is
the deal's realized win-probability delta from the decider's seat. The net
learns how position bends the value of a discard — if the win surface really
is affine in points far from the end, the best it can do is reproduce the
Score feature's ordering, and the gate will read zero.

Usage (from ml/):
  uv run scripts/train_discard_win.py --data data/discard-win-80k.jsonl \
      --out runs/discard-win-v1 --install ../internal/bot/lab/testdata/discard-win-v1.json
"""

import argparse
import json
import shutil
import time
from pathlib import Path

import numpy as np
import torch

from cribml.export import export_mlp
from cribml.model import build_mlp, param_count

DIMS = 128
VAL_EVERY = 10  # game % VAL_EVERY == 0 -> validation


def load(path):
    xs, wps, val = [], [], []
    with open(path) as f:
        for line in f:
            r = json.loads(line)
            xs.append(r["x"])
            wps.append(r["wp"])
            val.append(r["game"] % VAL_EVERY == 0)
    return (
        torch.tensor(np.array(xs, dtype=np.float32)),
        torch.tensor(np.array(wps, dtype=np.float32)),
        torch.tensor(np.array(val)),
    )


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--data", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--install", default="")
    ap.add_argument("--hidden", default="128,128")
    ap.add_argument("--epochs", type=int, default=12)
    ap.add_argument("--batch", type=int, default=4096)
    ap.add_argument("--lr", type=float, default=1e-3)
    ap.add_argument("--seed", type=int, default=0)
    args = ap.parse_args()

    torch.manual_seed(args.seed)
    dev = torch.device("mps" if torch.backends.mps.is_available() else "cpu")

    x, wp, val = load(args.data)
    tr = ~val
    print(f"device={dev} rows={len(x)} (train {int(tr.sum())}, val {int(val.sum())}) "
          f"wp-delta: mean {wp.mean():.4f} sd {wp.std():.4f}", flush=True)

    model = build_mlp([int(h) for h in args.hidden.split(",")], in_dim=DIMS).to(dev)
    print(f"model {DIMS}->{args.hidden}->1, {param_count(model)} params", flush=True)
    opt = torch.optim.Adam(model.parameters(), lr=args.lr)

    xt, gt = x[tr].to(dev), wp[tr].to(dev)
    xv, gv = x[val].to(dev), wp[val].to(dev)

    for epoch in range(1, args.epochs + 1):
        model.train()
        order = torch.randperm(len(xt), device=dev)
        t0, tot, nb = time.time(), 0.0, 0
        for lo in range(0, len(order), args.batch):
            i = order[lo:lo + args.batch]
            loss = torch.nn.functional.mse_loss(model(xt[i]).squeeze(1), gt[i])
            opt.zero_grad()
            loss.backward()
            opt.step()
            tot, nb = tot + loss.item(), nb + 1
        model.eval()
        with torch.no_grad():
            vloss, seen = 0.0, 0
            for lo in range(0, len(gv), 65536):
                p = model(xv[lo:lo + 65536]).squeeze(1)
                vloss += torch.nn.functional.mse_loss(p, gv[lo:lo + 65536], reduction="sum").item()
                seen += len(p)
        print(f"epoch {epoch:2d}  train_mse {tot / nb:.5f}  val_mse {vloss / seen:.5f}  "
              f"[{time.time() - t0:.0f}s]", flush=True)

    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    export_mlp(model.cpu(), out / "weights.json")
    (out / "metrics.json").write_text(json.dumps({"args": vars(args), "val_mse": vloss / seen}, indent=2))
    print(f"wrote {out}/weights.json", flush=True)
    if args.install:
        shutil.copy(out / "weights.json", args.install)
        print(f"installed to {args.install}", flush=True)


if __name__ == "__main__":
    main()
