#!/usr/bin/env bash
# One round of the pegging self-play iteration loop (docs/research/ml-bot
# chapter 5): generate self-play data with the CURRENT net (ε-greedy so the
# action space keeps being explored), retrain from scratch on that data,
# install the new weights, and run the pegging-isolated gate.
#
# Each round's Q answers "what is this move worth if everyone continues like
# round N−1" — one step of policy improvement per round. Fresh net + fresh
# data per round is the deliberately simple fitted-Q scheme; if rounds ever
# regress, suspect distribution shift and consider pooling data across rounds.
#
# Usage (from anywhere):  ml/scripts/peg_iterate.sh <round> [games] [epsilon]
set -euo pipefail

ROUND=$1
GAMES=${2:-20000}
EPS=${3:-0.2}
cd "$(dirname "$0")/../.."

W=internal/bot/lab/testdata/pegging-v1.json
DATA=ml/data/peg-iter${ROUND}.jsonl

echo "=== round ${ROUND}: generating ${GAMES} games (epsilon ${EPS}) ==="
go run ./cmd/pegdata -games "${GAMES}" -seed $((100 + ROUND)) \
    -policy net -weights "${W}" -epsilon "${EPS}" -out "${DATA}"

echo "=== round ${ROUND}: training ==="
(cd ml && uv run scripts/train_pegging.py --data "data/peg-iter${ROUND}.jsonl" \
    --out "runs/peg-r${ROUND}" --install "../${W}")

echo "=== round ${ROUND}: gate (2000 pairs) ==="
CHALLENGE=ml-peg PAIRS=2000 go test ./internal/bot/lab -run ChallengerVsChampion -v 2>&1 \
    | grep -E "win rate|margin|windiff|→"
