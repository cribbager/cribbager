// In-game "Evaluate game" — builds the same discard-analysis data the account-
// scoped endpoint produces (analysis.go), but entirely CLIENT-SIDE from the game
// you just played, so it needs no login and no stored result. It works for guests,
// bot games, and human-vs-human alike.
//
// Each hand you played is rated by POSTing its six dealt cards to the stateless,
// unauthenticated POST /tools/discard-eval (the same crib-aware EV the bot uses),
// then comparing your actual hold against the top-ranked one. The assembled object
// matches gameAnalysisResponse, so analysisRender draws it identically to the
// dedicated analysis page.

// evEpsilon mirrors the server's optimal threshold (analysis.go): a hold within
// this of the best is treated as optimal, absorbing float noise.
const evEpsilon = 1e-6;

const sameThrow = (a, b) =>
    (a[0] === b[0] && a[1] === b[1]) || (a[0] === b[1] && a[1] === b[0]);

// buildEvaluationData rates every captured hand and returns the analysis data
// shape. `hands` is an array of { hand:[6 codes], dealer:bool, throw:[2 codes] }.
// Throws if the rating endpoint is unreachable so the caller can show an error.
export async function buildEvaluationData(hands) {
    const discards = [];
    for (const capd of hands) {
        const res = await fetch('/tools/discard-eval', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ hand: capd.hand, dealer: capd.dealer }),
        });
        if (!res.ok) throw new Error('discard-eval HTTP ' + res.status);
        const { holds } = await res.json();
        if (!Array.isArray(holds) || !holds.length) continue;

        const best = holds[0];
        const mine = holds.find((hd) => sameThrow(hd.throw, capd.throw)) || best;
        const delta = best.ev - mine.ev;
        discards.push({
            hand: capd.hand,
            dealer: capd.dealer,
            throw: capd.throw,
            keep: mine.keep,
            keep_ev: mine.ev,
            best_throw: best.throw,
            best_keep: best.keep,
            best_ev: best.ev,
            delta_ev: delta,
            optimal: delta < evEpsilon,
        });
    }

    const optimal = discards.filter((d) => d.optimal).length;
    const lost = discards.reduce((sum, d) => sum + d.delta_ev, 0);
    return {
        discards,
        summary: { hands: discards.length, optimal_discards: optimal, total_ev_lost: lost },
    };
}
