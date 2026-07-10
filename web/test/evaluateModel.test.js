import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
    ENGINE_LABELS, engineLabel, defaultEngineIndex,
    formatDelta, formatValue,
    dealsByIndex, dealDisagreement, dealMarks,
    summaryLines, frameIndexForDeal,
} from '../public/src/ui/evaluateModel.js';

// The wire engine list, in the server's fixed order (analysis2.go).
const engines = [
    { name: 'ml', version: '2' },
    { name: 'champion', version: '3' },
    { name: 'exact-ev', version: '1' },
];

test('every wire engine has a display label, and unknown names pass through', () => {
    for (const e of engines) assert.ok(ENGINE_LABELS[e.name], `no label for ${e.name}`);
    assert.equal(engineLabel({ name: 'champion' }), 'Champion');
    assert.equal(engineLabel({ name: 'exact-ev' }), 'Exact EV');
    assert.equal(engineLabel({ name: 'future-engine' }), 'future-engine');
});

test('the default engine is ml when present, else the first', () => {
    assert.equal(defaultEngineIndex(engines), 0);
    assert.equal(defaultEngineIndex([engines[1], engines[0]]), 1);
    assert.equal(defaultEngineIndex([engines[1], engines[2]]), 0);
    assert.equal(defaultEngineIndex([]), 0);
});

test('formatDelta: zero (an optimal move) is null — callers show the check', () => {
    assert.equal(formatDelta(0, 'points'), null);
    assert.equal(formatDelta(0, 'winprob'), null);
    assert.equal(formatDelta(1e-12, 'points'), null); // server tie slack
});

test('formatDelta: point swings read as "−X pts", trailing zeros trimmed', () => {
    assert.equal(formatDelta(1.3, 'points'), '−1.3 pts');
    assert.equal(formatDelta(1.3042, 'points'), '−1.3 pts');
    assert.equal(formatDelta(0.25, 'points'), '−0.25 pts');
    assert.equal(formatDelta(2, 'points'), '−2 pts');
});

test('formatDelta: winprob swings read as percentage points, "−X% win"', () => {
    assert.equal(formatDelta(0.021, 'winprob'), '−2.1% win');
    assert.equal(formatDelta(0.1, 'winprob'), '−10% win');
    assert.equal(formatDelta(0.0009, 'winprob'), '−0.1% win');
    // A hair-thin loss floors instead of rendering a meaningless −0%.
    assert.equal(formatDelta(0.0001, 'winprob'), '−<0.1% win');
});

test('formatValue: points to two decimals, winprob as a percentage', () => {
    assert.equal(formatValue(20.4142, 'points'), '20.41');
    assert.equal(formatValue(0.642133, 'winprob'), '64.2%');
});

test('dealsByIndex keys by d.deal, tolerating gaps (a deal with no discard)', () => {
    const byIdx = dealsByIndex({ deals: [{ deal: 0 }, { deal: 2 }] });
    assert.ok(byIdx[0] && byIdx[2]);
    assert.equal(byIdx[1], undefined);
    assert.deepEqual(dealsByIndex(null), {});
});

// A deal fixture: discard agrees, one play decision disagrees.
const deal = {
    deal: 3,
    discard: { agree: true, engines: [] },
    plays: [
        { agree: true, engines: [] },
        { agree: false, engines: [] },
    ],
    rollup: [
        { discard_optimal: true, pegging_optimal: false },
        { discard_optimal: false, pegging_optimal: true },
    ],
};

test('dealDisagreement fires when any decision in the deal splits the engines', () => {
    assert.equal(dealDisagreement(deal), true);
    assert.equal(dealDisagreement({ discard: { agree: true }, plays: [{ agree: true }] }), false);
    assert.equal(dealDisagreement({ discard: { agree: false }, plays: [] }), true);
});

test('dealMarks reads the selected engine\'s rollup, not another engine\'s', () => {
    assert.deepEqual(dealMarks(deal, 0), { discard: true, pegging: false, playCount: 2, disagree: true });
    assert.deepEqual(dealMarks(deal, 1), { discard: false, pegging: true, playCount: 2, disagree: true });
});

test('summaryLines: counts plus per-unit loss sums, omitting zero losses', () => {
    const lines = summaryLines({
        hands: 9, optimal_discards: 7,
        discard_delta_points: 1.8342, discard_delta_winprob: 0,
        play_decisions: 24, optimal_plays: 21,
        play_delta_points: 2.1, play_delta_winprob: 0.021,
    });
    assert.deepEqual(lines, [
        'Discards: 7/9 optimal · −1.83 pts',
        'Pegging: 21/24 optimal · −2.1 pts · −2.1% win',
    ]);
    // A clean game shows plain counts with no loss tail.
    const clean = summaryLines({
        hands: 2, optimal_discards: 2, discard_delta_points: 0, discard_delta_winprob: 0,
        play_decisions: 5, optimal_plays: 5, play_delta_points: 0, play_delta_winprob: 0,
    });
    assert.deepEqual(clean, ['Discards: 2/2 optimal', 'Pegging: 5/5 optimal']);
});

test('frameIndexForDeal maps 0-based deals onto 1-based replay hands', () => {
    const starts = [{ hand: 1, index: 2 }, { hand: 2, index: 17 }];
    assert.equal(frameIndexForDeal(starts, 0), 2);
    assert.equal(frameIndexForDeal(starts, 1), 17);
    assert.equal(frameIndexForDeal(starts, 5), null);
});
