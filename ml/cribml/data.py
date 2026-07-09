"""Dataset loading and the canonical input encoding for the discard net.

THE ENCODING (the Go bot must mirror this exactly — see docs/research/ml-bot/02):

Input is a 105-dim float vector for one candidate split of one hand, one seat:

    [0:52)    multi-hot of the 4 KEPT cards
    [52:104)  multi-hot of the 2 DISCARDED cards
    [104]     1.0 if this seat is the dealer (owns the crib), else 0.0

Card index = 4*(rank-1) + suit, with rank A=1..K=13 and suit C=0 D=1 H=2 S=3,
matching internal/cribbage's Rank/Suit values. Text form is the engine's
two-letter notation ("5H", "TD").

Why multi-hot and not "six numbers"? Two reasons, both fatal to the naive
encoding. (1) A hand is a SET: feeding cards in slots makes the net's answer
depend on slot order, and it must waste capacity learning permutation
invariance we get for free from a bag-of-cards. (2) Rank is not a magnitude:
rank 13 (King) is not "13× an Ace" — for pips K and T are identical, for runs
they are neighbors of different cards. One-hot lets the net learn each rank's
meaning instead of fighting a fake linear structure.

The target is the exact split value for that seat, in points:
dealer: ehand + crib_ev, pone: ehand − crib_ev.
"""

import json
from dataclasses import dataclass
from pathlib import Path

import numpy as np
import torch

INPUT_DIM = 105
DEALER_FLAG = 104
SPLITS_PER_HAND = 15

_RANKS = "A23456789TJQK"
_SUITS = "CDHS"


def card_index(text: str) -> int:
    """Map the engine's two-letter card notation to its 0..51 index."""
    return 4 * _RANKS.index(text[0]) + _SUITS.index(text[1])


@dataclass
class DiscardData:
    """Flat split-level examples plus per-hand grouping for decision metrics.

    keep_idx  [N,4] int64   card indices of the kept four
    disc_idx  [N,2] int64   card indices of the thrown two
    target    [N,2] float32 exact split value as (dealer, pone)
    Rows come in hand-order, SPLITS_PER_HAND consecutive rows per hand.
    """

    keep_idx: torch.Tensor
    disc_idx: torch.Tensor
    target: torch.Tensor

    @property
    def hands(self) -> int:
        return len(self.keep_idx) // SPLITS_PER_HAND


def load(path: str | Path, max_hands: int | None = None) -> DiscardData:
    """Parse cmd/mldata JSONL into flat tensors."""
    keep, disc, tgt = [], [], []
    with open(path) as f:
        for hand_no, line in enumerate(f):
            if max_hands is not None and hand_no >= max_hands:
                break
            row = json.loads(line)
            splits = row["splits"]
            assert len(splits) == SPLITS_PER_HAND, f"hand {hand_no}: {len(splits)} splits"
            for s in splits:
                keep.append([card_index(c) for c in s["keep"]])
                disc.append([card_index(c) for c in s["discard"]])
                e, c = s["ehand"], s["crib_ev"]
                tgt.append([e + c, e - c])
    return DiscardData(
        keep_idx=torch.tensor(np.array(keep, dtype=np.int64)),
        disc_idx=torch.tensor(np.array(disc, dtype=np.int64)),
        target=torch.tensor(np.array(tgt, dtype=np.float32)),
    )


def encode(keep_idx: torch.Tensor, disc_idx: torch.Tensor, dealer: torch.Tensor) -> torch.Tensor:
    """Build [B,105] input vectors from index tensors ([B,4], [B,2], [B])."""
    x = torch.zeros(len(keep_idx), INPUT_DIM, device=keep_idx.device)
    x.scatter_(1, keep_idx, 1.0)
    x.scatter_(1, disc_idx + 52, 1.0)
    x[:, DEALER_FLAG] = dealer.float()
    return x
