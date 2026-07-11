import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
    LENSES, sortHolds, situationLines, situationGuidance, pct,
    BIG_HAND_THRESHOLD, distBars, distDomainMax,
} from '../public/src/ui/evaluatorLenses.js';

// A small fixture of holds shaped like the wire rows (only the numeric fields
// matter to the lenses). Deliberately constructed so the four lenses disagree:
// point EV likes A, max hand likes B, win likes C, upside likes D (a fat tail with
// a modest mean, the 5-5-J-Q archetype).
const holds = [
    { id: 'A', hand_ev: 8.0, crib_ev: 2.0, ev: 10.0, win: 0.20, hand_p_ge_12: 0.10, hand_ceiling: 14 },
    { id: 'B', hand_ev: 9.5, crib_ev: -1.0, ev: 8.5, win: 0.10, hand_p_ge_12: 0.15, hand_ceiling: 16 },
    { id: 'C', hand_ev: 7.0, crib_ev: 1.0, ev: 8.0, win: 0.35, hand_p_ge_12: 0.05, hand_ceiling: 12 },
    { id: 'D', hand_ev: 6.5, crib_ev: 0.5, ev: 7.0, win: 0.15, hand_p_ge_12: 0.40, hand_ceiling: 24 },
];
const ids = (rows) => rows.map((r) => r.id);

test('the four lenses re-sort the same rows by their own field', () => {
    assert.deepEqual(ids(sortHolds(holds, 'ev', true)), ['A', 'B', 'C', 'D']);
    assert.deepEqual(ids(sortHolds(holds, 'hand', true)), ['B', 'A', 'C', 'D']);
    assert.deepEqual(ids(sortHolds(holds, 'win', true)), ['C', 'A', 'D', 'B']);
    assert.deepEqual(ids(sortHolds(holds, 'upside', true)), ['D', 'B', 'A', 'C']);
});

test('the upside lens ranks by right-tail mass, not average — D wins on P(>=12) despite the lowest EV', () => {
    const top = sortHolds(holds, 'upside', true)[0];
    assert.equal(top.id, 'D');
    // It must genuinely differ from the EV order (otherwise the lens is redundant).
    assert.notDeepEqual(ids(sortHolds(holds, 'upside', true)), ids(sortHolds(holds, 'ev', true)));
});

test('upside ties break by ceiling, then point EV', () => {
    const tied = [
        { id: 'lowCeil', hand_p_ge_12: 0.3, hand_ceiling: 14, ev: 9.0 },
        { id: 'hiCeil', hand_p_ge_12: 0.3, hand_ceiling: 20, ev: 8.0 },
        { id: 'hiCeilLowEv', hand_p_ge_12: 0.3, hand_ceiling: 20, ev: 7.0 },
    ];
    assert.deepEqual(ids(sortHolds(tied, 'upside', true)), ['hiCeil', 'hiCeilLowEv', 'lowCeil']);
});

test('sortHolds never mutates its input', () => {
    const before = ids(holds);
    sortHolds(holds, 'win', true);
    assert.deepEqual(ids(holds), before);
});

test('the win lens defers to point EV when the endgame objective is inactive', () => {
    // Far from the end every win is 0 on the wire; ranking by it would be
    // meaningless, so the lens must fall back to the point-EV order.
    const far = holds.map((r) => ({ ...r, win: 0 }));
    assert.deepEqual(ids(sortHolds(far, 'win', false)), ['A', 'B', 'C', 'D']);
});

test('win-lens ties break by point EV, like the engine near-tie rule', () => {
    const tied = holds.map((r) => ({ ...r, win: 0.5 }));
    assert.deepEqual(ids(sortHolds(tied, 'win', true)), ['A', 'B', 'C', 'D']);
});

test('every declared lens sorts by a field the rows carry', () => {
    for (const lens of LENSES) {
        assert.ok(lens.field in holds[0], `rows lack ${lens.field}`);
        const sorted = sortHolds(holds, lens.id, true);
        assert.equal(sorted.length, holds.length);
    }
});

test('situation lines state deal, show order, the race, and win probability', () => {
    const sit = { my_score: 90, opp_score: 117, my_need: 31, opp_need: 4, win_prob: 0.1234, endgame: true };
    const asPone = situationLines(sit, false).join(' ');
    assert.match(asPone, /opponent deals/i);
    assert.match(asPone, /counts last/i);
    assert.match(asPone, /you count first at the show/i);
    assert.match(asPone, /need 31 points/i);
    assert.match(asPone, /needs 4/);
    assert.match(asPone, /12\.3%/);

    const asDealer = situationLines(sit, true).join(' ');
    assert.match(asDealer, /^You deal/);
    assert.match(asDealer, /they count first at the show/i);
});

test('guidance names the objective: EV far out, win probability in the endgame', () => {
    const far = { my_score: 20, opp_score: 30, my_need: 101, opp_need: 91, win_prob: 0.45, endgame: false };
    assert.match(situationGuidance(far, true), /expected points is the right objective/i);
    assert.match(situationGuidance(far, true), /agrees with point EV/i);

    const behindAsPone = { my_score: 90, opp_score: 117, my_need: 31, opp_need: 4, win_prob: 0.05, endgame: true };
    const g = situationGuidance(behindAsPone, false);
    assert.match(g, /behind 90–117/);
    assert.match(g, /you count first at the show/i);
    assert.match(g, /maximize your hand/i);
    assert.match(g, /crib may never count/i);

    const behindAsDealer = situationGuidance(behindAsPone, true);
    assert.match(behindAsDealer, /pone counts first/i);
    assert.match(behindAsDealer, /win probability/i);

    const ahead = { my_score: 117, opp_score: 90, my_need: 4, opp_need: 31, win_prob: 0.95, endgame: true };
    assert.match(situationGuidance(ahead, true), /win probability/i);
});

test('pct formats a probability with one decimal', () => {
    assert.equal(pct(0.5625), '56.3%');
    assert.equal(pct(0), '0.0%');
    assert.equal(pct(1), '100.0%');
});

test('distDomainMax is the largest ceiling across all holds — the shared x-axis', () => {
    assert.equal(distDomainMax(holds), 24);
    assert.equal(distDomainMax([{ hand_ceiling: 8 }, { hand_ceiling: 20 }, { hand_ceiling: 12 }]), 20);
    assert.equal(distDomainMax([]), 0);
});

test('distBars maps the histogram to one bar per score up to the domain max', () => {
    // A tiny distribution with mass at scores 4, 8, 14; ceiling shared at 16.
    const dist = new Array(30).fill(0);
    dist[4] = 0.5; dist[8] = 0.3; dist[14] = 0.2;
    const { bars, maxP } = distBars(dist, 16);
    assert.equal(bars.length, 17); // scores 0..16 inclusive
    assert.equal(maxP, 0.5); // the tallest bar, for y-scaling
    assert.deepEqual(bars[4], { score: 4, p: 0.5, big: false });
    assert.deepEqual(bars[8], { score: 8, p: 0.3, big: false });
    assert.deepEqual(bars[14], { score: 14, p: 0.2, big: true }); // 14 >= threshold
    assert.equal(bars[0].p, 0); // empty scores are still present (zero-height bar)
});

test('distBars marks the big-hand tail at exactly BIG_HAND_THRESHOLD', () => {
    const dist = new Array(30).fill(0.01);
    const { bars } = distBars(dist, 20);
    assert.equal(BIG_HAND_THRESHOLD, 12);
    assert.equal(bars[BIG_HAND_THRESHOLD - 1].big, false);
    assert.equal(bars[BIG_HAND_THRESHOLD].big, true);
});

test('distBars tolerates a missing distribution (all-zero bars)', () => {
    const { bars, maxP } = distBars(undefined, 5);
    assert.equal(bars.length, 6);
    assert.equal(maxP, 0);
    assert.ok(bars.every((b) => b.p === 0));
});
