"""Train the outcome-value discard network (docs/research/ml-bot chapter 7).

Chapter 2's net imitated an exact-but-partial label (hand + crib expectation:
pegging-blind by construction). This one learns from a complete-but-noisy
label: the realized net points of the whole deal that followed the split —
pegging included, under the production bot's pegging — from cmd/mldata
-mode outcomes. Volume pays for the noise; the argmax only needs the
RANKING of a hand's 15 splits to come out right.

Same 105-dim encoding as chapter 2 (cribml.data), already parity-tested
against the Go side.

Usage (from ml/):
  uv run scripts/train_discard_mc.py --data data/discard-mc-60k.jsonl \
      --out runs/discard-mc-v1 --install ../internal/bot/lab/testdata/discard-mc-v1.json
"""

import argparse
import json
import shutil
import time
from pathlib import Path

import numpy as np
import torch

from cribml.data import card_index, encode
from cribml.export import export_mlp
from cribml.model import build_mlp, param_count

VAL_EVERY = 10  # game % VAL_EVERY == 0 -> validation


def load(path, target):
    keep, disc, dealer, g, val = [], [], [], [], []
    with open(path) as f:
        for line in f:
            r = json.loads(line)
            keep.append([card_index(c) for c in r["keep"]])
            disc.append([card_index(c) for c in r["discard"]])
            dealer.append(r["dealer"])
            g.append(r[target])
            val.append(r["game"] % VAL_EVERY == 0)
    return (
        torch.tensor(np.array(keep, dtype=np.int64)),
        torch.tensor(np.array(disc, dtype=np.int64)),
        torch.tensor(np.array(dealer, dtype=np.float32)),
        torch.tensor(np.array(g, dtype=np.float32)),
        torch.tensor(np.array(val)),
    )


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--data", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--install", default="")
    ap.add_argument("--target", default="g", choices=["g", "peg_diff"],
                    help="g: whole-deal net points; peg_diff: pegging differential only (residual learning)")
    ap.add_argument("--hidden", default="128,128")
    ap.add_argument("--epochs", type=int, default=12)
    ap.add_argument("--batch", type=int, default=4096)
    ap.add_argument("--lr", type=float, default=1e-3)
    ap.add_argument("--seed", type=int, default=0)
    args = ap.parse_args()

    torch.manual_seed(args.seed)
    dev = torch.device("mps" if torch.backends.mps.is_available() else "cpu")

    keep, disc, dealer, g, val = load(args.data, args.target)
    tr = ~val
    print(f"device={dev} rows={len(g)} (train {int(tr.sum())}, val {int(val.sum())}) "
          f"return: mean {g.mean():.3f} sd {g.std():.3f}", flush=True)

    model = build_mlp([int(h) for h in args.hidden.split(",")]).to(dev)
    print(f"model 105->{args.hidden}->1, {param_count(model)} params", flush=True)
    opt = torch.optim.Adam(model.parameters(), lr=args.lr)

    kt, dt, ft, gt = keep[tr].to(dev), disc[tr].to(dev), dealer[tr].to(dev), g[tr].to(dev)
    kv, dv, fv, gv = keep[val].to(dev), disc[val].to(dev), dealer[val].to(dev), g[val].to(dev)

    for epoch in range(1, args.epochs + 1):
        model.train()
        order = torch.randperm(len(gt), device=dev)
        t0, tot, nb = time.time(), 0.0, 0
        for lo in range(0, len(order), args.batch):
            i = order[lo:lo + args.batch]
            x = encode(kt[i], dt[i], ft[i])
            loss = torch.nn.functional.mse_loss(model(x).squeeze(1), gt[i])
            opt.zero_grad()
            loss.backward()
            opt.step()
            tot, nb = tot + loss.item(), nb + 1
        model.eval()
        with torch.no_grad():
            vloss, seen = 0.0, 0
            for lo in range(0, len(gv), 65536):
                x = encode(kv[lo:lo + 65536], dv[lo:lo + 65536], fv[lo:lo + 65536])
                p = model(x).squeeze(1)
                vloss += torch.nn.functional.mse_loss(p, gv[lo:lo + 65536], reduction="sum").item()
                seen += len(p)
        print(f"epoch {epoch:2d}  train_mse {tot / nb:.4f}  val_mse {vloss / seen:.4f}  "
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
