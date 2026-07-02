// Discard evaluator lenses (T1) — the pure logic behind discardEvaluator.js,
// DOM-free so the node tests can exercise it. The server returns ONE set of 15
// holds (each with hand_ev, crib_ev, ev, win) plus a situation block; the three
// lenses are re-sorts of those same rows, never recomputations:
//   ev   — point EV (hand + signed crib), the champion's mid-game objective;
//   hand — expected hand value alone, the "I need points in MY hand" lens;
//   win  — win probability, the champion's endgame objective. It only
//          differentiates when someone is in reach of 121 (situation.endgame);
//          otherwise every win is 0 and the lens defers to point EV — callers
//          must present that state, not a column of zeros.

export const LENSES = [
    { id: 'ev', label: 'Point EV', field: 'ev' },
    { id: 'hand', label: 'Max hand', field: 'hand_ev' },
    { id: 'win', label: 'Win probability', field: 'win' },
];

// sortHolds returns a NEW array of the holds ordered best-first under the given
// lens. Ties fall back to point EV (matching the engine's own near-tie rule for
// the win objective), and Array.prototype.sort is stable, so equal rows keep the
// server's (point-EV) order. When the endgame objective is inactive the win lens
// defers to point EV outright — every win is 0, so ranking by it is meaningless.
export function sortHolds(holds, lens, endgame) {
    const sorted = [...holds];
    if (lens === 'hand') {
        sorted.sort((a, b) => b.hand_ev - a.hand_ev || b.ev - a.ev);
    } else if (lens === 'win' && endgame) {
        sorted.sort((a, b) => b.win - a.win || b.ev - a.ev);
    } else {
        sorted.sort((a, b) => b.ev - a.ev);
    }
    return sorted;
}

// situationLines turns the wire situation block (plus the dealer flag) into the
// plain-language facts of the position: who deals (and so owns the last-counted
// crib), who counts first at the show, the race, and the win probability.
export function situationLines(sit, dealer) {
    return [
        dealer
            ? 'You deal — the crib is yours, and it counts last.'
            : 'Your opponent deals — the crib is theirs, and it counts last.',
        dealer
            ? 'Your opponent is the pone: they count first at the show.'
            : 'You are the pone: you count first at the show.',
        `You need ${sit.my_need} point${sit.my_need === 1 ? '' : 's'} to win; your opponent needs ${sit.opp_need}.`,
        `Your win probability going into this deal: ${pct(sit.win_prob)}.`,
    ];
}

// situationGuidance is the one-sentence read on how the position shifts the
// discard decision — which lens matters and why.
export function situationGuidance(sit, dealer) {
    if (!sit.endgame) {
        return 'Far from the end, expected points is the right objective: win probability '
            + 'agrees with point EV here, so the Point EV ranking is the story.';
    }
    const score = `${sit.my_score}–${sit.opp_score}`;
    if (sit.my_need > sit.opp_need) {
        return dealer
            ? `You're behind ${score} and the pone counts first — they may go out before your hand `
            + `or your crib ever count. Only holds that can win in time matter now: `
            + `rank by win probability, not average points.`
            : `You're behind ${score} and you count first at the show — this could be your last hand: `
            + `maximize your hand's upside, the crib may never count. The win lens prices exactly that.`;
    }
    return `The end is in reach at ${score}: the order points count in matters as much as how many. `
        + `The engine ranks by win probability here — it may prefer a safer hold over maximum points.`;
}

// pct formats a 0..1 probability as a percentage with one decimal.
export function pct(p) {
    return `${(p * 100).toFixed(1)}%`;
}
