"""Train the pegging Q-network (docs/research/ml-bot chapter 4).

Input rows come pre-encoded from cmd/pegdata (the encoder lives in Go, where
inference lives — no cross-language encoding parity needed). Each row is one
pegging decision: state x (128 dims), action a (rank index), Monte-Carlo
return g (own future pegging points minus opponent's, to the end of the
deal's play).

The net maps state -> 13 values, one per rank: Q(s, a) ~= expected return of
playing rank a here, under the behavior policy that generated the data. The
loss touches ONLY the taken action's output — the other 12 predictions get no
gradient from that row; they learn from rows where they were taken (this is
why the behavior policy must explore). Acting greedily w.r.t. a Q fitted this
way is one step of policy improvement over the behavior policy.

Train/val split is BY GAME: rows within a game share deals and are correlated.

Usage (from ml/):
  uv run scripts/train_pegging.py --data data/peg-iter0.jsonl --out runs/peg-v1 \
      --install ../internal/bot/lab/testdata/pegging-v1.json
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

DIMS, ACTIONS = 128, 13
VAL_EVERY = 10  # game % VAL_EVERY == 0 -> validation


def load(path):
    xs, acts, rets, val = [], [], [], []
    with open(path) as f:
        for line in f:
            r = json.loads(line)
            xs.append(r["x"])
            acts.append(r["a"])
            rets.append(r["g"])
            val.append(r["game"] % VAL_EVERY == 0)
    return (
        torch.tensor(np.array(xs, dtype=np.float32)),
        torch.tensor(np.array(acts, dtype=np.int64)),
        torch.tensor(np.array(rets, dtype=np.float32)),
        torch.tensor(np.array(val)),
    )


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--data", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--install", default="", help="also copy weights to this path (the Go lab testdata)")
    ap.add_argument("--hidden", default="128,128")
    ap.add_argument("--epochs", type=int, default=12)
    ap.add_argument("--batch", type=int, default=4096)
    ap.add_argument("--lr", type=float, default=1e-3)
    ap.add_argument("--seed", type=int, default=0)
    args = ap.parse_args()

    torch.manual_seed(args.seed)
    dev = torch.device("mps" if torch.backends.mps.is_available() else "cpu")

    x, a, g, val = load(args.data)
    tr = ~val
    print(f"device={dev} rows={len(x)} (train {int(tr.sum())}, val {int(val.sum())}) "
          f"return: mean {g.mean():.3f} sd {g.std():.3f}", flush=True)

    model = build_mlp([int(h) for h in args.hidden.split(",")], in_dim=DIMS, out_dim=ACTIONS).to(dev)
    print(f"model {DIMS}->{args.hidden}->{ACTIONS}, {param_count(model)} params", flush=True)
    opt = torch.optim.Adam(model.parameters(), lr=args.lr)

    xt, at, gt = x[tr].to(dev), a[tr].to(dev), g[tr].to(dev)
    xv, av, gv = x[val].to(dev), a[val].to(dev), g[val].to(dev)

    def q_taken(model, xb, ab):
        return model(xb).gather(1, ab.unsqueeze(1)).squeeze(1)

    for epoch in range(1, args.epochs + 1):
        model.train()
        order = torch.randperm(len(xt), device=dev)
        t0, tot, nb = time.time(), 0.0, 0
        for lo in range(0, len(order), args.batch):
            idx = order[lo:lo + args.batch]
            loss = torch.nn.functional.mse_loss(q_taken(model, xt[idx], at[idx]), gt[idx])
            opt.zero_grad()
            loss.backward()
            opt.step()
            tot, nb = tot + loss.item(), nb + 1
        model.eval()
        with torch.no_grad():
            vmse = torch.nn.functional.mse_loss(q_taken(model, xv, av), gv).item()
        print(f"epoch {epoch:2d}  train_mse {tot / nb:.4f}  val_mse {vmse:.4f}  [{time.time() - t0:.0f}s]", flush=True)

    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    export_mlp(model.cpu(), out / "weights.json")
    (out / "metrics.json").write_text(json.dumps({"args": vars(args), "val_mse": vmse}, indent=2))
    print(f"wrote {out}/weights.json", flush=True)
    if args.install:
        shutil.copy(out / "weights.json", args.install)
        print(f"installed to {args.install}", flush=True)


if __name__ == "__main__":
    main()
