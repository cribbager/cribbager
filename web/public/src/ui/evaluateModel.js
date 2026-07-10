// Pure display logic for the unified replay+analysis ("Evaluate game") page —
// DOM-free, so the node tests can exercise it (like evaluatorLenses.js /
// scoringQuizGrading.js).
//
// The data is the multi-engine analysis payload of GET /games/{id}/analysis
// (contract: internal/server/analysis2.go): engines[] in a fixed order, and
// per-decision engine verdict arrays ALIGNED with it, so switching engines is
// pure client-side re-selection — no refetch. Every verdict carries an
// expected-value delta (best - chosen, >= 0) in one of two units:
//   "points"  — expected points per deal;
//   "winprob" — probability of winning the game (0..1), used by the champion
//               engine once either player is in reach of 121.

// deltaEpsilon mirrors the server's optimality slack (analysisEpsilon): a
// delta this small is a tie, shown as optimal. The wire also carries each
// verdict's own `optimal` flag; this guard is for defensive formatting only.
const deltaEpsilon = 1e-9;

// ENGINE_LABELS maps wire engine names to their display labels.
export const ENGINE_LABELS = { 'ml': 'ML', 'champion': 'Champion', 'exact-ev': 'Exact EV' };

export function engineLabel(engine) {
    return ENGINE_LABELS[engine.name] || engine.name;
}

// defaultEngineIndex picks the engine the page opens on: the production ml
// bot when present (it is the default opponent), else the first engine.
export function defaultEngineIndex(engines) {
    const i = (engines || []).findIndex((e) => e.name === 'ml');
    return i >= 0 ? i : 0;
}

// trimNum renders a number with up to `max` decimals, trailing zeros trimmed
// ("1.30" -> "1.3", "2.00" -> "2").
function trimNum(n, max) {
    return String(Number(Number(n).toFixed(max)));
}

// formatDelta renders a decision's expected-value swing the way the page
// shows it, or null for an optimal (zero-delta) decision — callers render the
// ✓ instead. Points read as "−1.3 pts"; winprob deltas read as percentage
// points, "−2.1% win" (a hair-thin winprob loss floors at "−<0.1% win" rather
// than showing a meaningless −0%).
export function formatDelta(delta, units) {
    const d = Number(delta);
    if (!(d > deltaEpsilon)) return null;
    if (units === 'winprob') {
        const pp = d * 100;
        if (pp < 0.05) return '−<0.1% win';
        return '−' + trimNum(pp, 1) + '% win';
    }
    return '−' + trimNum(d, 2) + ' pts';
}

// formatValue renders an engine's absolute value for a move: points to two
// decimals ("20.41"), win probabilities as a percentage ("64.2%").
export function formatValue(value, units) {
    if (units === 'winprob') return trimNum(Number(value) * 100, 1) + '%';
    return Number(value).toFixed(2);
}

// dealsByIndex keys the analysis deals by their 0-based deal index within the
// game (d.deal) — NOT by array position: the payload only includes deals the
// analyzed seat discarded in, so a truncated final deal can be absent. The
// replay's 1-based hand numbers align as hand = deal + 1.
export function dealsByIndex(analysis) {
    const out = {};
    for (const d of (analysis && analysis.deals) || []) out[d.deal] = d;
    return out;
}

// dealDisagreement reports whether the engines split on ANY decision in the
// deal — the "engines disagree" marker the rail surfaces per deal.
export function dealDisagreement(deal) {
    if (deal.discard && deal.discard.agree === false) return true;
    return ((deal && deal.plays) || []).some((p) => p.agree === false);
}

// dealMarks distills one deal into the rail's ✓/✗ row under one engine: the
// rollup's two optimality flags, how many graded pegging decisions there were
// (zero means "pegging optimal" was vacuous), and the divergence marker.
export function dealMarks(deal, engineIdx) {
    const r = ((deal && deal.rollup) || [])[engineIdx] || {};
    return {
        discard: !!r.discard_optimal,
        pegging: !!r.pegging_optimal,
        playCount: ((deal && deal.plays) || []).length,
        disagree: dealDisagreement(deal),
    };
}

// summaryLines renders one engine's whole-game summary as two lines —
// discards and pegging — each "N/M optimal" plus the summed losses in each
// unit (points and winprob accumulate separately; a game can contain both).
export function summaryLines(s) {
    const line = (label, optimal, total, dPts, dWin) => {
        let out = `${label}: ${optimal}/${total} optimal`;
        const pts = formatDelta(dPts, 'points');
        const win = formatDelta(dWin, 'winprob');
        if (pts) out += ' · ' + pts;
        if (win) out += ' · ' + win;
        return out;
    };
    return [
        line('Discards', s.optimal_discards, s.hands, s.discard_delta_points, s.discard_delta_winprob),
        line('Pegging', s.optimal_plays, s.play_decisions, s.play_delta_points, s.play_delta_winprob),
    ];
}

// frameIndexForDeal maps a 0-based deal index to the replay frame that starts
// it, via handStarts(frames) (replayFrames.js) — the rail's click-to-jump.
// Returns null when the replay has no such hand.
export function frameIndexForDeal(starts, dealIdx) {
    const s = (starts || []).find((x) => x.hand === dealIdx + 1);
    return s ? s.index : null;
}
