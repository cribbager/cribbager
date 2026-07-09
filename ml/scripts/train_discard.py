"""Train the discard value network (docs/research/ml-bot chapter 2).

Learns (keep, discard, dealer?) -> exact split value in points, supervised on
cmd/mldata output. Every JSONL row yields two examples (dealer and pone view
of the same split), so the net must learn to read the flag.

Validation is split BY HAND, never by row: the 15 splits of one hand share six
cards and are strongly correlated, so putting some of them in train and some
in val would leak and flatter every metric. The decision-level metrics are the
ones that matter:

  agreement  — how often the net's argmax split is exactly the optimal one
  regret     — exact points given up when it isn't (optimal split's true value
               minus the true value of the net's choice); the champion scores
               0 by definition

Usage (from ml/):
  uv run scripts/train_discard.py --data data/discard-120k.jsonl --out runs/discard-v1
"""

import argparse
import json
import time
from pathlib import Path

import torch

from cribml import data as D
from cribml.export import export_mlp
from cribml.model import build_mlp, param_count


def evaluate(model, dev, ds, rows_from, rows_to, batch=8192):
    """Decision metrics over whole hands in [rows_from, rows_to)."""
    model.eval()
    preds = torch.empty(rows_to - rows_from, 2)
    with torch.no_grad():
        for lo in range(rows_from, rows_to, batch):
            hi = min(lo + batch, rows_to)
            k = ds.keep_idx[lo:hi].to(dev)
            d = ds.disc_idx[lo:hi].to(dev)
            for seat, flag in ((0, 1.0), (1, 0.0)):  # seat 0 = dealer column
                flags = torch.full((hi - lo,), flag, device=dev)
                preds[lo - rows_from:hi - rows_from, seat] = model(D.encode(k, d, flags)).squeeze(1).cpu()
    exact = ds.target[rows_from:rows_to]

    mae = (preds - exact).abs().mean().item()
    p = preds.view(-1, D.SPLITS_PER_HAND, 2)   # [H, 15, seat]
    e = exact.view(-1, D.SPLITS_PER_HAND, 2)
    chosen = p.argmax(dim=1)                   # [H, seat]
    best_val = e.max(dim=1).values             # [H, seat]
    chosen_val = e.gather(1, chosen.unsqueeze(1)).squeeze(1)
    regret = best_val - chosen_val             # [H, seat]
    agree = (regret <= 1e-9).float()
    return {
        "mae": mae,
        "regret_dealer": regret[:, 0].mean().item(),
        "regret_pone": regret[:, 1].mean().item(),
        "agree_dealer": agree[:, 0].mean().item(),
        "agree_pone": agree[:, 1].mean().item(),
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--data", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--hidden", default="128,128")
    ap.add_argument("--epochs", type=int, default=15)
    ap.add_argument("--batch", type=int, default=4096)
    ap.add_argument("--lr", type=float, default=1e-3)
    ap.add_argument("--val-hands", type=int, default=8000)
    ap.add_argument("--max-hands", type=int, default=None)
    ap.add_argument("--seed", type=int, default=0)
    args = ap.parse_args()

    torch.manual_seed(args.seed)
    dev = torch.device("mps" if torch.backends.mps.is_available() else "cpu")

    ds = D.load(args.data, max_hands=args.max_hands)
    val_rows = args.val_hands * D.SPLITS_PER_HAND
    train_rows = len(ds.keep_idx) - val_rows
    assert train_rows > 0, "not enough hands for that validation size"
    print(f"device={dev} hands={ds.hands} (train {train_rows // 15}, val {args.val_hands})", flush=True)

    model = build_mlp([int(h) for h in args.hidden.split(",")]).to(dev)
    print(f"model 105->{args.hidden}->1, {param_count(model)} params", flush=True)
    opt = torch.optim.Adam(model.parameters(), lr=args.lr)

    keep = ds.keep_idx[:train_rows].to(dev)
    disc = ds.disc_idx[:train_rows].to(dev)
    tgt = ds.target[:train_rows].to(dev)

    for epoch in range(1, args.epochs + 1):
        model.train()
        # 2× train_rows examples per epoch: every split seen as dealer and pone.
        order = torch.randperm(2 * train_rows, device=dev)
        t0, total, nb = time.time(), 0.0, 0
        for lo in range(0, len(order), args.batch):
            ex = order[lo:lo + args.batch]
            row, seat = ex % train_rows, ex // train_rows  # seat 0 dealer, 1 pone
            x = D.encode(keep[row], disc[row], (seat == 0))
            y = tgt[row, seat]
            loss = torch.nn.functional.mse_loss(model(x).squeeze(1), y)
            opt.zero_grad()
            loss.backward()
            opt.step()
            total, nb = total + loss.item(), nb + 1
        m = evaluate(model, dev, ds, train_rows, len(ds.keep_idx))
        print(
            f"epoch {epoch:2d}  train_mse {total / nb:.4f}  val_mae {m['mae']:.3f}  "
            f"agree {m['agree_dealer']:.1%}/{m['agree_pone']:.1%}  "
            f"regret {m['regret_dealer']:.4f}/{m['regret_pone']:.4f} pts (dealer/pone)  "
            f"[{time.time() - t0:.0f}s]",
            flush=True,
        )

    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    export_mlp(model.cpu(), out / "weights.json")
    final = evaluate(model.to(dev), dev, ds, train_rows, len(ds.keep_idx))
    (out / "metrics.json").write_text(json.dumps({"args": vars(args), "final": final}, indent=2))
    print(f"wrote {out}/weights.json and metrics.json", flush=True)


if __name__ == "__main__":
    main()
