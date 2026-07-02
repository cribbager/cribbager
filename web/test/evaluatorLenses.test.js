import { test } from 'node:test';
import assert from 'node:assert/strict';
import { LENSES, sortHolds, situationLines, situationGuidance, pct } from '../public/src/ui/evaluatorLenses.js';

// A small fixture of holds shaped like the wire rows (only the numeric fields
// matter to the lenses). Deliberately constructed so the three lenses disagree:
// point EV likes A, max hand likes B, win likes C.
const holds = [
    { id: 'A', hand_ev: 8.0, crib_ev: 2.0, ev: 10.0, win: 0.20 },
    { id: 'B', hand_ev: 9.5, crib_ev: -1.0, ev: 8.5, win: 0.10 },
    { id: 'C', hand_ev: 7.0, crib_ev: 1.0, ev: 8.0, win: 0.35 },
];
const ids = (rows) => rows.map((r) => r.id);

test('the three lenses re-sort the same rows by their own field', () => {
    assert.deepEqual(ids(sortHolds(holds, 'ev', true)), ['A', 'B', 'C']);
    assert.deepEqual(ids(sortHolds(holds, 'hand', true)), ['B', 'A', 'C']);
    assert.deepEqual(ids(sortHolds(holds, 'win', true)), ['C', 'A', 'B']);
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
    assert.deepEqual(ids(sortHolds(far, 'win', false)), ['A', 'B', 'C']);
});

test('win-lens ties break by point EV, like the engine near-tie rule', () => {
    const tied = holds.map((r) => ({ ...r, win: 0.5 }));
    assert.deepEqual(ids(sortHolds(tied, 'win', true)), ['A', 'B', 'C']);
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
